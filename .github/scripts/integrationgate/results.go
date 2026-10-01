package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// Status of one suite after the run.
const (
	statusPassed  = "passed"
	statusFailed  = "failed"
	statusSkipped = "skipped"
	statusMissing = "not executed"
)

// Results holds what `go test -json` streams said about packages and top-level tests. Only terminal actions
// (pass, fail, skip) count: a test that has a "run" event but no outcome was never finished.
type Results struct {
	tests   map[string]map[string]string // import path -> top-level test -> status
	pkgFail map[string]bool              // import path -> the package itself reported fail
}

// NewResults returns an empty set of results.
func NewResults() *Results {
	return &Results{tests: map[string]map[string]string{}, pkgFail: map[string]bool{}}
}

type testEvent struct {
	Action  string
	Package string
	Test    string
}

// Read adds one `go test -json` stream. Streams from several runs can be read into the same Results.
// A failure outranks a skip and a skip outranks a pass, so a test that ran several times keeps its worst outcome.
func (r *Results) Read(src io.Reader) error {
	dec := json.NewDecoder(src)
	for {
		var e testEvent
		if err := dec.Decode(&e); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("not go test -json output: %w", err)
		}
		if e.Package == "" {
			continue
		}
		status, terminal := terminalStatus(e.Action)
		switch {
		case !terminal:
		case e.Test == "":
			if status == statusFailed {
				r.pkgFail[e.Package] = true
			}
		case !isTopLevel(e.Test):
		default:
			byTest := r.tests[e.Package]
			if byTest == nil {
				byTest = map[string]string{}
				r.tests[e.Package] = byTest
			}
			byTest[e.Test] = worse(byTest[e.Test], status)
		}
	}
}

func terminalStatus(action string) (string, bool) {
	switch action {
	case "pass":
		return statusPassed, true
	case "fail":
		return statusFailed, true
	case "skip":
		return statusSkipped, true
	}
	return "", false
}

func isTopLevel(test string) bool { return !strings.Contains(test, "/") }

func worse(a, b string) string {
	rank := map[string]int{"": 0, statusPassed: 1, statusSkipped: 2, statusFailed: 3}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

// Row is the observed status of one manifest suite.
type Row struct {
	Suite  Suite
	Status string
}

// importPath is the package path `go test -json` reports for a suite, given the module path of its module.
func importPath(modulePath string, s Suite) string {
	if s.Dir == "." {
		return modulePath
	}
	return path.Join(modulePath, s.Dir)
}

// Evaluate looks every suite up in the results and returns one row per suite plus one line per problem.
// importPaths maps a module directory to its module path. A package that failed as a whole (a build failure, a
// panic, a failing TestMain) is reported once, unless one of its listed tests failed, which already explains it.
func Evaluate(suites []Suite, importPaths map[string]string, res *Results) ([]Row, []string) {
	rows := make([]Row, 0, len(suites))
	var pkgProblems, problems []string
	pkgSeen := map[string]bool{}
	pkgHasFailedTest := map[string]bool{}

	for _, s := range suites {
		modulePath, known := importPaths[s.Module]
		if !known {
			rows = append(rows, Row{s, statusMissing})
			problems = append(problems, fmt.Sprintf("%s: never executed: the module path of %s is unknown", s, s.Module))
			continue
		}
		pkg := importPath(modulePath, s)
		status := res.tests[pkg][s.Test]
		if status == "" {
			status = statusMissing
		}
		rows = append(rows, Row{s, status})
		switch status {
		case statusFailed:
			pkgHasFailedTest[pkg] = true
			problems = append(problems, fmt.Sprintf("%s: failed", s))
		case statusSkipped:
			problems = append(problems, fmt.Sprintf("%s: skipped: a listed test must run, so check that the resource it needs is configured", s))
		case statusMissing:
			problems = append(problems, fmt.Sprintf("%s: never executed: no result in the go test output", s))
		}
	}
	for _, s := range suites {
		modulePath, known := importPaths[s.Module]
		if !known {
			continue
		}
		pkg := importPath(modulePath, s)
		if res.pkgFail[pkg] && !pkgHasFailedTest[pkg] && !pkgSeen[pkg] {
			pkgSeen[pkg] = true
			pkgProblems = append(pkgProblems, fmt.Sprintf("package %s: failed (build failure, panic or TestMain exit)", pkg))
		}
	}
	return rows, append(pkgProblems, problems...)
}
