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
	"testing"
)

// fixtureGraph mirrors the shape of the real ego module closely enough to
// exercise the selector: a root package, a regularly-imported package
// (command), a package imported only from test files (internal/pause,
// exactly like the real repo), an excluded package reached only via test
// imports (test/data/testpb, like the real repo), and one package per
// excluded segment plus one deliberately-similar package name that must NOT
// be excluded (examples2, proving segment-only matching).
func fixtureGraph() Graph {
	const mod = "github.com/x/mod"
	return Graph{
		ModulePath: mod,
		ModuleDir:  "/repo",
		Packages: []Package{
			{
				ImportPath:   mod,
				Dir:          "/repo",
				Imports:      []string{mod + "/command"},
				TestImports:  []string{mod + "/internal/pause", mod + "/mocks/ego", mod + "/test/data/testpb"},
				XTestImports: nil,
			},
			{
				ImportPath: mod + "/command",
				Dir:        "/repo/command",
			},
			{
				ImportPath: mod + "/internal/pause",
				Dir:        "/repo/internal/pause",
			},
			{
				ImportPath:  mod + "/testkit",
				Dir:         "/repo/testkit",
				Imports:     []string{mod + "/command"},
				TestImports: []string{mod + "/test/data/testpb"},
			},
			{
				ImportPath: mod + "/egopb",
				Dir:        "/repo/egopb",
			},
			{
				ImportPath: mod + "/mocks/ego",
				Dir:        "/repo/mocks/ego",
				Imports:    []string{mod},
			},
			{
				ImportPath: mod + "/example/durablestate",
				Dir:        "/repo/example/durablestate",
				Imports:    []string{mod},
			},
			{
				ImportPath: mod + "/example/examplepb",
				Dir:        "/repo/example/examplepb",
			},
			{
				ImportPath: mod + "/test/data/testpb",
				Dir:        "/repo/test/data/testpb",
			},
			{
				// Hypothetical package proving the "example" exclusion is a
				// whole-segment match, not a substring match.
				ImportPath: mod + "/examples2",
				Dir:        "/repo/examples2",
			},
		},
	}
}

func TestIncluded(t *testing.T) {
	g := fixtureGraph()
	got := Included(g)
	sort.Strings(got)

	want := []string{
		"github.com/x/mod",
		"github.com/x/mod/command",
		"github.com/x/mod/examples2",
		"github.com/x/mod/internal/pause",
		"github.com/x/mod/testkit",
	}

	if len(got) != len(want) {
		t.Fatalf("Included() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Included() = %v, want %v", got, want)
		}
	}
}

func TestIncluded_TestkitIsIncluded(t *testing.T) {
	included := Included(fixtureGraph())
	if !containsString(included, "github.com/x/mod/testkit") {
		t.Fatalf("expected testkit to be included, got %v", included)
	}
}

func TestIncluded_ExampleSegmentExcludedButExamples2IsNot(t *testing.T) {
	included := Included(fixtureGraph())
	if containsString(included, "github.com/x/mod/example/examplepb") {
		t.Fatalf("expected example/examplepb to be excluded (segment match), got %v", included)
	}
	if !containsString(included, "github.com/x/mod/examples2") {
		t.Fatalf("expected examples2 to remain included (not a segment match), got %v", included)
	}
}

func containsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}
