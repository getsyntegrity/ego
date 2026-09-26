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

// Command archcheck enforces the layer dependency rules of
// openspec/changes/ego-arch-001/design.md §3 (ego-arch-001 ADR, slice S2):
// it loads the repository's import graph — the root module via `go list`,
// every nested module (publisher/*, benchmark, example/cluster) via
// go/parser — and checks every production import edge against the rule
// table in internal/cmd/archcheck/rules. A violation not covered by the
// repository baseline (baseline.go) fails the check; a baseline entry that
// no longer matches a real violation fails it too, so the baseline can
// only shrink. See odd/tasks/arch-boundary-check.md for the full design.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pablogore/ego/v4/internal/cmd/archcheck/rules"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "archcheck: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("archcheck", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repoRootFlag := fs.String("repo-root", ".", "repository root to check")
	if err := fs.Parse(args); err != nil {
		return err
	}

	repoRoot, err := filepath.Abs(*repoRootFlag)
	if err != nil {
		return fmt.Errorf("resolving -repo-root: %w", err)
	}

	rootPkgs, err := loadRootModule(repoRoot)
	if err != nil {
		return fmt.Errorf("loading root module: %w", err)
	}
	nestedPkgs, err := loadNestedModules(repoRoot)
	if err != nil {
		return fmt.Errorf("loading nested modules: %w", err)
	}

	graph := rules.Graph{Packages: append(rootPkgs, nestedPkgs...)}
	ruleset := rules.DefaultRules()

	result, err := rules.Evaluate(graph, ruleset, repoBaseline)
	if err != nil {
		return fmt.Errorf("evaluating baseline: %w", err)
	}

	if len(result.Violations) > 0 || len(result.Stale) > 0 {
		fmt.Fprint(stdout, rules.FormatReport(result, ruleset))
	}

	packagesChecked, edgesChecked := countCheckedEdges(graph, ruleset)
	baselined := len(repoBaseline) - len(result.Stale)
	fmt.Fprintf(stdout, "archcheck: %d packages checked, %d edges checked, %d baselined, %d violation(s), %d stale entries\n",
		packagesChecked, edgesChecked, baselined, len(result.Violations), len(result.Stale))

	if len(result.Violations) > 0 || len(result.Stale) > 0 {
		return fmt.Errorf("%d violation(s), %d stale baseline entries", len(result.Violations), len(result.Stale))
	}
	return nil
}

// countCheckedEdges reports how many packages had at least one applicable
// rule, and how many (package, rule, non-stdlib import) edges were
// actually evaluated by Evaluate, for the summary line. It mirrors
// Evaluate's own iteration exactly so the count matches what was really
// checked, without Evaluate needing to expose internal counters through
// its public Result.
func countCheckedEdges(graph rules.Graph, ruleset []rules.Rule) (packagesChecked, edgesChecked int) {
	checked := make(map[string]bool, len(graph.Packages))
	for _, rule := range ruleset {
		for _, pkg := range graph.Packages {
			if !rule.Layer.Match(pkg) {
				continue
			}
			checked[pkg.ImportPath] = true
			for _, imp := range pkg.Imports {
				if rules.IsStdlib(imp) {
					continue
				}
				edgesChecked++
			}
		}
	}
	return len(checked), edgesChecked
}
