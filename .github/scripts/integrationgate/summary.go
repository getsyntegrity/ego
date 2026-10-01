package main

import (
	"fmt"
	"strings"
)

// Summary renders the Markdown the workflow appends to $GITHUB_STEP_SUMMARY: a verdict line, a table with one
// row per manifest suite, and the list of problems when there are any.
func Summary(rows []Row, problems []string) string {
	var b strings.Builder
	if len(problems) == 0 {
		fmt.Fprintf(&b, "### Integration gate: ok (%d suites executed and passed)\n", len(rows))
	} else {
		fmt.Fprintf(&b, "### Integration gate: %d problem(s)\n", len(problems))
	}
	b.WriteString("\n| Module | Package | Test | Status |\n|---|---|---|---|\n")
	for _, r := range rows {
		status := r.Status
		if status != statusPassed {
			status = "**" + status + "**"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", r.Suite.Module, r.Suite.Dir, r.Suite.Test, status)
	}
	if len(problems) > 0 {
		b.WriteString("\nProblems:\n\n")
		for _, p := range problems {
			fmt.Fprintf(&b, "- %s\n", p)
		}
	}
	return b.String()
}
