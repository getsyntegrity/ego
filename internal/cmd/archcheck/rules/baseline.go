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
	"errors"
	"fmt"
	"strings"
)

// BaselineEntry records one known, not-yet-fixed violation. Every entry
// must justify itself: an owner to ask, why the violation exists today, and
// what removes it. An entry that stops matching a real violation is stale
// (Evaluate reports it) so the baseline can only shrink, never silently
// keep dead weight.
type BaselineEntry struct {
	// Importer is the importing package's import path, exactly as it
	// appears in the violation it baselines.
	Importer string
	// Import is the forbidden import path, exactly as it appears in the
	// violation it baselines.
	Import string
	// Rule is the ID of the Rule this entry baselines (must exist in the
	// ruleset passed to Evaluate/ValidateBaseline).
	Rule string
	// Owner is who to ask about this entry, e.g. "@pablogore".
	Owner string
	// Justification explains why the violation exists today.
	Justification string
	// RemovalCriterion states what makes the entry removable.
	RemovalCriterion string
}

// ValidateBaseline checks that every entry in baseline is well formed:
// Importer and Import are non-empty, Owner, Justification and
// RemovalCriterion are non-empty, Rule names a rule that exists in
// ruleset, and no two entries share the same (Importer, Import, Rule) —
// Evaluate can only use the first of a pair of duplicates, so a duplicate
// is dead weight at best and a maintenance trap at worst. It reports every
// problem found, not just the first, joined with errors.Join, so a
// baseline with several broken entries is fixed in one pass.
func ValidateBaseline(baseline []BaselineEntry, ruleset []Rule) error {
	var errs []error
	seen := make(map[baselineKey]bool, len(baseline))
	for _, entry := range baseline {
		if strings.TrimSpace(entry.Importer) == "" {
			errs = append(errs, fmt.Errorf("baseline entry (importer=%q, import=%q, rule %s): missing importer", entry.Importer, entry.Import, entry.Rule))
		}
		if strings.TrimSpace(entry.Import) == "" {
			errs = append(errs, fmt.Errorf("baseline entry %s -> %q (rule %s): missing import", entry.Importer, entry.Import, entry.Rule))
		}
		if strings.TrimSpace(entry.Owner) == "" {
			errs = append(errs, fmt.Errorf("baseline entry %s -> %s (rule %s): missing owner", entry.Importer, entry.Import, entry.Rule))
		}
		if strings.TrimSpace(entry.Justification) == "" {
			errs = append(errs, fmt.Errorf("baseline entry %s -> %s (rule %s): missing justification", entry.Importer, entry.Import, entry.Rule))
		}
		if strings.TrimSpace(entry.RemovalCriterion) == "" {
			errs = append(errs, fmt.Errorf("baseline entry %s -> %s (rule %s): missing removal criterion", entry.Importer, entry.Import, entry.Rule))
		}
		if _, ok := ruleByID(ruleset, entry.Rule); !ok {
			errs = append(errs, unknownRuleErr(entry))
		}

		key := keyOf(entry.Importer, entry.Import, entry.Rule)
		if seen[key] {
			errs = append(errs, fmt.Errorf("baseline entry %s -> %s (rule %s): duplicate entry", entry.Importer, entry.Import, entry.Rule))
		}
		seen[key] = true
	}
	return errors.Join(errs...)
}

// baselineKey identifies a baseline entry, or a violation, by the triple
// Evaluate matches them on.
type baselineKey struct {
	Importer string
	Import   string
	Rule     string
}

func keyOf(importer, imp, rule string) baselineKey {
	return baselineKey{Importer: importer, Import: imp, Rule: rule}
}
