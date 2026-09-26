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

// End-to-end tests: unlike loader_test.go's other tests, these actually
// shell out to the real `go` toolchain (via loadRootModule) or drive the
// full runCheck flow, instead of exercising only the pure go/parser-based
// nested-module loader. Each one skips instead of failing when `go` is not
// on PATH, since that is an environment gap, not a bug in this package.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pablogore/ego/v4/internal/cmd/archcheck/rules"
)

func requireGo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	// The fixtures are self-contained modules: do not inherit the caller's
	// module mode (CI sets GOFLAGS=-mod=vendor) or an enclosing workspace.
	t.Setenv("GOFLAGS", "")
	t.Setenv("GOWORK", "off")
}

// TestRequireGo_IsolatesInheritedGoEnv checks that the fixtures do not
// inherit the caller's module settings: CI runs the tests with
// GOFLAGS=-mod=vendor, and the temp-dir modules have no vendor directory.
func TestRequireGo_IsolatesInheritedGoEnv(t *testing.T) {
	t.Setenv("GOFLAGS", "-mod=vendor")
	t.Setenv("GOWORK", "/nonexistent/go.work")
	requireGo(t)
	if got := os.Getenv("GOFLAGS"); got != "" {
		t.Errorf("GOFLAGS = %q, want empty", got)
	}
	if got := os.Getenv("GOWORK"); got != "off" {
		t.Errorf("GOWORK = %q, want off", got)
	}
}

// TestLoadRootModule_RealGoList builds a tiny module in t.TempDir() with a
// contract-like package, a "runtime-like" package standing in for a
// dependency a rule would forbid, and an adapter package that imports
// both, then loads it through the real `go list -e -json ./...`
// (loadRootModule), not an in-memory fixture. Every import here resolves
// offline: they are all within the temp module itself or stdlib, so this
// test never needs network access or a module download.
func TestLoadRootModule_RealGoList(t *testing.T) {
	requireGo(t)

	dir := t.TempDir()
	const modulePath = "github.com/example/tinymod"
	writeFile(t, filepath.Join(dir, "go.mod"), "module "+modulePath+"\n\ngo 1.21\n")
	writeFile(t, filepath.Join(dir, "contract", "contract.go"), `package contract

// Marker stands in for a contract type with no runtime dependency.
type Marker struct{}
`)
	writeFile(t, filepath.Join(dir, "runtimeish", "runtimeish.go"), `package runtimeish

import "sync"

// Engine stands in for a runtime engine type: offline and dependency-free,
// so this fixture never needs network access, but it plays the role a
// rule like external-adapter-no-runtime would forbid an adapter from
// importing.
type Engine struct {
	mu sync.Mutex
}
`)
	writeFile(t, filepath.Join(dir, "adapter", "adapter.go"), `package adapter

import (
	"`+modulePath+`/contract"
	"`+modulePath+`/runtimeish"
)

// Adapter imports both a contract and the "forbidden" runtimeish
// dependency — the edge a rule like external-adapter-no-runtime would
// reject. This test only exercises the real go list loader, not rule
// evaluation.
type Adapter struct {
	Contract contract.Marker
	Engine   runtimeish.Engine
}
`)
	writeFile(t, filepath.Join(dir, "adapter", "adapter_test.go"), `package adapter

import "testing"

func TestNothing(t *testing.T) {}
`)

	pkgs, err := loadRootModule(dir)
	if err != nil {
		t.Fatalf("loadRootModule: %v", err)
	}

	byPath := make(map[string]rules.Package, len(pkgs))
	for _, p := range pkgs {
		byPath[p.ImportPath] = p
	}

	adapterPkg, ok := byPath[modulePath+"/adapter"]
	if !ok {
		t.Fatalf("adapter package not found in %+v", pkgs)
	}
	if adapterPkg.Kind != rules.RootModule {
		t.Errorf("adapter Kind = %v, want RootModule", adapterPkg.Kind)
	}
	if !containsImport(adapterPkg.Imports, modulePath+"/runtimeish") {
		t.Errorf("adapter Imports = %v, want to contain %s/runtimeish", adapterPkg.Imports, modulePath)
	}
	if !containsImport(adapterPkg.Imports, modulePath+"/contract") {
		t.Errorf("adapter Imports = %v, want to contain %s/contract", adapterPkg.Imports, modulePath)
	}
	if containsImport(adapterPkg.Imports, "testing") {
		t.Errorf("adapter Imports = %v, must not contain the test-only import testing", adapterPkg.Imports)
	}
}

// TestLoadRootModule_LoadErrorFailsClosed proves loadRootModule fails
// closed: a package `go list -e` itself cannot fully load (here, an
// unterminated import block) makes loadRootModule return an error rather
// than silently omitting that package's imports from the graph.
func TestLoadRootModule_LoadErrorFailsClosed(t *testing.T) {
	requireGo(t)

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module github.com/example/brokenmod\n\ngo 1.21\n")
	// An unterminated import block: go list -e -json reports this package
	// with a non-nil "Error", unlike a syntax error later in the file
	// (which go list's own lightweight prescan does not always catch).
	writeFile(t, filepath.Join(dir, "broken", "broken.go"), "package broken\n\nimport (\n")

	if _, err := loadRootModule(dir); err == nil {
		t.Fatal("loadRootModule() = nil error, want an error for a package with a load error")
	}
}

// writeRunFixture builds a tiny repository fixture under t.TempDir(): a
// root module with one contract package (tenancy) and one application
// package (migration) that imports it, plus a nested adapter module
// (publisher/kafka) that either stays clean or imports the root module
// path directly — the one edge external-adapter-no-runtime forbids — so a
// single builder produces both a clean graph and a violating one for
// runCheck's end-to-end tests.
func writeRunFixture(t *testing.T, withViolation bool) (dir, modulePath string) {
	t.Helper()
	dir = t.TempDir()
	modulePath = "github.com/example/archcheckfixture"
	writeFile(t, filepath.Join(dir, "go.mod"), "module "+modulePath+"\n\ngo 1.21\n")
	writeFile(t, filepath.Join(dir, "tenancy", "tenancy.go"), "package tenancy\n\n// Marker is a contract type.\ntype Marker struct{}\n")
	writeFile(t, filepath.Join(dir, "migration", "migration.go"),
		"package migration\n\nimport \""+modulePath+"/tenancy\"\n\nvar _ = tenancy.Marker{}\n")
	writeFile(t, filepath.Join(dir, "publisher", "kafka", "go.mod"), "module "+modulePath+"/publisher/kafka\n\ngo 1.21\n")

	kafka := "package kafka\n\nimport \"fmt\"\n\nvar _ = fmt.Sprintf\n"
	if withViolation {
		kafka = "package kafka\n\nimport \"" + modulePath + "\"\n"
	}
	writeFile(t, filepath.Join(dir, "publisher", "kafka", "kafka.go"), kafka)
	return dir, modulePath
}

// TestRunCheck_CleanGraphPasses is runCheck's "clean graph" case: no
// violations, no baseline needed, nil error.
func TestRunCheck_CleanGraphPasses(t *testing.T) {
	requireGo(t)

	dir, _ := writeRunFixture(t, false)
	var stdout strings.Builder
	if err := runCheck(dir, nil, &stdout); err != nil {
		t.Fatalf("runCheck() = %v, want nil for a clean graph; output:\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), "0 violation(s), 0 stale entries") {
		t.Errorf("stdout = %q, want a clean summary line", stdout.String())
	}
}

// TestRunCheck_UnbaselinedViolationFails is runCheck's "unbaselined
// violation" case: a real forbidden import with no baseline entry for it
// must fail the check.
func TestRunCheck_UnbaselinedViolationFails(t *testing.T) {
	requireGo(t)

	dir, _ := writeRunFixture(t, true)
	var stdout strings.Builder
	err := runCheck(dir, nil, &stdout)
	if err == nil {
		t.Fatal("runCheck() = nil error, want an error for an unbaselined violation")
	}
	if !strings.Contains(stdout.String(), "external-adapter-no-runtime") {
		t.Errorf("stdout = %q, want it to name the broken rule", stdout.String())
	}
}

// TestRunCheck_StaleBaselineEntryFails is runCheck's "stale baseline
// entry" case: a clean graph plus a baseline entry that matches no real
// violation must still fail the check, so the baseline cannot silently
// accumulate dead entries.
func TestRunCheck_StaleBaselineEntryFails(t *testing.T) {
	requireGo(t)

	dir, modulePath := writeRunFixture(t, false)
	baseline := []rules.BaselineEntry{
		{
			Importer:         modulePath + "/publisher/kafka",
			Import:           modulePath,
			Rule:             "external-adapter-no-runtime",
			Owner:            "@fixture",
			Justification:    "test fixture: entry that matches nothing real",
			RemovalCriterion: "never; test only",
		},
	}
	var stdout strings.Builder
	err := runCheck(dir, baseline, &stdout)
	if err == nil {
		t.Fatal("runCheck() = nil error, want an error for a stale baseline entry")
	}
	if !strings.Contains(stdout.String(), "no longer matches a violation") {
		t.Errorf("stdout = %q, want it to name the stale entry", stdout.String())
	}
}
