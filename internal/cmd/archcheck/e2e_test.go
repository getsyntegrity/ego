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
	writeFile(t, filepath.Join(dir, "cmd", "tool", "main.go"), `package main

import "`+modulePath+`/adapter"

func main() { _ = adapter.Adapter{} }
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
	if adapterPkg.Name != "adapter" {
		t.Errorf("adapter Name = %q, want adapter", adapterPkg.Name)
	}
	toolPkg, ok := byPath[modulePath+"/cmd/tool"]
	if !ok {
		t.Fatalf("cmd/tool package not found in %+v", pkgs)
	}
	if toolPkg.Name != "main" {
		t.Errorf("cmd/tool Name = %q, want main", toolPkg.Name)
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
// root module with its root package, one contract package (tenancy), one
// application package (migration) that imports it and the composition
// package (compose), plus a nested adapter module
// (publisher/kafka) that either stays clean or imports the root module
// path directly — the one edge external-adapter-no-runtime forbids — so a
// single builder produces both a clean graph and a violating one for
// runCheck's end-to-end tests.
func writeRunFixture(t *testing.T, withViolation bool) (dir, modulePath string) {
	t.Helper()
	dir = t.TempDir()
	modulePath = "github.com/example/archcheckfixture"
	writeFile(t, filepath.Join(dir, "go.mod"), "module "+modulePath+"\n\ngo 1.21\n")
	writeFile(t, filepath.Join(dir, "root.go"), "package archcheckfixture\n\n// Runtime stands in for the root package ego.\ntype Runtime struct{}\n")
	writeFile(t, filepath.Join(dir, "tenancy", "tenancy.go"), "package tenancy\n\n// Marker is a contract type.\ntype Marker struct{}\n")
	writeFile(t, filepath.Join(dir, "compose", "compose.go"),
		"package compose\n\nimport \""+modulePath+"/tenancy\"\n\n// Spec stands in for compose.Spec.\ntype Spec struct{ Resolver tenancy.Marker }\n")
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

// TestRunCheck_CompositionNoRuntimeViolationFails: compose importing the
// root package (ego's stand-in) fails the check under
// composition-no-runtime (ego-arch-003 design §D8).
func TestRunCheck_CompositionNoRuntimeViolationFails(t *testing.T) {
	requireGo(t)

	dir, modulePath := writeRunFixture(t, false)
	writeFile(t, filepath.Join(dir, "compose", "compose.go"),
		"package compose\n\nimport \""+modulePath+"\"\n\n// Spec leaks the runtime.\ntype Spec struct{ R archcheckfixture.Runtime }\n")
	var stdout strings.Builder
	err := runCheck(dir, nil, &stdout)
	if err == nil {
		t.Fatalf("runCheck() = nil error, want composition-no-runtime to fail; output:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "composition-no-runtime") {
		t.Errorf("stdout = %q, want it to name composition-no-runtime", stdout.String())
	}
}

// TestRunCheck_CompositionLeafViolationFails: a production package that is
// neither under compose/ nor main must not import compose.
func TestRunCheck_CompositionLeafViolationFails(t *testing.T) {
	requireGo(t)

	dir, modulePath := writeRunFixture(t, false)
	writeFile(t, filepath.Join(dir, "helper", "helper.go"),
		"package helper\n\nimport \""+modulePath+"/compose\"\n\nvar _ = compose.Spec{}\n")
	var stdout strings.Builder
	err := runCheck(dir, nil, &stdout)
	if err == nil {
		t.Fatalf("runCheck() = nil error, want composition-leaf to fail; output:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "composition-leaf") {
		t.Errorf("stdout = %q, want it to name composition-leaf", stdout.String())
	}
}

// TestRunCheck_CompositionLeafAllowsMainAndTests: a main package and a
// non-main package's test file may import compose; only production imports
// reach the graph, and main packages are exempt.
func TestRunCheck_CompositionLeafAllowsMainAndTests(t *testing.T) {
	requireGo(t)

	dir, modulePath := writeRunFixture(t, false)
	writeFile(t, filepath.Join(dir, "cmd", "app", "main.go"),
		"package main\n\nimport \""+modulePath+"/compose\"\n\nfunc main() { _ = compose.Spec{} }\n")
	writeFile(t, filepath.Join(dir, "helper", "helper.go"), "package helper\n\n// Name is a plain value.\nconst Name = \"helper\"\n")
	writeFile(t, filepath.Join(dir, "helper", "helper_test.go"),
		"package helper\n\nimport (\n\t\"testing\"\n\n\t\""+modulePath+"/compose\"\n)\n\nfunc TestSpec(t *testing.T) { _ = compose.Spec{} }\n")
	var stdout strings.Builder
	if err := runCheck(dir, nil, &stdout); err != nil {
		t.Fatalf("runCheck() = %v, want nil; output:\n%s", err, stdout.String())
	}
}

// TestLoadModuleTable_ReadsInRepoRequirements builds a root module and two
// nested modules, one requiring the root and the other nested module (the
// shape test/compat has, ego-arch-006 slice S1) plus a third-party module,
// and checks that loadModuleTable lists every module with only its
// in-repository requirements.
func TestLoadModuleTable_ReadsInRepoRequirements(t *testing.T) {
	requireGo(t)

	dir := t.TempDir()
	const modulePath = "github.com/example/tablefixture"
	writeFile(t, filepath.Join(dir, "go.mod"), "module "+modulePath+"\n\ngo 1.21\n")
	writeFile(t, filepath.Join(dir, "adapter", "go.mod"),
		"module "+modulePath+"/adapter\n\ngo 1.21\n\nrequire "+modulePath+" v0.0.0\n\nreplace "+modulePath+" => ../\n")
	writeFile(t, filepath.Join(dir, "test", "it", "go.mod"),
		"module "+modulePath+"/test/it\n\ngo 1.21\n\nrequire (\n\t"+modulePath+" v0.0.0\n\t"+modulePath+"/adapter v0.0.0\n\tgithub.com/google/uuid v1.6.0\n)\n")

	modules, err := loadModuleTable(dir)
	if err != nil {
		t.Fatalf("loadModuleTable: %v", err)
	}
	byPath := make(map[string]rules.Module, len(modules))
	for _, m := range modules {
		byPath[m.Path] = m
	}
	if len(modules) != 3 {
		t.Fatalf("modules = %+v, want the root and two nested modules", modules)
	}
	if got := byPath[modulePath].Requires; len(got) != 0 {
		t.Errorf("root Requires = %v, want none", got)
	}
	if got := byPath[modulePath+"/adapter"].Requires; len(got) != 1 || got[0] != modulePath {
		t.Errorf("adapter Requires = %v, want [%s]", got, modulePath)
	}
	got := byPath[modulePath+"/test/it"].Requires
	if len(got) != 2 || got[0] != modulePath || got[1] != modulePath+"/adapter" {
		t.Errorf("test/it Requires = %v, want the root and adapter only (no third-party module)", got)
	}
}

// TestRunCheck_ModuleCycleFails: the root requiring a nested module that
// requires the root back is the cycle ego-arch-001 §3 forbids, and runCheck
// must fail on it (no-module-cycle, ego-arch-006 slice S1).
func TestRunCheck_ModuleCycleFails(t *testing.T) {
	requireGo(t)

	dir, modulePath := writeRunFixture(t, false)
	writeFile(t, filepath.Join(dir, "go.mod"),
		"module "+modulePath+"\n\ngo 1.21\n\nrequire "+modulePath+"/publisher/kafka v0.0.0\n\nreplace "+modulePath+"/publisher/kafka => ./publisher/kafka\n")
	writeFile(t, filepath.Join(dir, "publisher", "kafka", "go.mod"),
		"module "+modulePath+"/publisher/kafka\n\ngo 1.21\n\nrequire "+modulePath+" v0.0.0\n\nreplace "+modulePath+" => ../../\n")
	var stdout strings.Builder
	err := runCheck(dir, nil, &stdout)
	if err == nil {
		t.Fatalf("runCheck() = nil error, want no-module-cycle to fail; output:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "no-module-cycle") {
		t.Errorf("stdout = %q, want it to name no-module-cycle", stdout.String())
	}
}

// TestRunCheck_NestedToNestedInternalFails: a nested module importing
// another nested module's internal/ package fails no-cross-module-internal,
// which before ego-arch-006 slice S1 only covered imports of the root's
// internal/ packages.
func TestRunCheck_NestedToNestedInternalFails(t *testing.T) {
	requireGo(t)

	dir, modulePath := writeRunFixture(t, false)
	writeFile(t, filepath.Join(dir, "publisher", "kafka", "internal", "codec", "codec.go"),
		"package codec\n\n// Name is a plain value.\nconst Name = \"codec\"\n")
	writeFile(t, filepath.Join(dir, "test", "it", "go.mod"), "module "+modulePath+"/test/it\n\ngo 1.21\n")
	writeFile(t, filepath.Join(dir, "test", "it", "it.go"),
		"package it\n\nimport \""+modulePath+"/publisher/kafka/internal/codec\"\n\nvar _ = codec.Name\n")
	var stdout strings.Builder
	err := runCheck(dir, nil, &stdout)
	if err == nil {
		t.Fatalf("runCheck() = nil error, want no-cross-module-internal to fail; output:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "no-cross-module-internal") {
		t.Errorf("stdout = %q, want it to name no-cross-module-internal", stdout.String())
	}
}

// TestLoadModuleTable_MalformedGoModFailsClosed: a nested go.mod that
// `go mod edit -json` cannot parse makes loadModuleTable return an error
// naming the module directory, rather than a table without that module.
func TestLoadModuleTable_MalformedGoModFailsClosed(t *testing.T) {
	requireGo(t)

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module github.com/example/malformed\n\ngo 1.21\n")
	writeFile(t, filepath.Join(dir, "broken", "go.mod"), "module github.com/example/malformed/broken\n\nrequire (\n")

	_, err := loadModuleTable(dir)
	if err == nil {
		t.Fatal("loadModuleTable() = nil error, want an error for a malformed go.mod")
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Errorf("error %q does not name the broken module directory", err)
	}
}

// TestLoadModuleTable_EmptyModulePathFailsClosed: a go.mod with no module
// directive declares no module path. readGoModRequirements must not hand
// back a usable empty path, and loadModuleTable refuses it rather than
// indexing a module whose empty path every import path would match.
func TestLoadModuleTable_EmptyModulePathFailsClosed(t *testing.T) {
	requireGo(t)

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module github.com/example/nopath\n\ngo 1.21\n")
	writeFile(t, filepath.Join(dir, "anon", "go.mod"), "go 1.21\n")

	if path, _, err := readGoModRequirements(filepath.Join(dir, "anon")); err == nil && path != "" {
		t.Fatalf("readGoModRequirements() = (%q, nil), want an empty path or an error for a go.mod with no module directive", path)
	}
	_, err := loadModuleTable(dir)
	if err == nil {
		t.Fatal("loadModuleTable() = nil error, want an error for a go.mod with no module path")
	}
	if !strings.Contains(err.Error(), "anon") {
		t.Errorf("error %q does not name the anon module directory", err)
	}
}
