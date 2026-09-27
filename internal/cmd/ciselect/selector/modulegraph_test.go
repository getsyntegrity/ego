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

package selector

import (
	"strings"
	"testing"
)

// The fixtures in this file are the fixture repository of the
// ego-arch-006 design (§5.5): an in-memory module graph, no filesystem.
//
//	Dir        Path                     Deps (local replace)      Pinned                     Imports (parser)
//	.          example.com/r            example.com/r/port        -                          example.com/r/port, example.com/r/port/sub
//	port       example.com/r/port       -                         -                          -
//	adapter/a  example.com/r/adapter/a  example.com/r/port        -                          example.com/r/port
//	adapter/b  example.com/r/adapter/b  example.com/r             -                          example.com/r
//	it         example.com/r/it         example.com/r/adapter/a,  -                          example.com/r/adapter/a
//	                                    example.com/r/port
//	tools      example.com/r/tools      -                         -                          -
//	pinned     example.com/r/pinned     -                         example.com/r/port@v0.3.0  example.com/r/port
//	adapter/c  example.com/r/adapter/c  example.com/r/port        -                          example.com/r/port (only in the "new module" case)
//
// The root module's own package graph (graphRoot) has a root package that
// imports svc, svc importing the port module, lib importing port only
// from its external tests, a util package nothing imports and that
// imports nothing, and an unrelated package (other), so a root lane
// seeded through port is a proper "affected" subset.

const fx = "example.com/r"

func fixtureModules() []ModuleInfo {
	return []ModuleInfo{
		{Dir: ".", Path: fx, Deps: []string{fx + "/port"}, Imports: []string{fx + "/port", fx + "/port/sub"}},
		{Dir: "port", Path: fx + "/port"},
		{Dir: "adapter/a", Path: fx + "/adapter/a", Deps: []string{fx + "/port"}, Imports: []string{fx + "/port"}},
		{Dir: "adapter/b", Path: fx + "/adapter/b", Deps: []string{fx}, Imports: []string{fx}},
		{Dir: "it", Path: fx + "/it", Deps: []string{fx + "/adapter/a", fx + "/port"}, Imports: []string{fx + "/adapter/a"}},
		{Dir: "tools", Path: fx + "/tools"},
		{Dir: "pinned", Path: fx + "/pinned", Pinned: []string{fx + "/port@v0.3.0"}, Imports: []string{fx + "/port"}},
	}
}

func fixtureModulesWithC() []ModuleInfo {
	return append(fixtureModules(), ModuleInfo{Dir: "adapter/c", Path: fx + "/adapter/c", Deps: []string{fx + "/port"}, Imports: []string{fx + "/port"}})
}

func graphRoot() Graph {
	return Graph{
		ModulePath: fx,
		ModuleDir:  "/repo",
		Packages: []Package{
			{ImportPath: fx, Dir: "/repo", Imports: []string{fx + "/svc"}},
			{ImportPath: fx + "/svc", Dir: "/repo/svc", Imports: []string{fx + "/port"}},
			{ImportPath: fx + "/lib", Dir: "/repo/lib", XTestImports: []string{fx + "/port/sub"}},
			{ImportPath: fx + "/util", Dir: "/repo/util"},
			{ImportPath: fx + "/other", Dir: "/repo/other"},
		},
	}
}

func selectFixture(changed []string, modules []ModuleInfo) Result {
	return Select(graphRoot(), changed, Options{Modules: modules})
}

// selectWithBase runs Select as with -base: goMods records, for each
// changed nested go.mod, whether it exists at base and at head.
func selectWithBase(changed []string, modules []ModuleInfo, goMods map[string]GoModPresence) Result {
	return Select(graphRoot(), changed, Options{Modules: modules, GoMods: goMods})
}

func planFor(t *testing.T, res Result, dir string) ModulePlan {
	t.Helper()
	for _, p := range res.Plan {
		if p.Dir == dir {
			return p
		}
	}
	t.Fatalf("plan has no entry for module %q: %+v", dir, res.Plan)
	return ModulePlan{}
}

func chainOf(p ModulePlan) string {
	return strings.Join(p.Chain, " ← ")
}

// #102 case "leaf": a change inside a module nothing requires selects only
// that module, and the root lane stays empty.
func TestModuleGraph_Leaf(t *testing.T) {
	res := selectFixture([]string{"tools/main.go"}, fixtureModules())

	assertSameSet(t, moduleDirs(res.Modules), []string{"tools"})
	if res.Mode != ModeNone {
		t.Fatalf("root Mode = %s, want %s (reasons=%v)", res.Mode, ModeNone, res.Reasons)
	}
	if p := planFor(t, res, "tools"); !p.Selected || !strings.Contains(p.Reason, "changed files") {
		t.Fatalf("tools plan = %+v, want selected for changed files", p)
	}
}

// #102 case "shared contract": a change to a module others require selects
// it plus every module that transitively requires it, seeds the root lane
// from the root packages that import it, and reports (without selecting)
// a module pinned to a published version of it.
func TestModuleGraph_SharedContract(t *testing.T) {
	res := selectFixture([]string{"port/p.go"}, fixtureModules())

	assertSameSet(t, moduleDirs(res.Modules), []string{"port", "adapter/a", "adapter/b", "it"})
	if res.Mode != ModeAffected {
		t.Fatalf("root Mode = %s, want %s (reasons=%v)", res.Mode, ModeAffected, res.Reasons)
	}
	assertSameSet(t, res.Selected, []string{fx, fx + "/svc", fx + "/lib"})

	if p := planFor(t, res, "."); !p.Selected || chainOf(p) != ". ← port" {
		t.Fatalf("root plan = %+v, want selected with chain \". ← port\"", p)
	}
	if p := planFor(t, res, "adapter/b"); chainOf(p) != "adapter/b ← . ← port" {
		t.Fatalf("adapter/b chain = %q, want %q", chainOf(p), "adapter/b ← . ← port")
	}
	if p := planFor(t, res, "it"); chainOf(p) != "it ← adapter/a ← port" {
		t.Fatalf("it chain = %q, want %q", chainOf(p), "it ← adapter/a ← port")
	}
	p := planFor(t, res, "pinned")
	if p.Selected || !strings.Contains(p.Reason, "pinned to") || !strings.Contains(p.Reason, "v0.3.0") {
		t.Fatalf("pinned plan = %+v, want not selected, reason naming the pinned v0.3.0", p)
	}
	if p := planFor(t, res, "tools"); p.Selected {
		t.Fatalf("tools plan = %+v, want not selected", p)
	}
}

// #102 case "transitive consumer": a module that requires a changed module
// through another module is selected, with the chain that reached it; the
// root, which does not require adapter/a, is untouched.
func TestModuleGraph_TransitiveConsumer(t *testing.T) {
	res := selectFixture([]string{"adapter/a/a.go"}, fixtureModules())

	assertSameSet(t, moduleDirs(res.Modules), []string{"adapter/a", "it"})
	if res.Mode != ModeNone {
		t.Fatalf("root Mode = %s, want %s (reasons=%v)", res.Mode, ModeNone, res.Reasons)
	}
	if p := planFor(t, res, "it"); chainOf(p) != "it ← adapter/a" {
		t.Fatalf("it chain = %q, want %q", chainOf(p), "it ← adapter/a")
	}
}

// #102 case "new module": a new nested go.mod carves a directory out of its
// parent (here the root), so the parent is fully changed.
func TestModuleGraph_NewModule(t *testing.T) {
	res := selectWithBase([]string{"adapter/c/go.mod", "adapter/c/c.go"}, fixtureModulesWithC(),
		map[string]GoModPresence{"adapter/c/go.mod": {AtHead: true}})

	assertSameSet(t, moduleDirs(res.Modules), []string{"adapter/c", "adapter/b"})
	assertFull(t, graphRoot(), res)
	if !containsSubstring(res.Reasons, "module boundary changed: adapter/c/go.mod") {
		t.Fatalf("root reasons = %v, want one naming the module boundary change", res.Reasons)
	}
	if p := planFor(t, res, "adapter/c"); !strings.Contains(p.Reason, "changed files") {
		t.Fatalf("adapter/c plan = %+v, want reason naming its changed files", p)
	}
	if p := planFor(t, res, "."); !strings.Contains(p.Reason, "module boundary changed: adapter/c/go.mod") {
		t.Fatalf("root plan = %+v, want the module boundary reason", p)
	}
	if p := planFor(t, res, "adapter/b"); chainOf(p) != "adapter/b ← ." {
		t.Fatalf("adapter/b chain = %q, want %q", chainOf(p), "adapter/b ← .")
	}
}

// #102 case "global change": go.work, and every other global path of the
// design (§5.3), selects every module and forces the root lane to full.
func TestModuleGraph_GlobalChange(t *testing.T) {
	for _, path := range []string{
		"go.work",
		"go.work.sum",
		".golangci.yml",
		"Makefile",
		"Dockerfile.ci",
		"buf.yaml",
		"buf.gen.yaml",
		".github/workflows/x.yml",
		"scripts/ci/go-test.sh",
		"internal/cmd/ciselect/main.go",
		"protos/ego/v4/ego.proto",
	} {
		t.Run(path, func(t *testing.T) {
			res := selectFixture([]string{path}, fixtureModulesWithC())

			if !res.Global {
				t.Fatalf("Global = false, want true for %s", path)
			}
			assertSameSet(t, moduleDirs(res.Modules), []string{"port", "adapter/a", "adapter/b", "adapter/c", "it", "tools", "pinned"})
			assertFull(t, graphRoot(), res)
			if len(res.Plan) != 8 {
				t.Fatalf("plan has %d modules, want all 8", len(res.Plan))
			}
			want := "global: " + path + " changed"
			for _, p := range res.Plan {
				if !p.Selected || p.Reason != want {
					t.Fatalf("plan entry %+v, want selected with reason %q", p, want)
				}
			}
		})
	}
}

func TestModuleGraph_DocsOnlySelectsNothing(t *testing.T) {
	res := selectFixture([]string{"docs/x.md"}, fixtureModules())

	if len(res.Modules) != 0 || res.Mode != ModeNone || res.Global {
		t.Fatalf("modules=%v mode=%s global=%v, want nothing selected", res.Modules, res.Mode, res.Global)
	}
}

// A file under a directory the discovery walk skips (odd/) belongs to the
// root and goes through the root classifier: documentation, nothing runs.
func TestModuleGraph_SkippedDirDocsSelectsNothing(t *testing.T) {
	res := selectFixture([]string{"odd/tasks/x.md"}, fixtureModules())

	if len(res.Modules) != 0 || res.Mode != ModeNone {
		t.Fatalf("modules=%v mode=%s, want nothing selected", res.Modules, res.Mode)
	}
	if res.Changed[0].Class != ClassNoTest {
		t.Fatalf("odd/tasks/x.md classified %s, want %s", res.Changed[0].Class, ClassNoTest)
	}
}

// A nested go.mod edit is a boundary change for its parent (the root), and
// the root requires port, so its lane runs full; the closure is port's.
func TestModuleGraph_NestedGoModEditSelectsClosureAndRootFull(t *testing.T) {
	res := selectFixture([]string{"port/go.mod"}, fixtureModules())

	assertSameSet(t, moduleDirs(res.Modules), []string{"port", "adapter/a", "adapter/b", "it"})
	assertFull(t, graphRoot(), res)
}

// A root package change reaches only the modules that require the root:
// it requires adapter/a, not the root, so the walk follows edges, not
// proximity.
func TestModuleGraph_RootPackageChangeFollowsEdges(t *testing.T) {
	res := selectFixture([]string{"engine.go"}, fixtureModules())

	assertSameSet(t, moduleDirs(res.Modules), []string{"adapter/b"})
	assertFull(t, graphRoot(), res)
	if p := planFor(t, res, "."); !p.Selected {
		t.Fatalf("root plan = %+v, want selected", p)
	}
	if p := planFor(t, res, "it"); p.Selected {
		t.Fatalf("it plan = %+v, want not selected", p)
	}
}

// The root go.mod is deliberately not global (§5.3): it fully changes the
// root, and the closure selects only the modules that require the root.
func TestModuleGraph_RootGoModIsNotGlobal(t *testing.T) {
	res := selectFixture([]string{"go.mod"}, fixtureModules())

	if res.Global {
		t.Fatalf("Global = true, want false for the root go.mod")
	}
	assertFull(t, graphRoot(), res)
	assertSameSet(t, moduleDirs(res.Modules), []string{"adapter/b"})
}

// A root affected lane that includes a package a consumer imports selects
// that consumer through the filtered edge.
func TestModuleGraph_RootAffectedLaneSelectsImportingConsumer(t *testing.T) {
	res := selectFixture([]string{"svc/s.go"}, fixtureModules())

	if res.Mode != ModeAffected {
		t.Fatalf("root Mode = %s, want %s (reasons=%v)", res.Mode, ModeAffected, res.Reasons)
	}
	assertSameSet(t, res.Selected, []string{fx, fx + "/svc"})
	assertSameSet(t, moduleDirs(res.Modules), []string{"adapter/b"})
}

// A dependency module's go.sum change sends the root lane to full when the
// root requires that module (§5.2 step 6).
func TestModuleGraph_DependencyGoSumSendsRootFull(t *testing.T) {
	res := selectFixture([]string{"port/go.sum"}, fixtureModules())

	assertFull(t, graphRoot(), res)
	assertSameSet(t, moduleDirs(res.Modules), []string{"port", "adapter/a", "adapter/b", "it"})
}

// A go.mod added inside a nested module carves a directory out of that
// module, not out of the root: the nested parent is changed, the root is
// untouched.
func TestModuleGraph_BoundaryInsideNestedModuleChangesThatModule(t *testing.T) {
	res := selectFixture([]string{"adapter/a/sub/go.mod"}, fixtureModules())

	if res.Mode != ModeNone {
		t.Fatalf("root Mode = %s, want %s (reasons=%v)", res.Mode, ModeNone, res.Reasons)
	}
	assertSameSet(t, moduleDirs(res.Modules), []string{"adapter/a", "it"})
	if p := planFor(t, res, "adapter/a"); !strings.Contains(p.Reason, "module boundary changed: adapter/a/sub/go.mod") {
		t.Fatalf("adapter/a plan = %+v, want the module boundary reason", p)
	}
}

// The root's parser-read imports (build tags ignored) name tools, so the
// root is reached; but no root package in the go list graph imports it (a
// build-tagged importer). The root lane must not silently test nothing:
// full fallback.
func TestModuleGraph_RootImporterOnlyBehindBuildTagFallsBackToFull(t *testing.T) {
	mods := fixtureModules()
	mods[0].Deps = append(mods[0].Deps, fx+"/tools")
	mods[0].Imports = append(mods[0].Imports, fx+"/tools")
	res := selectFixture([]string{"tools/main.go"}, mods)

	assertFull(t, graphRoot(), res)
	assertSameSet(t, moduleDirs(res.Modules), []string{"tools", "adapter/b"})
}

// Import filter: the root requires tools but imports none of its packages,
// and tools' manifest is unchanged, so a tools change does not reach the
// root (§5.1 "When the import filter is sound").
func TestModuleGraph_RootRequiringButNotImportingIsNotReached(t *testing.T) {
	mods := fixtureModules()
	mods[0].Deps = append(mods[0].Deps, fx+"/tools")
	res := selectFixture([]string{"tools/main.go"}, mods)

	if res.Mode != ModeNone {
		t.Fatalf("root Mode = %s, want %s (reasons=%v)", res.Mode, ModeNone, res.Reasons)
	}
	assertSameSet(t, moduleDirs(res.Modules), []string{"tools"})
	if p := planFor(t, res, "."); p.Selected || !strings.Contains(p.Reason, "imports none of its affected packages") {
		t.Fatalf("root plan = %+v, want not selected by the import filter", p)
	}
}

// ...but when tools' go.mod changed, the unfiltered requirement edge
// applies, and a changed manifest of a required module sends the root lane
// to full.
func TestModuleGraph_RequiredManifestChangeIsUnfiltered(t *testing.T) {
	mods := fixtureModules()
	mods[0].Deps = append(mods[0].Deps, fx+"/tools")
	res := selectWithBase([]string{"tools/go.mod"}, mods, map[string]GoModPresence{"tools/go.mod": {AtBase: true, AtHead: true}})

	assertFull(t, graphRoot(), res)
	assertSameSet(t, moduleDirs(res.Modules), []string{"tools", "adapter/b"})
}

// Import filter (§5.5): a change to root package util makes the root lane
// affected with only util; adapter/b requires the root but imports only the
// root package, so it is not selected.
func TestModuleGraph_ImportFilterSkipsConsumerNotImportingAffectedPackage(t *testing.T) {
	res := selectFixture([]string{"util/u.go"}, fixtureModules())

	if res.Mode != ModeAffected {
		t.Fatalf("root Mode = %s, want %s (reasons=%v)", res.Mode, ModeAffected, res.Reasons)
	}
	assertSameSet(t, res.Selected, []string{fx + "/util"})
	if len(res.Modules) != 0 {
		t.Fatalf("modules = %v, want none: adapter/b does not import util", res.Modules)
	}
	if p := planFor(t, res, "adapter/b"); p.Selected || !strings.Contains(p.Reason, "imports none of its affected packages") {
		t.Fatalf("adapter/b plan = %+v, want not selected by the import filter", p)
	}
}

// Manifest edit versus add versus delete, all with -base (§5.5).

// Edit: adapter/a/go.mod exists at base and head. No boundary effect: the
// root lane stays none; adapter/a's manifest changed, so it reaches it
// through the unfiltered edge.
func TestModuleGraph_WithBase_GoModEditHasNoBoundaryEffect(t *testing.T) {
	res := selectWithBase([]string{"adapter/a/go.mod"}, fixtureModules(),
		map[string]GoModPresence{"adapter/a/go.mod": {AtBase: true, AtHead: true}})

	if res.Mode != ModeNone {
		t.Fatalf("root Mode = %s, want %s (reasons=%v)", res.Mode, ModeNone, res.Reasons)
	}
	assertSameSet(t, moduleDirs(res.Modules), []string{"adapter/a", "it"})
}

// Add: the "new module" case, with adapter/c/go.mod absent at base.
func TestModuleGraph_WithBase_GoModAddIsBoundary(t *testing.T) {
	res := selectWithBase([]string{"adapter/c/go.mod", "adapter/c/c.go"}, fixtureModulesWithC(),
		map[string]GoModPresence{"adapter/c/go.mod": {AtHead: true}})

	assertSameSet(t, moduleDirs(res.Modules), []string{"adapter/c", "adapter/b"})
	assertFull(t, graphRoot(), res)
	if !containsSubstring(res.Reasons, "module boundary changed: adapter/c/go.mod") {
		t.Fatalf("root reasons = %v, want the module boundary reason", res.Reasons)
	}
}

// Delete: adapter/b/go.mod exists at base only, so adapter/b is no longer a
// discovered module and its files belong to the root: root lane full, and
// every remaining module requiring the root (none here) is selected.
func TestModuleGraph_WithBase_GoModDeleteIsBoundary(t *testing.T) {
	var mods []ModuleInfo
	for _, m := range fixtureModules() {
		if m.Dir != "adapter/b" {
			mods = append(mods, m)
		}
	}
	res := selectWithBase([]string{"adapter/b/go.mod"}, mods,
		map[string]GoModPresence{"adapter/b/go.mod": {AtBase: true}})

	assertFull(t, graphRoot(), res)
	if !containsSubstring(res.Reasons, "module boundary changed: adapter/b/go.mod") {
		t.Fatalf("root reasons = %v, want the module boundary reason", res.Reasons)
	}
	if len(res.Modules) != 0 {
		t.Fatalf("modules = %v, want none (no remaining module requires the root)", res.Modules)
	}
}

// Without -base, the edit case is treated as a boundary change: root lane
// full, the conservative fallback.
func TestModuleGraph_WithoutBase_GoModEditIsBoundary(t *testing.T) {
	res := selectFixture([]string{"adapter/a/go.mod"}, fixtureModules())

	assertFull(t, graphRoot(), res)
	assertSameSet(t, moduleDirs(res.Modules), []string{"adapter/a", "adapter/b", "it"})
}

// With -base, a changed go.mod that is absent from the presence map is
// treated conservatively as a boundary change.
func TestModuleGraph_WithBase_UnknownPresenceIsBoundary(t *testing.T) {
	res := selectWithBase([]string{"adapter/a/go.mod"}, fixtureModules(), map[string]GoModPresence{})

	assertFull(t, graphRoot(), res)
}

// A port/go.mod edit selects port plus its whole closure unfiltered, and
// the root lane is full because the root requires port and port's manifest
// changed (§5.2 step 6), even with no boundary effect.
func TestModuleGraph_WithBase_RequiredModuleGoModEditSendsRootFull(t *testing.T) {
	res := selectWithBase([]string{"port/go.mod"}, fixtureModules(),
		map[string]GoModPresence{"port/go.mod": {AtBase: true, AtHead: true}})

	assertFull(t, graphRoot(), res)
	assertSameSet(t, moduleDirs(res.Modules), []string{"port", "adapter/a", "adapter/b", "it"})
	if containsSubstring(res.Reasons, "module boundary changed") {
		t.Fatalf("root reasons = %v, want no boundary reason for an edit", res.Reasons)
	}
}

func TestModuleGraph_EmptyChangedListIsGlobal(t *testing.T) {
	res := selectFixture(nil, fixtureModules())

	if !res.Global {
		t.Fatalf("Global = false, want true for an empty changed list")
	}
	assertFull(t, graphRoot(), res)
	if len(res.Modules) != 6 {
		t.Fatalf("modules = %v, want all 6 nested modules", res.Modules)
	}
}

func TestModuleGraph_AllIsGlobal(t *testing.T) {
	res := Select(graphRoot(), nil, Options{All: true, Modules: fixtureModules()})

	if !res.Global || len(res.Modules) != 6 {
		t.Fatalf("global=%v modules=%v, want every nested module", res.Global, res.Modules)
	}
	for _, p := range res.Plan {
		if p.Reason != "global: -all requested" {
			t.Fatalf("plan entry %+v, want reason %q", p, "global: -all requested")
		}
	}
}

// The plan lists every discovered module, selected or not, each with a
// reason, root first then by directory, whatever the input order.
func TestModuleGraph_PlanIsCompleteAndDeterministic(t *testing.T) {
	mods := fixtureModules()
	reversed := make([]ModuleInfo, len(mods))
	for i := range mods {
		reversed[len(mods)-1-i] = mods[i]
	}
	a := selectFixture([]string{"port/p.go", "adapter/a/a.go"}, mods)
	b := selectFixture([]string{"adapter/a/a.go", "port/p.go"}, reversed)

	if len(a.Plan) != len(mods) {
		t.Fatalf("plan has %d entries, want %d", len(a.Plan), len(mods))
	}
	if a.Plan[0].Dir != "." {
		t.Fatalf("plan[0] = %q, want the root first", a.Plan[0].Dir)
	}
	for i := range a.Plan {
		pa, pb := a.Plan[i], b.Plan[i]
		if pa.Dir != pb.Dir || pa.Selected != pb.Selected || pa.Reason != pb.Reason || chainOf(pa) != chainOf(pb) {
			t.Fatalf("plan differs with input order at %d: %+v vs %+v", i, pa, pb)
		}
		if pa.Reason == "" {
			t.Fatalf("plan entry %+v has no reason", pa)
		}
	}
}

func TestModuleGraph_SummaryHasWhyTable(t *testing.T) {
	res := selectFixture([]string{"port/p.go"}, fixtureModules())
	summary := BuildSummary(res)

	for _, want := range []string{
		"| module | selected | why |",
		"| `adapter/b` | yes | adapter/b ← . ← port |",
		"| `tools` | no |",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary lacks %q:\n%s", want, summary)
		}
	}
}

func containsSubstring(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
