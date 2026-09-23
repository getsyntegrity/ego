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
	"sort"
	"strings"
	"testing"
)

func satelliteOpts(extra ...string) Options {
	dirs := append([]string{"benchmark", "example/cluster"}, extra...)
	return Options{SatelliteDirs: dirs}
}

func TestSelect_LeafChangeSelectsPackageAndRdepsViaTestImports(t *testing.T) {
	g := fixtureGraph()
	res := Select(g, []string{"internal/pause/x.go"}, satelliteOpts())

	if res.Mode != ModeAffected {
		t.Fatalf("Mode = %s, want %s (reasons=%v)", res.Mode, ModeAffected, res.Reasons)
	}
	want := []string{"github.com/x/mod", "github.com/x/mod/internal/pause"}
	assertSameSet(t, res.Selected, want)
}

func TestSelect_ExcludedPackageReachesIncludedPackagesViaTestImports(t *testing.T) {
	g := fixtureGraph()
	res := Select(g, []string{"test/data/testpb/x.go"}, satelliteOpts())

	if res.Mode != ModeAffected {
		t.Fatalf("Mode = %s, want %s (reasons=%v)", res.Mode, ModeAffected, res.Reasons)
	}
	// test/data/testpb itself is excluded from Included, so it must not
	// appear in Selected even though it is the changed package.
	want := []string{"github.com/x/mod", "github.com/x/mod/testkit"}
	assertSameSet(t, res.Selected, want)
	if containsString(res.Selected, "github.com/x/mod/test/data/testpb") {
		t.Fatalf("excluded package leaked into Selected: %v", res.Selected)
	}
}

func TestSelect_TestdataMapsToNearestAncestorPackage(t *testing.T) {
	g := fixtureGraph()
	res := Select(g, []string{"command/testdata/nested/fixture.json"}, satelliteOpts())

	if res.Mode != ModeAffected {
		t.Fatalf("Mode = %s, want %s (reasons=%v)", res.Mode, ModeAffected, res.Reasons)
	}
	// command is imported by both root and testkit, so all three are affected.
	want := []string{"github.com/x/mod", "github.com/x/mod/command", "github.com/x/mod/testkit"}
	assertSameSet(t, res.Selected, want)
}

func TestSelect_RootPackageChangeIsFull(t *testing.T) {
	g := fixtureGraph()
	res := Select(g, []string{"engine.go"}, satelliteOpts())
	assertFull(t, g, res)
}

func TestSelect_EgopbChangeIsFull(t *testing.T) {
	g := fixtureGraph()
	res := Select(g, []string{"egopb/types.pb.go"}, satelliteOpts())
	assertFull(t, g, res)
}

func TestSelect_GoModChangeIsFull(t *testing.T) {
	g := fixtureGraph()
	res := Select(g, []string{"go.mod"}, satelliteOpts())
	assertFull(t, g, res)
}

func TestSelect_GithubWorkflowChangeIsFull(t *testing.T) {
	g := fixtureGraph()
	res := Select(g, []string{".github/workflows/pull_request.yml"}, satelliteOpts())
	assertFull(t, g, res)
}

func TestSelect_SelectorSourceChangeIsFull(t *testing.T) {
	g := fixtureGraph()
	res := Select(g, []string{"internal/cmd/ciselect/main.go"}, satelliteOpts())
	assertFull(t, g, res)
}

func TestSelect_DocsOnlyIsNone(t *testing.T) {
	g := fixtureGraph()
	res := Select(g, []string{"README.md", "docs/guide.md"}, satelliteOpts())

	if res.Mode != ModeNone {
		t.Fatalf("Mode = %s, want %s (reasons=%v)", res.Mode, ModeNone, res.Reasons)
	}
	if len(res.Selected) != 0 {
		t.Fatalf("Selected = %v, want empty", res.Selected)
	}
}

func TestSelect_SatelliteOnlyIsNone(t *testing.T) {
	g := fixtureGraph()
	res := Select(g, []string{"benchmark/bench_test.go"}, satelliteOpts())

	if res.Mode != ModeNone {
		t.Fatalf("Mode = %s, want %s (reasons=%v)", res.Mode, ModeNone, res.Reasons)
	}
}

func TestSelect_DocsPlusLeafIsAffected(t *testing.T) {
	g := fixtureGraph()
	res := Select(g, []string{"README.md", "internal/pause/x.go"}, satelliteOpts())

	if res.Mode != ModeAffected {
		t.Fatalf("Mode = %s, want %s (reasons=%v)", res.Mode, ModeAffected, res.Reasons)
	}
	want := []string{"github.com/x/mod", "github.com/x/mod/internal/pause"}
	assertSameSet(t, res.Selected, want)
}

func TestSelect_UnknownPathIsFull(t *testing.T) {
	g := fixtureGraph()
	res := Select(g, []string{"resources/x.sql"}, satelliteOpts())
	assertFull(t, g, res)

	found := false
	for _, r := range res.Reasons {
		if strings.Contains(r, "resources/x.sql") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a reason mentioning the offending path, got %v", res.Reasons)
	}
}

func TestSelect_EmptyChangedListIsFull(t *testing.T) {
	g := fixtureGraph()
	res := Select(g, nil, satelliteOpts())
	assertFull(t, g, res)
}

func TestSelect_All(t *testing.T) {
	g := fixtureGraph()
	res := Select(g, nil, Options{All: true})
	assertFull(t, g, res)
}

func TestSelect_ExcludedPackageWithNoRdepsIsEmptySelectionFallback(t *testing.T) {
	g := fixtureGraph()
	// example/durablestate is excluded and nothing (build or test) imports it,
	// so the naive affected-intersect-included computation would be empty.
	// The selector must fail safe to full instead of testing nothing.
	res := Select(g, []string{"example/durablestate/main.go"}, satelliteOpts())
	assertFull(t, g, res)
}

func TestSelect_AffectedEqualToFullIncludedSetBecomesFull(t *testing.T) {
	// A tiny two-package graph where changing the leaf affects every
	// included package must report mode "full", not "affected".
	g := Graph{
		ModulePath: "github.com/x/mod",
		ModuleDir:  "/repo",
		Packages: []Package{
			{ImportPath: "github.com/x/mod/a", Dir: "/repo/a", Imports: []string{"github.com/x/mod/b"}},
			{ImportPath: "github.com/x/mod/b", Dir: "/repo/b"},
		},
	}
	res := Select(g, []string{"b/x.go"}, Options{})
	assertFull(t, g, res)
}

func assertFull(t *testing.T, g Graph, res Result) {
	t.Helper()
	if res.Mode != ModeFull {
		t.Fatalf("Mode = %s, want %s (reasons=%v)", res.Mode, ModeFull, res.Reasons)
	}
	included := Included(g)
	assertSameSet(t, res.Selected, included)
	if len(res.Reasons) == 0 {
		t.Fatalf("expected at least one reason for full mode")
	}
}

func assertSameSet(t *testing.T, got, want []string) {
	t.Helper()
	g := append([]string{}, got...)
	w := append([]string{}, want...)
	sort.Strings(g)
	sort.Strings(w)
	if len(g) != len(w) {
		t.Fatalf("got %v, want %v", g, w)
	}
	for i := range w {
		if g[i] != w[i] {
			t.Fatalf("got %v, want %v", g, w)
		}
	}
}
