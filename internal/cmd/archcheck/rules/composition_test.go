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

package rules

import (
	"strings"
	"testing"
)

// Tests for the two rules ego-arch-003 adds (design.md §D8):
// composition-no-runtime and composition-leaf.

const compositionSource = "ego-arch-003/design.md §D8"

func TestCompositionRules_SourceCitesArch003(t *testing.T) {
	for _, id := range []string{"composition-no-runtime", "composition-leaf"} {
		rule, ok := ruleByID(DefaultRules(root), id)
		if !ok {
			t.Fatalf("%s rule not found in DefaultRules", id)
		}
		if rule.Source != compositionSource {
			t.Errorf("%s Source = %q, want %q", id, rule.Source, compositionSource)
		}
	}
}

// composition-no-runtime rejects compose and compose/internal/lifecycle
// importing package ego, internal/extensions or GoAkt — the same denylist
// as application-no-runtime.
func TestCompositionNoRuntime_ForbidsRootAndGoAktAndExtensions(t *testing.T) {
	forbidden := []string{root, root + "/internal/extensions", "github.com/tochemey/goakt/v4/actor"}
	graph := Graph{Packages: []Package{
		{ImportPath: root + "/compose", Name: "compose", Kind: RootModule, Imports: forbidden},
		{ImportPath: root + "/compose/internal/lifecycle", Name: "lifecycle", Kind: RootModule, Imports: forbidden},
	}}
	result, err := Evaluate(graph, rulesFor(t, "composition-no-runtime"), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Violations) != 6 {
		t.Fatalf("len(Violations) = %d, want 6 (3 imports x 2 packages): %+v", len(result.Violations), result.Violations)
	}
	for _, v := range result.Violations {
		if v.Rule != "composition-no-runtime" {
			t.Errorf("Rule = %q, want composition-no-runtime: %+v", v.Rule, v)
		}
	}
	report := FormatReport(result, rulesFor(t, "composition-no-runtime"))
	if !strings.Contains(report, "composition") {
		t.Errorf("report does not name the composition layer:\n%s", report)
	}
}

// compose may import contract packages; compose/goakt, the GoAkt
// composition root, is outside composition-no-runtime's layer and may
// import the runtime.
func TestCompositionNoRuntime_AllowsContractsAndLeavesGoAktRootAlone(t *testing.T) {
	graph := Graph{Packages: []Package{
		{ImportPath: root + "/compose", Name: "compose", Kind: RootModule, Imports: []string{root + "/persistence", root + "/port/publishing", "reflect"}},
		{ImportPath: root + "/compose/goakt", Name: "goakt", Kind: RootModule, Imports: []string{root, root + "/compose", "github.com/tochemey/goakt/v4/actor"}},
	}}
	result, err := Evaluate(graph, rulesFor(t, "composition-no-runtime"), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Violations) != 0 {
		t.Fatalf("len(Violations) = %d, want 0: %+v", len(result.Violations), result.Violations)
	}
	stat, _ := ruleStat(result.RuleStats, "composition-no-runtime")
	if stat.PackagesMatched != 1 {
		t.Errorf("composition-no-runtime matched %d packages, want 1 (compose only, not compose/goakt)", stat.PackagesMatched)
	}
}

// application-no-runtime is not widened: its layer still matches only
// migration, never the composition packages.
func TestApplicationNoRuntime_StillMatchesOnlyMigration(t *testing.T) {
	rule, ok := ruleByID(DefaultRules(root), "application-no-runtime")
	if !ok {
		t.Fatal("application-no-runtime rule not found")
	}
	cases := map[string]bool{
		root + "/migration":                  true,
		root + "/migration/sub":              true,
		root + "/compose":                    false,
		root + "/compose/internal/lifecycle": false,
		root + "/compose/goakt":              false,
		root:                                 false,
		root + "/tenancy":                    false,
	}
	for path, want := range cases {
		if got := rule.Layer.Match(Package{ImportPath: path, Kind: RootModule}); got != want {
			t.Errorf("application-no-runtime Layer.Match(%s) = %v, want %v", path, got, want)
		}
	}
}

// composition-leaf: a root-module production package outside compose/
// that is not main must not import compose or anything under it.
func TestCompositionLeaf_RejectsNonMainImporterOutsideCompose(t *testing.T) {
	graph := Graph{Packages: []Package{
		{ImportPath: root, Name: "ego", Kind: RootModule, Imports: []string{root + "/compose"}},
		{ImportPath: root + "/testkit", Name: "testkit", Kind: RootModule, Imports: []string{root + "/compose/goakt"}},
		{ImportPath: root + "/internal/runner", Name: "runner", Kind: RootModule, Imports: []string{root + "/compose/internal/lifecycle"}},
	}}
	result, err := Evaluate(graph, rulesFor(t, "composition-leaf"), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Violations) != 3 {
		t.Fatalf("len(Violations) = %d, want 3: %+v", len(result.Violations), result.Violations)
	}
	for _, v := range result.Violations {
		if v.Rule != "composition-leaf" {
			t.Errorf("Rule = %q, want composition-leaf", v.Rule)
		}
		if !strings.Contains(v.Reason, "compose") {
			t.Errorf("Reason = %q, want it to name the composition root", v.Reason)
		}
	}
}

// A package whose name the loader could not determine is treated as not
// main, so the rule fails closed.
func TestCompositionLeaf_UnknownNameIsNotMain(t *testing.T) {
	graph := Graph{Packages: []Package{
		{ImportPath: root + "/cmd/tool", Kind: RootModule, Imports: []string{root + "/compose"}},
	}}
	result, err := Evaluate(graph, rulesFor(t, "composition-leaf"), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Violations) != 1 {
		t.Fatalf("len(Violations) = %d, want 1: %+v", len(result.Violations), result.Violations)
	}
}

// Packages under compose/, main packages, examples and the benchmark
// module may import compose. (Test files never reach the graph: the
// loaders keep production imports only; see e2e_test.go.)
func TestCompositionLeaf_AllowsComposeMainExamplesAndBenchmark(t *testing.T) {
	graph := Graph{Packages: []Package{
		// Keeps the layer non-empty and must itself pass.
		{ImportPath: root + "/tenancy", Name: "tenancy", Kind: RootModule, Imports: []string{"context"}},
		{ImportPath: root + "/compose/goakt", Name: "goakt", Kind: RootModule, Imports: []string{root + "/compose", root + "/compose/internal/lifecycle"}},
		{ImportPath: root + "/compose/internal/lifecycle", Name: "lifecycle", Kind: RootModule, Imports: []string{root + "/compose"}},
		{ImportPath: root + "/internal/cmd/tool", Name: "main", Kind: RootModule, Imports: []string{root + "/compose/goakt"}},
		{ImportPath: root + "/example/eventssourced", Name: "main", Kind: RootModule, Imports: []string{root + "/compose", root + "/compose/goakt"}},
		{ImportPath: root + "/example/examplepb", Name: "samplepb", Kind: RootModule, Imports: []string{root + "/compose"}},
		{ImportPath: root + "/benchmark", Name: "benchmark", Kind: NestedModule, Imports: []string{root + "/compose/goakt"}},
		{ImportPath: root + "/example/cluster", Name: "main", Kind: NestedModule, Imports: []string{root + "/compose/goakt"}},
	}}
	result, err := Evaluate(graph, rulesFor(t, "composition-leaf"), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Violations) != 0 {
		t.Fatalf("len(Violations) = %d, want 0: %+v", len(result.Violations), result.Violations)
	}
	stat, _ := ruleStat(result.RuleStats, "composition-leaf")
	if stat.PackagesMatched != 1 {
		t.Errorf("composition-leaf matched %d packages, want 1 (tenancy only)", stat.PackagesMatched)
	}
}

// A path that merely starts with "compose" is not the composition root.
func TestCompositionRules_MatchWholePathSegments(t *testing.T) {
	graph := Graph{Packages: []Package{
		{ImportPath: root + "/tenancy", Name: "tenancy", Kind: RootModule, Imports: []string{root + "/composer"}},
		{ImportPath: root + "/composer", Name: "composer", Kind: RootModule, Imports: []string{root}},
		{ImportPath: root + "/compose", Name: "compose", Kind: RootModule, Imports: []string{"context"}},
	}}
	result, err := Evaluate(graph, rulesFor(t, "composition-leaf", "composition-no-runtime"), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Violations) != 0 {
		t.Fatalf("len(Violations) = %d, want 0 (composer is not compose): %+v", len(result.Violations), result.Violations)
	}
}
