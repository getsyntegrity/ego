// MIT License
//
// Copyright (c) 2022-2026 Arsene Tochemey Gandote
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package adapter_test

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const (
	rootModule  = "github.com/getsyntegrity/ego/v4"
	adapterPath = rootModule + "/port/adapter"
)

// contractPorts lists, for each contract package that owns a composition
// slot, the port-name constants its port.go must declare (ego-arch-004
// design §D3): constant name -> the interface it names. The directory is
// relative to the repository root.
var contractPorts = map[string]map[string]string{
	"port/publishing": {"PortEventPublisher": "EventPublisher", "PortStatePublisher": "StatePublisher"},
	"persistence":     {"PortEventsStore": "EventsStore", "PortStateStore": "StateStore", "PortSnapshotStore": "SnapshotStore"},
	"offsetstore":     {"PortOffsetStore": "OffsetStore"},
	"tenancy":         {"PortTenantResolver": "TenantResolver"},
	"encryption":      {"PortEncryptor": "Encryptor"},
}

// TestAdapterDependsOnlyOnStdlib walks the resolved import graph of
// port/adapter with a real `go list -deps` subprocess, so transitive
// dependencies are checked too. The allowlist of non-standard-library
// packages is empty (ego-arch-004 design §D1).
func TestAdapterDependsOnlyOnStdlib(t *testing.T) {
	deps := goList(t, "list", "-deps", ".")
	if !slices.Contains(deps, adapterPath) || !slices.Contains(deps, "context") {
		t.Fatalf("go list -deps . = %v, want it to list %s and context; the test would prove nothing", deps, adapterPath)
	}
	for _, dep := range deps {
		if dep == adapterPath || isStdlib(dep) {
			continue
		}
		t.Errorf("port/adapter must not depend on %q: it may import only the standard library (openspec/changes/ego-arch-004/design.md §D1)", dep)
	}
}

// Spec scenario "moving port/publishing stays cycle-free": none of the five
// contract packages imports port/adapter, directly or transitively.
func TestContractPackagesDoNotImportAdapter(t *testing.T) {
	pkgs := make([]string, 0, len(contractPorts))
	for dir := range contractPorts {
		pkgs = append(pkgs, rootModule+"/"+dir)
	}
	slices.Sort(pkgs)

	for _, pkg := range pkgs {
		deps := goList(t, "list", "-deps", pkg)
		if !slices.Contains(deps, pkg) {
			t.Fatalf("go list -deps %s did not list the package itself: %v", pkg, deps)
		}
		if slices.Contains(deps, adapterPath) {
			t.Errorf("%s depends on %s; contract packages declare port names as untyped constants so they never need it (openspec/changes/ego-arch-004/design.md §D3)", pkg, adapterPath)
		}
	}
}

// TestPortNameConstantsAreUntyped reads each contract package's port.go and
// checks that it declares exactly the expected port-name constants, that
// each is an untyped string constant (a typed adapter.Port constant would
// import port/adapter), that its value is "<package>.<Interface>", and
// that the named interface exists in the package.
func TestPortNameConstantsAreUntyped(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	for dir, want := range contractPorts {
		t.Run(dir, func(t *testing.T) {
			fset := token.NewFileSet()
			pkgDir := filepath.Join(repoRoot, filepath.FromSlash(dir))
			file, err := parser.ParseFile(fset, filepath.Join(pkgDir, "port.go"), nil, 0)
			if err != nil {
				t.Fatalf("parsing %s/port.go: %v", dir, err)
			}
			pkgName := file.Name.Name

			got := map[string]string{}
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.CONST {
					continue
				}
				for _, spec := range gen.Specs {
					vs := spec.(*ast.ValueSpec)
					if vs.Type != nil {
						t.Errorf("%s/port.go: constant %s has a type; port names must be untyped", dir, vs.Names[0].Name)
					}
					for i, name := range vs.Names {
						if i >= len(vs.Values) {
							t.Errorf("%s/port.go: constant %s has no explicit value", dir, name.Name)
							continue
						}
						lit, ok := vs.Values[i].(*ast.BasicLit)
						if !ok || lit.Kind != token.STRING {
							t.Errorf("%s/port.go: constant %s is not a string literal", dir, name.Name)
							continue
						}
						value, _ := strconv.Unquote(lit.Value)
						got[name.Name] = value
					}
				}
			}

			if len(got) != len(want) {
				t.Errorf("%s/port.go declares %v, want exactly the constants %v", dir, got, want)
			}
			interfaces := interfacesIn(t, pkgDir)
			for constName, iface := range want {
				value, ok := got[constName]
				if !ok {
					t.Errorf("%s/port.go does not declare %s", dir, constName)
					continue
				}
				if wantValue := pkgName + "." + iface; value != wantValue {
					t.Errorf("%s.%s = %q, want %q", pkgName, constName, value, wantValue)
				}
				if !interfaces[iface] {
					t.Errorf("%s.%s names %s, which is not an interface in package %s", pkgName, constName, iface, pkgName)
				}
			}
		})
	}
}

// interfacesIn returns the names of the interface types declared in the
// non-test files of dir.
func interfacesIn(t *testing.T, dir string) map[string]bool {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	fset := token.NewFileSet()
	for _, path := range matches {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if ts, ok := n.(*ast.TypeSpec); ok {
				if _, ok := ts.Type.(*ast.InterfaceType); ok {
					out[ts.Name.Name] = true
				}
			}
			return true
		})
	}
	return out
}

// isStdlib reports whether an import path belongs to the standard library:
// its first path element has no dot.
func isStdlib(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

func goList(t *testing.T, args ...string) []string {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("go toolchain not found on PATH: %v", err)
	}
	cmd := exec.Command(goBin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go %s failed: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.Fields(stdout.String())
}
