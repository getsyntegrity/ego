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

// Command releaseplan computes, without side effects, the release order
// and next tag of every released Go module in this repository: no git, no
// network, no GitHub API, and it never creates a tag. Its output depends
// only on the repository's go.mod files and its explicit inputs (the
// released-module list, existing tags, the requested version bump) — see
// docs/ci.md and openspec/changes/ego-arch-006/design.md §3 (D2 (a), D3,
// D5) for the release-order and tag-naming rules it implements.
//
// build.yml runs it as a dry run on every push to main and writes its
// summary to the job summary; nothing publishes or tags from that run.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "releaseplan: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("releaseplan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoRootFlag := fs.String("repo-root", ".", "repository root to scan for go.mod files")
	releaseFlag := fs.String("release", "", "path to the file listing released module directories, one per line, '.' for the root, '#' comments allowed (required)")
	tagsFlag := fs.String("tags", "", "path to a file of existing tags, one per line; may be empty (required)")
	bumpFlag := fs.String("bump", "patch", "version bump to apply: patch, minor or major")
	outDirFlag := fs.String("out-dir", "", "directory to write plan.json and summary.md into (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *releaseFlag == "" {
		return errors.New("-release is required")
	}
	if *tagsFlag == "" {
		return errors.New("-tags is required")
	}
	if *outDirFlag == "" {
		return errors.New("-out-dir is required")
	}

	repoRoot, err := filepath.Abs(*repoRootFlag)
	if err != nil {
		return fmt.Errorf("resolving -repo-root: %w", err)
	}

	graph, err := discoverGraph(repoRoot)
	if err != nil {
		return fmt.Errorf("discovering modules: %w", err)
	}

	releaseDirs, err := readReleaseList(*releaseFlag)
	if err != nil {
		return fmt.Errorf("reading -release: %w", err)
	}
	tags, err := readLines(*tagsFlag)
	if err != nil {
		return fmt.Errorf("reading -tags: %w", err)
	}

	plan, err := buildPlan(graph, releaseDirs, tags, *bumpFlag)
	if err != nil {
		return err
	}

	summary := renderSummary(plan)
	if err := writeOutputs(*outDirFlag, plan, summary); err != nil {
		return fmt.Errorf("writing outputs: %w", err)
	}

	fmt.Fprint(stdout, summary)
	return nil
}

// readReleaseList reads the released-module directory list at path: one
// repository-relative directory per line ("." for the root), blank lines
// and "#" comments skipped.
func readReleaseList(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var dirs []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		dirs = append(dirs, filepath.ToSlash(filepath.Clean(line)))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return dirs, nil
}

// readLines reads path as newline-separated entries, trimmed, blank
// lines skipped. Used for -tags: every existing tag in the repository,
// one per line.
func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

// writeOutputs writes plan.json and summary.md into outDir, creating it
// if necessary.
func writeOutputs(outDir string, plan Plan, summary string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	planJSON, err := renderPlanJSON(plan)
	if err != nil {
		return fmt.Errorf("encoding plan.json: %w", err)
	}
	files := map[string]string{
		"plan.json":  planJSON,
		"summary.md": summary,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(outDir, name), []byte(content), 0o600); err != nil {
			return fmt.Errorf("writing %s: %w", name, err)
		}
	}
	return nil
}
