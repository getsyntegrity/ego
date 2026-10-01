package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func main() {
	root := flag.String("root", ".", "repository root to scan")
	pending := flag.String("pending", ".github/unit-test-gate-pending.txt", "temporary allowlist, relative to -root")
	resources := flag.String("resources", ".github/unit-test-gate-resources.txt", "resource allowlist, relative to -root")
	flag.Parse()

	pendingText, err := os.ReadFile(filepath.Join(*root, *pending))
	if err != nil {
		say(os.Stderr, "unit-test gate: %v\n", err)
		os.Exit(2)
	}
	resourcesText, err := os.ReadFile(filepath.Join(*root, *resources))
	if err != nil {
		say(os.Stderr, "unit-test gate: %v\n", err)
		os.Exit(2)
	}
	os.Exit(run(os.DirFS(*root), string(pendingText), string(resourcesText), os.Stdout))
}

// run scans fsys, checks the findings against the two allowlists and prints the result to out.
// Exit codes: 0 clean, 1 problems found, 2 the gate itself could not run.
func run(fsys fs.FS, pendingText, resourcesText string, out io.Writer) int {
	pending, err := ParseAllowlist(pendingText)
	if err != nil {
		say(out, "unit-test gate: pending list: %v\n", err)
		return 2
	}
	resources, err := ParseAllowlist(resourcesText)
	if err != nil {
		say(out, "unit-test gate: resources list: %v\n", err)
		return 2
	}
	findings, err := Scan(fsys)
	if err != nil {
		say(out, "unit-test gate: %v\n", err)
		return 2
	}
	problems := Evaluate(findings, pending, resources)
	if len(problems) == 0 {
		say(out, "unit-test gate: ok (%d pending entries, %d resource entries)\n", len(pending), len(resources))
		return 0
	}
	say(out, "unit-test gate: %d problem(s)\n", len(problems))
	for _, p := range problems {
		say(out, "  %s\n", p)
	}
	say(out, "See docs/testing/go-specs.md, section \"The unit-test gate\".\n")
	return 1
}

// say writes a line of report. A failed write to the terminal has nowhere better to go, so it is dropped.
func say(w io.Writer, format string, args ...any) { _, _ = fmt.Fprintf(w, format, args...) }
