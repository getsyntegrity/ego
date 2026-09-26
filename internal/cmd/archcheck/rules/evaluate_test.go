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
	return Graph{Packages: []Package{
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
			ImportPath: root + "/publisher/kafka",
			Kind:       NestedModule,
			Imports:    []string{root + "/egopb", "github.com/segmentio/kafka-go"},
		},
		{
			ImportPath: root + "/benchmark",
			Kind:       NestedModule,
			Imports:    []string{root + "/persistence"},
		},
	}}
}

func TestEvaluate_ForbiddenImportInContractPackageFails(t *testing.T) {
	result, err := Evaluate(forbiddenGraph(), DefaultRules(), nil)
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

	report := FormatReport(result, DefaultRules())
	for _, want := range []string{v.Importer, v.Import, v.Rule} {
		if !strings.Contains(report, want) {
			t.Errorf("report %q does not contain %q", report, want)
		}
	}
}

func TestEvaluate_AllowedGraphPasses(t *testing.T) {
	result, err := Evaluate(allowedGraph(), DefaultRules(), nil)
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
	result, err := Evaluate(allowedGraph(), DefaultRules(), nil)
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
	result, err := Evaluate(forbiddenGraph(), DefaultRules(), baseline)
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
	result, err := Evaluate(forbiddenGraph(), DefaultRules(), baseline)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Stale) != 1 {
		t.Fatalf("len(Stale) = %d, want 1: %+v", len(result.Stale), result.Stale)
	}
	if len(result.Violations) != 1 {
		t.Fatalf("len(Violations) = %d, want 1 (unbaselined real violation): %+v", len(result.Violations), result.Violations)
	}

	report := FormatReport(result, DefaultRules())
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
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := ValidateBaseline([]BaselineEntry{c.entry}, DefaultRules()); err == nil {
				t.Fatalf("ValidateBaseline() = nil error, want a rejection for %+v", c.entry)
			}
		})
	}
}

func TestEvaluate_RejectsMalformedBaseline(t *testing.T) {
	baseline := []BaselineEntry{
		{Importer: "a", Import: "b", Rule: "contract-allowlist"}, // no owner/justification/removal
	}
	if _, err := Evaluate(allowedGraph(), DefaultRules(), baseline); err == nil {
		t.Fatal("Evaluate() = nil error, want a rejection for a malformed baseline")
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
			ImportPath: root + "/migration",
			Kind:       RootModule,
			Imports:    []string{"context", "os", "sync", "encoding/json"},
		},
		{
			ImportPath: root + "/publisher/kafka",
			Kind:       NestedModule,
			Imports:    []string{"fmt", "net/http"},
		},
	}}
	result, err := Evaluate(graph, DefaultRules(), nil)
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
	first, err := Evaluate(graph, DefaultRules(), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	// Reverse the package order and re-evaluate: the sorted output must be
	// identical regardless of input order.
	reversed := Graph{Packages: []Package{graph.Packages[1], graph.Packages[0]}}
	second, err := Evaluate(reversed, DefaultRules(), nil)
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
	result, err := Evaluate(graph, DefaultRules(), nil)
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
	result, err := Evaluate(graph, DefaultRules(), nil)
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if len(result.Violations) != 2 {
		t.Fatalf("len(Violations) = %d, want 2 (root + goakt, not egopb): %+v", len(result.Violations), result.Violations)
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
	result, err := Evaluate(graph, DefaultRules(), nil)
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
