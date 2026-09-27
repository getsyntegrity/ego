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
	"strings"
	"testing"
)

const root = "github.com/pablogore/ego/v4"

// rulesFor returns the subset of DefaultRules(root) named by ids, in that
// order. Evaluate now rejects a ruleset where any rule's Layer matched zero
// packages (R1), so a test whose fixture graph deliberately populates only
// one or two layers must narrow the ruleset to just the rule(s) under test,
// rather than passing every DefaultRules(root) rule against a partial
// graph.
func rulesFor(t *testing.T, ids ...string) []Rule {
	t.Helper()
	all := DefaultRules(root)
	out := make([]Rule, 0, len(ids))
	for _, id := range ids {
		r, ok := ruleByID(all, id)
		if !ok {
			t.Fatalf("unknown rule id %q", id)
		}
		out = append(out, r)
	}
	return out
}

// repoModules is the module table matching the fixture graphs below: the
// root module and the nested modules they use, each requiring the root.
// The module-aware rules (no-cross-module-internal, no-module-cycle) match
// nothing without a module table, so a fixture evaluated against them, or
// against the full DefaultRules, carries this one.
func repoModules() []Module {
	return []Module{
		{Path: root},
		{Path: root + "/publisher/kafka", Requires: []string{root}},
		{Path: root + "/benchmark", Requires: []string{root}},
		{Path: root + "/example/cluster", Requires: []string{root}},
	}
}

// withRepoModules returns g with repoModules as its module table.
func withRepoModules(g Graph) Graph {
	g.Modules = repoModules()
	return g
}

// ruleStat looks up one rule's stat from a Result's RuleStats by ID.
func ruleStat(stats []RuleStat, id string) (RuleStat, bool) {
	for _, s := range stats {
		if s.RuleID == id {
			return s, true
		}
	}
	return RuleStat{}, false
}

// forbiddenGraph has one contract package (tenancy) that directly imports
// the GoAkt runtime: a plain forbidden import in a contract package.
func forbiddenGraph() Graph {
	return Graph{Packages: []Package{
		{
			ImportPath: root + "/tenancy",
			Kind:       RootModule,
			Imports:    []string{"context", "github.com/tochemey/goakt/v4/actor"},
		},
	}}
}

// allowedGraph exercises every allowed edge the rules table names, plus the
// specific persistence/conformance carve-out, and must produce zero
// violations.
func allowedGraph() Graph {
	return withRepoModules(Graph{Packages: []Package{
		{
			ImportPath: root + "/tenancy",
			Kind:       RootModule,
			Imports:    []string{"context", "fmt"},
		},
		{
			ImportPath: root + "/command",
			Kind:       RootModule,
			Imports:    []string{root + "/tenancy", "google.golang.org/protobuf/proto"},
		},
		{
			ImportPath: root + "/persistence",
			Kind:       RootModule,
			Imports:    []string{root + "/egopb", root + "/tenancy"},
		},
		{
			ImportPath: root + "/eventstream",
			Kind:       RootModule,
			Imports:    []string{root + "/internal/queue", root + "/internal/syncmap", "github.com/google/uuid", "go.uber.org/atomic"},
		},
		{
			// Test support, not a contract: it may import anything, e.g.
			// testify, and must not be flagged.
			ImportPath: root + "/persistence/conformance",
			Kind:       RootModule,
			Imports:    []string{"github.com/stretchr/testify/require"},
		},
		{
			ImportPath: root + "/migration",
			Kind:       RootModule,
			Imports:    []string{root + "/persistence", root + "/tenancy"},
		},
		{
			// The composition package may import contracts
			// (composition-no-runtime), and the composition root under
			// compose/ may import it (composition-leaf).
			ImportPath: root + "/compose",
			Name:       "compose",
			Kind:       RootModule,
			Imports:    []string{root + "/persistence", root + "/port/publishing"},
		},
		{
			ImportPath: root + "/compose/goakt",
			Name:       "goakt",
			Kind:       RootModule,
			Imports:    []string{root, root + "/compose", "github.com/tochemey/goakt/v4"},
		},
		{
			// A main package may import the composition root.
			ImportPath: root + "/example/eventssourced",
			Name:       "main",
			Kind:       RootModule,
			Imports:    []string{root + "/compose/goakt"},
		},
		{
			ImportPath: root + "/publisher/kafka",
			Kind:       NestedModule,
			Imports:    []string{root + "/egopb", "github.com/segmentio/kafka-go"},
		},
		{
			ImportPath: root + "/benchmark",
			Kind:       NestedModule,
			Imports:    []string{root + "/persistence"},
		},
	}})
}

func TestEvaluate_ForbiddenImportInContractPackageFails(t *testing.T) {
	ruleset := rulesFor(t, "contract-allowlist")
	result, err := Evaluate(forbiddenGraph(), ruleset, nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Violations) != 1 {
		t.Fatalf("len(Violations) = %d, want 1: %+v", len(result.Violations), result.Violations)
	}
	v := result.Violations[0]
	if v.Importer != root+"/tenancy" {
		t.Errorf("Importer = %q, want %s/tenancy", v.Importer, root)
	}
	if v.Import != "github.com/tochemey/goakt/v4/actor" {
		t.Errorf("Import = %q, want github.com/tochemey/goakt/v4/actor", v.Import)
	}
	if v.Rule != "contract-allowlist" {
		t.Errorf("Rule = %q, want contract-allowlist", v.Rule)
	}

	report := FormatReport(result, ruleset)
	for _, want := range []string{v.Importer, v.Import, v.Rule} {
		if !strings.Contains(report, want) {
			t.Errorf("report %q does not contain %q", report, want)
		}
	}
}

func TestEvaluate_AllowedGraphPasses(t *testing.T) {
	result, err := Evaluate(allowedGraph(), DefaultRules(root), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Violations) != 0 {
		t.Fatalf("len(Violations) = %d, want 0: %+v", len(result.Violations), result.Violations)
	}
	if len(result.Stale) != 0 {
		t.Fatalf("len(Stale) = %d, want 0: %+v", len(result.Stale), result.Stale)
	}
}

func TestEvaluate_PersistenceConformanceImportingTestifyIsNotAViolation(t *testing.T) {
	result, err := Evaluate(allowedGraph(), DefaultRules(root), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	for _, v := range result.Violations {
		if v.Importer == root+"/persistence/conformance" {
			t.Fatalf("persistence/conformance must not be flagged, got %+v", v)
		}
	}
}

func TestEvaluate_BaselinedViolationPasses(t *testing.T) {
	baseline := []BaselineEntry{
		{
			Importer:         root + "/tenancy",
			Import:           "github.com/tochemey/goakt/v4/actor",
			Rule:             "contract-allowlist",
			Owner:            "@pablogore",
			Justification:    "test fixture",
			RemovalCriterion: "never; test only",
		},
	}
	result, err := Evaluate(forbiddenGraph(), rulesFor(t, "contract-allowlist"), baseline)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Violations) != 0 {
		t.Fatalf("len(Violations) = %d, want 0 (baselined): %+v", len(result.Violations), result.Violations)
	}
	if len(result.Stale) != 0 {
		t.Fatalf("len(Stale) = %d, want 0 (entry was used): %+v", len(result.Stale), result.Stale)
	}
}

func TestEvaluate_StaleBaselineEntryIsReported(t *testing.T) {
	baseline := []BaselineEntry{
		{
			Importer:         root + "/tenancy",
			Import:           "github.com/tochemey/goakt/v4/nonexistent",
			Rule:             "contract-allowlist",
			Owner:            "@pablogore",
			Justification:    "test fixture",
			RemovalCriterion: "never; test only",
		},
	}
	// forbiddenGraph's real violation (.../actor) is NOT in this baseline,
	// so it must still be reported, alongside the stale entry above.
	ruleset := rulesFor(t, "contract-allowlist")
	result, err := Evaluate(forbiddenGraph(), ruleset, baseline)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Stale) != 1 {
		t.Fatalf("len(Stale) = %d, want 1: %+v", len(result.Stale), result.Stale)
	}
	if len(result.Violations) != 1 {
		t.Fatalf("len(Violations) = %d, want 1 (unbaselined real violation): %+v", len(result.Violations), result.Violations)
	}

	report := FormatReport(result, ruleset)
	if !strings.Contains(report, "no longer matches a violation") {
		t.Errorf("report %q does not mention the stale entry", report)
	}
}

func TestValidateBaseline_MissingFieldsRejected(t *testing.T) {
	cases := []struct {
		name  string
		entry BaselineEntry
	}{
		{"missing owner", BaselineEntry{Importer: "a", Import: "b", Rule: "contract-allowlist", Justification: "j", RemovalCriterion: "r"}},
		{"missing justification", BaselineEntry{Importer: "a", Import: "b", Rule: "contract-allowlist", Owner: "@x", RemovalCriterion: "r"}},
		{"missing removal criterion", BaselineEntry{Importer: "a", Import: "b", Rule: "contract-allowlist", Owner: "@x", Justification: "j"}},
		{"unknown rule", BaselineEntry{Importer: "a", Import: "b", Rule: "no-such-rule", Owner: "@x", Justification: "j", RemovalCriterion: "r"}},
		{"empty importer", BaselineEntry{Importer: "", Import: "b", Rule: "contract-allowlist", Owner: "@x", Justification: "j", RemovalCriterion: "r"}},
		{"blank importer", BaselineEntry{Importer: "   ", Import: "b", Rule: "contract-allowlist", Owner: "@x", Justification: "j", RemovalCriterion: "r"}},
		{"empty import", BaselineEntry{Importer: "a", Import: "", Rule: "contract-allowlist", Owner: "@x", Justification: "j", RemovalCriterion: "r"}},
		{"blank import", BaselineEntry{Importer: "a", Import: "   ", Rule: "contract-allowlist", Owner: "@x", Justification: "j", RemovalCriterion: "r"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := ValidateBaseline([]BaselineEntry{c.entry}, DefaultRules(root)); err == nil {
				t.Fatalf("ValidateBaseline() = nil error, want a rejection for %+v", c.entry)
			}
		})
	}
}

func TestValidateBaseline_DuplicateEntryRejected(t *testing.T) {
	entry := BaselineEntry{
		Importer:         root + "/publisher/kafka",
		Import:           root,
		Rule:             "external-adapter-no-runtime",
		Owner:            "@pablogore",
		Justification:    "test fixture",
		RemovalCriterion: "never; test only",
	}
	// Two entries with the same (Importer, Import, Rule): the second is a
	// duplicate even though the whole struct is byte-identical.
	err := ValidateBaseline([]BaselineEntry{entry, entry}, DefaultRules(root))
	if err == nil {
		t.Fatal("ValidateBaseline() = nil error, want a rejection for a duplicate entry")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error %q does not mention duplicate", err)
	}
}

func TestValidateBaseline_DuplicateEntryDifferentRuleIsNotADuplicate(t *testing.T) {
	// Same Importer and Import, but a different Rule: not the same key, so
	// not a duplicate (a real import could plausibly break two rules at
	// once, e.g. a nested module's root-package import).
	a := BaselineEntry{
		Importer:         root + "/publisher/kafka",
		Import:           root,
		Rule:             "external-adapter-no-runtime",
		Owner:            "@pablogore",
		Justification:    "test fixture",
		RemovalCriterion: "never; test only",
	}
	b := a
	b.Rule = "no-cross-module-internal"
	if err := ValidateBaseline([]BaselineEntry{a, b}, DefaultRules(root)); err != nil {
		t.Fatalf("ValidateBaseline() = %v, want nil: same importer/import but different rules is not a duplicate", err)
	}
}

func TestEvaluate_RejectsMalformedBaseline(t *testing.T) {
	baseline := []BaselineEntry{
		{Importer: "a", Import: "b", Rule: "contract-allowlist"}, // no owner/justification/removal
	}
	if _, err := Evaluate(allowedGraph(), DefaultRules(root), baseline); err == nil {
		t.Fatal("Evaluate() = nil error, want a rejection for a malformed baseline")
	}
}

// TestEvaluate_ZeroMatchRulesFailClosed reproduces the R1 bug directly: a
// graph whose packages live under a different module path than the one
// DefaultRules was built for. Every rule's Layer is root-module-path
// specific (or requires a nested module, which this graph also has none
// of), so every rule matches zero packages, and Evaluate must reject that
// instead of silently reporting a clean run.
func TestEvaluate_ZeroMatchRulesFailClosed(t *testing.T) {
	graph := Graph{Packages: []Package{
		{
			ImportPath: "github.com/other/module/tenancy",
			Kind:       RootModule,
			Imports:    []string{"context"},
		},
	}}
	_, err := Evaluate(graph, DefaultRules(root), nil)
	if err == nil {
		t.Fatal("Evaluate() = nil error, want an error naming every rule that matched zero packages")
	}
	for _, id := range []string{"contract-allowlist", "application-no-runtime", "external-adapter-no-runtime", "no-cross-module-internal", "no-module-cycle", "composition-no-runtime", "composition-leaf"} {
		if !strings.Contains(err.Error(), id) {
			t.Errorf("error %q does not name rule %s", err, id)
		}
	}
}

// TestEvaluate_ContractAllowlistDoesNotNeedPortPackages proves the
// zero-match check is per rule, not per contract root: contract-allowlist
// covers nine roots (tenancy, command, ..., port), and a graph that has no
// port/... package at all must not trip the check, because the rule as a
// whole still matched real contract packages.
func TestEvaluate_ContractAllowlistDoesNotNeedPortPackages(t *testing.T) {
	graph := Graph{Packages: []Package{
		{ImportPath: root + "/tenancy", Kind: RootModule, Imports: []string{"context"}},
		{ImportPath: root + "/migration", Kind: RootModule, Imports: []string{root + "/tenancy"}},
		{ImportPath: root + "/compose", Name: "compose", Kind: RootModule, Imports: []string{"context"}},
		{ImportPath: root + "/publisher/kafka", Kind: NestedModule, Imports: []string{root + "/egopb"}},
	}}
	result, err := Evaluate(withRepoModules(graph), DefaultRules(root), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error even though every rule matched something: %v", err)
	}
	if len(result.Violations) != 0 {
		t.Fatalf("len(Violations) = %d, want 0: %+v", len(result.Violations), result.Violations)
	}
	stat, ok := ruleStat(result.RuleStats, "contract-allowlist")
	if !ok {
		t.Fatalf("no RuleStat for contract-allowlist in %+v", result.RuleStats)
	}
	if stat.PackagesMatched == 0 {
		t.Fatalf("contract-allowlist PackagesMatched = 0, want > 0 (tenancy alone should count) even with no port/... package present")
	}
}

// TestEvaluate_SummaryCountsAreExactAndDeduped exercises the exact scenario
// the R1 problem statement names: publisher/kafka is a nested module, so
// both external-adapter-no-runtime and no-cross-module-internal check its
// imports; PackagesChecked and EdgesChecked must count each package and
// each (importer, import) pair once, not once per rule that inspected it.
func TestEvaluate_SummaryCountsAreExactAndDeduped(t *testing.T) {
	graph := Graph{Packages: []Package{
		{ImportPath: root + "/tenancy", Kind: RootModule, Imports: []string{"context"}},
		{ImportPath: root + "/migration", Kind: RootModule, Imports: []string{root + "/tenancy"}},
		{ImportPath: root + "/compose", Name: "compose", Kind: RootModule, Imports: []string{"context"}},
		{ImportPath: root + "/publisher/kafka", Kind: NestedModule, Imports: []string{root, root + "/egopb"}},
	}}
	baseline := []BaselineEntry{
		{
			Importer:         root + "/publisher/kafka",
			Import:           root,
			Rule:             "external-adapter-no-runtime",
			Owner:            "@pablogore",
			Justification:    "test fixture",
			RemovalCriterion: "never; test only",
		},
	}
	result, err := Evaluate(withRepoModules(graph), DefaultRules(root), baseline)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Violations) != 0 {
		t.Fatalf("len(Violations) = %d, want 0 (baselined): %+v", len(result.Violations), result.Violations)
	}
	if len(result.Stale) != 0 {
		t.Fatalf("len(Stale) = %d, want 0 (entry was used): %+v", len(result.Stale), result.Stale)
	}
	// tenancy, migration, compose and publisher/kafka: 4 distinct
	// packages, each matched by at least one rule (tenancy and migration by
	// two: their own layer and composition-leaf), counted once each.
	if result.PackagesChecked != 4 {
		t.Errorf("PackagesChecked = %d, want 4: %+v", result.PackagesChecked, result.RuleStats)
	}
	// migration -> tenancy (1, checked by application-no-runtime and
	// composition-leaf but counted once), publisher/kafka -> root (1,
	// checked by two rules but counted once) and publisher/kafka -> egopb
	// (1) = 3. tenancy and compose have no non-stdlib imports.
	if result.EdgesChecked != 3 {
		t.Errorf("EdgesChecked = %d, want 3 (deduped): %+v", result.EdgesChecked, result.RuleStats)
	}
}

// TestEvaluate_ViolationReasonNamesForbiddenPrefix proves a denylist rule's
// Violation.Reason names the specific forbidden prefix or import that
// matched, not just a generic restatement of the rule's layer name.
func TestEvaluate_ViolationReasonNamesForbiddenPrefix(t *testing.T) {
	cases := []struct {
		name       string
		ruleID     string
		graph      Graph
		wantImport string
		wantSubstr string
	}{
		{
			name:   "application-no-runtime root import",
			ruleID: "application-no-runtime",
			graph: Graph{Packages: []Package{
				{ImportPath: root + "/migration", Kind: RootModule, Imports: []string{root}},
			}},
			wantImport: root,
			wantSubstr: root,
		},
		{
			name:   "application-no-runtime internal/extensions import",
			ruleID: "application-no-runtime",
			graph: Graph{Packages: []Package{
				{ImportPath: root + "/migration", Kind: RootModule, Imports: []string{root + "/internal/extensions"}},
			}},
			wantImport: root + "/internal/extensions",
			wantSubstr: "internal/extensions",
		},
		{
			name:   "application-no-runtime goakt import",
			ruleID: "application-no-runtime",
			graph: Graph{Packages: []Package{
				{ImportPath: root + "/migration", Kind: RootModule, Imports: []string{"github.com/tochemey/goakt/v4/actor"}},
			}},
			wantImport: "github.com/tochemey/goakt/v4/actor",
			wantSubstr: "goakt",
		},
		{
			name:   "external-adapter-no-runtime goakt import",
			ruleID: "external-adapter-no-runtime",
			graph: Graph{Packages: []Package{
				{ImportPath: root + "/publisher/kafka", Kind: NestedModule, Imports: []string{"github.com/tochemey/goakt/v4"}},
			}},
			wantImport: "github.com/tochemey/goakt/v4",
			wantSubstr: "goakt",
		},
		{
			name:   "no-cross-module-internal import",
			ruleID: "no-cross-module-internal",
			graph: Graph{Packages: []Package{
				{ImportPath: root + "/benchmark", Kind: NestedModule, Imports: []string{root + "/internal/queue"}},
			}},
			wantImport: root + "/internal/queue",
			wantSubstr: "internal",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result, err := Evaluate(withRepoModules(c.graph), rulesFor(t, c.ruleID), nil)
			if err != nil {
				t.Fatalf("Evaluate returned error: %v", err)
			}
			if len(result.Violations) != 1 {
				t.Fatalf("len(Violations) = %d, want 1: %+v", len(result.Violations), result.Violations)
			}
			v := result.Violations[0]
			if v.Import != c.wantImport {
				t.Fatalf("Import = %q, want %q", v.Import, c.wantImport)
			}
			if !strings.Contains(v.Reason, c.wantSubstr) {
				t.Errorf("Reason = %q, want it to contain %q (rule-specific, not just the layer name)", v.Reason, c.wantSubstr)
			}
		})
	}
}

func TestIsStdlib(t *testing.T) {
	cases := map[string]bool{
		"fmt":                              true,
		"os/exec":                          true,
		"context":                          true,
		"github.com/google/uuid":           false,
		"google.golang.org/protobuf/proto": false,
	}
	for path, want := range cases {
		if got := IsStdlib(path); got != want {
			t.Errorf("IsStdlib(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestEvaluate_StdlibIsAlwaysAllowed(t *testing.T) {
	graph := Graph{Packages: []Package{
		{
			ImportPath: root + "/tenancy",
			Kind:       RootModule,
			Imports:    []string{"context"},
		},
		{
			ImportPath: root + "/migration",
			Kind:       RootModule,
			Imports:    []string{"context", "os", "sync", "encoding/json"},
		},
		{
			ImportPath: root + "/compose",
			Name:       "compose",
			Kind:       RootModule,
			Imports:    []string{"errors", "reflect", "net/http"},
		},
		{
			ImportPath: root + "/publisher/kafka",
			Kind:       NestedModule,
			Imports:    []string{"fmt", "net/http"},
		},
	}}
	result, err := Evaluate(withRepoModules(graph), DefaultRules(root), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Violations) != 0 {
		t.Fatalf("len(Violations) = %d, want 0: %+v", len(result.Violations), result.Violations)
	}
}

// TestContractAllowlist_ForbidsTransportAndDatabaseStdlib proves the I1
// restriction (design.md §3): contract-allowlist forbids net/http, net/rpc
// and database/sql, and everything under them, matched by whole path
// segment, even though they are standard library — while the rest of
// stdlib (e.g. net/url) stays allowed, and a lookalike path that merely
// shares a prefix character-wise (net/httpx) is not caught by the
// segment-based match.
func TestContractAllowlist_ForbidsTransportAndDatabaseStdlib(t *testing.T) {
	cases := []struct {
		name          string
		importPath    string
		wantViolation bool
	}{
		{"net/http forbidden", "net/http", true},
		{"net/http/httptest forbidden (subpackage)", "net/http/httptest", true},
		{"net/rpc forbidden", "net/rpc", true},
		{"database/sql forbidden", "database/sql", true},
		{"net/url allowed (rest of stdlib)", "net/url", false},
		{"net/httpx allowed (path-segment match only, not substring)", "net/httpx", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			graph := Graph{Packages: []Package{
				{ImportPath: root + "/tenancy", Kind: RootModule, Imports: []string{c.importPath}},
			}}
			result, err := Evaluate(graph, rulesFor(t, "contract-allowlist"), nil)
			if err != nil {
				t.Fatalf("Evaluate returned error: %v", err)
			}
			if !c.wantViolation {
				if len(result.Violations) != 0 {
					t.Fatalf("len(Violations) = %d, want 0: %+v", len(result.Violations), result.Violations)
				}
				return
			}
			if len(result.Violations) != 1 {
				t.Fatalf("len(Violations) = %d, want 1: %+v", len(result.Violations), result.Violations)
			}
			v := result.Violations[0]
			if v.Importer != root+"/tenancy" {
				t.Errorf("Importer = %q, want %s/tenancy", v.Importer, root)
			}
			if v.Import != c.importPath {
				t.Errorf("Import = %q, want %q", v.Import, c.importPath)
			}
			if v.Rule != "contract-allowlist" {
				t.Errorf("Rule = %q, want contract-allowlist", v.Rule)
			}
			if !strings.Contains(v.Reason, c.importPath) {
				t.Errorf("Reason = %q, want it to name the forbidden import %q", v.Reason, c.importPath)
			}
		})
	}
}

// TestApplicationNoRuntime_NetHTTPImportPasses proves the I1 stdlib
// restriction applies only to contract-allowlist: a non-contract package
// (migration, checked by application-no-runtime) may still import
// net/http, because application-no-runtime carries no stdlib denylist.
func TestApplicationNoRuntime_NetHTTPImportPasses(t *testing.T) {
	graph := Graph{Packages: []Package{
		{ImportPath: root + "/migration", Kind: RootModule, Imports: []string{"net/http"}},
	}}
	result, err := Evaluate(graph, rulesFor(t, "application-no-runtime"), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Violations) != 0 {
		t.Fatalf("len(Violations) = %d, want 0: %+v", len(result.Violations), result.Violations)
	}
}

func TestEvaluate_OutputOrderingIsDeterministic(t *testing.T) {
	graph := Graph{Packages: []Package{
		{
			ImportPath: root + "/migration",
			Kind:       RootModule,
			Imports:    []string{root, "github.com/tochemey/goakt/v4"},
		},
		{
			ImportPath: root + "/command",
			Kind:       RootModule,
			Imports:    []string{"github.com/tochemey/goakt/v4/actor"},
		},
	}}
	ruleset := rulesFor(t, "application-no-runtime", "contract-allowlist")
	first, err := Evaluate(graph, ruleset, nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	// Reverse the package order and re-evaluate: the sorted output must be
	// identical regardless of input order.
	reversed := Graph{Packages: []Package{graph.Packages[1], graph.Packages[0]}}
	second, err := Evaluate(reversed, ruleset, nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(first.Violations) < 2 {
		t.Fatalf("expected at least 2 violations to prove ordering, got %d", len(first.Violations))
	}
	for i := range first.Violations {
		if first.Violations[i] != second.Violations[i] {
			t.Fatalf("non-deterministic ordering: %+v vs %+v", first.Violations, second.Violations)
		}
	}
	for i := 1; i < len(first.Violations); i++ {
		a, b := first.Violations[i-1], first.Violations[i]
		if a.Importer > b.Importer {
			t.Fatalf("Violations not sorted by Importer: %+v then %+v", a, b)
		}
	}
}

func TestApplicationNoRuntime_ForbidsRootAndGoAktAndExtensions(t *testing.T) {
	graph := Graph{Packages: []Package{
		{
			ImportPath: root + "/migration",
			Kind:       RootModule,
			Imports:    []string{root, root + "/internal/extensions", "github.com/tochemey/goakt/v4"},
		},
	}}
	result, err := Evaluate(graph, rulesFor(t, "application-no-runtime"), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Violations) != 3 {
		t.Fatalf("len(Violations) = %d, want 3: %+v", len(result.Violations), result.Violations)
	}
}

func TestExternalAdapterNoRuntime_ForbidsRootAndGoAktOnly(t *testing.T) {
	graph := Graph{Packages: []Package{
		{
			ImportPath: root + "/publisher/kafka",
			Kind:       NestedModule,
			Imports:    []string{root, "github.com/tochemey/goakt/v4/actor", root + "/egopb"},
		},
	}}
	result, err := Evaluate(graph, rulesFor(t, "external-adapter-no-runtime"), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Violations) != 2 {
		t.Fatalf("len(Violations) = %d, want 2 (root + goakt, not egopb): %+v", len(result.Violations), result.Violations)
	}
}

// TestApplicationNoRuntime_SourceIsDesignDoc proves I3 (design.md §3 now
// documents an explicit Application section for migration, matching this
// rule): application-no-runtime's Source names design.md §3 directly,
// rather than the older "#107 scope" placeholder from before the ADR
// caught up with what archcheck actually enforces.
func TestApplicationNoRuntime_SourceIsDesignDoc(t *testing.T) {
	rule, ok := ruleByID(DefaultRules(root), "application-no-runtime")
	if !ok {
		t.Fatal("application-no-runtime rule not found in DefaultRules")
	}
	if rule.Source != "design.md §3" {
		t.Errorf("Source = %q, want %q", rule.Source, "design.md §3")
	}
}

// The description is printed with every violation, so it must name the
// standard-library packages the rule forbids.
func TestContractAllowlist_DescriptionNamesStdlibExceptions(t *testing.T) {
	rule, ok := ruleByID(DefaultRules(root), "contract-allowlist")
	if !ok {
		t.Fatal("contract-allowlist rule not found in DefaultRules")
	}
	for _, want := range []string{"net/http", "net/rpc", "database/sql"} {
		if !strings.Contains(rule.Description, want) {
			t.Errorf("Description = %q, want it to mention %q", rule.Description, want)
		}
	}
}

func TestNoCrossModuleInternal_ForbidsRootInternal(t *testing.T) {
	graph := Graph{Packages: []Package{
		{
			ImportPath: root + "/benchmark",
			Kind:       NestedModule,
			Imports:    []string{root + "/internal/queue"},
		},
	}}
	result, err := Evaluate(withRepoModules(graph), rulesFor(t, "no-cross-module-internal"), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Violations) != 1 {
		t.Fatalf("len(Violations) = %d, want 1: %+v", len(result.Violations), result.Violations)
	}
	if result.Violations[0].Rule != "no-cross-module-internal" {
		t.Errorf("Rule = %q, want no-cross-module-internal", result.Violations[0].Rule)
	}
}
