// Command integrationgate proves that the integration lane really ran what the repository says it runs.
//
// The manifest (.github/integration-suites.txt) lists every top-level test the lane must execute. The gate
// compares it with the `go test -json` output of the lane and fails when a listed test never ran, was skipped,
// failed, no longer exists, or when an integration-tagged test is missing from the manifest.
//
//	integrationgate -plan                          print the -run regex per module and package
//	integrationgate -check-manifest                static checks only, no test output needed
//	integrationgate -summary out.md run1.json ...  full evaluation of one or more go test -json files
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type options struct {
	Manifest      string // manifest path relative to the filesystem root
	CheckManifest bool   // static checks only
	Plan          bool   // print the -run plan and stop
}

// stringList is a repeatable string flag.
type stringList []string

func (l *stringList) String() string     { return strings.Join(*l, ",") }
func (l *stringList) Set(v string) error { *l = append(*l, v); return nil }

func main() {
	root := flag.String("root", ".", "repository root")
	manifest := flag.String("manifest", ".github/integration-suites.txt", "suite manifest, relative to -root")
	checkManifest := flag.Bool("check-manifest", false, "only parse the manifest and scan the source (stale entries, orphans); no test output needed")
	plan := flag.Bool("plan", false, "print `module-dir<TAB>package-dir<TAB>regex` for each package and exit")
	summaryPath := flag.String("summary", "", "write the Markdown summary to this file")
	var jsonFiles stringList
	flag.Var(&jsonFiles, "json", "go test -json output file (repeatable; files may also be given as arguments)")
	flag.Parse()
	jsonFiles = append(jsonFiles, flag.Args()...)

	var readers []io.Reader
	var open []*os.File
	for _, name := range jsonFiles {
		f, err := os.Open(name)
		if err != nil {
			say(os.Stderr, "integration gate: %v\n", err)
			os.Exit(2)
		}
		open = append(open, f)
		readers = append(readers, f)
	}

	var summary bytes.Buffer
	code := run(os.DirFS(*root), options{Manifest: filepath.ToSlash(*manifest), CheckManifest: *checkManifest, Plan: *plan},
		readers, os.Stdout, os.Stderr, &summary)
	for _, f := range open {
		_ = f.Close()
	}
	if *summaryPath != "" && summary.Len() > 0 {
		if err := os.WriteFile(*summaryPath, summary.Bytes(), 0o644); err != nil {
			say(os.Stderr, "integration gate: %v\n", err)
			code = 2
		}
	}
	os.Exit(code)
}

// run executes the gate over fsys. Plan lines and the verdict go to out, problems to errw and the Markdown
// summary to summary (full evaluation only). Exit codes: 0 clean, 1 problems found, 2 the gate could not run.
func run(fsys fs.FS, opts options, jsons []io.Reader, out, errw, summary io.Writer) int {
	text, err := fs.ReadFile(fsys, opts.Manifest)
	if err != nil {
		say(errw, "integration gate: %v\n", err)
		return 2
	}
	suites, err := ParseManifest(string(text))
	if err != nil {
		say(errw, "integration gate: manifest: %v\n", err)
		return 1
	}
	if opts.Plan {
		for _, line := range Plan(suites) {
			say(out, "%s\n", line)
		}
		return 0
	}

	modules, problems := ModulePaths(fsys, suites)
	if opts.CheckManifest {
		problems = append(problems, CheckSource(fsys, suites)...)
		if len(problems) == 0 {
			say(out, "integration gate: manifest ok (%d suites)\n", len(suites))
			return 0
		}
		return report(errw, problems)
	}

	if len(jsons) == 0 {
		say(errw, "integration gate: no go test -json input (pass files with -json or as arguments, or use -check-manifest)\n")
		return 2
	}
	res := NewResults()
	for _, r := range jsons {
		if err := res.Read(r); err != nil {
			say(errw, "integration gate: %v\n", err)
			return 2
		}
	}
	rows, executionProblems := Evaluate(suites, modules, res)
	problems = append(executionProblems, append(problems, CheckSource(fsys, suites)...)...)
	if summary != nil {
		_, _ = io.WriteString(summary, Summary(rows, problems))
	}
	if len(problems) == 0 {
		say(out, "integration gate: ok (%d suites)\n", len(suites))
		return 0
	}
	return report(errw, problems)
}

func report(errw io.Writer, problems []string) int {
	say(errw, "integration gate: %d problem(s)\n", len(problems))
	for _, p := range problems {
		say(errw, "  %s\n", p)
	}
	return 1
}

// say writes a line of report. A failed write to the terminal has nowhere better to go, so it is dropped.
func say(w io.Writer, format string, args ...any) { _, _ = fmt.Fprintf(w, format, args...) }
