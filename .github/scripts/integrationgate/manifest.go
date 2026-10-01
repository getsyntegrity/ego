package main

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// Suite is one manifest line: a top-level test the integration lane must execute.
type Suite struct {
	Module string // module directory relative to the repository root, "." for the root module
	Dir    string // package directory relative to the module
	Test   string // top-level test function name
	Line   int    // manifest line, for error messages
}

// String renders the suite the way the manifest spells it, which is how problems name it.
func (s Suite) String() string { return s.Module + " | " + s.Dir + " | " + s.Test }

// PkgDir is the package directory relative to the repository root.
func (s Suite) PkgDir() string { return path.Join(s.Module, s.Dir) }

// testName matches TestXxx the way `go test` does: what follows "Test" must not start with a lower case letter.
var testName = regexp.MustCompile(`^Test([A-Z0-9_]\w*)?$`)

const manifestShape = "want `module-dir | package-dir | TestName`"

// ParseManifest reads lines of the form `module-dir | package-dir | TestName`. Blank lines and lines starting
// with # are ignored. Every field is required, the test must be a top-level test name, the directories must stay
// inside the repository, and a suite may be listed only once. The error names the first offending line.
func ParseManifest(text string) ([]Suite, error) {
	var out []Suite
	first := map[Suite]int{}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		n := i + 1
		fields := strings.Split(line, "|")
		if len(fields) != 3 {
			return nil, fmt.Errorf("line %d: %s", n, manifestShape)
		}
		module, dir, test := strings.TrimSpace(fields[0]), strings.TrimSpace(fields[1]), strings.TrimSpace(fields[2])
		switch {
		case module == "" || dir == "" || test == "":
			return nil, fmt.Errorf("line %d: the module, package and test fields are all required", n)
		case !testName.MatchString(test):
			return nil, fmt.Errorf("line %d: %q is not a top-level test name (TestXxx)", n, test)
		case !insideRepo(module) || !insideRepo(dir):
			return nil, fmt.Errorf("line %d: directories must be relative and stay inside the repository", n)
		}
		s := Suite{Module: path.Clean(module), Dir: path.Clean(dir), Test: test}
		if at, dup := first[s]; dup {
			return nil, fmt.Errorf("line %d: duplicate entry %s (first at line %d)", n, s, at)
		}
		first[s] = n
		s.Line = n
		out = append(out, s)
	}
	return out, nil
}

func insideRepo(dir string) bool {
	if path.IsAbs(dir) {
		return false
	}
	c := path.Clean(dir)
	return c != ".." && !strings.HasPrefix(c, "../")
}

// PlanLine is the `go test -run` regex of one package of one module.
type PlanLine struct{ Module, Dir, Run string }

// String is the line the workflow reads: module directory, package directory and regex, separated by tabs.
func (p PlanLine) String() string { return p.Module + "\t" + p.Dir + "\t" + p.Run }

// Plan groups the suites by module and package, in manifest order, with an anchored -run regex for each group.
func Plan(suites []Suite) []PlanLine {
	var lines []PlanLine
	var names [][]string
	index := map[[2]string]int{}
	for _, s := range suites {
		key := [2]string{s.Module, s.Dir}
		i, ok := index[key]
		if !ok {
			i = len(lines)
			index[key] = i
			lines = append(lines, PlanLine{Module: s.Module, Dir: s.Dir})
			names = append(names, nil)
		}
		names[i] = append(names[i], s.Test)
	}
	for i := range lines {
		lines[i].Run = "^(" + strings.Join(names[i], "|") + ")$"
	}
	return lines
}
