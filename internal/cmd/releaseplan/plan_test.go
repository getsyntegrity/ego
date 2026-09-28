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
	"strings"
	"testing"
)

func TestBuildPlan_LinearChain_OrderAndRequires(t *testing.T) {
	g, err := discoverGraph("testdata/linear-chain")
	if err != nil {
		t.Fatalf("discoverGraph: %v", err)
	}
	plan, err := buildPlan(g, []string{".", "moda", "modb"}, nil, "patch")
	if err != nil {
		t.Fatalf("buildPlan: %v", err)
	}
	if len(plan.Modules) != 3 {
		t.Fatalf("len(plan.Modules) = %d, want 3", len(plan.Modules))
	}
	var dirs []string
	for _, m := range plan.Modules {
		dirs = append(dirs, m.Dir)
	}
	if want := []string{"modb", "moda", "."}; !equalStrings(dirs, want) {
		t.Fatalf("order = %v, want %v", dirs, want)
	}

	root := plan.Modules[2]
	if root.Path != "example.com/root" {
		t.Fatalf("root.Path = %q", root.Path)
	}
	if !equalStrings(root.Requires, []string{"example.com/root/moda"}) {
		t.Fatalf("root.Requires = %v, want [example.com/root/moda]", root.Requires)
	}
	if root.CurrentTag != "" {
		t.Fatalf("root.CurrentTag = %q, want empty (no tags given)", root.CurrentTag)
	}
	if root.NextTag == "" {
		t.Fatal("root.NextTag is empty")
	}
}

func TestBuildPlan_Cycle(t *testing.T) {
	g, err := discoverGraph("testdata/cycle")
	if err != nil {
		t.Fatalf("discoverGraph: %v", err)
	}
	_, err = buildPlan(g, []string{".", "moda", "modb"}, nil, "patch")
	if err == nil {
		t.Fatal("expected a cycle error")
	}
}

// TestBuildPlan_CycleConfinedToUnreleasedModules covers PR #169 review
// finding 2's second case: modx and mody cycle with each other, but only
// the root is released. buildPlan must still refuse, because detectCycle
// runs over the whole discovered graph (not only the released subset)
// before releasedSet is ever consulted — a cycle nobody released is still
// a cycle the repository's module graph cannot honor a release order for.
func TestBuildPlan_CycleConfinedToUnreleasedModules(t *testing.T) {
	g, err := discoverGraph("testdata/unreleased-cycle")
	if err != nil {
		t.Fatalf("discoverGraph: %v", err)
	}
	_, err = buildPlan(g, []string{"."}, nil, "patch")
	if err == nil {
		t.Fatal("expected a cycle error even though modx and mody are never released")
	}
	if !strings.Contains(err.Error(), "modx") || !strings.Contains(err.Error(), "mody") {
		t.Fatalf("cycle error %q does not name the unreleased modules that cycle", err.Error())
	}
}

func TestBuildPlan_ReleasedRequiresUnreleased(t *testing.T) {
	g, err := discoverGraph("testdata/released-requires-unreleased")
	if err != nil {
		t.Fatalf("discoverGraph: %v", err)
	}
	_, err = buildPlan(g, []string{".", "modx"}, nil, "patch")
	if err == nil {
		t.Fatal("expected a released-requires-unreleased error")
	}
}

func TestBuildPlan_MissingListedDir(t *testing.T) {
	g, err := discoverGraph("testdata/missing-listed-dir")
	if err != nil {
		t.Fatalf("discoverGraph: %v", err)
	}
	_, err = buildPlan(g, []string{".", "ghost"}, nil, "patch")
	if err == nil {
		t.Fatal("expected a missing-listed-dir error")
	}
}

func TestBuildPlan_MajorRefusal(t *testing.T) {
	g, err := discoverGraph("testdata/tagscheme")
	if err != nil {
		t.Fatalf("discoverGraph: %v", err)
	}
	_, err = buildPlan(g, []string{".", "pub"}, nil, "major")
	if err == nil {
		t.Fatal("expected a major-bump refusal")
	}
}

func TestRenderPlanJSON_NeverNullRequires(t *testing.T) {
	plan := Plan{Bump: "patch", Modules: []PlanModule{{Dir: ".", Path: "example.com/root", NextTag: "v1.0.0"}}}
	out, err := renderPlanJSON(plan)
	if err != nil {
		t.Fatalf("renderPlanJSON: %v", err)
	}
	if strings.Contains(out, "null") {
		t.Fatalf("plan.json has a null field: %s", out)
	}
	if !strings.Contains(out, `"requires": []`) {
		t.Fatalf("plan.json missing an empty requires array: %s", out)
	}
}

func TestRenderSummary_ContainsOrderAndTags(t *testing.T) {
	plan := Plan{Bump: "patch", Modules: []PlanModule{
		{Dir: ".", Path: "example.com/root", CurrentTag: "v1.2.3", NextTag: "v1.2.4"},
	}}
	summary := renderSummary(plan)
	if !strings.Contains(summary, "v1.2.3") || !strings.Contains(summary, "v1.2.4") {
		t.Fatalf("summary missing current/next tags: %s", summary)
	}
	if !strings.Contains(summary, "patch") {
		t.Fatalf("summary missing the bump kind: %s", summary)
	}
}
