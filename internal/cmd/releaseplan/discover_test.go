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
	"sort"
	"testing"
)

func TestDiscoverGraph_LinearChain(t *testing.T) {
	g, err := discoverGraph("testdata/linear-chain")
	if err != nil {
		t.Fatalf("discoverGraph: %v", err)
	}

	var dirs []string
	for _, m := range g.Modules {
		dirs = append(dirs, m.Dir)
	}
	sort.Strings(dirs)
	if want := []string{".", "moda", "modb"}; !equalStrings(dirs, want) {
		t.Fatalf("dirs = %v, want %v", dirs, want)
	}

	root, ok := g.ByDir(".")
	if !ok {
		t.Fatal("root module not discovered")
	}
	if root.Path != "example.com/root" {
		t.Fatalf("root.Path = %q, want example.com/root", root.Path)
	}
	if !equalStrings(root.Requires, []string{"example.com/root/moda"}) {
		t.Fatalf("root.Requires = %v, want [example.com/root/moda]", root.Requires)
	}

	modb, ok := g.ByDir("modb")
	if !ok {
		t.Fatal("modb not discovered")
	}
	if len(modb.Requires) != 0 {
		t.Fatalf("modb.Requires = %v, want none", modb.Requires)
	}
}

func TestDiscoverGraph_DirOfPath(t *testing.T) {
	g, err := discoverGraph("testdata/diamond")
	if err != nil {
		t.Fatalf("discoverGraph: %v", err)
	}
	dir, ok := g.DirOfPath("example.com/root/modc")
	if !ok || dir != "modc" {
		t.Fatalf("DirOfPath(modc) = %q, %v, want modc, true", dir, ok)
	}
	if _, ok := g.DirOfPath("example.com/not-in-repo"); ok {
		t.Fatal("DirOfPath found a module path that was never discovered")
	}
}

func TestDiscoverGraph_MissingRoot(t *testing.T) {
	if _, err := discoverGraph("testdata/does-not-exist"); err == nil {
		t.Fatal("expected an error for a repository root with no go.mod")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
