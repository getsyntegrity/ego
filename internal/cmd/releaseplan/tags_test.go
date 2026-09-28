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
	"testing"
)

// The testdata/tagscheme fixture: root module "example.com/repo/v4" (a
// /v4 path suffix, so D2 (a) forces major 4), nested module "pub" at
// "example.com/repo/v4/pub" (no suffix of its own, so major is only ever
// 0 or 1).

func TestNextTag_NoExistingTags_SuffixedModuleGetsBaselineForEveryBumpKind(t *testing.T) {
	g, err := discoverGraph("testdata/tagscheme")
	if err != nil {
		t.Fatalf("discoverGraph: %v", err)
	}
	root, _ := g.ByDir(".")

	// The root module's path carries a /v4 suffix (D2 (a) forces major 4)
	// and has no tag at all: there is no earlier v4 release to bump from,
	// so every valid bump kind lands on the same first release, v4.0.0 —
	// never v4.0.1, v4.1.0 or v5.0.0.
	for _, kind := range []string{"patch", "minor", "major"} {
		t.Run(kind, func(t *testing.T) {
			cur, next, err := nextTag(".", root.Path, nil, kind)
			if err != nil {
				t.Fatalf("nextTag(%q): %v", kind, err)
			}
			if cur != "" {
				t.Fatalf("current = %q, want none (no existing tags)", cur)
			}
			if next.String() != "4.0.0" {
				t.Fatalf("next = %s, want 4.0.0", next.String())
			}
		})
	}
}

func TestNextTag_NoExistingTags_SuffixlessModuleBumpsFromZero(t *testing.T) {
	g, err := discoverGraph("testdata/tagscheme")
	if err != nil {
		t.Fatalf("discoverGraph: %v", err)
	}
	pub, _ := g.ByDir("pub")

	// "pub" has no path suffix, so it keeps today's release.yml baseline
	// (CURRENT="0.0.0" when no publisher tag exists): the first patch is
	// v0.0.1, the first minor v0.1.0, and the first major v1.0.0 (legal
	// without a suffix; v2+ still needs one, covered from a v1 tag by
	// TestNextTag_ExistingMajorRefusal).
	cases := []struct {
		kind string
		want string
	}{
		{"patch", "0.0.1"},
		{"minor", "0.1.0"},
		{"major", "1.0.0"},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			_, next, err := nextTag("pub", pub.Path, nil, c.kind)
			if err != nil {
				t.Fatalf("nextTag(%q): %v", c.kind, err)
			}
			if next.String() != c.want {
				t.Fatalf("next = %s, want %s", next.String(), c.want)
			}
		})
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

// Tag prefix collisions (PR #169 review finding 3): tagPrefix's dir-based
// prefix must not accidentally match a tag belonging to a different
// module. These are pure tests of latestTag/tagPrefix, no fixture needed.

func TestLatestTag_IgnoresSiblingDirWithSharedPrefix(t *testing.T) {
	// "publisher/kafka" must not match a tag belonging to the sibling
	// directory "publisher/kafka-x" just because "publisher/kafka" is a
	// textual prefix of "publisher/kafka-x".
	tags := []string{"publisher/kafka-x/v9.9.9"}
	if _, tag, found := latestTag("publisher/kafka", tags); found {
		t.Fatalf("publisher/kafka matched %q, which belongs to publisher/kafka-x", tag)
	}
}

func TestLatestTag_RootIgnoresNestedModuleTag(t *testing.T) {
	// The root's "v" prefix must not match a nested module's own tag.
	tags := []string{"publisher/kafka/v1.0.0"}
	if _, tag, found := latestTag(".", tags); found {
		t.Fatalf("root prefix \"v\" matched %q, which belongs to publisher/kafka", tag)
	}
}

func TestLatestTag_NestedModuleIgnoresRootTag(t *testing.T) {
	// A nested module must not match the root's own tag.
	tags := []string{"v4.9.9"}
	if _, tag, found := latestTag("publisher/kafka", tags); found {
		t.Fatalf("publisher/kafka matched %q, which belongs to the root", tag)
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
