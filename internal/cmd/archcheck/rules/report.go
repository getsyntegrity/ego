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
	"strings"
)

// FormatReport renders result as human-readable text: one block per
// violation (importer, import, rule, description and a fix/baseline hint),
// then one block per stale baseline entry. ruleset resolves each
// violation's and stale entry's Rule ID back to its Description and
// Source; it should be the same ruleset Evaluate was called with.
//
// FormatReport returns "" when result has neither violations nor stale
// entries.
func FormatReport(result Result, ruleset []Rule) string {
	var b strings.Builder

	if len(result.Violations) > 0 {
		fmt.Fprintf(&b, "%d unbaselined violation(s):\n\n", len(result.Violations))
		for _, v := range result.Violations {
			writeViolation(&b, v, ruleset)
		}
	}

	if len(result.Stale) > 0 {
		fmt.Fprintf(&b, "%d stale baseline entries:\n\n", len(result.Stale))
		for _, entry := range result.Stale {
			writeStaleEntry(&b, entry)
		}
	}

	return b.String()
}

func writeViolation(b *strings.Builder, v Violation, ruleset []Rule) {
	rule, ok := ruleByID(ruleset, v.Rule)
	source := "unknown rule"
	description := ""
	if ok {
		source = rule.Source
		description = rule.Description
	}

	fmt.Fprintf(b, "%s imports %s: rule %s (%s): %s\n", v.Importer, v.Import, v.Rule, source, description)
	fmt.Fprintf(b, "  reason: %s\n", v.Reason)
	fmt.Fprintf(b, "  hint: depend on an allowed package instead, or add a baseline entry for %s -> %s (rule %s) with an owner, a justification and a removal criterion\n\n", v.Importer, v.Import, v.Rule)
}

func writeStaleEntry(b *strings.Builder, entry BaselineEntry) {
	fmt.Fprintf(b, "baseline entry %s -> %s (rule %s, owner %s) no longer matches a violation; delete it\n\n", entry.Importer, entry.Import, entry.Rule, entry.Owner)
}
