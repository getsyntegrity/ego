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

// This file holds the pure decision logic the publisher-tag-release
// continuation workflow (issue #159 F4, release-publishers.yml, task T4)
// needs, wired into main.go as the "-continuation-*" flags: SHA/version
// format validation, the required-root-version check, publishers-only
// next-tag computation, and the tag-existence conflict check. Like the
// rest of this package, none of it runs git, touches the network, or
// calls the GitHub API — the workflow gathers that evidence (checked-out
// SHA, `git tag -l`, `git ls-remote --tags origin`) and passes it in.
package main

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// shaPattern and versionPattern are the two formats the continuation
// workflow's inputs must satisfy before anything else runs against them:
// a full, lowercase, 40-character git commit SHA, and an "ego_version"
// shaped exactly like the tags releaseplan itself produces for the root
// module (vX.Y.Z, no pre-release or build metadata — the same shape
// tags.go's semver already assumes, just with the leading "v" tags carry
// and bare semvers don't).
var (
	shaPattern     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	versionPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)
)

// validateSHA reports an error unless sha is exactly 40 lowercase hex
// characters.
func validateSHA(sha string) error {
	if !shaPattern.MatchString(sha) {
		return fmt.Errorf("invalid SHA %q: want exactly 40 lowercase hex characters", sha)
	}
	return nil
}

// validateVersion reports an error unless version matches vX.Y.Z (a
// leading "v", three dot-separated non-negative integers, no pre-release
// or build suffix).
func validateVersion(version string) error {
	if !versionPattern.MatchString(version) {
		return fmt.Errorf("invalid version %q: want vX.Y.Z (semver with a leading \"v\", no pre-release or build suffix)", version)
	}
	return nil
}

// requiredVersion reads the go.mod in dir (an absolute or working-
// directory-relative path, as readGoMod already expects) and returns the
// version its "require" section declares for modPath, and whether such a
// require line exists at all. A "replace" directive for modPath is not
// consulted: it is a local-build-only override, never the version that
// matters for "does this module require modPath at exactly version V".
func requiredVersion(dir, modPath string) (version string, found bool, err error) {
	mod, err := readGoMod(dir)
	if err != nil {
		return "", false, err
	}
	for _, req := range mod.Require {
		if req.Path == modPath {
			return req.Version, true, nil
		}
	}
	return "", false, nil
}

// checkRequiredRootVersion confirms that every module directory in dirs,
// except the root ("." — the root never requires itself) requires
// rootModPath at exactly wantVersion in its own go.mod, reading each
// go.mod directly (not through a pre-built Graph, so this works from any
// dirs list, e.g. a release list an operator or workflow supplies, exactly
// as -release already does elsewhere in this package: dirs are read
// verbatim, no publisher name or path is ever hard-coded here). It
// returns a single error naming every mismatched module and both the
// version it actually requires and the version that was wanted, sorted by
// directory so the message is deterministic.
func checkRequiredRootVersion(repoRoot, rootModPath, wantVersion string, dirs []string) error {
	var problems []string
	for _, dir := range dirs {
		if dir == "." {
			continue
		}
		version, found, err := requiredVersion(filepath.Join(repoRoot, filepath.FromSlash(dir)), rootModPath)
		if err != nil {
			return fmt.Errorf("reading go.mod for %s: %w", dir, err)
		}
		if !found {
			problems = append(problems, fmt.Sprintf("%s: does not require %s at all (wanted %s)", dir, rootModPath, wantVersion))
			continue
		}
		if version != wantVersion {
			problems = append(problems, fmt.Sprintf("%s: requires %s %s, wanted %s", dir, rootModPath, version, wantVersion))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("required-root-version check failed for %d module(s):\n  %s", len(problems), strings.Join(problems, "\n  "))
}

// planPublisherTags computes the next tag for every module directory in
// dirs against g, existing tags and bumpKind, using the same nextTag math
// buildPlan uses for the ordinary full release plan (tags.go). dirs must
// not include the root ("."): by the time this continuation runs, the
// root has already been tagged by the earlier release.yml run, so this
// function neither expects nor produces a plan entry for it — the caller
// (main.go, and ultimately release-publishers.yml) is responsible for
// filtering the release list before calling this.
//
// Unlike buildPlan, this does not topologically order dirs against each
// other or enforce that every in-repository require of a listed module
// (e.g. the already-tagged root) is itself present in dirs: publishers
// don't depend on each other, and the root's already-existing tag is not
// something this check needs to re-validate (that's
// checkRequiredRootVersion's job). Order is preserved from dirs, so a
// deterministic release-list file yields a deterministic plan.
func planPublisherTags(g *Graph, dirs []string, tags []string, bumpKind string) ([]PlanModule, error) {
	modules := make([]PlanModule, 0, len(dirs))
	for _, dir := range dirs {
		if dir == "." {
			return nil, fmt.Errorf("publishers-only tag plan must not include the root (%q); exclude it from the release list, it is tagged separately before this runs", dir)
		}
		mod, ok := g.ByDir(dir)
		if !ok {
			return nil, fmt.Errorf("released module directory %q has no go.mod", dir)
		}
		currentTag, next, err := nextTag(dir, mod.Path, tags, bumpKind)
		if err != nil {
			return nil, err
		}
		modules = append(modules, PlanModule{
			Dir:        dir,
			Path:       mod.Path,
			CurrentTag: currentTag,
			NextTag:    tagPrefix(dir) + next.String(),
		})
	}
	return modules, nil
}

// checkTagConflicts fails, naming every offending tag, if any computed
// module's NextTag already appears in existing — the combined local +
// origin tag list a caller gathers with `git tag -l` and `git ls-remote
// --tags origin` (this function itself never runs git). Order in the
// returned error message follows computed's order, which is deterministic
// given a deterministic release list.
func checkTagConflicts(computed []PlanModule, existing []string) error {
	existingSet := make(map[string]bool, len(existing))
	for _, t := range existing {
		existingSet[t] = true
	}

	var conflicts []string
	for _, m := range computed {
		if existingSet[m.NextTag] {
			conflicts = append(conflicts, m.NextTag)
		}
	}
	if len(conflicts) == 0 {
		return nil
	}
	return fmt.Errorf("tag(s) already exist, refusing to continue: %s", strings.Join(conflicts, ", "))
}

// continuationParams is every flag runContinuation reads, gathered from
// main.go's flag.FlagSet: the shared -repo-root/-release/-tags/-bump/
// -out-dir flags (reused as-is wherever they already mean the right
// thing) plus the "-continuation-*" flags themselves.
type continuationParams struct {
	repoRoot string
	release  string
	tags     string
	bump     string
	outDir   string

	checkSHA          string
	checkVersion      string
	checkRequiredVer  string
	planPublishers    bool
	checkTagConflicts string
	retired           string
	checkNotRetired   string
}

// runContinuation dispatches every "-continuation-*" flag main.go's run
// recognized. Each requested check runs in this fixed order (SHA, then
// version, then required-root-version, then the publishers-only plan and
// its optional tag-conflict check) and runContinuation returns on the
// first failure, so a caller always knows exactly which check failed.
// This function is only ever called when at least one continuation flag
// was set; the default (no new flags) codepath in run never reaches it.
func runContinuation(p continuationParams, stdout, _ io.Writer) error {
	if p.checkSHA != "" {
		if err := validateSHA(p.checkSHA); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "SHA %s: valid\n", p.checkSHA)
	}

	if p.checkVersion != "" {
		if err := validateVersion(p.checkVersion); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "version %s: valid\n", p.checkVersion)
	}

	if p.checkNotRetired != "" {
		if p.retired == "" {
			return errors.New("-continuation-check-not-retired requires -retired")
		}
		retired, err := readRetiredTags(p.retired)
		if err != nil {
			return fmt.Errorf("reading -retired: %w", err)
		}
		if err := checkNotRetired(retired, p.checkNotRetired); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "tag %s: not retired\n", p.checkNotRetired)
	}

	if p.checkRequiredVer != "" {
		if p.release == "" {
			return errors.New("-continuation-check-required-version requires -release")
		}
		repoRoot, err := filepath.Abs(p.repoRoot)
		if err != nil {
			return fmt.Errorf("resolving -repo-root: %w", err)
		}
		graph, err := discoverGraph(repoRoot)
		if err != nil {
			return fmt.Errorf("discovering modules: %w", err)
		}
		root, ok := graph.ByDir(".")
		if !ok {
			return fmt.Errorf("no root module discovered at %s", repoRoot)
		}
		dirs, err := readReleaseList(p.release)
		if err != nil {
			return fmt.Errorf("reading -release: %w", err)
		}
		if err := checkRequiredRootVersion(repoRoot, root.Path, p.checkRequiredVer, dirs); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "required-root-version check: every module in %s requires %s at %s\n", p.release, root.Path, p.checkRequiredVer)
	}

	if p.planPublishers {
		if p.release == "" {
			return errors.New("-continuation-plan-publishers requires -release")
		}
		if p.tags == "" {
			return errors.New("-continuation-plan-publishers requires -tags")
		}
		if p.outDir == "" {
			return errors.New("-continuation-plan-publishers requires -out-dir")
		}
		repoRoot, err := filepath.Abs(p.repoRoot)
		if err != nil {
			return fmt.Errorf("resolving -repo-root: %w", err)
		}
		graph, err := discoverGraph(repoRoot)
		if err != nil {
			return fmt.Errorf("discovering modules: %w", err)
		}
		dirs, err := readReleaseList(p.release)
		if err != nil {
			return fmt.Errorf("reading -release: %w", err)
		}
		tags, err := readLines(p.tags)
		if err != nil {
			return fmt.Errorf("reading -tags: %w", err)
		}
		modules, err := planPublisherTags(graph, dirs, tags, p.bump)
		if err != nil {
			return err
		}
		if err := guardRetired(p.retired, tags, modules); err != nil {
			return err
		}

		if p.checkTagConflicts != "" {
			existing, err := readLines(p.checkTagConflicts)
			if err != nil {
				return fmt.Errorf("reading -continuation-check-tag-conflicts: %w", err)
			}
			if err := checkTagConflicts(modules, existing); err != nil {
				return err
			}
		}

		plan := Plan{Bump: p.bump, Modules: modules}
		summary := renderSummary(plan)
		if err := writeOutputs(p.outDir, plan, summary); err != nil {
			return fmt.Errorf("writing outputs: %w", err)
		}
		fmt.Fprint(stdout, summary)
		return nil
	}

	if p.checkTagConflicts != "" {
		return errors.New("-continuation-check-tag-conflicts requires -continuation-plan-publishers")
	}

	return nil
}
