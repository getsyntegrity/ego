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

// Module paths for the fixtures below, shaped like this repository's
// modules after ego-arch-006 slice S1 (openspec/changes/ego-arch-006/design.md
// §2.1).
const (
	kafkaMod  = root + "/publisher/kafka"
	natsMod   = root + "/publisher/nats"
	compatMod = root + "/test/compat"
)

// s1Modules is the in-repository module graph after slice S1: the
// publishers require the root, test/compat requires the root and the
// publishers, and nothing requires test/compat. It has no cycle.
func s1Modules() []Module {
	return []Module{
		{Path: root},
		{Path: kafkaMod, Requires: []string{root}},
		{Path: natsMod, Requires: []string{root}},
		{Path: compatMod, Requires: []string{root, kafkaMod, natsMod}},
	}
}

// s1Packages is one production package per s1Modules module, each with a
// harmless import, so every package-level rule under test matches
// something.
func s1Packages() []Package {
	return []Package{
		{ImportPath: root, Name: "ego", Kind: RootModule, Imports: []string{root + "/internal/queue"}},
		{ImportPath: root + "/internal/queue", Name: "queue", Kind: RootModule},
		{ImportPath: kafkaMod, Name: "kafka", Kind: NestedModule, Imports: []string{root + "/egopb", kafkaMod + "/internal/codec"}},
		{ImportPath: kafkaMod + "/internal/codec", Name: "codec", Kind: NestedModule},
		{ImportPath: natsMod, Name: "nats", Kind: NestedModule, Imports: []string{root + "/egopb"}},
		{ImportPath: compatMod, Name: "compat", Kind: NestedModule, Imports: []string{root, kafkaMod}},
	}
}

func TestNoModuleCycle_AcceptsTheS1ModuleGraph(t *testing.T) {
	graph := Graph{Packages: s1Packages(), Modules: s1Modules()}
	result, err := Evaluate(graph, rulesFor(t, "no-module-cycle"), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Violations) != 0 {
		t.Fatalf("len(Violations) = %d, want 0: %+v", len(result.Violations), result.Violations)
	}
	stat, ok := ruleStat(result.RuleStats, "no-module-cycle")
	if !ok || stat.PackagesMatched != 4 {
		t.Fatalf("no-module-cycle stat = %+v (found %v), want 4 modules checked", stat, ok)
	}
}

// TestNoModuleCycle_RejectsACycle builds the cycle ego-arch-001 §3 forbids
// and ego-arch-006 D7 warns about: a contracts module that requires the root
// while the root requires it. Every requirement edge on the cycle is
// reported, and an edge merely pointing into the cycle (nats -> root) is not.
func TestNoModuleCycle_RejectsACycle(t *testing.T) {
	contracts := root + "/contracts"
	modules := []Module{
		{Path: root, Requires: []string{contracts}},
		{Path: contracts, Requires: []string{root}},
		{Path: natsMod, Requires: []string{root}},
	}
	graph := Graph{Packages: s1Packages(), Modules: modules}
	result, err := Evaluate(graph, rulesFor(t, "no-module-cycle"), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	want := []Violation{
		{Importer: root, Import: contracts, Rule: "no-module-cycle"},
		{Importer: contracts, Import: root, Rule: "no-module-cycle"},
	}
	if len(result.Violations) != len(want) {
		t.Fatalf("len(Violations) = %d, want %d: %+v", len(result.Violations), len(want), result.Violations)
	}
	for i, w := range want {
		got := result.Violations[i]
		if got.Importer != w.Importer || got.Import != w.Import || got.Rule != w.Rule {
			t.Errorf("Violations[%d] = %+v, want importer %s, import %s, rule %s", i, got, w.Importer, w.Import, w.Rule)
		}
	}
	if reason := result.Violations[0].Reason; !strings.Contains(reason, root+" -> "+contracts+" -> "+root) {
		t.Errorf("Reason = %q, want it to spell out the cycle %s -> %s -> %s", reason, root, contracts, root)
	}
}

func TestNoModuleCycle_RejectsALongerCycle(t *testing.T) {
	modules := []Module{
		{Path: root, Requires: []string{kafkaMod}},
		{Path: kafkaMod, Requires: []string{compatMod}},
		{Path: compatMod, Requires: []string{root}},
		{Path: natsMod, Requires: []string{root}},
	}
	graph := Graph{Packages: s1Packages(), Modules: modules}
	result, err := Evaluate(graph, rulesFor(t, "no-module-cycle"), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Violations) != 3 {
		t.Fatalf("len(Violations) = %d, want 3 (every edge of the three-module cycle): %+v", len(result.Violations), result.Violations)
	}
	for _, v := range result.Violations {
		if v.Importer == natsMod {
			t.Errorf("nats -> root is not on the cycle but was reported: %+v", v)
		}
	}
}

func TestNoModuleCycle_RejectsASelfRequirement(t *testing.T) {
	modules := []Module{{Path: root}, {Path: kafkaMod, Requires: []string{kafkaMod}}}
	graph := Graph{Packages: s1Packages(), Modules: modules}
	result, err := Evaluate(graph, rulesFor(t, "no-module-cycle"), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Violations) != 1 || result.Violations[0].Importer != kafkaMod || result.Violations[0].Import != kafkaMod {
		t.Fatalf("Violations = %+v, want exactly kafka -> kafka", result.Violations)
	}
}

// A baseline entry covers a cycle edge exactly as it covers an import edge,
// keyed by (requiring module, required module, rule).
func TestNoModuleCycle_BaselineCoversAnEdge(t *testing.T) {
	modules := []Module{{Path: root}, {Path: kafkaMod, Requires: []string{kafkaMod}}}
	baseline := []BaselineEntry{{
		Importer:         kafkaMod,
		Import:           kafkaMod,
		Rule:             "no-module-cycle",
		Owner:            "@fixture",
		Justification:    "test fixture",
		RemovalCriterion: "never; test only",
	}}
	graph := Graph{Packages: s1Packages(), Modules: modules}
	result, err := Evaluate(graph, rulesFor(t, "no-module-cycle"), baseline)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Violations) != 0 || len(result.Stale) != 0 {
		t.Fatalf("result = %+v, want the one cycle edge baselined and nothing stale", result)
	}
}

func TestNoCrossModuleInternal_AllowsSameModuleInternal(t *testing.T) {
	graph := Graph{Packages: s1Packages(), Modules: s1Modules()}
	result, err := Evaluate(graph, rulesFor(t, "no-cross-module-internal"), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Violations) != 0 {
		t.Fatalf("len(Violations) = %d, want 0 (root -> root/internal and kafka -> kafka/internal stay inside their module): %+v", len(result.Violations), result.Violations)
	}
}

// The generalized rule: a nested module importing another nested module's
// internal/ package is rejected, which the pre-S1 rule (nested -> root
// internal/ only) missed.
func TestNoCrossModuleInternal_ForbidsNestedToNestedInternal(t *testing.T) {
	pkgs := append(s1Packages(), Package{
		ImportPath: compatMod + "/probe",
		Name:       "probe",
		Kind:       NestedModule,
		Imports:    []string{kafkaMod + "/internal/codec"},
	})
	graph := Graph{Packages: pkgs, Modules: s1Modules()}
	result, err := Evaluate(graph, rulesFor(t, "no-cross-module-internal"), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Violations) != 1 {
		t.Fatalf("len(Violations) = %d, want 1: %+v", len(result.Violations), result.Violations)
	}
	v := result.Violations[0]
	if v.Importer != compatMod+"/probe" || v.Import != kafkaMod+"/internal/codec" {
		t.Errorf("violation = %+v, want test/compat/probe -> publisher/kafka/internal/codec", v)
	}
	if !strings.Contains(v.Reason, kafkaMod) || !strings.Contains(v.Reason, compatMod) {
		t.Errorf("Reason = %q, want it to name both modules", v.Reason)
	}
}

// The root module importing a nested module's internal/ package is
// rejected too: the rule now applies to any pair of modules.
func TestNoCrossModuleInternal_ForbidsRootToNestedInternal(t *testing.T) {
	pkgs := append(s1Packages(), Package{
		ImportPath: root + "/tooling",
		Name:       "tooling",
		Kind:       RootModule,
		Imports:    []string{kafkaMod + "/internal/codec"},
	})
	graph := Graph{Packages: pkgs, Modules: s1Modules()}
	result, err := Evaluate(graph, rulesFor(t, "no-cross-module-internal"), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Violations) != 1 || result.Violations[0].Importer != root+"/tooling" {
		t.Fatalf("Violations = %+v, want exactly root/tooling -> publisher/kafka/internal/codec", result.Violations)
	}
}

// A module-aware rule needs the graph's module table. Without one it has
// checked nothing, and Evaluate must say so instead of passing vacuously.
func TestModuleAwareRules_FailClosedWithoutAModuleTable(t *testing.T) {
	graph := Graph{Packages: s1Packages()}
	_, err := Evaluate(graph, rulesFor(t, "no-cross-module-internal", "no-module-cycle"), nil)
	if err == nil {
		t.Fatal("Evaluate() = nil error, want an error: no module table, so both module-aware rules checked nothing")
	}
	for _, id := range []string{"no-cross-module-internal", "no-module-cycle"} {
		if !strings.Contains(err.Error(), id) {
			t.Errorf("error %q does not name rule %s", err, id)
		}
	}
}

// A package that belongs to no module in the table means the loaders and
// the module table disagree; Evaluate refuses rather than guessing.
func TestModuleAwareRules_FailClosedOnAPackageOutsideEveryModule(t *testing.T) {
	pkgs := append(s1Packages(), Package{ImportPath: "example.com/stray", Kind: NestedModule})
	graph := Graph{Packages: pkgs, Modules: s1Modules()}
	_, err := Evaluate(graph, rulesFor(t, "no-cross-module-internal"), nil)
	if err == nil || !strings.Contains(err.Error(), "example.com/stray") {
		t.Fatalf("Evaluate() error = %v, want one naming example.com/stray", err)
	}
}
