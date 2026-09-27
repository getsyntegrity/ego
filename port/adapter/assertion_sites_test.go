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
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// optionalInterfaces are the optional SPI interfaces that may be
// type-asserted only inside their single accessor (ego-arch-004 design
// §D3, "one assertion per optional interface"). The check matches them by
// name, qualified or not.
var optionalInterfaces = []string{"Describer", "Starter", "Pinger", "FixedTenantResolver"}

// optionalMethods are the methods of those interfaces. An inline or local
// interface whose own methods are all among them is the same assertion in
// disguise (compose/goakt's former private pinger interface), and is
// reported too.
var optionalMethods = []string{"Describe", "Start", "Ping", "FixedTenant"}

// allowedAssertionSites are the only functions in production code that may
// assert an optional interface, as "<file relative to the repository
// root>:<function>".
var allowedAssertionSites = []string{
	"port/adapter/adapter.go:Describe",
	"port/adapter/adapter.go:PingerOf",
	"port/adapter/adapter.go:StarterOf",
	"tenancy/resolver.go:AsFixedTenantResolver",
}

// TestOptionalInterfacesAreAssertedOnlyInTheirAccessors scans every
// production Go file of the repository, nested modules included, and
// requires that Describer, Starter, Pinger and FixedTenantResolver are
// type-asserted (in a type assertion or a type switch) only inside
// adapter.Describe, adapter.StarterOf, adapter.PingerOf and
// tenancy.AsFixedTenantResolver. The set of sites found must equal the
// allowed set exactly, so the scan also proves it sees the four accessors.
func TestOptionalInterfacesAreAssertedOnlyInTheirAccessors(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var sites []string
	var scanned int
	walkErr := filepath.WalkDir(repoRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != repoRoot && skipDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isProductionGoFile(path) {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		found, err := assertionSites(filepath.ToSlash(rel), src)
		if err != nil {
			return err
		}
		scanned++
		sites = append(sites, found...)
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	if scanned == 0 {
		t.Fatal("the scan found no Go sources, so it proves nothing")
	}

	slices.Sort(sites)
	sites = slices.Compact(sites)
	for _, site := range sites {
		if !slices.Contains(allowedAssertionSites, site) {
			t.Errorf("%s type-asserts an optional adapter interface; call adapter.Describe, StarterOf, PingerOf or tenancy.AsFixedTenantResolver instead (openspec/changes/ego-arch-004/design.md §D3)", site)
		}
	}
	for _, site := range allowedAssertionSites {
		if !slices.Contains(sites, site) {
			t.Errorf("the scan did not find the accessor %s; either it moved or the scan is broken", site)
		}
	}
}

// TestAssertionSitesNegativeControl proves the scan reports every form of
// the forbidden assertion and nothing else, on a synthetic source.
func TestAssertionSitesNegativeControl(t *testing.T) {
	src := `package sample

import (
	"context"
	"fmt"
)

type pinger interface{ Ping(context.Context) error }

func qualified(v any)  { _, _ = v.(adapter.Starter) }
func bare(v any)       { _, _ = v.(Describer) }
func inline(v any)     { _, _ = v.(interface{ Ping(context.Context) error }) }
func local(v any)      { _, _ = v.(pinger) }
func switched(v any) {
	switch v.(type) {
	case fmt.Stringer:
	case tenancy.FixedTenantResolver:
	}
}
func unrelated(v any) {
	_, _ = v.(fmt.Stringer)
	_, _ = v.(interface{ Close() error })
	_, _ = v.(interface {
		Ping(context.Context) error
		Close() error
	})
	switch x := v.(type) {
	case error:
		_ = x
	}
}
`
	got, err := assertionSites("sample/sample.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"sample/sample.go:bare",
		"sample/sample.go:inline",
		"sample/sample.go:local",
		"sample/sample.go:qualified",
		"sample/sample.go:switched",
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("assertionSites = %v, want %v", got, want)
	}
}

// skipDir reports whether a directory holds no first-party production Go
// code: hidden tooling directories (.git, .codegraph, .claude, …), vendored
// code, test data and the openspec documents.
func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata" || name == "openspec" || name == "node_modules"
}

// isProductionGoFile reports whether path is hand-written production Go:
// not a test file and not generated protobuf code.
func isProductionGoFile(path string) bool {
	return strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") && !strings.HasSuffix(path, ".pb.go")
}

// assertionSites parses one file and returns "<rel>:<function>" for every
// type assertion or type-switch case on an optional interface.
func assertionSites(rel string, src []byte) ([]string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), rel, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	local := localOptionalInterfaces(file)
	var sites []string
	for _, decl := range file.Decls {
		scope := "<package scope>"
		if fn, ok := decl.(*ast.FuncDecl); ok {
			scope = fn.Name.Name
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			var types []ast.Expr
			switch node := n.(type) {
			case *ast.TypeAssertExpr:
				if node.Type != nil { // nil in a type switch guard; its cases are handled below
					types = append(types, node.Type)
				}
			case *ast.TypeSwitchStmt:
				for _, stmt := range node.Body.List {
					types = append(types, stmt.(*ast.CaseClause).List...)
				}
			}
			for _, typ := range types {
				if isOptionalInterface(typ, local) {
					sites = append(sites, rel+":"+scope)
				}
			}
			return true
		})
	}
	return sites, nil
}

// isOptionalInterface reports whether typ names an optional interface, is
// an inline interface made only of optional methods, or names a local
// interface that is.
func isOptionalInterface(typ ast.Expr, local map[string]bool) bool {
	switch t := typ.(type) {
	case *ast.Ident:
		return slices.Contains(optionalInterfaces, t.Name) || local[t.Name]
	case *ast.SelectorExpr:
		return slices.Contains(optionalInterfaces, t.Sel.Name)
	case *ast.InterfaceType:
		return onlyOptionalMethods(t)
	case *ast.ParenExpr:
		return isOptionalInterface(t.X, local)
	default:
		return false
	}
}

// localOptionalInterfaces returns the file's interface types whose own
// methods are all optional methods.
func localOptionalInterfaces(file *ast.File) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		if spec, ok := n.(*ast.TypeSpec); ok {
			if iface, ok := spec.Type.(*ast.InterfaceType); ok && onlyOptionalMethods(iface) {
				out[spec.Name.Name] = true
			}
		}
		return true
	})
	return out
}

// onlyOptionalMethods reports whether iface declares at least one method,
// embeds nothing, and declares only optional methods.
func onlyOptionalMethods(iface *ast.InterfaceType) bool {
	if iface.Methods == nil || len(iface.Methods.List) == 0 {
		return false
	}
	for _, field := range iface.Methods.List {
		if len(field.Names) == 0 {
			return false // an embedded interface or a type constraint
		}
		for _, name := range field.Names {
			if !slices.Contains(optionalMethods, name.Name) {
				return false
			}
		}
	}
	return true
}
