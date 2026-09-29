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

	"github.com/getsyntegrity/ego/internal/cmd/archcheck/rules"
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
	writeFile(t, goMod, "module github.com/getsyntegrity/ego/publisher/kafka\n\ngo 1.21\n\nrequire github.com/getsyntegrity/ego v4.0.0\n")

	got, err := parseGoModModulePath(goMod)
	if err != nil {
		t.Fatalf("parseGoModModulePath: %v", err)
	}
	if want := "github.com/getsyntegrity/ego/publisher/kafka"; got != want {
		t.Errorf("parseGoModModulePath() = %q, want %q", got, want)
	}
}

// TestParseGoModModulePath_Forms covers every module-declaration form the
// go.mod grammar allows (https://go.dev/ref/mod#go-mod-file-module), since
// this repository does not depend on golang.org/x/mod and hand-parses
// go.mod files itself.
func TestParseGoModModulePath_Forms(t *testing.T) {
	const want = "github.com/getsyntegrity/ego"
	cases := []struct {
		name    string
		content string
	}{
		{"plain", "module github.com/getsyntegrity/ego\n\ngo 1.21\n"},
		{"quoted", "module \"github.com/getsyntegrity/ego\"\n\ngo 1.21\n"},
		{"trailing comment", "module github.com/getsyntegrity/ego // root module\n\ngo 1.21\n"},
		{"quoted with trailing comment", "module \"github.com/getsyntegrity/ego\" // root module\n\ngo 1.21\n"},
		{"block form", "module (\n\tgithub.com/getsyntegrity/ego\n)\n\ngo 1.21\n"},
		{"block form quoted", "module (\n\t\"github.com/getsyntegrity/ego\"\n)\n\ngo 1.21\n"},
		{"block form with comment on the path line", "module (\n\tgithub.com/getsyntegrity/ego // root module\n)\n\ngo 1.21\n"},
		{"leading comment line", "// this is the root module\nmodule github.com/getsyntegrity/ego\n\ngo 1.21\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			goMod := filepath.Join(dir, "go.mod")
			writeFile(t, goMod, c.content)

			got, err := parseGoModModulePath(goMod)
			if err != nil {
				t.Fatalf("parseGoModModulePath(%q): %v", c.content, err)
			}
			if got != want {
				t.Errorf("parseGoModModulePath() = %q, want %q", got, want)
			}
		})
	}
}

func TestParseGoModModulePath_NoModuleLine(t *testing.T) {
	dir := t.TempDir()
	goMod := filepath.Join(dir, "go.mod")
	writeFile(t, goMod, "go 1.21\n")

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
	writeFile(t, filepath.Join(dir, "go.mod"), "module github.com/example/nested\n\ngo 1.21\n")
	writeFile(t, filepath.Join(dir, "root.go"), `package nested

import (
	"context"

	"github.com/getsyntegrity/ego"
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

import "github.com/getsyntegrity/ego/internal/queue"
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
	if !containsImport(root.Imports, "github.com/getsyntegrity/ego") {
		t.Errorf("root Imports = %v, want to contain github.com/getsyntegrity/ego", root.Imports)
	}
	if containsImport(root.Imports, "testing") {
		t.Errorf("root Imports = %v, must not contain the test-only import testing", root.Imports)
	}

	if root.Name != "nested" {
		t.Errorf("root Name = %q, want nested (from its package clause)", root.Name)
	}

	sub := pkgs[1]
	if sub.ImportPath != "github.com/example/nested/sub" {
		t.Errorf("sub ImportPath = %q, want github.com/example/nested/sub", sub.ImportPath)
	}
	if sub.Name != "sub" {
		t.Errorf("sub Name = %q, want sub", sub.Name)
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

// TestDiscoverNestedModuleDirs_FindsModuleNestedInsideAnotherModule proves
// discoverNestedModuleDirs keeps descending once it finds a module's
// go.mod, so a module nested inside another nested module is discovered
// too, instead of stopping at the outer module and never seeing the inner
// one.
func TestDiscoverNestedModuleDirs_FindsModuleNestedInsideAnotherModule(t *testing.T) {
	repoRoot := t.TempDir()
	writeFile(t, filepath.Join(repoRoot, "go.mod"), "module github.com/example/root\n")
	writeFile(t, filepath.Join(repoRoot, "outer", "go.mod"), "module github.com/example/outer\n")
	writeFile(t, filepath.Join(repoRoot, "outer", "inner", "go.mod"), "module github.com/example/inner\n")

	dirs, err := discoverNestedModuleDirs(repoRoot)
	if err != nil {
		t.Fatalf("discoverNestedModuleDirs: %v", err)
	}
	want := []string{
		filepath.Join(repoRoot, "outer"),
		filepath.Join(repoRoot, "outer", "inner"),
	}
	if len(dirs) != len(want) {
		t.Fatalf("dirs = %v, want %v", dirs, want)
	}
	for i, w := range want {
		if dirs[i] != w {
			t.Errorf("dirs[%d] = %q, want %q", i, dirs[i], w)
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

// TestLoadNestedModule_DoesNotMergeInnerModule proves loadNestedModule
// stops descending at a subdirectory that has its own go.mod: the outer
// module's package set must not include the inner module's files, and the
// inner module loads separately, under its own module path, when
// loadNestedModule is called on it directly (as discoverNestedModuleDirs
// now does).
func TestLoadNestedModule_DoesNotMergeInnerModule(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module github.com/example/outer\n\ngo 1.21\n")
	writeFile(t, filepath.Join(dir, "outer.go"), `package outer

import "fmt"

var _ = fmt.Sprintf
`)
	writeFile(t, filepath.Join(dir, "inner", "go.mod"), "module github.com/example/inner\n\ngo 1.21\n")
	writeFile(t, filepath.Join(dir, "inner", "inner.go"), `package inner

import "context"

var _ = context.Background
`)

	outerPkgs, err := loadNestedModule(dir)
	if err != nil {
		t.Fatalf("loadNestedModule(outer): %v", err)
	}
	if len(outerPkgs) != 1 {
		t.Fatalf("outer pkgs = %+v, want exactly 1 (inner must not be merged in)", outerPkgs)
	}
	if outerPkgs[0].ImportPath != "github.com/example/outer" {
		t.Errorf("outer ImportPath = %q, want github.com/example/outer", outerPkgs[0].ImportPath)
	}
	if containsImport(outerPkgs[0].Imports, "context") {
		t.Errorf("outer Imports = %v, must not contain the inner module's import context", outerPkgs[0].Imports)
	}

	innerPkgs, err := loadNestedModule(filepath.Join(dir, "inner"))
	if err != nil {
		t.Fatalf("loadNestedModule(inner): %v", err)
	}
	if len(innerPkgs) != 1 {
		t.Fatalf("inner pkgs = %+v, want exactly 1", innerPkgs)
	}
	if innerPkgs[0].ImportPath != "github.com/example/inner" {
		t.Errorf("inner ImportPath = %q, want github.com/example/inner", innerPkgs[0].ImportPath)
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
