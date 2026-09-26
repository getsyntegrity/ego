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

import "sort"

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

// Result is the outcome of Evaluate.
type Result struct {
	// Violations are the violations no baseline entry covers, sorted by
	// (Importer, Import, Rule).
	Violations []Violation
	// Stale are the baseline entries that matched no real violation,
	// sorted the same way.
	Stale []BaselineEntry
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
	for _, rule := range ruleset {
		for _, pkg := range graph.Packages {
			if !rule.Layer.Match(pkg) {
				continue
			}
			for _, imp := range pkg.Imports {
				if IsStdlib(imp) {
					continue
				}
				if !rule.Forbids(imp) {
					continue
				}
				key := keyOf(pkg.ImportPath, imp, rule.ID)
				if _, ok := baselineByKey[key]; ok {
					used[key] = true
					continue
				}
				violations = append(violations, Violation{
					Importer: pkg.ImportPath,
					Import:   imp,
					Rule:     rule.ID,
					Reason:   reasonFor(rule),
				})
			}
		}
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

	return Result{Violations: violations, Stale: stale}, nil
}

// reasonFor gives a short, human explanation of why a rule forbids an
// import, phrased by the rule's semantics.
func reasonFor(rule Rule) string {
	if rule.Semantics == Allowlist {
		return "not on the " + rule.Layer.Name + " allowlist"
	}
	return "matches a forbidden import for " + rule.Layer.Name
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
