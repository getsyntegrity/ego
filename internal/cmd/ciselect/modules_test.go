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
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/getsyntegrity/ego/internal/cmd/ciselect/selector"
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
	// Parser-read imports: tests included, build tags ignored, own-module
	// and third-party imports dropped.
	writeFile(t, filepath.Join(root, "moda", "a.go"), "package moda\n\nimport _ \""+rootModule+"/command\"\n")
	writeFile(t, filepath.Join(root, "moda", "a_test.go"), "package moda\n\nimport _ \""+rootModule+"/testkit\"\n")
	writeFile(t, filepath.Join(root, "modb", "b_compat.go"), "//go:build compat\n\npackage modb\n\nimport (\n\t_ \""+rootModule+"/moda/sub\"\n\t_ \"example.com/thirdparty/x\"\n)\n")
	writeFile(t, filepath.Join(root, "modb", "self.go"), "package modb\n\nimport _ \""+rootModule+"/modb/inner\"\n")
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
		{Dir: "moda", Path: rootModule + "/moda", Deps: []string{rootModule}, Imports: []string{rootModule + "/command", rootModule + "/testkit"}},
		{Dir: "modb", Path: rootModule + "/modb", Deps: []string{rootModule + "/moda"}, Imports: []string{rootModule + "/moda/sub"}},
		{Dir: "modc", Path: rootModule + "/modc", Pinned: []string{rootModule + "/moda@v0.3.0"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discoverModules() =\n%+v\nwant\n%+v", got, want)
	}
}

// The root module's import set covers only the root's own files: nested
// module directories and testdata/ are not the root's.
func TestModuleDiscovery_RootImportsSkipNestedModulesAndTestdata(t *testing.T) {
	root := writeModuleTree(t)
	writeFile(t, filepath.Join(root, "pkg", "p.go"), "package pkg\n\nimport (\n\t_ \""+rootModule+"/modb\"\n\t_ \""+rootModule+"/pkg2\"\n)\n")
	writeFile(t, filepath.Join(root, "pkg", "testdata", "t.go"), "package t\n\nimport _ \""+rootModule+"/modc\"\n")

	got, err := discoverModules(root)
	if err != nil {
		t.Fatalf("discoverModules: %v", err)
	}
	if got[0].Dir != "." || !reflect.DeepEqual(got[0].Imports, []string{rootModule + "/modb"}) {
		t.Fatalf("root module = %+v, want Imports [%s/modb]", got[0], rootModule)
	}
}

// gitRepo creates a git repository at a new temporary directory with the
// given files committed, and returns its path.
func gitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for p, c := range files {
		writeFile(t, filepath.Join(root, filepath.FromSlash(p)), c)
	}
	gitRun(t, root, "init", "-q")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "base")
	return root
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// -base: a changed nested go.mod is classified by its existence at base
// (git) and at head (the working tree): edit, add or delete. Other paths
// and the root go.mod are not reported.
func TestGoModPresence_EditAddDelete(t *testing.T) {
	root := gitRepo(t, map[string]string{
		"go.mod":      "module " + rootModule + "\n",
		"moda/go.mod": "module " + rootModule + "/moda\n",
		"modd/go.mod": "module " + rootModule + "/modd\n",
	})
	base := gitRun(t, root, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(root, "moda", "go.mod"), "module "+rootModule+"/moda\n\ngo 1.26.0\n")
	writeFile(t, filepath.Join(root, "modn", "go.mod"), "module "+rootModule+"/modn\n")
	if err := os.Remove(filepath.Join(root, "modd", "go.mod")); err != nil {
		t.Fatal(err)
	}

	got, err := goModPresence(root, base, []string{"moda/go.mod", "modn/go.mod", "modd/go.mod", "moda/x.go", "go.mod"})
	if err != nil {
		t.Fatalf("goModPresence: %v", err)
	}
	want := map[string]selector.GoModPresence{
		"moda/go.mod": {AtBase: true, AtHead: true},
		"modn/go.mod": {AtHead: true},
		"modd/go.mod": {AtBase: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("goModPresence() = %v, want %v", got, want)
	}
}

// An unknown -base revision fails the selection (and the workflow falls
// back to -all), never a silent "absent at base".
func TestGoModPresence_UnknownBaseFails(t *testing.T) {
	root := gitRepo(t, map[string]string{"go.mod": "module " + rootModule + "\n"})

	if _, err := goModPresence(root, "0000000000000000000000000000000000000000", []string{"moda/go.mod"}); err == nil {
		t.Fatalf("goModPresence() error = nil, want an error for an unknown base")
	}
	// A revision can never start with "-": that would be a git option.
	if _, err := goModPresence(root, "--git-dir=/nonexistent", []string{"moda/go.mod"}); err == nil || !strings.Contains(err.Error(), "must not start with") {
		t.Fatalf("goModPresence() error = %v, want an option-like base rejected", err)
	}
}

// With a real merge-base, as the workflow passes it: a go.mod added on the
// base branch after the branch point is absent at the merge-base, so the
// pull request's own add of the same file is still classified as an add
// (the three-dot diff that produced the changed-file list agrees).
func TestGoModPresence_MergeBaseMatchesThreeDotDiff(t *testing.T) {
	root := gitRepo(t, map[string]string{"go.mod": "module " + rootModule + "\n"})
	gitRun(t, root, "branch", "-M", "main")
	gitRun(t, root, "checkout", "-q", "-b", "pr")
	writeFile(t, filepath.Join(root, "modn", "go.mod"), "module "+rootModule+"/modn\n")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "pr adds modn")
	gitRun(t, root, "checkout", "-q", "main")
	writeFile(t, filepath.Join(root, "modn", "go.mod"), "module "+rootModule+"/modn\n\ngo 1.26.0\n")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "main adds modn too")
	gitRun(t, root, "checkout", "-q", "pr")

	changed := gitRun(t, root, "diff", "--name-only", "--no-renames", "main...pr")
	if changed != "modn/go.mod" {
		t.Fatalf("three-dot diff = %q, want modn/go.mod", changed)
	}
	mergeBase := gitRun(t, root, "merge-base", "main", "pr")
	got, err := goModPresence(root, mergeBase, []string{changed})
	if err != nil {
		t.Fatalf("goModPresence: %v", err)
	}
	if p := got["modn/go.mod"]; p.AtBase || !p.AtHead {
		t.Fatalf("presence at merge-base = %+v, want added (absent at base, present at head)", p)
	}
	tip, err := goModPresence(root, "main", []string{changed})
	if err != nil {
		t.Fatalf("goModPresence(main): %v", err)
	}
	if p := tip["modn/go.mod"]; !p.AtBase {
		t.Fatalf("presence at the base branch tip = %+v; this test documents why the merge-base is required", p)
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

// A local replace whose target is inside the repository but is not a
// discovered module would hide an edge from the graph: fail closed.
func TestModuleDiscovery_ReplaceToUndiscoveredRepoDirFails(t *testing.T) {
	root := writeModuleTree(t)
	writeFile(t, filepath.Join(root, "notamodule", "x.go"), "package notamodule\n")
	writeFile(t, filepath.Join(root, "modb", "go.mod"), `module `+rootModule+`/modb

go 1.26.0

require example.com/elsewhere v0.0.0

replace example.com/elsewhere => ../notamodule
`)

	if _, err := discoverModules(root); err == nil {
		t.Fatalf("discoverModules() error = nil, want an error for a replace into an undiscovered repository directory")
	}
}

// A local replace pointing outside the repository is not an in-repository
// edge and stays allowed.
func TestModuleDiscovery_ReplaceOutsideRepoIsIgnored(t *testing.T) {
	root := writeModuleTree(t)
	writeFile(t, filepath.Join(root, "modb", "go.mod"), `module `+rootModule+`/modb

go 1.26.0

require example.com/elsewhere v0.0.0

replace example.com/elsewhere => ../../outside
`)

	if _, err := discoverModules(root); err != nil {
		t.Fatalf("discoverModules() error = %v, want a replace outside the repository to be ignored", err)
	}
}

// Go ignores testdata/ directories, and so must discovery: a fixture
// go.mod under testdata/ is never a module of the repository.
func TestModuleDiscovery_SkipsTestdata(t *testing.T) {
	root := writeModuleTree(t)
	writeFile(t, filepath.Join(root, "internal", "tool", "testdata", "fixture", "go.mod"), "this fixture is not parsed\n")

	dirs, err := findSatelliteDirs(root)
	if err != nil {
		t.Fatalf("findSatelliteDirs: %v", err)
	}
	sort.Strings(dirs)
	if want := []string{"moda", "modb", "modc"}; !reflect.DeepEqual(dirs, want) {
		t.Fatalf("findSatelliteDirs() = %v, want %v", dirs, want)
	}
	if _, err := discoverModules(root); err != nil {
		t.Fatalf("discoverModules() error = %v, want the testdata go.mod never parsed", err)
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

// writeUnparsablePackage writes a root-module Go file that `go list -e
// -json` cannot even parse, so it reports a package-level load error
// (Error.Err set) rather than a DepsErrors entry on some other package.
// This is the fixture for the package-graph load-error path: main.go's
// run() must fail closed on it instead of silently building a partial
// graph, in both -changed and -all mode.
func writeUnparsablePackage(t *testing.T, root string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "broken", "broken.go"), "this is not valid go source at all !!! ###\n")
}

// A package-level load error (`go list -e -json`'s Error field) must fail
// the whole selection, exactly like an unreadable nested go.mod: ciselect
// must never silently select from a partial or wrong package graph
// (main.go's run(), the "package graph has load errors" check).
func TestRun_ChangedFailsOnPackageLoadError(t *testing.T) {
	root := writeModuleTree(t)
	writeRootPackage(t, root)
	writeUnparsablePackage(t, root)
	changed := filepath.Join(t.TempDir(), "changed.txt")
	writeFile(t, changed, "moda/x.go\n")

	err := run([]string{"-changed", changed, "-module-dir", root, "-repo-root", root, "-out-dir", t.TempDir()}, io.Discard, io.Discard)
	if err == nil {
		t.Fatalf("run() error = nil, want a package-graph load error to fail the selection")
	}
	if !strings.Contains(err.Error(), "load errors") {
		t.Fatalf("run() error = %v, want it to mention the package graph's load errors", err)
	}
}

// Unlike an unreadable nested go.mod (which -all's directory-only fallback
// tolerates, see TestRun_AllSurvivesBrokenNestedGoMod), a load error in the
// root module's own package graph is not something -all can route around:
// goListPackages/the load-error check run before -all's module-discovery
// fallback even applies, so -all must fail too, never silently select
// less than the true package set.
func TestRun_AllFailsOnPackageLoadError(t *testing.T) {
	root := writeModuleTree(t)
	writeRootPackage(t, root)
	writeUnparsablePackage(t, root)

	err := run([]string{"-all", "-module-dir", root, "-repo-root", root, "-out-dir", t.TempDir()}, io.Discard, io.Discard)
	if err == nil {
		t.Fatalf("run(-all) error = nil, want a package-graph load error to fail even the -all fallback")
	}
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
	if string(modules) != "[\".\",\"moda\",\"modb\",\"modc\"]\n" {
		t.Fatalf("modules.json = %q, want every discovered module, root included", modules)
	}
	// Without a readable go.mod the module path is unknown: plan.json
	// omits the field instead of printing an empty path.
	plan, err := os.ReadFile(filepath.Join(out, "plan.json"))
	if err != nil {
		t.Fatalf("reading plan.json: %v", err)
	}
	if strings.Contains(string(plan), `"path"`) {
		t.Fatalf("plan.json has a path field in the directory-only fallback:\n%s", plan)
	}
}

// A bad -base fails a -changed run (fail closed), while the -all fallback
// never reads it, so the workflow's fallback cannot fail on it.
func TestRun_BaseFailsClosedButAllIgnoresIt(t *testing.T) {
	root := writeModuleTree(t)
	writeRootPackage(t, root)
	changed := filepath.Join(t.TempDir(), "changed.txt")
	writeFile(t, changed, "moda/go.mod\n")

	if err := run([]string{"-changed", changed, "-base", "no-such-rev", "-module-dir", root, "-repo-root", root, "-out-dir", t.TempDir()}, io.Discard, io.Discard); err == nil {
		t.Fatalf("run(-changed, bad -base) error = nil, want failure")
	}
	if err := run([]string{"-all", "-base", "no-such-rev", "-module-dir", root, "-repo-root", root, "-out-dir", t.TempDir()}, io.Discard, io.Discard); err != nil {
		t.Fatalf("run(-all, bad -base) error = %v, want success", err)
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

// modules.json drives the workflows' matrix, and the matrix now runs the
// root module too (ego-arch-006 spec 1, C2): when the root's Plan entry is
// Selected, "." must appear in modules.json exactly like any other
// selected module, root first because Plan lists the root first.
func TestWriteOutputs_ModulesJSONIncludesSelectedRoot(t *testing.T) {
	res := selector.Result{
		Mode:    selector.ModeAffected,
		Reasons: []string{"affected by 1 changed package(s)"},
		Plan: []selector.ModulePlan{
			{Dir: ".", Path: rootModule, Selected: true, Reason: "affected by 1 changed package(s)"},
			{Dir: "moda", Path: rootModule + "/moda", Selected: true, Reason: "changed files in moda", Chain: []string{"moda"}},
			{Dir: "modb", Path: rootModule + "/modb", Reason: "not affected"},
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
	if string(modules) != "[\".\",\"moda\"]\n" {
		t.Fatalf("modules.json = %q, want %q (root included, root first)", modules, "[\".\",\"moda\"]\n")
	}
}

// When the root's Plan entry is not Selected, modules.json keeps its
// nested-only shape, "[]" when nothing at all was selected.
func TestWriteOutputs_ModulesJSONOmitsUnselectedRoot(t *testing.T) {
	res := selector.Result{
		Mode: selector.ModeNone,
		Plan: []selector.ModulePlan{
			{Dir: ".", Path: rootModule, Reason: "not affected"},
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
	if string(modules) != "[]\n" {
		t.Fatalf("modules.json = %q, want %q", modules, "[]\n")
	}
}
