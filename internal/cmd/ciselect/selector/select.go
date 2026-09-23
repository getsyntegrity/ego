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

package selector

import (
	"fmt"
	"sort"
)

// Mode is the selector's overall decision for a run.
type Mode string

const (
	// ModeFull means every included package must be tested and covered:
	// either explicitly requested (-all), forced by a high-impact path,
	// or the natural result of the affected computation covering
	// everything anyway.
	ModeFull Mode = "full"
	// ModeAffected means a proper, non-empty, non-total subset of the
	// included packages must be tested and covered.
	ModeAffected Mode = "affected"
	// ModeNone means no package needs testing: every changed file was
	// documentation/governance content or belonged to a satellite
	// module.
	ModeNone Mode = "none"
)

// Options configures a Select call.
type Options struct {
	// All, when true, selects the full suite unconditionally and without
	// reading or classifying any changed files.
	All bool
	// Reason is optional extra context to record in the summary, e.g.
	// "selector failed; full-suite fallback" when the caller is retrying
	// with -all after a prior selector run failed.
	Reason string
	// SatelliteDirs are module-relative, forward-slash directories known
	// to contain their own go.mod (and therefore fall outside this
	// module's package graph).
	SatelliteDirs []string
}

// Result is the outcome of a Select call.
type Result struct {
	// Mode is the overall decision.
	Mode Mode
	// Included is the sorted, full set of tested-and-covered packages
	// (every module package minus the excluded segments). It never
	// varies with Mode; it is always the coverage denominator.
	Included []string
	// Selected is the sorted set of packages to actually test and cover
	// for this run. It equals Included when Mode == ModeFull, and is
	// empty when Mode == ModeNone.
	Selected []string
	// Reasons explains the Mode decision, in the order the reasons were
	// found.
	Reasons []string
	// ExtraReason is Options.Reason, carried through unchanged for the
	// summary.
	ExtraReason string
	// Changed is one entry per input changed path, in input order, with
	// its classification.
	Changed []ChangedFile
}

// Select decides which packages g's changes must test and cover.
//
// changed is the list of changed files exactly as given by the caller
// (repo-relative paths are fine; Select classifies them against g's
// directories and the well-known high-impact paths without needing the
// filesystem). It is ignored entirely when opts.All is set.
func Select(g Graph, changed []string, opts Options) Result {
	included := Included(g)
	res := Result{
		Included:    included,
		ExtraReason: opts.Reason,
	}

	if opts.All {
		res.Mode = ModeFull
		res.Selected = included
		res.Reasons = []string{"-all requested: full suite selected"}
		return res
	}

	if len(changed) == 0 {
		res.Mode = ModeFull
		res.Selected = included
		res.Reasons = []string{"no changed files detected"}
		return res
	}

	dirIndex := buildDirIndex(g)

	var fallbackReasons []string
	changedPkgs := map[string]bool{}
	hasPackageChange := false

	cfs := make([]ChangedFile, 0, len(changed))
	for _, c := range changed {
		cf := classify(c, opts.SatelliteDirs, dirIndex)
		cfs = append(cfs, cf)
		switch cf.Class {
		case ClassFullFallback, ClassUnknown:
			fallbackReasons = append(fallbackReasons, cf.Reason)
		case ClassPackage:
			hasPackageChange = true
			changedPkgs[cf.Package] = true
		case ClassNoTest, ClassSatellite:
			// Needs no test run on its own.
		}
	}
	res.Changed = cfs

	if len(fallbackReasons) > 0 {
		res.Mode = ModeFull
		res.Selected = included
		res.Reasons = fallbackReasons
		return res
	}

	if !hasPackageChange {
		res.Mode = ModeNone
		res.Reasons = []string{"only documentation/governance or satellite-module changes"}
		return res
	}

	includedSet := make(map[string]bool, len(included))
	for _, p := range included {
		includedSet[p] = true
	}

	affected := affectedByChange(g, changedPkgs)
	var selected []string
	for p := range affected {
		if includedSet[p] {
			selected = append(selected, p)
		}
	}
	sort.Strings(selected)

	switch {
	case len(selected) == 0:
		// Fail safe: a package changed, but nothing tested-and-covered
		// is reachable from it (e.g. only an excluded, leaf, unimported
		// generated package changed). Never silently select nothing.
		res.Mode = ModeFull
		res.Selected = included
		res.Reasons = []string{"selection unexpectedly empty: falling back to full suite"}
	case len(selected) == len(included):
		res.Mode = ModeFull
		res.Selected = included
		res.Reasons = []string{"affected set equals the full included suite"}
	default:
		res.Mode = ModeAffected
		res.Selected = selected
		res.Reasons = []string{fmt.Sprintf("affected by %d changed package(s)", len(changedPkgs))}
	}
	return res
}
