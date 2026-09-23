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
	"strings"
)

// summaryOrder is the fixed order changed-file groups appear in the
// summary, package changes first since they are the most actionable.
var summaryOrder = []Class{
	ClassPackage,
	ClassFullFallback,
	ClassSatellite,
	ClassNoTest,
	ClassUnknown,
}

// BuildSummary renders r as the markdown job-summary evidence: the mode,
// why it was chosen, how many of the included packages were selected, the
// selected package list, the changed files grouped by classification, and
// a satellite-module notice when relevant.
func BuildSummary(r Result) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# ciselect summary\n\n")
	fmt.Fprintf(&b, "**Mode:** `%s`\n\n", r.Mode)

	if len(r.Reasons) > 0 {
		fmt.Fprintf(&b, "**Reason(s):**\n\n")
		for _, reason := range r.Reasons {
			fmt.Fprintf(&b, "- %s\n", reason)
		}
		fmt.Fprintln(&b)
	}
	if r.ExtraReason != "" {
		fmt.Fprintf(&b, "**Additional context:** %s\n\n", r.ExtraReason)
	}

	fmt.Fprintf(&b, "**Selected:** %d of %d included packages.\n\n", len(r.Selected), len(r.Included))

	if len(r.Selected) > 0 {
		fmt.Fprintf(&b, "## Selected packages\n\n")
		for _, p := range r.Selected {
			fmt.Fprintf(&b, "- `%s`\n", p)
		}
		fmt.Fprintln(&b)
	}

	if len(r.Changed) > 0 {
		groups := make(map[Class][]ChangedFile)
		for _, cf := range r.Changed {
			groups[cf.Class] = append(groups[cf.Class], cf)
		}

		fmt.Fprintf(&b, "## Changed files by classification\n\n")
		for _, class := range summaryOrder {
			files := groups[class]
			if len(files) == 0 {
				continue
			}
			fmt.Fprintf(&b, "### %s (%d)\n\n", class, len(files))
			for _, cf := range files {
				if cf.Class == ClassPackage {
					fmt.Fprintf(&b, "- `%s` -> `%s`\n", cf.Path, cf.Package)
				} else {
					fmt.Fprintf(&b, "- `%s`\n", cf.Path)
				}
			}
			fmt.Fprintln(&b)
		}
	}

	if hasClass(r.Changed, ClassSatellite) {
		fmt.Fprintf(&b, "> Satellite-module changes are not covered by this lane (see #104).\n")
	}

	return b.String()
}

func hasClass(cfs []ChangedFile, class Class) bool {
	for _, cf := range cfs {
		if cf.Class == class {
			return true
		}
	}
	return false
}
