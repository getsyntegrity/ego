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

	"github.com/pablogore/ego/v4/internal/cmd/archcheck/rules"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func TestParseGoModModulePath(t *testing.T) {
	dir := t.TempDir()
	goMod := filepath.Join(dir, "go.mod")
	writeFile(t, goMod, "module github.com/pablogore/ego/v4/publisher/kafka\n\ngo 1.26.0\n\nrequire github.com/pablogore/ego/v4 v4.0.0\n")

	got, err := parseGoModModulePath(goMod)
	if err != nil {
		t.Fatalf("parseGoModModulePath: %v", err)
	}
	if want := "github.com/pablogore/ego/v4/publisher/kafka"; got != want {
		t.Errorf("parseGoModModulePath() = %q, want %q", got, want)
	}
}

func TestParseGoModModulePath_NoModuleLine(t *testing.T) {
	dir := t.TempDir()
	goMod := filepath.Join(dir, "go.mod")
	writeFile(t, goMod, "go 1.26.0\n")

	if _, err := parseGoModModulePath(goMod); err == nil {
		t.Fatal("parseGoModModulePath() = nil error, want an error for a go.mod with no module line")
	}
}

// fixtureNestedModule builds a tiny two-package nested module under a
// t.TempDir(): a module-root package with one production and one test file
// (whose test-only import must never appear in the loaded graph), and a
// subpackage.
func fixtureNestedModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module github.com/example/nested\n\ngo 1.26.0\n")
	writeFile(t, filepath.Join(dir, "root.go"), `package nested

import (
	"context"

	"github.com/pablogore/ego/v4"
)

var _ = context.Background
var _ = ego.ErrPublisherNotStarted
`)
	writeFile(t, filepath.Join(dir, "root_test.go"), `package nested

import "testing"

func TestNothing(t *testing.T) {}
`)
	writeFile(t, filepath.Join(dir, "sub", "sub.go"), `package sub

import "fmt"

var _ = fmt.Sprintf
`)
	// A vendor directory must never be walked into.
	writeFile(t, filepath.Join(dir, "vendor", "bad", "bad.go"), `package bad

import "github.com/pablogore/ego/v4/internal/queue"
`)
	return dir
}

func TestLoadNestedModule(t *testing.T) {
	dir := fixtureNestedModule(t)

	pkgs, err := loadNestedModule(dir)
	if err != nil {
		t.Fatalf("loadNestedModule: %v", err)
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].ImportPath < pkgs[j].ImportPath })

	if len(pkgs) != 2 {
		t.Fatalf("len(pkgs) = %d, want 2: %+v", len(pkgs), pkgs)
	}

	root := pkgs[0]
	if root.ImportPath != "github.com/example/nested" {
		t.Errorf("root ImportPath = %q, want github.com/example/nested", root.ImportPath)
	}
	if root.Kind != rules.NestedModule {
		t.Errorf("root Kind = %v, want NestedModule", root.Kind)
	}
	if !containsImport(root.Imports, "github.com/pablogore/ego/v4") {
		t.Errorf("root Imports = %v, want to contain github.com/pablogore/ego/v4", root.Imports)
	}
	if containsImport(root.Imports, "testing") {
		t.Errorf("root Imports = %v, must not contain the test-only import testing", root.Imports)
	}

	sub := pkgs[1]
	if sub.ImportPath != "github.com/example/nested/sub" {
		t.Errorf("sub ImportPath = %q, want github.com/example/nested/sub", sub.ImportPath)
	}
	if !containsImport(sub.Imports, "fmt") {
		t.Errorf("sub Imports = %v, want to contain fmt", sub.Imports)
	}
}

func TestLoadNestedModule_SkipsVendor(t *testing.T) {
	dir := fixtureNestedModule(t)

	pkgs, err := loadNestedModule(dir)
	if err != nil {
		t.Fatalf("loadNestedModule: %v", err)
	}
	for _, p := range pkgs {
		if p.ImportPath == "github.com/example/nested/vendor/bad" {
			t.Fatalf("loadNestedModule must not descend into vendor/, got %+v", p)
		}
	}
}

func TestDiscoverNestedModuleDirs(t *testing.T) {
	repoRoot := t.TempDir()
	writeFile(t, filepath.Join(repoRoot, "go.mod"), "module github.com/example/root\n")
	writeFile(t, filepath.Join(repoRoot, "publisher", "kafka", "go.mod"), "module github.com/example/root/publisher/kafka\n")
	writeFile(t, filepath.Join(repoRoot, "vendor", "x", "go.mod"), "module should.not.be.found\n")
	writeFile(t, filepath.Join(repoRoot, ".hidden", "go.mod"), "module should.not.be.found.either\n")
	writeFile(t, filepath.Join(repoRoot, "testdata", "fixture", "go.mod"), "module should.not.be.found.testdata\n")

	dirs, err := discoverNestedModuleDirs(repoRoot)
	if err != nil {
		t.Fatalf("discoverNestedModuleDirs: %v", err)
	}
	if len(dirs) != 1 {
		t.Fatalf("dirs = %v, want exactly the publisher/kafka module", dirs)
	}
	want := filepath.Join(repoRoot, "publisher", "kafka")
	if dirs[0] != want {
		t.Errorf("dirs[0] = %q, want %q", dirs[0], want)
	}
}

func containsImport(imports []string, path string) bool {
	for _, imp := range imports {
		if imp == path {
			return true
		}
	}
	return false
}
