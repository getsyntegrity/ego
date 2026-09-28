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

// The testdata/tagscheme fixture: root module "example.com/repo/v4" (a
// /v4 path suffix, so D2 (a) forces major 4), nested module "pub" at
// "example.com/repo/v4/pub" (no suffix of its own, so major is only ever
// 0 or 1).

func TestNextTag_NoExistingTags_PatchSucceeds(t *testing.T) {
	g, err := discoverGraph("testdata/tagscheme")
	if err != nil {
		t.Fatalf("discoverGraph: %v", err)
	}
	root, _ := g.ByDir(".")
	pub, _ := g.ByDir("pub")

	rootCur, rootNext, err := nextTag(".", root.Path, nil, "patch")
	if err != nil {
		t.Fatalf("root nextTag: %v", err)
	}
	if rootCur != "" {
		t.Fatalf("root current = %q, want none (no existing tags)", rootCur)
	}
	if rootNext.String() != "4.0.1" {
		t.Fatalf("root next = %s, want 4.0.1", rootNext.String())
	}

	_, pubNext, err := nextTag("pub", pub.Path, nil, "patch")
	if err != nil {
		t.Fatalf("pub nextTag: %v", err)
	}
	if pubNext.String() != "1.0.1" {
		t.Fatalf("pub next = %s, want 1.0.1", pubNext.String())
	}
}

func TestNextTag_NoExistingTags_MajorRefusesBoth(t *testing.T) {
	g, err := discoverGraph("testdata/tagscheme")
	if err != nil {
		t.Fatalf("discoverGraph: %v", err)
	}
	root, _ := g.ByDir(".")
	pub, _ := g.ByDir("pub")

	_, _, err = nextTag(".", root.Path, nil, "major")
	if err == nil {
		t.Fatal("expected root major bump (v4 -> v5) to be refused: path suffix is /v4")
	}
	if !strings.Contains(err.Error(), "v5") && !strings.Contains(err.Error(), "5") {
		t.Fatalf("root refusal %q does not mention the offending major", err.Error())
	}

	_, _, err = nextTag("pub", pub.Path, nil, "major")
	if err == nil {
		t.Fatal("expected pub major bump (v1 -> v2) to be refused: pub has no /vN suffix")
	}
	if !strings.Contains(err.Error(), "pub") {
		t.Fatalf("pub refusal %q does not name the module", err.Error())
	}
}

func TestNextTag_ExistingTags_PatchAndMinor(t *testing.T) {
	g, err := discoverGraph("testdata/tagscheme")
	if err != nil {
		t.Fatalf("discoverGraph: %v", err)
	}
	root, _ := g.ByDir(".")
	pub, _ := g.ByDir("pub")
	tags := []string{"v4.2.5", "pub/v0.3.1"}

	rootCur, rootNext, err := nextTag(".", root.Path, tags, "patch")
	if err != nil {
		t.Fatalf("root nextTag: %v", err)
	}
	if rootCur != "v4.2.5" {
		t.Fatalf("root current = %q, want v4.2.5", rootCur)
	}
	if rootNext.String() != "4.2.6" {
		t.Fatalf("root next = %s, want 4.2.6", rootNext.String())
	}

	pubCur, pubNext, err := nextTag("pub", pub.Path, tags, "minor")
	if err != nil {
		t.Fatalf("pub nextTag: %v", err)
	}
	if pubCur != "pub/v0.3.1" {
		t.Fatalf("pub current = %q, want pub/v0.3.1", pubCur)
	}
	if pubNext.String() != "0.4.0" {
		t.Fatalf("pub next = %s, want 0.4.0", pubNext.String())
	}
}

func TestNextTag_ExistingMajorRefusal(t *testing.T) {
	g, err := discoverGraph("testdata/tagscheme")
	if err != nil {
		t.Fatalf("discoverGraph: %v", err)
	}
	root, _ := g.ByDir(".")
	pub, _ := g.ByDir("pub")

	_, _, err = nextTag(".", root.Path, []string{"v4.9.9"}, "major")
	if err == nil {
		t.Fatal("expected root v4.9.9 -bump major (-> v5) to be refused")
	}

	_, _, err = nextTag("pub", pub.Path, []string{"pub/v1.9.0"}, "major")
	if err == nil {
		t.Fatal("expected pub v1.9.0 -bump major (-> v2, no /v2 suffix) to be refused")
	}
}

func TestNextTag_UnknownBumpKind(t *testing.T) {
	g, err := discoverGraph("testdata/tagscheme")
	if err != nil {
		t.Fatalf("discoverGraph: %v", err)
	}
	root, _ := g.ByDir(".")
	if _, _, err := nextTag(".", root.Path, nil, "banana"); err == nil {
		t.Fatal("expected an error for an unknown -bump kind")
	}
}

func TestParseSemver(t *testing.T) {
	cases := []struct {
		in string
		ok bool
	}{
		{"1.2.3", true},
		{"0.0.0", true},
		{"1.2", false},
		{"1.2.3.4", false},
		{"a.b.c", false},
		{"-1.2.3", false},
	}
	for _, c := range cases {
		_, ok := parseSemver(c.in)
		if ok != c.ok {
			t.Errorf("parseSemver(%q) ok = %v, want %v", c.in, ok, c.ok)
		}
	}
}
