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
	// module's package graph). The directory of every nested entry of
	// Modules is added to it automatically.
	SatelliteDirs []string
	// Modules describes every Go module discovered in the repository,
	// the root module (Dir ".") included, with its in-repository
	// requirements. It drives module selection (see modulegraph.go),
	// which also feeds dependency changes back into the root package
	// lane.
	Modules []ModuleInfo
	// GoMods is set when the caller knows the base revision (-base): for
	// each changed nested go.mod (module-relative path), whether it
	// exists at base and at head. A go.mod present at both was edited and
	// has no module boundary effect. When GoMods is nil, or a changed
	// nested go.mod is missing from it, the change is treated as a
	// boundary change: the conservative behavior.
	GoMods map[string]GoModPresence
}

// GoModPresence records whether a go.mod exists at the base revision and
// at head.
type GoModPresence struct {
	AtBase bool
	AtHead bool
}

// ModuleInfo is one discovered Go module. All fields come from its go.mod
// (`go mod edit -json`) and from parsing its files; nothing is
// hand-listed.
type ModuleInfo struct {
	// Dir is the module's directory relative to the repository root,
	// using forward slashes; "." for the root module.
	Dir string
	// Path is the module path declared in its go.mod.
	Path string
	// Deps are the in-repository module paths this module requires AND
	// resolves from the working tree (a replace to that module's local
	// directory). These are the module graph's edges.
	Deps []string
	// Pinned are the in-repository modules this module requires at a
	// published version with no local replace, as "path@version". They
	// do not compile against the working tree, so they are reported,
	// never followed as edges.
	Pinned []string
	// Imports are the in-repository import paths of other modules found
	// by go/parser in this module's own files: all files, tests included,
	// build tags ignored. They filter a requirement edge (a consumer is
	// selected only if it imports an affected package of the module it
	// requires), never create one.
	Imports []string
}

// ModuleSelection is one nested module selected to be built, vetted,
// linted and tested for this run, and why.
type ModuleSelection struct {
	// Dir is the module's directory, exactly as given in Options.Modules.
	Dir string
	// Reason explains why this module was selected.
	Reason string
	// Chain is the path of module directories that reached this module,
	// from the module itself back to a changed one (see ModulePlan).
	Chain []string
}

// ModulePlan is the selection decision for one discovered module, selected
// or not. The plan lists every module, so it can be reused by a pipeline
// other than GitHub Actions.
type ModulePlan struct {
	// Dir is the module's directory ("." for the root module).
	Dir string
	// Path is the module path.
	Path string
	// Selected reports whether the module must be verified for this run.
	Selected bool
	// Reason explains the decision, selected or not.
	Reason string
	// Chain is set for a selected module: the module directories from
	// this module back to the changed module that reached it, e.g.
	// ["it", "adapter/a", "port"] for "it ← adapter/a ← port". A module
	// that changed itself has a one-element chain.
	Chain []string
}

// Result is the outcome of a Select call.
type Result struct {
	// Mode is the root package lane's overall decision.
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
	// Modules is the sorted-by-Dir set of nested modules (never the
	// root) selected for this run, and why.
	Modules []ModuleSelection
	// Plan is the decision for every module in Options.Modules, root
	// first, then sorted by Dir.
	Plan []ModulePlan
	// Global reports that a global change (or -all, or an empty changed
	// list) selected every module and the full root lane.
	Global bool
}

// Select decides which packages g's changes must test and cover, and which
// modules (opts.Modules) must be built, vetted, linted and tested.
//
// changed is the list of changed files exactly as given by the caller
// (repo-relative paths are fine; Select classifies them against g's
// directories and the well-known high-impact paths without needing the
// filesystem). It is ignored entirely when opts.All is set.
func Select(g Graph, changed []string, opts Options) Result {
	return selectWithModules(g, changed, opts)
}

// rootInputs is what the module graph feeds into the root package lane.
type rootInputs struct {
	// satelliteDirs are the nested module directories the classifier
	// treats as satellite.
	satelliteDirs []string
	// forceFull are reasons that force the root lane to full: a global
	// change, a module boundary change carved out of the root, or a
	// changed go.mod/go.sum of a module the root requires.
	forceFull []string
	// seeds are root packages to treat as changed because they import a
	// changed module the root requires.
	seeds map[string]bool
	// seedReasons explain the seeds.
	seedReasons []string
}

// selectRoot is the root package Mode/Selected/Included decision. With
// empty inputs it is exactly Select's original root behavior.
func selectRoot(g Graph, changed []string, opts Options, in rootInputs) Result {
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

	fallbackReasons := append([]string{}, in.forceFull...)
	changedPkgs := map[string]bool{}
	hasPackageChange := false

	cfs := make([]ChangedFile, 0, len(changed))
	for _, c := range changed {
		cf := classify(c, in.satelliteDirs, dirIndex)
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

	if !hasPackageChange && len(in.seeds) == 0 {
		res.Mode = ModeNone
		res.Reasons = []string{"only documentation/governance or satellite-module changes"}
		return res
	}
	for p := range in.seeds {
		changedPkgs[p] = true
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
		res.Reasons = append([]string{"affected set equals the full included suite"}, in.seedReasons...)
	default:
		res.Mode = ModeAffected
		res.Selected = selected
		res.Reasons = append([]string{fmt.Sprintf("affected by %d changed package(s)", len(changedPkgs))}, in.seedReasons...)
	}
	return res
}
