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

// This file pins the first publisher release (#134): with only the root tag
// v4.0.0 published and no publisher tag yet, a run of
// .github/workflows/release-publishers.yml that keeps its default "bump"
// input must plan publisher/<name>/v0.1.0 for all four publishers — never
// v0.0.1, which is what the earlier "patch" default produced.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// workflowInputDefault returns the "default:" value of the workflow_dispatch
// input named input in the workflow file at path. It reads the file line by
// line instead of parsing YAML, so the root module gains no YAML dependency;
// the input block is the lines indented deeper than "<input>:".
func workflowInputDefault(t *testing.T, path, input string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	inBlock := false
	blockIndent := 0
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if !inBlock {
			if trimmed == input+":" {
				inBlock = true
				blockIndent = indent
			}
			continue
		}
		if trimmed == "" {
			continue
		}
		if indent <= blockIndent {
			break
		}
		if value, ok := strings.CutPrefix(trimmed, "default:"); ok {
			return strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	t.Fatalf("no default for input %q in %s", input, path)
	return ""
}

func TestFirstPublisherRelease_DefaultBumpPlansV010ForEveryPublisher(t *testing.T) {
	repoRoot := filepath.Join("..", "..", "..")
	if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err != nil {
		t.Skipf("repository root not found at %s: %v", repoRoot, err)
	}

	bump := workflowInputDefault(t, filepath.Join(repoRoot, ".github", "workflows", "release-publishers.yml"), "bump")

	// The release list the workflow feeds releaseplan: release-modules.txt
	// without the root, which release.yml already tagged.
	releaseBytes, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "ci", "release-modules.txt"))
	if err != nil {
		t.Fatalf("reading release-modules.txt: %v", err)
	}
	var publishers []string
	for _, line := range strings.Split(string(releaseBytes), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "." || strings.HasPrefix(line, "#") {
			continue
		}
		publishers = append(publishers, line)
	}

	dir := t.TempDir()
	releaseFile := filepath.Join(dir, "release-publishers-only.txt")
	writeLines(t, releaseFile, publishers)
	// Only the published root tag: this is the first publisher release.
	tagsFile := filepath.Join(dir, "tags.txt")
	writeLines(t, tagsFile, []string{"v4.0.0"})
	outDir := filepath.Join(dir, "plan")

	var stdout, stderr bytes.Buffer
	if err := run([]string{
		"-repo-root", repoRoot,
		"-release", releaseFile,
		"-tags", tagsFile,
		"-bump", bump,
		"-continuation-plan-publishers",
		"-continuation-check-tag-conflicts", tagsFile,
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
			Dir     string `json:"dir"`
			NextTag string `json:"nextTag"`
		} `json:"modules"`
	}
	if err := json.Unmarshal(planBytes, &doc); err != nil {
		t.Fatalf("decoding plan.json: %v", err)
	}

	var got []string
	for _, m := range doc.Modules {
		got = append(got, m.NextTag)
	}
	sort.Strings(got)
	want := []string{
		"publisher/kafka/v0.1.0",
		"publisher/nats/v0.1.0",
		"publisher/pulsar/v0.1.0",
		"publisher/websocket/v0.1.0",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("first publisher release with default bump %q plans %v, want %v", bump, got, want)
	}
}
