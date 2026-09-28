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

// Tests for external-adapter-no-composition (ego-arch-004 design §D7,
// maintainer decision O1): a nested adapter module must not import the
// composition root, compose or anything under it.

const adapterNoComposition = "external-adapter-no-composition"

func TestExternalAdapterNoComposition_IsDefinedOnTheAdapterLayer(t *testing.T) {
	rule, ok := ruleByID(DefaultRules(root), adapterNoComposition)
	if !ok {
		t.Fatalf("%s rule not found in DefaultRules", adapterNoComposition)
	}
	if rule.Source != "ego-arch-004/design.md §D7" {
		t.Errorf("Source = %q, want %q", rule.Source, "ego-arch-004/design.md §D7")
	}
	if rule.Semantics != Denylist {
		t.Errorf("Semantics = %v, want denylist", rule.Semantics)
	}
	if !strings.Contains(rule.Description, "compose") {
		t.Errorf("Description = %q, want it to name compose", rule.Description)
	}
	if rule.Layer.Name != ExternalAdapterLayer(root).Name {
		t.Errorf("Layer = %q, want the external adapter layer %q", rule.Layer.Name, ExternalAdapterLayer(root).Name)
	}
}

// Spec scenario "the exploration spike becomes a failing graph":
// publisher/kafka importing compose/goakt is reported under the new rule,
// and its reason names the import and what adapters may import instead.
func TestExternalAdapterNoComposition_ExplorationSpikeFails(t *testing.T) {
	graph := Graph{Packages: []Package{
		{ImportPath: repoRoot + "/publisher/kafka", Name: "kafka", Kind: NestedModule, Imports: []string{root + "/port/publishing", root + "/compose/goakt"}},
	}}
	result, err := Evaluate(graph, rulesFor(t, adapterNoComposition), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Violations) != 1 {
		t.Fatalf("len(Violations) = %d, want 1: %+v", len(result.Violations), result.Violations)
	}
	v := result.Violations[0]
	if v.Rule != adapterNoComposition || v.Importer != repoRoot+"/publisher/kafka" || v.Import != root+"/compose/goakt" {
		t.Errorf("violation = %+v, want %s for publisher/kafka -> compose/goakt", v, adapterNoComposition)
	}
	for _, want := range []string{root + "/compose/goakt", "contract", "egopb"} {
		if !strings.Contains(v.Reason, want) {
			t.Errorf("Reason = %q, want it to mention %q", v.Reason, want)
		}
	}
}

// compose, compose/goakt and compose/internal/lifecycle are all forbidden,
// including from a main package inside the adapter module: there is no
// main or example exemption. compose/internal/lifecycle also breaks
// no-cross-module-internal; both rules report that edge.
func TestExternalAdapterNoComposition_ForbidsEveryCompositionPackage(t *testing.T) {
	composition := []string{root + "/compose", root + "/compose/goakt", root + "/compose/internal/lifecycle"}
	graph := withRepoModules(Graph{Packages: []Package{
		{ImportPath: repoRoot + "/publisher/kafka", Name: "kafka", Kind: NestedModule, Imports: composition},
		{ImportPath: repoRoot + "/publisher/kafka/cmd/demo", Name: "main", Kind: NestedModule, Imports: []string{root + "/compose"}},
		{ImportPath: repoRoot + "/publisher/kafka/example", Name: "example", Kind: NestedModule, Imports: []string{root + "/compose/goakt"}},
	}})
	result, err := Evaluate(graph, rulesFor(t, adapterNoComposition, "no-cross-module-internal"), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}

	got := map[string][]string{}
	for _, v := range result.Violations {
		got[v.Rule] = append(got[v.Rule], v.Importer+" -> "+v.Import)
	}
	if n := len(got[adapterNoComposition]); n != 5 {
		t.Errorf("%s reported %d edges, want 5 (3 from kafka, 1 from its main, 1 from its example): %v", adapterNoComposition, n, got[adapterNoComposition])
	}
	internal := got["no-cross-module-internal"]
	if len(internal) != 1 || internal[0] != repoRoot+"/publisher/kafka -> "+root+"/compose/internal/lifecycle" {
		t.Errorf("no-cross-module-internal reported %v, want only publisher/kafka -> compose/internal/lifecycle", internal)
	}
	stat, _ := ruleStat(result.RuleStats, adapterNoComposition)
	if stat.PackagesMatched != 3 {
		t.Errorf("%s matched %d packages, want 3 (every package of the adapter module)", adapterNoComposition, stat.PackagesMatched)
	}
}

// Spec scenario "legitimate importers stay allowed": a root-module main
// package, packages under compose/, the benchmark module and
// example/cluster may import compose; none of them is in the adapter
// layer. An adapter importing contracts, egopb or a path that only starts
// with "compose" passes.
func TestExternalAdapterNoComposition_AllowsLegitimateImporters(t *testing.T) {
	graph := Graph{Packages: []Package{
		{ImportPath: repoRoot + "/publisher/kafka", Name: "kafka", Kind: NestedModule, Imports: []string{root + "/port/publishing", root + "/egopb", root + "/composer", "github.com/segmentio/kafka-go"}},
		{ImportPath: root + "/internal/cmd/tool", Name: "main", Kind: RootModule, Imports: []string{root + "/compose"}},
		{ImportPath: root + "/example/eventssourced", Name: "main", Kind: RootModule, Imports: []string{root + "/compose", root + "/compose/goakt"}},
		{ImportPath: root + "/compose/goakt", Name: "goakt", Kind: RootModule, Imports: []string{root + "/compose"}},
		{ImportPath: root + "/benchmark", Name: "benchmark", Kind: NestedModule, Imports: []string{root + "/compose/goakt"}},
		{ImportPath: root + "/example/cluster", Name: "main", Kind: NestedModule, Imports: []string{root + "/compose/goakt"}},
	}}
	result, err := Evaluate(graph, rulesFor(t, adapterNoComposition), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Violations) != 0 {
		t.Fatalf("len(Violations) = %d, want 0: %+v", len(result.Violations), result.Violations)
	}
	stat, _ := ruleStat(result.RuleStats, adapterNoComposition)
	if stat.PackagesMatched != 1 {
		t.Errorf("%s matched %d packages, want 1 (publisher/kafka only)", adapterNoComposition, stat.PackagesMatched)
	}
}

// The full rule table on a graph with a root main importing compose and a
// clean adapter: no rule fires, so the new rule does not change what the
// existing rules allow.
func TestExternalAdapterNoComposition_RootMainIsAllowedByEveryRule(t *testing.T) {
	graph := allowedGraph()
	graph.Packages = append(graph.Packages, Package{
		ImportPath: root + "/cmd/app", Name: "main", Kind: RootModule, Imports: []string{root + "/compose"},
	})
	result, err := Evaluate(graph, DefaultRules(root), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Violations) != 0 {
		t.Fatalf("len(Violations) = %d, want 0: %+v", len(result.Violations), result.Violations)
	}
}
