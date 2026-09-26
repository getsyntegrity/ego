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

package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// writeFile creates path (and its parent directories) with the given
// content, failing the test on any error.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

// TestModuleDiscovery_FindsNestedModulesAndTheirRootImports builds a small
// repository tree with two real nested modules (moda, modb) and a go.mod
// nested inside a skipped directory (vendor/), and asserts that:
//   - findSatelliteDirs discovers exactly moda and modb, and never descends
//     into vendor/, so the go.mod hiding there is never even visited;
//   - discoverModuleImports parses moda's and modb's non-vendor .go files,
//     including _test.go (a nested module's tests can import a root
//     package its production code does not), with go/parser only (no
//     module download, no network), and returns only the imports that
//     start with the root module's own path.
func TestModuleDiscovery_FindsNestedModulesAndTheirRootImports(t *testing.T) {
	root := t.TempDir()
	const rootModule = "github.com/example/root"

	writeFile(t, filepath.Join(root, "go.mod"), "module "+rootModule+"\n\ngo 1.27.0\n")

	writeFile(t, filepath.Join(root, "moda", "go.mod"), "module "+rootModule+"/moda\n\ngo 1.26.0\n")
	writeFile(t, filepath.Join(root, "moda", "producer.go"), `package moda

import (
	"fmt"

	_ "`+rootModule+`/command"
)

var _ = fmt.Sprintf
`)
	writeFile(t, filepath.Join(root, "moda", "producer_test.go"), `package moda

import (
	_ "`+rootModule+`/testkit"
)
`)

	writeFile(t, filepath.Join(root, "modb", "go.mod"), "module "+rootModule+"/modb\n\ngo 1.26.0\n")
	writeFile(t, filepath.Join(root, "modb", "consumer.go"), `package modb

import "strings"

var _ = strings.ToUpper
`)

	// A go.mod hidden inside vendor/ must never be treated as a nested
	// module: findSatelliteDirs must not even descend into vendor/.
	writeFile(t, filepath.Join(root, "vendor", "example.com", "dep", "go.mod"), "module example.com/dep\n\ngo 1.26.0\n")
	writeFile(t, filepath.Join(root, "vendor", "example.com", "dep", "dep.go"), "package dep\n")

	dirs, err := findSatelliteDirs(root)
	if err != nil {
		t.Fatalf("findSatelliteDirs: %v", err)
	}
	sort.Strings(dirs)
	wantDirs := []string{"moda", "modb"}
	if len(dirs) != len(wantDirs) {
		t.Fatalf("findSatelliteDirs() = %v, want %v", dirs, wantDirs)
	}
	for i := range wantDirs {
		if dirs[i] != wantDirs[i] {
			t.Fatalf("findSatelliteDirs() = %v, want %v", dirs, wantDirs)
		}
	}

	modaImports, err := discoverModuleImports(root, "moda", rootModule)
	if err != nil {
		t.Fatalf("discoverModuleImports(moda): %v", err)
	}
	wantModaImports := []string{rootModule + "/command", rootModule + "/testkit"}
	if len(modaImports) != len(wantModaImports) {
		t.Fatalf("discoverModuleImports(moda) = %v, want %v", modaImports, wantModaImports)
	}
	for i := range wantModaImports {
		if modaImports[i] != wantModaImports[i] {
			t.Fatalf("discoverModuleImports(moda) = %v, want %v", modaImports, wantModaImports)
		}
	}

	modbImports, err := discoverModuleImports(root, "modb", rootModule)
	if err != nil {
		t.Fatalf("discoverModuleImports(modb): %v", err)
	}
	if len(modbImports) != 0 {
		t.Fatalf("discoverModuleImports(modb) = %v, want none (only imports stdlib)", modbImports)
	}
}
