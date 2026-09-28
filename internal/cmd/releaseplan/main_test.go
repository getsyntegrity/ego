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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLines(t *testing.T, path string, lines []string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func TestRun_WritesOutputsAndStdout(t *testing.T) {
	dir := t.TempDir()
	releaseFile := filepath.Join(dir, "release.txt")
	writeLines(t, releaseFile, []string{"# comment", "", ".", "moda", "modb"})
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
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run: %v (stderr: %s)", err, stderr.String())
	}

	planBytes, err := os.ReadFile(filepath.Join(outDir, "plan.json"))
	if err != nil {
		t.Fatalf("reading plan.json: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(planBytes, &doc); err != nil {
		t.Fatalf("plan.json is not valid JSON: %v\n%s", err, planBytes)
	}
	modules, ok := doc["modules"].([]any)
	if !ok || len(modules) != 3 {
		t.Fatalf("plan.json modules = %v, want 3 entries", doc["modules"])
	}

	summaryBytes, err := os.ReadFile(filepath.Join(outDir, "summary.md"))
	if err != nil {
		t.Fatalf("reading summary.md: %v", err)
	}
	if stdout.String() != string(summaryBytes) {
		t.Fatal("stdout does not match the written summary.md")
	}
}

func TestRun_RefusalIsNonZero(t *testing.T) {
	dir := t.TempDir()
	releaseFile := filepath.Join(dir, "release.txt")
	writeLines(t, releaseFile, []string{".", "pub"})
	tagsFile := filepath.Join(dir, "tags.txt")
	writeLines(t, tagsFile, []string{})
	outDir := filepath.Join(dir, "out")

	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-repo-root", "testdata/tagscheme",
		"-release", releaseFile,
		"-tags", tagsFile,
		"-bump", "major",
		"-out-dir", outDir,
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected run to return a non-nil error on refusal")
	}
}

func TestRun_MissingRequiredFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-repo-root", "."}, &stdout, &stderr); err == nil {
		t.Fatal("expected an error when -release, -tags and -out-dir are missing")
	}
}

// TestRun_RealRepository exercises releaseplan against this actual
// repository (internal/cmd/releaseplan/../../.. is the repo root),
// mirroring the feature document's "Real run on this repository" check:
// a patch bump succeeds and orders the root before every publisher; a
// major bump is refused because the root's path ends in /v4 (an untagged
// publisher's first major, v1.0.0, would be legal on its own).
func TestRun_RealRepository(t *testing.T) {
	repoRoot := filepath.Join("..", "..", "..")
	if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err != nil {
		t.Skipf("repository root not found at %s: %v", repoRoot, err)
	}
	releaseFile := filepath.Join(repoRoot, "scripts", "ci", "release-modules.txt")

	t.Run("patch bump succeeds, root before publishers", func(t *testing.T) {
		dir := t.TempDir()
		tagsFile := filepath.Join(dir, "tags.txt")
		writeLines(t, tagsFile, []string{})
		outDir := filepath.Join(dir, "out")

		var stdout, stderr bytes.Buffer
		if err := run([]string{
			"-repo-root", repoRoot,
			"-release", releaseFile,
			"-tags", tagsFile,
			"-bump", "patch",
			"-out-dir", outDir,
		}, &stdout, &stderr); err != nil {
			t.Fatalf("run: %v (stderr: %s)", err, stderr.String())
		}

		planBytes, err := os.ReadFile(filepath.Join(outDir, "plan.json"))
		if err != nil {
			t.Fatalf("reading plan.json: %v", err)
		}
		var doc struct {
			Modules []struct {
				Dir string `json:"dir"`
			} `json:"modules"`
		}
		if err := json.Unmarshal(planBytes, &doc); err != nil {
			t.Fatalf("decoding plan.json: %v", err)
		}
		// The hard-coded 5 is intentional (PR #169 review finding 4): this
		// test must fail loudly the moment scripts/ci/release-modules.txt
		// gains or loses an entry, rather than silently keep passing
		// against whatever count the file happens to hold that day.
		if len(doc.Modules) != 5 {
			t.Fatalf("len(modules) = %d, want 5 (root + 4 publishers)", len(doc.Modules))
		}
		rootIdx, lastPublisherIdx := -1, -1
		for i, m := range doc.Modules {
			if m.Dir == "." {
				rootIdx = i
			}
			if strings.HasPrefix(m.Dir, "publisher/") {
				lastPublisherIdx = i
			}
		}
		if rootIdx == -1 || lastPublisherIdx == -1 || rootIdx > lastPublisherIdx {
			t.Fatalf("root (index %d) is not before the publishers (last index %d): %v", rootIdx, lastPublisherIdx, doc.Modules)
		}
	})

	t.Run("major bump refuses root and every publisher", func(t *testing.T) {
		dir := t.TempDir()
		tagsFile := filepath.Join(dir, "tags.txt")
		writeLines(t, tagsFile, []string{})
		outDir := filepath.Join(dir, "out")

		var stdout, stderr bytes.Buffer
		err := run([]string{
			"-repo-root", repoRoot,
			"-release", releaseFile,
			"-tags", tagsFile,
			"-bump", "major",
			"-out-dir", outDir,
		}, &stdout, &stderr)
		if err == nil {
			t.Fatal("expected a major bump to be refused for the root or a publisher")
		}
	})
}

// TestWriteOutputs_SecondWriteFailure_LeavesNoPartialOutput exercises the
// all-or-nothing contract writeOutputs must hold (PR #169 review finding
// 1): plan.json and summary.md are written to temp files in -out-dir and
// renamed into place only after both writes succeed, plan.json first
// (deterministic order); on any error, the temps are removed and neither
// final file is left behind. writeTempFile is the injectable seam: the
// real filesystem write is overridden here to fail on its second call
// (summary.md), without needing a read-only directory to force the
// failure at a specific point.
func TestWriteOutputs_SecondWriteFailure_LeavesNoPartialOutput(t *testing.T) {
	dir := t.TempDir()

	calls := 0
	orig := writeTempFile
	defer func() { writeTempFile = orig }()
	writeTempFile = func(outDir, finalName string, content []byte) (string, error) {
		calls++
		if calls == 2 {
			return "", fmt.Errorf("injected failure writing %s", finalName)
		}
		return orig(outDir, finalName, content)
	}

	plan := Plan{Bump: "patch", Modules: []PlanModule{{Dir: ".", Path: "example.com/root", NextTag: "v0.0.1"}}}
	err := writeOutputs(dir, plan, "# Release plan\n")
	if err == nil {
		t.Fatal("expected writeOutputs to return an error when the second write fails")
	}
	if calls != 2 {
		t.Fatalf("writeTempFile called %d times, want 2 (plan.json then summary.md)", calls)
	}

	if _, statErr := os.Stat(filepath.Join(dir, "plan.json")); !os.IsNotExist(statErr) {
		t.Fatalf("plan.json must not exist after a failed write, stat error = %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "summary.md")); !os.IsNotExist(statErr) {
		t.Fatalf("summary.md must not exist after a failed write, stat error = %v", statErr)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading out-dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("out-dir must be empty after a failed write (no leftover temp files), got %v", entries)
	}
}

// TestWriteOutputs_RenameFailure_UndoesTheFirstFile covers the other half
// of the all-or-nothing contract: if renaming plan.json into place
// succeeds but renaming summary.md fails, plan.json must be removed too,
// so a caller never sees plan.json without summary.md.
func TestWriteOutputs_RenameFailure_UndoesTheFirstFile(t *testing.T) {
	dir := t.TempDir()

	// summary.md as a pre-existing directory makes os.Rename fail with
	// ENOTEMPTY/EISDIR when writeOutputs tries to rename the summary.md
	// temp file over it.
	if err := os.Mkdir(filepath.Join(dir, "summary.md"), 0o755); err != nil {
		t.Fatalf("seeding summary.md as a directory: %v", err)
	}

	plan := Plan{Bump: "patch"}
	err := writeOutputs(dir, plan, "# Release plan\n")
	if err == nil {
		t.Fatal("expected writeOutputs to return an error when the summary.md rename fails")
	}

	if _, statErr := os.Stat(filepath.Join(dir, "plan.json")); !os.IsNotExist(statErr) {
		t.Fatalf("plan.json must be undone when the summary.md rename fails, stat error = %v", statErr)
	}
}
