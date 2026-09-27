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
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/pablogore/ego/v4/internal/cmd/ciselect/selector"
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

const rootModule = "github.com/example/root"

// writeModuleTree builds a small repository with a root module and three
// nested modules:
//   - moda requires the root through a local replace (a module edge);
//   - modb requires moda through a local replace (a nested-to-nested edge)
//     plus a third-party module (never an edge);
//   - modc requires moda at a published version with no replace (pinned,
//     reported but never an edge);
//
// plus a go.mod hidden inside vendor/, which discovery must never visit.
func writeModuleTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module "+rootModule+"\n\ngo 1.26.0\n")
	writeFile(t, filepath.Join(root, "moda", "go.mod"), `module `+rootModule+`/moda

go 1.26.0

require `+rootModule+` v0.0.0

replace `+rootModule+` => ../
`)
	writeFile(t, filepath.Join(root, "modb", "go.mod"), `module `+rootModule+`/modb

go 1.26.0

require (
	`+rootModule+`/moda v0.0.0
	example.com/thirdparty v1.2.3
)

replace `+rootModule+`/moda => ../moda
`)
	writeFile(t, filepath.Join(root, "modc", "go.mod"), `module `+rootModule+`/modc

go 1.26.0

require `+rootModule+`/moda v0.3.0
`)
	writeFile(t, filepath.Join(root, "vendor", "example.com", "dep", "go.mod"), "module example.com/dep\n\ngo 1.26.0\n")
	return root
}

// TestModuleDiscovery_FindsNestedModules asserts that findSatelliteDirs
// discovers exactly the nested modules and never descends into vendor/,
// so the go.mod hiding there is never even visited.
func TestModuleDiscovery_FindsNestedModules(t *testing.T) {
	root := writeModuleTree(t)

	dirs, err := findSatelliteDirs(root)
	if err != nil {
		t.Fatalf("findSatelliteDirs: %v", err)
	}
	sort.Strings(dirs)
	if want := []string{"moda", "modb", "modc"}; !reflect.DeepEqual(dirs, want) {
		t.Fatalf("findSatelliteDirs() = %v, want %v", dirs, want)
	}
}

// TestModuleDiscovery_BuildsRequirementGraph asserts that discoverModules
// reads every module's go.mod through `go mod edit -json` and keeps only
// in-repository requirements: those resolved to the working tree through a
// local replace become Deps, those pinned to a published version become
// Pinned, and third-party requirements are dropped.
func TestModuleDiscovery_BuildsRequirementGraph(t *testing.T) {
	root := writeModuleTree(t)

	got, err := discoverModules(root)
	if err != nil {
		t.Fatalf("discoverModules: %v", err)
	}
	want := []selector.ModuleInfo{
		{Dir: ".", Path: rootModule},
		{Dir: "moda", Path: rootModule + "/moda", Deps: []string{rootModule}},
		{Dir: "modb", Path: rootModule + "/modb", Deps: []string{rootModule + "/moda"}},
		{Dir: "modc", Path: rootModule + "/modc", Pinned: []string{rootModule + "/moda@v0.3.0"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discoverModules() =\n%+v\nwant\n%+v", got, want)
	}
}

// A go.mod that cannot be read must fail the whole selection, so the
// workflow falls back to the full suite, never a silent partial graph.
func TestModuleDiscovery_BrokenGoModFails(t *testing.T) {
	root := writeModuleTree(t)
	writeFile(t, filepath.Join(root, "modb", "go.mod"), "this is not a go.mod\n")

	if _, err := discoverModules(root); err == nil {
		t.Fatalf("discoverModules() error = nil, want an error for a malformed go.mod")
	}
}

// A local replace that points an in-repository requirement at a directory
// other than that module's own is ambiguous: fail closed.
func TestModuleDiscovery_ReplaceToWrongDirectoryFails(t *testing.T) {
	root := writeModuleTree(t)
	writeFile(t, filepath.Join(root, "modb", "go.mod"), `module `+rootModule+`/modb

go 1.26.0

require `+rootModule+`/moda v0.0.0

replace `+rootModule+`/moda => ../modc
`)

	if _, err := discoverModules(root); err == nil {
		t.Fatalf("discoverModules() error = nil, want an error for a replace to the wrong module directory")
	}
}

// Every go subprocess ciselect starts runs outside workspace mode, so a
// go.work can never satisfy a requirement a go.mod does not declare.
func TestGoCommand_ForcesGOWORKOff(t *testing.T) {
	t.Setenv("GOWORK", "/somewhere/go.work")
	cmd := goCommand(t.TempDir(), "env", "GOWORK")

	// Only GOWORK entries are reported: the rest of the environment may
	// hold secrets and must never reach a test log.
	var gowork []string
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "GOWORK=") {
			gowork = append(gowork, kv)
		}
	}
	if !reflect.DeepEqual(gowork, []string{"GOWORK=off"}) {
		t.Fatalf("go subprocess GOWORK entries = %v, want exactly [GOWORK=off]", gowork)
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go env GOWORK: %v", err)
	}
	if got := string(out); got != "off\n" {
		t.Fatalf("go env GOWORK = %q, want %q", got, "off\n")
	}
}

// writeRootPackage gives the temporary root module one package, so the
// root lane's `go list ./...` has something to load.
func writeRootPackage(t *testing.T, root string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "root.go"), "package root\n")
}

// A selector run on a changed-file list fails on an unreadable nested
// go.mod, so the workflow's `-all` fallback takes over.
func TestRun_ChangedFailsOnBrokenNestedGoMod(t *testing.T) {
	root := writeModuleTree(t)
	writeRootPackage(t, root)
	writeFile(t, filepath.Join(root, "modb", "go.mod"), "this is not a go.mod\n")
	changed := filepath.Join(t.TempDir(), "changed.txt")
	writeFile(t, changed, "moda/x.go\n")

	err := run([]string{"-changed", changed, "-module-dir", root, "-repo-root", root, "-out-dir", t.TempDir()}, io.Discard, io.Discard)
	if err == nil {
		t.Fatalf("run() error = nil, want the broken go.mod to fail the selection")
	}
}

// The `-all` fallback must not fail on the very thing it falls back from:
// with an unreadable nested go.mod it still selects every discovered
// module by directory.
func TestRun_AllSurvivesBrokenNestedGoMod(t *testing.T) {
	root := writeModuleTree(t)
	writeRootPackage(t, root)
	writeFile(t, filepath.Join(root, "modb", "go.mod"), "this is not a go.mod\n")
	out := t.TempDir()

	if err := run([]string{"-all", "-module-dir", root, "-repo-root", root, "-out-dir", out}, io.Discard, io.Discard); err != nil {
		t.Fatalf("run(-all) error = %v, want success", err)
	}
	modules, err := os.ReadFile(filepath.Join(out, "modules.json"))
	if err != nil {
		t.Fatalf("reading modules.json: %v", err)
	}
	if string(modules) != "[\"moda\",\"modb\",\"modc\"]\n" {
		t.Fatalf("modules.json = %q, want every discovered module", modules)
	}
}

// plan.json lists every module, selected or not, and keeps modules.json's
// shape: nested directories only, "[]" when empty.
func TestWriteOutputs_PlanAndModulesJSON(t *testing.T) {
	res := selector.Result{
		Mode:    selector.ModeNone,
		Reasons: []string{"only documentation/governance or satellite-module changes"},
		Modules: []selector.ModuleSelection{{Dir: "moda", Reason: "changed files in moda", Chain: []string{"moda"}}},
		Plan: []selector.ModulePlan{
			{Dir: ".", Path: rootModule, Reason: "not affected"},
			{Dir: "moda", Path: rootModule + "/moda", Selected: true, Reason: "changed files in moda", Chain: []string{"moda"}},
		},
	}
	out := t.TempDir()
	if err := writeOutputs(out, res, "summary"); err != nil {
		t.Fatalf("writeOutputs: %v", err)
	}

	modules, err := os.ReadFile(filepath.Join(out, "modules.json"))
	if err != nil {
		t.Fatalf("reading modules.json: %v", err)
	}
	if string(modules) != "[\"moda\"]\n" {
		t.Fatalf("modules.json = %q, want %q", modules, "[\"moda\"]\n")
	}

	raw, err := os.ReadFile(filepath.Join(out, "plan.json"))
	if err != nil {
		t.Fatalf("reading plan.json: %v", err)
	}
	var plan struct {
		Global  bool     `json:"global"`
		Reasons []string `json:"reasons"`
		Root    struct {
			Mode     string   `json:"mode"`
			Selected []string `json:"selected"`
		} `json:"root"`
		Modules []struct {
			Dir      string   `json:"dir"`
			Path     string   `json:"path"`
			Selected bool     `json:"selected"`
			Reason   string   `json:"reason"`
			Chain    []string `json:"chain"`
		} `json:"modules"`
	}
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatalf("plan.json is not valid JSON: %v\n%s", err, raw)
	}
	if plan.Global || plan.Root.Mode != "none" || plan.Root.Selected == nil || len(plan.Modules) != 2 {
		t.Fatalf("plan.json = %+v, want root mode none, selected [] (not null) and both modules", plan)
	}
	if m := plan.Modules[1]; m.Dir != "moda" || !m.Selected || m.Path != rootModule+"/moda" || m.Reason == "" {
		t.Fatalf("plan.json modules[1] = %+v, want moda selected with its path and reason", m)
	}
}
