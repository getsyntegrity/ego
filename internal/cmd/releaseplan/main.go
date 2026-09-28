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

	// Publisher-tag-release continuation flags (issue #159 F4, task T3):
	// none of these change default behavior when unset, and each reuses
	// -repo-root/-release/-tags/-bump/-out-dir above wherever those already
	// mean the right thing, rather than inventing parallel flags.
	continuationCheckSHAFlag := fs.String("continuation-check-sha", "", "continuation mode: validate that this is exactly 40 lowercase hex characters, then exit (no other flags required)")
	continuationCheckVersionFlag := fs.String("continuation-check-version", "", "continuation mode: validate that this matches vX.Y.Z (semver, leading \"v\", no pre-release/build suffix), then exit (no other flags required)")
	continuationCheckRequiredVersionFlag := fs.String("continuation-check-required-version", "", "continuation mode: for every directory in -release except the root ('.'), confirm its go.mod \"require\" line for the root module is exactly this version; reuses -repo-root and -release")
	continuationPlanPublishersFlag := fs.Bool("continuation-plan-publishers", false, "continuation mode: compute next tags only for the directories in -release, which must exclude the root ('.') — it is tagged separately before this runs; reuses -repo-root, -release, -tags, -bump and -out-dir, writing plan.json/summary.md exactly like the default full release plan")
	continuationCheckTagConflictsFlag := fs.String("continuation-check-tag-conflicts", "", "continuation mode: path to a combined local+origin tag list (e.g. output of \"git tag -l\" plus \"git ls-remote --tags origin\"); fails, naming every conflict, if any tag -continuation-plan-publishers computed already exists there. Requires -continuation-plan-publishers")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *continuationCheckSHAFlag != "" || *continuationCheckVersionFlag != "" || *continuationCheckRequiredVersionFlag != "" ||
		*continuationPlanPublishersFlag || *continuationCheckTagConflictsFlag != "" {
		return runContinuation(continuationParams{
			repoRoot:          *repoRootFlag,
			release:           *releaseFlag,
			tags:              *tagsFlag,
			bump:              *bumpFlag,
			outDir:            *outDirFlag,
			checkSHA:          *continuationCheckSHAFlag,
			checkVersion:      *continuationCheckVersionFlag,
			checkRequiredVer:  *continuationCheckRequiredVersionFlag,
			planPublishers:    *continuationPlanPublishersFlag,
			checkTagConflicts: *continuationCheckTagConflictsFlag,
		}, stdout, stderr)
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
// if necessary, all-or-nothing: each is first written to a temp file in
// outDir, and only once both writes succeed are they renamed into place,
// plan.json first (a fixed, deterministic order — not the random order a
// map range would give). If any write or rename fails, every temp file
// and any already-renamed final file is removed, so a caller never
// observes plan.json without summary.md or vice versa.
func writeOutputs(outDir string, plan Plan, summary string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	planJSON, err := renderPlanJSON(plan)
	if err != nil {
		return fmt.Errorf("encoding plan.json: %w", err)
	}

	planTmp, err := writeTempFile(outDir, "plan.json", []byte(planJSON))
	if err != nil {
		return fmt.Errorf("writing plan.json: %w", err)
	}
	summaryTmp, err := writeTempFile(outDir, "summary.md", []byte(summary))
	if err != nil {
		_ = os.Remove(planTmp)
		return fmt.Errorf("writing summary.md: %w", err)
	}

	planFinal := filepath.Join(outDir, "plan.json")
	summaryFinal := filepath.Join(outDir, "summary.md")

	if err := os.Rename(planTmp, planFinal); err != nil {
		_ = os.Remove(planTmp)
		_ = os.Remove(summaryTmp)
		return fmt.Errorf("finalizing plan.json: %w", err)
	}
	if err := os.Rename(summaryTmp, summaryFinal); err != nil {
		_ = os.Remove(planFinal)
		_ = os.Remove(summaryTmp)
		return fmt.Errorf("finalizing summary.md: %w", err)
	}
	return nil
}

// writeTempFile writes content to a new temp file in dir named after
// finalName (e.g. "plan.json.tmp-<random>") and returns its path, or
// removes the temp file and returns an error. It is a package-level
// variable so tests can inject a failure on a specific call (e.g. the
// second one) without relying on filesystem permissions to force the
// failure at a precise point.
var writeTempFile = func(dir, finalName string, content []byte) (string, error) {
	f, err := os.CreateTemp(dir, finalName+".tmp-*")
	if err != nil {
		return "", err
	}
	tmpName := f.Name()
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	return tmpName, nil
}
