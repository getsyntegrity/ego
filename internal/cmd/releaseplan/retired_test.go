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
	"bytes"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// retiredNames are the five tag names of the withdrawn first release. They
// were published, then deleted; the Go module proxy and checksum database
// keep the old content, so reusing a name for different content would be a
// checksum mismatch for every consumer that already saw it.
var retiredNames = []string{
	"publisher/kafka/v0.1.0",
	"publisher/nats/v0.1.0",
	"publisher/pulsar/v0.1.0",
	"publisher/websocket/v0.1.0",
	"v4.0.0",
}

func TestReadRetiredTags_SkipsCommentsAndBlanks(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "retired.txt")
	writeLines(t, f, []string{"# comment", "", "v4.0.0", "  publisher/kafka/v0.1.0  ", "# another"})
	got, err := readRetiredTags(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got["v4.0.0"] || !got["publisher/kafka/v0.1.0"] {
		t.Errorf("got %v", got)
	}
	if _, err := readRetiredTags(filepath.Join(dir, "missing.txt")); err == nil {
		t.Error("a missing retired list must be an error, never an empty list")
	}
}

func TestCheckNotRetired(t *testing.T) {
	retired := map[string]bool{"v4.0.0": true, "publisher/kafka/v0.1.0": true}
	if err := checkNotRetired(retired, "v1.0.0", "publisher/kafka/v0.2.0"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	err := checkNotRetired(retired, "publisher/kafka/v0.1.0", "v1.0.0", "v4.0.0")
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"publisher/kafka/v0.1.0", "v4.0.0", "retired"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "v1.0.0") {
		t.Errorf("error must name only the offending tags: %v", err)
	}
	if checkNotRetired(nil, "v4.0.0") != nil {
		t.Error("no retired list means no restriction")
	}
}

// The committed list must hold exactly the five retired names.
func TestCommittedRetiredList(t *testing.T) {
	got, err := readRetiredTags(filepath.Join("..", "..", "..", "scripts", "ci", "retired-tags.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for n := range got {
		names = append(names, n)
	}
	sort.Strings(names)
	if strings.Join(names, ",") != strings.Join(retiredNames, ",") {
		t.Errorf("retired list = %v, want %v", names, retiredNames)
	}
}

func retiredFile(t *testing.T, names ...string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "retired.txt")
	writeLines(t, f, names)
	return f
}

func realRepoArgs(t *testing.T, tags []string, extra ...string) []string {
	t.Helper()
	dir := t.TempDir()
	tagsFile := filepath.Join(dir, "tags.txt")
	writeLines(t, tagsFile, tags)
	repoRoot := filepath.Join("..", "..", "..")
	return append([]string{
		"-repo-root", repoRoot,
		"-release", filepath.Join(repoRoot, "scripts", "ci", "release-modules.txt"),
		"-tags", tagsFile,
		"-out-dir", filepath.Join(dir, "out"),
	}, extra...)
}

func TestRun_RefusesToPlanARetiredTag(t *testing.T) {
	var out, errb bytes.Buffer
	// empty tags + minor: the root plans v0.1.0 and every publisher v0.1.0
	args := realRepoArgs(t, nil, "-bump", "minor", "-retired", retiredFile(t, "publisher/nats/v0.1.0"))
	err := run(args, &out, &errb)
	if err == nil || !strings.Contains(err.Error(), "publisher/nats/v0.1.0") || !strings.Contains(err.Error(), "retired") {
		t.Fatalf("err = %v, want a refusal naming publisher/nats/v0.1.0", err)
	}

	// the same plan without the list, or with an unrelated one, is fine
	if err := run(realRepoArgs(t, nil, "-bump", "minor", "-retired", retiredFile(t, "v4.0.0")), &out, &errb); err != nil {
		t.Fatalf("unrelated retired list: %v", err)
	}
	if err := run(realRepoArgs(t, nil, "-bump", "minor"), &out, &errb); err != nil {
		t.Fatalf("no retired list: %v", err)
	}
}

func TestRun_RefusesWhenARetiredTagExistsAgain(t *testing.T) {
	var out, errb bytes.Buffer
	args := realRepoArgs(t, []string{"v4.0.0"}, "-bump", "minor", "-retired", retiredFile(t, "v4.0.0"))
	err := run(args, &out, &errb)
	if err == nil || !strings.Contains(err.Error(), "v4.0.0") {
		t.Fatalf("err = %v, want a refusal because a retired tag exists in -tags", err)
	}
}

func TestRun_ContinuationPlanRefusesARetiredTag(t *testing.T) {
	dir := t.TempDir()
	rel := filepath.Join(dir, "publishers.txt")
	writeLines(t, rel, []string{"publisher/kafka", "publisher/nats"})
	tagsFile := filepath.Join(dir, "tags.txt")
	writeLines(t, tagsFile, nil)
	repoRoot := filepath.Join("..", "..", "..")

	var out, errb bytes.Buffer
	err := run([]string{
		"-repo-root", repoRoot, "-release", rel, "-tags", tagsFile, "-bump", "minor",
		"-continuation-plan-publishers", "-out-dir", filepath.Join(dir, "out"),
		"-retired", retiredFile(t, "publisher/kafka/v0.1.0"),
	}, &out, &errb)
	if err == nil || !strings.Contains(err.Error(), "publisher/kafka/v0.1.0") || !strings.Contains(err.Error(), "retired") {
		t.Fatalf("err = %v", err)
	}
}

func TestRun_ContinuationCheckNotRetired(t *testing.T) {
	list := filepath.Join("..", "..", "..", "scripts", "ci", "retired-tags.txt")
	var out, errb bytes.Buffer
	if err := run([]string{"-retired", list, "-continuation-check-not-retired", "v4.0.0"}, &out, &errb); err == nil {
		t.Error("v4.0.0 must be refused")
	}
	if err := run([]string{"-retired", list, "-continuation-check-not-retired", "publisher/websocket/v0.1.0"}, &out, &errb); err == nil {
		t.Error("publisher/websocket/v0.1.0 must be refused")
	}
	if err := run([]string{"-retired", list, "-continuation-check-not-retired", "v0.1.0"}, &out, &errb); err != nil {
		t.Errorf("v0.1.0 on the root is not retired (only the five names are): %v", err)
	}
	if err := run([]string{"-continuation-check-not-retired", "v4.0.0"}, &out, &errb); err == nil {
		t.Error("-continuation-check-not-retired without -retired must be an error, never a silent pass")
	}
}
