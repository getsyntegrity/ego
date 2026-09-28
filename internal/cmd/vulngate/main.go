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

// Command vulngate turns a `govulncheck -format json` report into a pass/fail
// decision for one Go module, against a reviewed, expiring exception list
// (scripts/ci/govulncheck-allow.json). It exists because `govulncheck
// -format json` always exits 0, even when it finds vulnerabilities (the exit
// code alone cannot gate CI), and because a bare `govulncheck ./...` step has
// no way to accept a specific, reviewed, pre-existing finding without
// silencing every future one too.
//
// A finding only blocks when govulncheck reports it as actually reachable
// from the scanned module's own code: govulncheck's JSON finding.trace lists
// the vulnerable symbol first and the calling code last (verified against a
// real `govulncheck -format json` run in this repository, since
// golang.org/x/vuln/internal/govulncheck's JSON schema is internal and not
// directly importable), so a finding blocks exactly when its first trace
// frame names a function — the same set govulncheck's own text mode reports
// as "Your code is affected". A finding whose vulnerable package is only
// required or only imported, never called, never blocks. The same OSV ID
// can legitimately block through more than one dependency module in one
// scan (an OSV record can list several affected modules, e.g. a package and
// its fork), so vulngate tracks the pair (ID, vulnerable module), never the
// bare ID.
//
// An exception is identified by the triple (scanned module directory, OSV
// ID, vulnerable dependency module) — all three, not just the first two:
// an entry for a different module, or the same ID surfacing through a
// different dependency module than the one the entry names, does not apply
// and the finding blocks. Every entry also carries a review_by date; once
// today is past it, the exception no longer applies and the gate fails as
// expired, so an exception cannot silently outlive its review. An entry
// scoped to the module being scanned whose exact (ID, vulnerable module)
// pair no longer has any blocking finding fails as stale, so a fixed
// vulnerability's exception is forced out of the list instead of
// lingering — this is evaluated on the full triple too: if the same ID
// starts blocking through a different module, the old entry goes stale
// (its own pair is gone) at the same time as the new pair blocks (nothing
// names it). Reporting both is correct, not a presentation bug: they are
// two distinct fixable facts that happen to share an ID, and hiding either
// one would let a reviewer miss it. An entry scoped to a module this run
// did not scan is ignored entirely: it is neither matched nor ever reported
// stale.
//
// See docs/ci.md, "The govulncheck exception gate (vulngate)", for the
// allow-file format and how to add or retire an entry.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

// dateLayout is the exact form every date in the allow file and every
// -today value must use: an unambiguous, lexicographically sortable ISO
// date with no time component. Comparing two such strings with the ordinary
// string "<" and ">" operators is equivalent to comparing the dates they
// name, which is exactly how expiry is decided below.
const dateLayout = "2006-01-02"

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "vulngate: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("vulngate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	moduleFlag := fs.String("module", "", "repo-relative directory of the module the report was scanned for (\".\" for the root)")
	reportFlag := fs.String("report", "", "path to a govulncheck -format json report")
	allowFlag := fs.String("allow", "", "path to the allow-list JSON file (scripts/ci/govulncheck-allow.json)")
	todayFlag := fs.String("today", "", "override today's date (YYYY-MM-DD, UTC) for review_by expiry checks; defaults to now")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *moduleFlag == "" {
		return errors.New("-module is required")
	}
	if *reportFlag == "" {
		return errors.New("-report is required")
	}
	if *allowFlag == "" {
		return errors.New("-allow is required")
	}

	today := *todayFlag
	if today == "" {
		today = time.Now().UTC().Format(dateLayout)
	} else if _, err := time.Parse(dateLayout, today); err != nil {
		return fmt.Errorf("-today: %w", err)
	}

	reportFile, err := os.Open(*reportFlag)
	if err != nil {
		return fmt.Errorf("opening -report: %w", err)
	}
	defer func() { _ = reportFile.Close() }()

	blocking, err := parseReport(reportFile)
	if err != nil {
		return fmt.Errorf("parsing -report: %w", err)
	}

	allow, err := loadAllowList(*allowFlag)
	if err != nil {
		return fmt.Errorf("loading -allow: %w", err)
	}

	res := evaluate(*moduleFlag, blocking, allow, today)
	fmt.Fprint(stdout, formatSummary(*moduleFlag, res))

	if len(res.Blocked) > 0 || len(res.Stale) > 0 || len(res.Expired) > 0 {
		return fmt.Errorf("govulncheck gate failed for %s: %d blocked, %d stale, %d expired",
			*moduleFlag, len(res.Blocked), len(res.Stale), len(res.Expired))
	}
	return nil
}

// allowEntry is one reviewed, expiring exception: a known vulnerability
// (id, in vulnerableModule) that module is allowed to carry until reviewBy,
// with the human context a reviewer needs to judge it later. Every field is
// required (see (allowEntry).validate); scripts/ci/govulncheck-allow.json is
// decoded with DisallowUnknownFields, so a typo in a field name fails
// loudly instead of silently doing nothing.
type allowEntry struct {
	Module           string `json:"module"`
	ID               string `json:"id"`
	VulnerableModule string `json:"vulnerable_module"`
	Owner            string `json:"owner"`
	Reason           string `json:"reason"`
	Exposure         string `json:"exposure"`
	Removal          string `json:"removal"`
	ReviewBy         string `json:"review_by"`
}

func (e allowEntry) validate() error {
	for _, f := range []struct {
		name, value string
	}{
		{"module", e.Module},
		{"id", e.ID},
		{"vulnerable_module", e.VulnerableModule},
		{"owner", e.Owner},
		{"reason", e.Reason},
		{"exposure", e.Exposure},
		{"removal", e.Removal},
		{"review_by", e.ReviewBy},
	} {
		if strings.TrimSpace(f.value) == "" {
			return fmt.Errorf("%s is required", f.name)
		}
	}
	if _, err := time.Parse(dateLayout, e.ReviewBy); err != nil {
		return fmt.Errorf("review_by %q is not a valid %s date: %w", e.ReviewBy, dateLayout, err)
	}
	return nil
}

// loadAllowList strictly decodes the allow file at path: unknown fields,
// missing required fields and an invalid review_by date each fail the load,
// so a malformed exception can never silently grant less (or more) than a
// reviewer intended.
func loadAllowList(path string) ([]allowEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	var entries []allowEntry
	if err := dec.Decode(&entries); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", path, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("%s has trailing content after its JSON array", path)
	}
	for i, e := range entries {
		if err := e.validate(); err != nil {
			return nil, fmt.Errorf("%s entry %d (%s): %w", path, i, e.ID, err)
		}
	}
	return entries, nil
}

// traceFrame mirrors one frame of a govulncheck JSON finding's trace, as
// emitted by golang.org/x/vuln/cmd/govulncheck@v1.8.0's `-format json`: the
// first frame is the vulnerable symbol itself, and the last is the entry
// point in the scanned module's own code. Only the fields vulngate reads are
// declared; every other field of the real, richer schema decodes into
// nothing and is ignored.
type traceFrame struct {
	Module   string `json:"module"`
	Function string `json:"function"`
}

// findingMsg mirrors a govulncheck JSON report's {"finding": ...} message.
type findingMsg struct {
	OSV   string       `json:"osv"`
	Trace []traceFrame `json:"trace"`
}

// reportLine mirrors one decoded object of a govulncheck -format json
// stream. The stream also contains {"progress": ...}, {"osv": ...} and
// {"SBOM": ...} messages; leaving them undeclared here means json.Decoder
// simply ignores them; only the object's recognized fields ever populate
// this struct, which is intentional and not indicative of report-writer
// failure.
type reportLine struct {
	Config  json.RawMessage `json:"config"`
	Finding *findingMsg     `json:"finding"`
}

// findingID identifies one distinct blocking (called) vulnerability: an OSV
// ID together with the dependency module its call trace actually reaches
// (the finding's first trace frame). This pair, not the bare ID, is the
// unit both a report's blocking findings and an allow-list entry's own
// exception are keyed by — see the package doc comment for why.
type findingID struct {
	ID     string
	Module string
}

// parseReport decodes a govulncheck -format json stream and returns the set
// of every (OSV ID, vulnerable module) pair with at least one call-level
// (blocking) finding. It fails on malformed JSON, on a report with no
// decodable objects at all, and on a report that never contained a "config"
// message — the first object every real govulncheck JSON report writes, so
// its absence means the report is truncated, was produced by something
// else, or is otherwise not trustworthy input.
func parseReport(r io.Reader) (map[findingID]bool, error) {
	dec := json.NewDecoder(r)
	blocking := make(map[findingID]bool)
	sawConfig := false
	count := 0
	for {
		var line reportLine
		if err := dec.Decode(&line); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decoding report: %w", err)
		}
		count++
		if line.Config != nil {
			sawConfig = true
		}
		if line.Finding == nil || len(line.Finding.Trace) == 0 {
			continue
		}
		first := line.Finding.Trace[0]
		if first.Function == "" {
			// Module-level or package-level only: govulncheck reports this
			// as required or imported, but never called, which is not
			// "Your code is affected" territory.
			continue
		}
		blocking[findingID{ID: line.Finding.OSV, Module: first.Module}] = true
	}
	if count == 0 {
		return nil, errors.New("report is empty")
	}
	if !sawConfig {
		return nil, errors.New("report never contains a govulncheck config message")
	}
	return blocking, nil
}

// blockedItem is a blocking (ID, vulnerable module) pair with no matching,
// valid exception: a genuinely new pair, or a known ID surfacing through a
// different dependency module than the one an existing entry names (in
// which case that entry is separately reported as stale — see (gateResult)
// and blockedReason).
type blockedItem struct {
	ID     string
	Module string
	Reason string
}

// exceptedItem is a blocking finding a valid, unexpired allow-list entry
// covers.
type exceptedItem struct {
	ID       string
	Module   string
	Owner    string
	ReviewBy string
}

// staleItem is an allow-list entry scoped to the scanned module whose exact
// (ID, vulnerable module) pair no longer has any blocking finding: the
// specific vulnerability instance this entry was written for is gone (fixed
// outright, or now arriving through a different, uncovered dependency
// module instead), so the entry itself must be removed.
type staleItem struct {
	ID     string
	Module string
}

// expiredItem is a blocking finding whose otherwise-matching exception has
// passed its review_by date.
type expiredItem struct {
	ID       string
	Module   string
	ReviewBy string
}

// gateResult is the complete, ordered outcome of evaluating one module's
// blocking findings against its allow-list entries.
type gateResult struct {
	Blocked  []blockedItem
	Excepted []exceptedItem
	Stale    []staleItem
	Expired  []expiredItem
}

// evaluate decides, for the module at moduleDir, which of its blocking
// (ID, vulnerable module) pairs (as parseReport returns) are excepted,
// blocked, or expired, and which of allow's entries scoped to moduleDir are
// stale. Both blocking findings and allow-list entries are keyed by the
// full (ID, vulnerable module) pair: an entry and a finding that share an ID
// but name different vulnerable modules do not match each other at all —
// the finding blocks (nothing names its exact pair) and the entry goes
// stale (its own exact pair no longer has any blocking finding). That is
// two distinct, independently actionable facts, not one finding reported
// twice: fixing one (adding the finding's own entry) does not fix the other
// (removing the now-pointless old entry), and vice versa.
//
// Entries scoped to any other module are ignored entirely: never matched,
// and never reported stale.
func evaluate(moduleDir string, blocking map[findingID]bool, allow []allowEntry, today string) gateResult {
	scoped := make(map[findingID]allowEntry)
	byID := make(map[string][]allowEntry) // every scoped entry sharing an ID, for a clearer blocked reason
	for _, e := range allow {
		if e.Module != moduleDir {
			continue
		}
		scoped[findingID{ID: e.ID, Module: e.VulnerableModule}] = e
		byID[e.ID] = append(byID[e.ID], e)
	}

	var res gateResult
	for _, key := range sortedFindingIDs(blocking) {
		entry, ok := scoped[key]
		switch {
		case !ok:
			res.Blocked = append(res.Blocked, blockedItem{ID: key.ID, Module: key.Module, Reason: blockedReason(key, byID[key.ID])})
		case today > entry.ReviewBy:
			res.Expired = append(res.Expired, expiredItem{ID: key.ID, Module: key.Module, ReviewBy: entry.ReviewBy})
		default:
			res.Excepted = append(res.Excepted, exceptedItem{ID: key.ID, Module: key.Module, Owner: entry.Owner, ReviewBy: entry.ReviewBy})
		}
	}

	for _, key := range sortedScopedFindingIDs(scoped) {
		if blocking[key] {
			continue
		}
		res.Stale = append(res.Stale, staleItem(key))
	}

	return res
}

// blockedReason explains why key has no matching exception. When
// otherEntries names at least one entry for the same ID under a different
// vulnerable module, the reason says so explicitly — that other entry is
// separately reported stale in the same gateResult, and a reader seeing
// only "no exception entry" would otherwise have no way to connect the two.
func blockedReason(key findingID, otherEntries []allowEntry) string {
	if len(otherEntries) == 0 {
		return fmt.Sprintf("no exception entry for %s in this module's allow list", key.ID)
	}
	named := make([]string, len(otherEntries))
	for i, e := range otherEntries {
		named[i] = e.VulnerableModule
	}
	sort.Strings(named)
	return fmt.Sprintf("no exception entry names vulnerable_module %q for %s; this module's allow list has %s for vulnerable_module %s instead, which no longer matches any finding and is reported stale",
		key.Module, key.ID, key.ID, strings.Join(named, ", "))
}

func sortFindingIDs(keys []findingID) {
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].ID != keys[j].ID {
			return keys[i].ID < keys[j].ID
		}
		return keys[i].Module < keys[j].Module
	})
}

func sortedFindingIDs(m map[findingID]bool) []findingID {
	keys := make([]findingID, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortFindingIDs(keys)
	return keys
}

func sortedScopedFindingIDs(m map[findingID]allowEntry) []findingID {
	keys := make([]findingID, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortFindingIDs(keys)
	return keys
}

// formatSummary renders res as a short markdown report for the module at
// moduleDir, suitable for a CI job's $GITHUB_STEP_SUMMARY or plain stdout.
func formatSummary(moduleDir string, res gateResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### govulncheck exception gate: `%s`\n\n", moduleDir)

	fmt.Fprintf(&b, "- **Blocked:** %d\n", len(res.Blocked))
	for _, item := range res.Blocked {
		fmt.Fprintf(&b, "  - `%s` (found in `%s`): %s\n", item.ID, item.Module, item.Reason)
	}
	fmt.Fprintf(&b, "- **Excepted:** %d\n", len(res.Excepted))
	for _, item := range res.Excepted {
		fmt.Fprintf(&b, "  - `%s` (owner %s, review by %s)\n", item.ID, item.Owner, item.ReviewBy)
	}
	fmt.Fprintf(&b, "- **Stale:** %d\n", len(res.Stale))
	for _, item := range res.Stale {
		fmt.Fprintf(&b, "  - `%s`: no longer appears as a called vulnerability in this module; remove its allow-list entry\n", item.ID)
	}
	fmt.Fprintf(&b, "- **Expired:** %d\n", len(res.Expired))
	for _, item := range res.Expired {
		fmt.Fprintf(&b, "  - `%s`: review_by %s has passed\n", item.ID, item.ReviewBy)
	}
	b.WriteString("\n")
	return b.String()
}
