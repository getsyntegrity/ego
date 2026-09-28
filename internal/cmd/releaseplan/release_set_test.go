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

func TestReleasedSet_MissingListedDir(t *testing.T) {
	g, err := discoverGraph("testdata/missing-listed-dir")
	if err != nil {
		t.Fatalf("discoverGraph: %v", err)
	}
	_, err = releasedSet(g, []string{".", "ghost"})
	if err == nil {
		t.Fatal("expected an error for a released directory with no go.mod")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("error %q does not name the missing directory", err.Error())
	}
}

func TestReleasedSet_ReleasedRequiresUnreleased(t *testing.T) {
	g, err := discoverGraph("testdata/released-requires-unreleased")
	if err != nil {
		t.Fatalf("discoverGraph: %v", err)
	}
	_, err = releasedSet(g, []string{".", "modx"})
	if err == nil {
		t.Fatal("expected an error: modx requires the unreleased mody")
	}
	if !strings.Contains(err.Error(), "modx") || !strings.Contains(err.Error(), "mody") {
		t.Fatalf("error %q does not name both modules", err.Error())
	}
}

func TestReleasedSet_OK(t *testing.T) {
	g, err := discoverGraph("testdata/linear-chain")
	if err != nil {
		t.Fatalf("discoverGraph: %v", err)
	}
	set, err := releasedSet(g, []string{".", "moda", "modb"})
	if err != nil {
		t.Fatalf("releasedSet: %v", err)
	}
	if !set["."] || !set["moda"] || !set["modb"] {
		t.Fatalf("releasedSet = %v, want all three released", set)
	}
}
