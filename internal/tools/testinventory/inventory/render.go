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

package inventory

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
)

var destinationTitles = map[string]string{
	"#208": "architecture and subprocess tests",
	"#210": "integration workflow: external process or endpoint",
	"#211": "PostgreSQL",
	"#212": "cluster and real network",
	"#213": "Kafka, NATS and Pulsar brokers",
	"#214": "examples and benchmarks",
}

var integrationName = regexp.MustCompile(`(?i)integration|e2e|end.?to.?end`)

// RenderMarkdown builds the human summary of an inventory. It is a pure function of
// the inventory, so a stale summary can be detected by comparison.
func RenderMarkdown(inv Inventory) string {
	var b strings.Builder
	renderHeader(&b, inv)
	renderCounts(&b, inv)
	renderLeaving(&b, inv)
	renderNames(&b, inv)
	renderSkips(&b, inv)
	renderMixed(&b, inv)
	renderReviewQueue(&b, inv)
	renderWatchLists(&b, inv)
	renderTimings(&b, inv)
	renderLimits(&b, inv)
	return b.String()
}

func tick(s string) string { return "`" + s + "`" }

func renderHeader(b *strings.Builder, inv Inventory) {
	h := inv.Header
	b.WriteString("# Test inventory\n\n")
	b.WriteString("This file is generated from [`inventory.json`](inventory.json) by `go run ./internal/tools/testinventory -update`. Do not edit it by hand. ")
	b.WriteString("It applies the lane contract in [`lanes.md`](lanes.md) to every `Test` function of every module and lists the tests that must leave the pull request lane. The plan behind it is epic #201; this inventory is issue #202.\n\n")
	if h.RunRecorded {
		fmt.Fprintf(b, "The recorded run used %s on %s/%s with %d CPUs at commit `%s`. Timings are indicative, taken from one local run, and are not a CI baseline. The race detector was not enabled.\n\n", h.GoVersion, h.GOOS, h.GOARCH, h.CPUs, h.Commit)
	} else {
		b.WriteString("No run is recorded yet: subtests, skips and timings are missing. Run `go run ./internal/tools/testinventory -run`.\n\n")
	}
}

func renderCounts(b *strings.Builder, inv Inventory) {
	b.WriteString("## Counts\n\n")
	b.WriteString("Top-level `Test` functions per module and lane.\n\n")
	b.WriteString("| Module | unit | component | integration | architecture | example | Total |\n|---|---|---|---|---|---|---|\n")
	totals := map[Lane]int{}
	for _, m := range inv.Modules {
		row := map[Lane]int{}
		n := 0
		for _, e := range inv.Tests {
			if e.Module == m.Path {
				row[e.Lane]++
				totals[e.Lane]++
				n++
			}
		}
		fmt.Fprintf(b, "| %s |", tick(m.Dir))
		for _, l := range Lanes {
			fmt.Fprintf(b, " %d |", row[l])
		}
		fmt.Fprintf(b, " %d |\n", n)
	}
	b.WriteString("| **Total** |")
	for _, l := range Lanes {
		fmt.Fprintf(b, " %d |", totals[l])
	}
	fmt.Fprintf(b, " %d |\n\n", len(inv.Tests))

	subtests, withRun, statuses := 0, 0, map[string]int{}
	for _, e := range inv.Tests {
		if e.Run == nil {
			continue
		}
		withRun++
		statuses[e.Run.Status]++
		subtests += len(e.Run.Subtests)
	}
	fmt.Fprintf(b, "Tests with a recorded run: %d of %d (pass %d, skip %d, fail %d). Subtests recorded: %d.\n\n", withRun, len(inv.Tests), statuses[StatusPass], statuses[StatusSkip], statuses[StatusFail], subtests)
	stay, leave := 0, 0
	for _, e := range inv.Tests {
		if e.LeavesPR {
			leave++
		} else {
			stay++
		}
	}
	fmt.Fprintf(b, "%d tests stay in the pull request lane (unit and component) and %d leave it.\n\n", stay, leave)
}

func groupByPackage(entries []Entry) ([]string, map[string][]string) {
	byPkg := map[string][]string{}
	for _, e := range entries {
		byPkg[packageDir(e)] = append(byPkg[packageDir(e)], e.Name)
	}
	dirs := make([]string, 0, len(byPkg))
	for d := range byPkg {
		dirs = append(dirs, d)
		sort.Strings(byPkg[d])
	}
	sort.Strings(dirs)
	return dirs, byPkg
}

func renderLeaving(b *strings.Builder, inv Inventory) {
	b.WriteString("## Tests that leave the pull request lane\n\n")
	b.WriteString("Grouped by the issue that will run them again. Each line is a package directory followed by its tests.\n\n")
	byDest := map[string][]Entry{}
	for _, e := range inv.Tests {
		if e.LeavesPR {
			byDest[e.Destination] = append(byDest[e.Destination], e)
		}
	}
	dests := make([]string, 0, len(byDest))
	for d := range byDest {
		dests = append(dests, d)
	}
	sort.Strings(dests)
	for _, d := range dests {
		title := destinationTitles[d]
		fmt.Fprintf(b, "### %s: %s (%d tests)\n\n", d, title, len(byDest[d]))
		dirs, byPkg := groupByPackage(byDest[d])
		for _, dir := range dirs {
			names := make([]string, len(byPkg[dir]))
			for i, n := range byPkg[dir] {
				names[i] = tick(n)
			}
			fmt.Fprintf(b, "- %s: %s\n", tick(dir), strings.Join(names, ", "))
		}
		b.WriteString("\n")
	}
}

func signalList(e Entry) string {
	var ids []string
	for _, s := range e.Signals {
		if !slices.Contains(ids, s.ID) {
			ids = append(ids, s.ID)
		}
	}
	if len(ids) == 0 {
		return "none"
	}
	return strings.Join(ids, ", ")
}

func renderNames(b *strings.Builder, inv Inventory) {
	var inMemory, real []Entry
	for _, e := range inv.Tests {
		if !integrationName.MatchString(e.Name) {
			continue
		}
		if e.Lane == LaneIntegration {
			real = append(real, e)
		} else {
			inMemory = append(inMemory, e)
		}
	}
	b.WriteString("## Names versus behavior\n\n")
	b.WriteString("A test's name says nothing about its resources, so no test is classified by name. These are the tests whose name suggests integration.\n\n")
	fmt.Fprintf(b, "### Tests named like integration or end-to-end but not integration (%d)\n\n", len(inMemory))
	nameTable(b, inMemory)
	fmt.Fprintf(b, "### Tests named like integration or end-to-end that really are integration (%d)\n\n", len(real))
	nameTable(b, real)
}

func nameTable(b *strings.Builder, entries []Entry) {
	if len(entries) == 0 {
		b.WriteString("None.\n\n")
		return
	}
	b.WriteString("| Test | Lane | Evidence | Package |\n|---|---|---|---|\n")
	for _, e := range entries {
		fmt.Fprintf(b, "| %s | %s | %s | %s |\n", tick(e.Name), e.Lane, signalList(e), tick(packageDir(e)))
	}
	b.WriteString("\n")
}

type skipRow struct{ name, pkg string }

func renderSkips(b *strings.Builder, inv Inventory) {
	byCause := map[string][]skipRow{}
	var failures []string
	for _, e := range inv.Tests {
		if e.Run == nil {
			continue
		}
		if e.Run.Status == StatusSkip {
			byCause[e.Run.Note] = append(byCause[e.Run.Note], skipRow{e.Name, packageDir(e)})
		}
		if e.Run.Status == StatusFail {
			failures = append(failures, fmt.Sprintf("%s in %s: %s", tick(e.Name), tick(packageDir(e)), e.Run.Note))
		}
		for _, s := range e.Run.Subtests {
			if s.Status == StatusSkip && e.Run.Status != StatusSkip {
				byCause["subtest: "+s.Note] = append(byCause["subtest: "+s.Note], skipRow{s.Name, packageDir(e)})
			}
		}
	}
	b.WriteString("## Skips and failures\n\n")
	if len(byCause) == 0 {
		b.WriteString("No skipped test is recorded.\n\n")
	}
	causes := make([]string, 0, len(byCause))
	for c := range byCause {
		causes = append(causes, c)
	}
	sort.Strings(causes)
	for _, c := range causes {
		rows := byCause[c]
		label := c
		if label == "" {
			label = "no cause reported"
		}
		fmt.Fprintf(b, "### %s (%d)\n\n", label, len(rows))
		dirs := map[string][]string{}
		for _, r := range rows {
			dirs[r.pkg] = append(dirs[r.pkg], tick(r.name))
		}
		keys := make([]string, 0, len(dirs))
		for d := range dirs {
			keys = append(keys, d)
		}
		sort.Strings(keys)
		for _, d := range keys {
			sort.Strings(dirs[d])
			fmt.Fprintf(b, "- %s: %s\n", tick(d), strings.Join(dirs[d], ", "))
		}
		b.WriteString("\n")
	}
	if len(failures) > 0 {
		fmt.Fprintf(b, "### Failures in the recorded run (%d)\n\n", len(failures))
		for _, f := range failures {
			fmt.Fprintf(b, "- %s\n", f)
		}
		b.WriteString("\n")
	}
}

func renderMixed(b *strings.Builder, inv Inventory) {
	lanesByFile := map[string]map[Lane]int{}
	for _, e := range inv.Tests {
		if lanesByFile[e.File] == nil {
			lanesByFile[e.File] = map[Lane]int{}
		}
		lanesByFile[e.File][e.Lane]++
	}
	files := make([]string, 0)
	for f, lanes := range lanesByFile {
		if len(lanes) > 1 {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	fmt.Fprintf(b, "## Mixed files (%d)\n\n", len(files))
	b.WriteString("Files whose tests belong to more than one lane. Splitting them is left to the migration issues.\n\n")
	if len(files) == 0 {
		b.WriteString("None.\n\n")
		return
	}
	b.WriteString("| File | Lanes |\n|---|---|\n")
	for _, f := range files {
		var parts []string
		for _, l := range Lanes {
			if n := lanesByFile[f][l]; n > 0 {
				parts = append(parts, fmt.Sprintf("%s %d", l, n))
			}
		}
		fmt.Fprintf(b, "| %s | %s |\n", tick(f), strings.Join(parts, ", "))
	}
	b.WriteString("\n")
}

// renderReviewQueue lists unit tests that call Start or Spawn on something. The scanner
// cannot tell whether that something is a real actor system, so a person confirms each one
// with an override; the queue is what is still unconfirmed.
func renderReviewQueue(b *strings.Builder, inv Inventory) {
	var queue []Entry
	for _, e := range inv.Tests {
		if e.Lane != LaneUnit || e.Override != "" {
			continue
		}
		for _, s := range e.Signals {
			if s.ID == SigLifecycle {
				queue = append(queue, e)
				break
			}
		}
	}
	fmt.Fprintf(b, "## Review queue (%d)\n\n", len(queue))
	b.WriteString("Unit tests that call `Start` or `Spawn` on some value. Static analysis cannot tell a fake from a real actor system, so each one needs a person to confirm its lane in `inventory-overrides.json`.\n\n")
	listEntries(b, queue)
}

func renderWatchLists(b *strings.Builder, inv Inventory) {
	var httpLocal, overrides []Entry
	var waitMS int64
	waiting := 0
	for _, e := range inv.Tests {
		for _, s := range e.Signals {
			if s.ID == SigHTTPTestServer || s.ID == SigNetListen || s.ID == SigNetDial {
				httpLocal = append(httpLocal, e)
				break
			}
		}
		if e.Override != "" {
			overrides = append(overrides, e)
		}
		if e.FixedWaitMS > 0 {
			waiting++
			waitMS += e.FixedWaitMS
		}
	}
	b.WriteString("## Other lists\n\n")
	fmt.Fprintf(b, "### Local sockets and `httptest` (%d)\n\n", len(httpLocal))
	b.WriteString("Classified by the loopback rule in `lanes.md`, so they stay out of `unit` but are not integration unless another signal says so.\n\n")
	listEntries(b, httpLocal)
	fmt.Fprintf(b, "### Reclassified by override (%d)\n\n", len(overrides))
	if len(overrides) == 0 {
		b.WriteString("None.\n\n")
	}
	for _, e := range overrides {
		fmt.Fprintf(b, "- %s in %s is %s: %s\n", tick(e.Name), tick(packageDir(e)), e.Lane, e.Override)
	}
	if len(overrides) > 0 {
		b.WriteString("\n")
	}
	fmt.Fprintf(b, "### Fixed waits\n\n%d tests contain constant `time.Sleep` or `pause.For` waits that add up to %.1f seconds (once per occurrence, a lower bound). Issue #207 owns them.\n\n", waiting, float64(waitMS)/1000)
}

func listEntries(b *strings.Builder, entries []Entry) {
	if len(entries) == 0 {
		b.WriteString("None.\n\n")
		return
	}
	dirs, byPkg := groupByPackage(entries)
	for _, d := range dirs {
		names := make([]string, len(byPkg[d]))
		for i, n := range byPkg[d] {
			names[i] = tick(n)
		}
		fmt.Fprintf(b, "- %s: %s\n", tick(d), strings.Join(names, ", "))
	}
	b.WriteString("\n")
}

func renderTimings(b *strings.Builder, inv Inventory) {
	if len(inv.Packages) == 0 {
		return
	}
	pkgs := slices.Clone(inv.Packages)
	sort.SliceStable(pkgs, func(i, j int) bool { return pkgs[i].ElapsedS > pkgs[j].ElapsedS })
	b.WriteString("## Slowest packages\n\nElapsed time per package in the recorded run. `inventory.json` has all of them.\n\n")
	b.WriteString("| Package | Seconds | Status |\n|---|---|---|\n")
	for _, p := range pkgs[:min(len(pkgs), 15)] {
		fmt.Fprintf(b, "| %s | %.1f | %s |\n", tick(p.Package), p.ElapsedS, p.Status)
	}
	b.WriteString("\n")
}

func renderLimits(b *strings.Builder, inv Inventory) {
	b.WriteString("## Limits\n\n")
	b.WriteString("Signals come from each `Test` function and the local helpers it calls, one level deep. Subtests exist only in the recorded run. ")
	b.WriteString("What static analysis cannot see is corrected by [`inventory-overrides.json`](inventory-overrides.json). See `lanes.md` for the full list.\n")
	if len(inv.RunUnmatched) > 0 {
		fmt.Fprintf(b, "\nThe run reported %d tests that the static scan did not find: %s.\n", len(inv.RunUnmatched), strings.Join(inv.RunUnmatched, ", "))
	}
}
