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

package rules

import (
	"fmt"
	"sort"
	"strings"
)

// Violation is one import edge that breaks a rule and is not covered by any
// baseline entry.
type Violation struct {
	// Importer is the importing package's import path.
	Importer string
	// Import is the forbidden import path.
	Import string
	// Rule is the ID of the Rule that forbids Import for Importer.
	Rule string
	// Reason is a short, rule-specific explanation of why Import broke
	// this rule for Importer (e.g. which allowed-list check it failed, or
	// which forbidden prefix it matched).
	Reason string
}

// RuleStat reports how many packages one rule's Layer matched, so a rule
// that silently checks nothing (e.g. because the root module path passed
// to DefaultRules no longer matches any package in the graph) is visible
// instead of passing vacuously. See Evaluate's zero-match check.
type RuleStat struct {
	// RuleID is the Rule's ID.
	RuleID string
	// Layer is the Rule's Layer name.
	Layer string
	// PackagesMatched is how many packages in the graph this rule's
	// Layer.Match selected.
	PackagesMatched int
}

// Result is the outcome of Evaluate.
type Result struct {
	// Violations are the violations no baseline entry covers, sorted by
	// (Importer, Import, Rule).
	Violations []Violation
	// Stale are the baseline entries that matched no real violation,
	// sorted the same way.
	Stale []BaselineEntry
	// PackagesChecked is how many distinct packages in the graph were
	// matched by at least one rule's Layer.
	PackagesChecked int
	// EdgesChecked is how many distinct (importer, import) pairs were
	// inspected by at least one rule: every non-stdlib import of a
	// package matched by that rule's Layer, whether or not the import
	// turned out to be forbidden, plus every stdlib import that actually
	// matched a rule's StdlibDenylist (design.md §3, I1). A stdlib import
	// that does not match any rule's StdlibDenylist is not counted, the
	// same as before I1: this keeps EdgesChecked's meaning "edges the
	// rules actually had an opinion about" rather than "every import in
	// the graph". A pair inspected by more than one rule (e.g. a
	// publisher/* import checked by both external-adapter-no-runtime and
	// no-cross-module-internal) is counted once.
	EdgesChecked int
	// RuleStats is one entry per rule in the ruleset Evaluate was called
	// with, in that order.
	RuleStats []RuleStat
	// ModulesChecked is how many in-repository modules the graph's module
	// table held (Graph.Modules), the set no-module-cycle checks.
	ModulesChecked int
}

// edgeKey identifies one (importer, import) pair for EdgesChecked's
// dedup set.
type edgeKey struct {
	Importer string
	Import   string
}

// Evaluate checks every production import edge in graph against every rule
// in ruleset, and reconciles the result against baseline:
//
//  1. A rule violation covered by a matching baseline entry is dropped from
//     Result.Violations; the baseline entry is considered used.
//  2. A rule violation not covered by any baseline entry is added to
//     Result.Violations.
//  3. A baseline entry that covered no real violation is added to
//     Result.Stale.
//
// Evaluate first calls ValidateBaseline and returns its error unchanged if
// the baseline itself is malformed; a malformed baseline can hide a real
// violation, so Evaluate refuses to reconcile against it.
//
// Evaluate then rejects a ruleset where any rule's Layer matched zero
// packages in graph: a rule that checks nothing passes vacuously, most
// often because the root module path handed to DefaultRules no longer
// matches the graph (see internal/cmd/archcheck's odd/tasks review notes,
// R1). The error names every such rule by ID and layer.
func Evaluate(graph Graph, ruleset []Rule, baseline []BaselineEntry) (Result, error) {
	if err := ValidateBaseline(baseline, ruleset); err != nil {
		return Result{}, err
	}

	baselineByKey := make(map[baselineKey]BaselineEntry, len(baseline))
	used := make(map[baselineKey]bool, len(baseline))
	for _, entry := range baseline {
		baselineByKey[keyOf(entry.Importer, entry.Import, entry.Rule)] = entry
	}

	var violations []Violation
	packagesChecked := make(map[string]bool)
	edgesChecked := make(map[edgeKey]bool)
	ruleStats := make([]RuleStat, len(ruleset))
	var emptyRules []string

	modules := NewModuleIndex(graph.Modules)
	record := func(v Violation) {
		key := keyOf(v.Importer, v.Import, v.Rule)
		if _, ok := baselineByKey[key]; ok {
			used[key] = true
			return
		}
		violations = append(violations, v)
	}

	for i, rule := range ruleset {
		matched := 0
		if rule.CheckModules != nil {
			matched = len(graph.Modules)
			if matched > 0 {
				for _, v := range rule.CheckModules(graph.Modules) {
					v.Rule = rule.ID
					record(v)
				}
			}
			ruleStats[i] = RuleStat{RuleID: rule.ID, Layer: rule.Layer.Name, PackagesMatched: matched}
			if matched == 0 {
				emptyRules = append(emptyRules, fmt.Sprintf("%s (layer %q)", rule.ID, rule.Layer.Name))
			}
			continue
		}
		for _, pkg := range graph.Packages {
			if !rule.Layer.Match(pkg) {
				continue
			}
			if rule.ForbidsEdge != nil {
				if len(graph.Modules) == 0 {
					// No module table: this rule cannot place anything, so it
					// matches nothing and the zero-match check below fails
					// the run.
					continue
				}
				if modules.Owner(pkg.ImportPath) == "" {
					return Result{}, fmt.Errorf("rule %s: package %s belongs to no module in the graph's module table; the loaders and the module table disagree", rule.ID, pkg.ImportPath)
				}
			}
			matched++
			packagesChecked[pkg.ImportPath] = true
			for _, imp := range pkg.Imports {
				if IsStdlib(imp) {
					prefix, denied := matchStdlibDenylist(rule.StdlibDenylist, imp)
					if !denied {
						continue
					}
					edgesChecked[edgeKey{Importer: pkg.ImportPath, Import: imp}] = true
					key := keyOf(pkg.ImportPath, imp, rule.ID)
					if _, ok := baselineByKey[key]; ok {
						used[key] = true
						continue
					}
					violations = append(violations, Violation{
						Importer: pkg.ImportPath,
						Import:   imp,
						Rule:     rule.ID,
						Reason:   reasonForStdlib(rule, imp, prefix),
					})
					continue
				}
				edgesChecked[edgeKey{Importer: pkg.ImportPath, Import: imp}] = true
				forbidden, reason := decide(rule, modules, pkg.ImportPath, imp)
				if !forbidden {
					continue
				}
				record(Violation{
					Importer: pkg.ImportPath,
					Import:   imp,
					Rule:     rule.ID,
					Reason:   reason,
				})
			}
		}
		ruleStats[i] = RuleStat{RuleID: rule.ID, Layer: rule.Layer.Name, PackagesMatched: matched}
		if matched == 0 {
			emptyRules = append(emptyRules, fmt.Sprintf("%s (layer %q)", rule.ID, rule.Layer.Name))
		}
	}

	if len(emptyRules) > 0 {
		return Result{}, fmt.Errorf("rule(s) matched zero packages in the graph — check the root module path and the layer definitions: %s", strings.Join(emptyRules, "; "))
	}

	var stale []BaselineEntry
	for _, entry := range baseline {
		key := keyOf(entry.Importer, entry.Import, entry.Rule)
		if !used[key] {
			stale = append(stale, entry)
		}
	}

	sortViolations(violations)
	sortBaselineEntries(stale)

	return Result{
		Violations:      violations,
		Stale:           stale,
		PackagesChecked: len(packagesChecked),
		EdgesChecked:    len(edgesChecked),
		RuleStats:       ruleStats,
		ModulesChecked:  len(graph.Modules),
	}, nil
}

// decide applies rule to one non-stdlib import edge: through ForbidsEdge
// for a module-aware rule, otherwise through Forbids and reasonFor.
func decide(rule Rule, modules ModuleIndex, importer, importPath string) (bool, string) {
	if rule.ForbidsEdge != nil {
		return rule.ForbidsEdge(modules, importer, importPath)
	}
	if !rule.Forbids(importPath) {
		return false, ""
	}
	return true, reasonFor(rule, importPath)
}

// reasonFor gives a short, human explanation of why a rule forbids an
// import, phrased by the rule's semantics.
func reasonFor(rule Rule, importPath string) string {
	if rule.Reason != nil {
		return rule.Reason(importPath)
	}
	if rule.Semantics == Allowlist {
		return "not on the " + rule.Layer.Name + " allowlist"
	}
	return "matches a forbidden import for " + rule.Layer.Name
}

// matchStdlibDenylist reports whether importPath (already known to be
// stdlib) matches one of rule's forbidden prefixes, matched by whole path
// segment (hasPathOrSubpath), and returns the matched prefix. denylist is
// typically a Rule.StdlibDenylist; nil or empty means nothing is denied.
func matchStdlibDenylist(denylist []string, importPath string) (prefix string, denied bool) {
	for _, p := range denylist {
		if hasPathOrSubpath(importPath, p) {
			return p, true
		}
	}
	return "", false
}

// reasonForStdlib explains why a stdlib import broke rule's StdlibDenylist,
// naming both the forbidden import and the denylist entry it matched (I1,
// design.md §3).
func reasonForStdlib(rule Rule, importPath, matchedPrefix string) string {
	return fmt.Sprintf("imports the standard-library package %s, forbidden for %s (matches stdlib denylist entry %s): contracts must not depend on transport or database packages", importPath, rule.Layer.Name, matchedPrefix)
}

func sortViolations(vs []Violation) {
	sort.Slice(vs, func(i, j int) bool {
		if vs[i].Importer != vs[j].Importer {
			return vs[i].Importer < vs[j].Importer
		}
		if vs[i].Import != vs[j].Import {
			return vs[i].Import < vs[j].Import
		}
		return vs[i].Rule < vs[j].Rule
	})
}

func sortBaselineEntries(es []BaselineEntry) {
	sort.Slice(es, func(i, j int) bool {
		if es[i].Importer != es[j].Importer {
			return es[i].Importer < es[j].Importer
		}
		if es[i].Import != es[j].Import {
			return es[i].Import < es[j].Import
		}
		return es[i].Rule < es[j].Rule
	})
}
