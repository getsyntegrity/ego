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

// This file exercises the publisher-tag-release continuation's pure
// decision logic (issue #159 F4, task T3): SHA/version format validation,
// the required-root-version check, publishers-only next-tag computation
// and the tag-existence conflict check. None of it touches git, the
// network or the GitHub API — see continuation.go.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- (a) SHA / version format validation ---

func TestValidateSHA(t *testing.T) {
	cases := []struct {
		name string
		sha  string
		ok   bool
	}{
		{"valid 40 lowercase hex", strings.Repeat("a1b2c3d4e5", 4), true},
		{"all zeros", strings.Repeat("0", 40), true},
		{"too short", strings.Repeat("a", 39), false},
		{"too long", strings.Repeat("a", 41), false},
		{"uppercase hex", strings.Repeat("A", 40), false},
		{"non-hex character", strings.Repeat("g", 40), false},
		{"empty", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateSHA(c.sha)
			if c.ok && err != nil {
				t.Fatalf("validateSHA(%q) = %v, want nil", c.sha, err)
			}
			if !c.ok && err == nil {
				t.Fatalf("validateSHA(%q) = nil, want an error", c.sha)
			}
		})
	}
}

func TestValidateVersion(t *testing.T) {
	cases := []struct {
		name    string
		version string
		ok      bool
	}{
		{"valid", "v4.5.0", true},
		{"valid zero", "v0.0.0", true},
		{"valid multi-digit", "v12.34.567", true},
		{"missing leading v", "4.5.0", false},
		{"pre-release suffix", "v4.5.0-rc.1", false},
		{"build metadata", "v4.5.0+build", false},
		{"missing patch", "v4.5", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateVersion(c.version)
			if c.ok && err != nil {
				t.Fatalf("validateVersion(%q) = %v, want nil", c.version, err)
			}
			if !c.ok && err == nil {
				t.Fatalf("validateVersion(%q) = nil, want an error", c.version)
			}
		})
	}
}

// --- (d) required-root-version check ---

func TestCheckRequiredRootVersion_OK(t *testing.T) {
	repoRoot, err := filepath.Abs("testdata/required-version")
	if err != nil {
		t.Fatalf("resolving repo root: %v", err)
	}
	err = checkRequiredRootVersion(repoRoot, "example.com/repo", "v1.2.3", []string{".", "pub1"})
	if err != nil {
		t.Fatalf("checkRequiredRootVersion = %v, want nil (pub1 requires exactly v1.2.3)", err)
	}
}

func TestCheckRequiredRootVersion_Mismatch(t *testing.T) {
	repoRoot, err := filepath.Abs("testdata/required-version")
	if err != nil {
		t.Fatalf("resolving repo root: %v", err)
	}
	err = checkRequiredRootVersion(repoRoot, "example.com/repo", "v1.2.3", []string{".", "pub1", "pub2"})
	if err == nil {
		t.Fatal("expected an error: pub2 requires v1.2.0, wanted v1.2.3")
	}
	if !strings.Contains(err.Error(), "pub2") {
		t.Fatalf("error %q does not name the mismatched module pub2", err.Error())
	}
	if !strings.Contains(err.Error(), "v1.2.0") || !strings.Contains(err.Error(), "v1.2.3") {
		t.Fatalf("error %q does not name both the actual and wanted versions", err.Error())
	}
	if strings.Contains(err.Error(), "pub1") {
		t.Fatalf("error %q names pub1, which matches the wanted version and should not be reported", err.Error())
	}
}

func TestCheckRequiredRootVersion_SkipsRoot(t *testing.T) {
	// The root module never requires itself; "." in dirs must not be
	// treated as a module that has to declare a require line for itself.
	repoRoot, err := filepath.Abs("testdata/required-version")
	if err != nil {
		t.Fatalf("resolving repo root: %v", err)
	}
	if err := checkRequiredRootVersion(repoRoot, "example.com/repo", "v1.2.3", []string{"."}); err != nil {
		t.Fatalf("checkRequiredRootVersion with only root listed = %v, want nil", err)
	}
}

// --- (e) publishers-only next-tag computation ---

func TestPlanPublisherTags_ComputesEachPublisher(t *testing.T) {
	g, err := discoverGraph("testdata/linear-chain")
	if err != nil {
		t.Fatalf("discoverGraph: %v", err)
	}
	modules, err := planPublisherTags(g, []string{"moda", "modb"}, nil, "patch")
	if err != nil {
		t.Fatalf("planPublisherTags: %v", err)
	}
	if len(modules) != 2 {
		t.Fatalf("len(modules) = %d, want 2", len(modules))
	}
	if modules[0].Dir != "moda" || modules[0].NextTag != "moda/v0.0.1" {
		t.Fatalf("modules[0] = %+v, want Dir=moda NextTag=moda/v0.0.1", modules[0])
	}
	if modules[1].Dir != "modb" || modules[1].NextTag != "modb/v0.0.1" {
		t.Fatalf("modules[1] = %+v, want Dir=modb NextTag=modb/v0.0.1", modules[1])
	}
}

func TestPlanPublisherTags_RefusesRoot(t *testing.T) {
	g, err := discoverGraph("testdata/linear-chain")
	if err != nil {
		t.Fatalf("discoverGraph: %v", err)
	}
	_, err = planPublisherTags(g, []string{".", "moda"}, nil, "patch")
	if err == nil {
		t.Fatal("expected an error: root must be excluded from a publishers-only tag plan")
	}
}

func TestPlanPublisherTags_UnknownDirectory(t *testing.T) {
	g, err := discoverGraph("testdata/linear-chain")
	if err != nil {
		t.Fatalf("discoverGraph: %v", err)
	}
	_, err = planPublisherTags(g, []string{"ghost"}, nil, "patch")
	if err == nil {
		t.Fatal("expected an error for a directory with no go.mod")
	}
}

// --- (f) tag-existence conflict check ---

func TestCheckTagConflicts_NoConflicts(t *testing.T) {
	computed := []PlanModule{
		{Dir: "publisher/kafka", NextTag: "publisher/kafka/v1.2.4"},
		{Dir: "publisher/nats", NextTag: "publisher/nats/v1.2.4"},
	}
	existing := []string{"v4.4.3", "publisher/kafka/v1.2.3"}
	if err := checkTagConflicts(computed, existing); err != nil {
		t.Fatalf("checkTagConflicts = %v, want nil", err)
	}
}

func TestCheckTagConflicts_ReportsEveryConflict(t *testing.T) {
	computed := []PlanModule{
		{Dir: "publisher/kafka", NextTag: "publisher/kafka/v1.2.4"},
		{Dir: "publisher/nats", NextTag: "publisher/nats/v1.2.4"},
		{Dir: "publisher/pulsar", NextTag: "publisher/pulsar/v1.2.4"},
	}
	existing := []string{"publisher/kafka/v1.2.4", "publisher/nats/v1.2.4"}
	err := checkTagConflicts(computed, existing)
	if err == nil {
		t.Fatal("expected an error: two of the three computed tags already exist")
	}
	if !strings.Contains(err.Error(), "publisher/kafka/v1.2.4") {
		t.Fatalf("error %q does not name publisher/kafka/v1.2.4", err.Error())
	}
	if !strings.Contains(err.Error(), "publisher/nats/v1.2.4") {
		t.Fatalf("error %q does not name publisher/nats/v1.2.4", err.Error())
	}
	if strings.Contains(err.Error(), "publisher/pulsar/v1.2.4") {
		t.Fatalf("error %q names publisher/pulsar/v1.2.4, which has no conflict", err.Error())
	}
}

// --- main.go wiring: the "-continuation-*" flags ---

func TestRun_ContinuationCheckSHA(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-continuation-check-sha", strings.Repeat("a1b2c3d4e5", 4)}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v (stderr: %s)", err, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	err := run([]string{"-continuation-check-sha", "not-a-sha"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected run to fail for an invalid SHA")
	}
}

func TestRun_ContinuationCheckVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-continuation-check-version", "v4.5.0"}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v (stderr: %s)", err, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	err := run([]string{"-continuation-check-version", "4.5.0"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected run to fail for a version missing its leading v")
	}
}

func TestRun_ContinuationCheckRequiredVersion_PassAndFail(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-repo-root", "testdata/required-version",
		"-release", "testdata/required-version/release-ok.txt",
		"-continuation-check-required-version", "v1.2.3",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run (release-ok.txt, want v1.2.3): %v (stderr: %s)", err, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	err = run([]string{
		"-repo-root", "testdata/required-version",
		"-release", "testdata/required-version/release-mismatch.txt",
		"-continuation-check-required-version", "v1.2.3",
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected run to fail: pub2 in release-mismatch.txt requires v1.2.0, not v1.2.3")
	}
	if !strings.Contains(err.Error(), "pub2") {
		t.Fatalf("error %q does not name pub2", err.Error())
	}
}

func TestRun_ContinuationPlanPublishers(t *testing.T) {
	dir := t.TempDir()
	releaseFile := filepath.Join(dir, "publishers.txt")
	writeLines(t, releaseFile, []string{"moda", "modb"})
	tagsFile := filepath.Join(dir, "tags.txt")
	writeLines(t, tagsFile, []string{})
	outDir := filepath.Join(dir, "out")

	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-repo-root", "testdata/linear-chain",
		"-release", releaseFile,
		"-tags", tagsFile,
		"-bump", "patch",
		"-out-dir", outDir,
		"-continuation-plan-publishers",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v (stderr: %s)", err, stderr.String())
	}

	planBytes, err := os.ReadFile(filepath.Join(outDir, "plan.json"))
	if err != nil {
		t.Fatalf("reading plan.json: %v", err)
	}
	var doc struct {
		Modules []struct {
			Dir     string `json:"dir"`
			NextTag string `json:"nextTag"`
		} `json:"modules"`
	}
	if err := json.Unmarshal(planBytes, &doc); err != nil {
		t.Fatalf("decoding plan.json: %v", err)
	}
	if len(doc.Modules) != 2 {
		t.Fatalf("len(modules) = %d, want 2 (root excluded)", len(doc.Modules))
	}
	for _, m := range doc.Modules {
		if m.Dir == "." {
			t.Fatalf("plan.json includes the root, want publishers only: %+v", doc.Modules)
		}
	}
}

func TestRun_ContinuationPlanPublishers_RootInReleaseListFails(t *testing.T) {
	dir := t.TempDir()
	releaseFile := filepath.Join(dir, "publishers.txt")
	writeLines(t, releaseFile, []string{".", "moda", "modb"})
	tagsFile := filepath.Join(dir, "tags.txt")
	writeLines(t, tagsFile, []string{})
	outDir := filepath.Join(dir, "out")

	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-repo-root", "testdata/linear-chain",
		"-release", releaseFile,
		"-tags", tagsFile,
		"-bump", "patch",
		"-out-dir", outDir,
		"-continuation-plan-publishers",
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected run to fail: the release list includes the root")
	}
}

func TestRun_ContinuationCheckTagConflicts(t *testing.T) {
	dir := t.TempDir()
	releaseFile := filepath.Join(dir, "publishers.txt")
	writeLines(t, releaseFile, []string{"moda", "modb"})
	tagsFile := filepath.Join(dir, "tags.txt")
	writeLines(t, tagsFile, []string{})
	outDir := filepath.Join(dir, "out")
	conflictsFile := filepath.Join(dir, "combined-tags.txt")
	writeLines(t, conflictsFile, []string{"moda/v0.0.1"})

	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-repo-root", "testdata/linear-chain",
		"-release", releaseFile,
		"-tags", tagsFile,
		"-bump", "patch",
		"-out-dir", outDir,
		"-continuation-plan-publishers",
		"-continuation-check-tag-conflicts", conflictsFile,
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected run to fail: moda/v0.0.1 already exists in the combined tag list")
	}
	if !strings.Contains(err.Error(), "moda/v0.0.1") {
		t.Fatalf("error %q does not name the conflicting tag moda/v0.0.1", err.Error())
	}
	if _, statErr := os.Stat(filepath.Join(outDir, "plan.json")); !os.IsNotExist(statErr) {
		t.Fatalf("plan.json must not be written when a tag conflict is refused, stat error = %v", statErr)
	}
}

func TestRun_ContinuationCheckTagConflicts_RequiresPlanPublishers(t *testing.T) {
	dir := t.TempDir()
	conflictsFile := filepath.Join(dir, "combined-tags.txt")
	writeLines(t, conflictsFile, []string{"moda/v0.0.1"})

	var stdout, stderr bytes.Buffer
	err := run([]string{"-continuation-check-tag-conflicts", conflictsFile}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected run to fail: -continuation-check-tag-conflicts without -continuation-plan-publishers")
	}
}

// TestRun_ContinuationCheckRequiredVersion_RealRepository is the manual
// dry run against this repository's actual state the T3 verification
// requires: today the real publishers all require the root at v4.4.3
// (their go.mod "require" line), so wanting a plausible next version like
// v4.5.0 must fail with a clear message naming every mismatched
// publisher. It mirrors TestRun_RealRepository's own repo-root discovery
// and is skipped the same way if the repository root cannot be found.
func TestRun_ContinuationCheckRequiredVersion_RealRepository(t *testing.T) {
	repoRoot := filepath.Join("..", "..", "..")
	if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err != nil {
		t.Skipf("repository root not found at %s: %v", repoRoot, err)
	}
	releaseFile := filepath.Join(repoRoot, "scripts", "ci", "release-modules.txt")

	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-repo-root", repoRoot,
		"-release", releaseFile,
		"-continuation-check-required-version", "v4.5.0",
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected the real publishers (require v4.4.3 today) to fail a v4.5.0 required-version check")
	}
	t.Logf("real-repository dry run failure (expected):\n%s", err.Error())
}
