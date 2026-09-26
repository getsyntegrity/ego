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

import "fmt"

// Kind records whether a Rule is written as an allowlist (only the named
// imports are permitted; everything else is forbidden) or a denylist (only
// the named imports are forbidden; everything else is permitted). Evaluate
// never branches on it — Forbids already encodes the semantics — but a
// report uses it to phrase a rule's hint correctly.
type Kind int

const (
	// Allowlist rules forbid anything not on their allowed list.
	Allowlist Kind = iota
	// Denylist rules forbid only the named targets.
	Denylist
)

// String renders k for report hints.
func (k Kind) String() string {
	switch k {
	case Allowlist:
		return "allowlist"
	case Denylist:
		return "denylist"
	default:
		return "unknown"
	}
}

// Rule is one dependency-direction rule from design.md §3 (or, for
// application-no-runtime and no-cross-module-internal, from the #107
// issue scope that extends it to layers the ADR names but does not yet
// enforce). A Rule applies to every package Layer.Match selects, and
// forbids every non-stdlib import Forbids reports true for; Evaluate
// filters out stdlib imports before calling Forbids, so Forbids is never
// asked about one.
type Rule struct {
	// ID is the rule's stable identifier, used in reports and by
	// BaselineEntry.Rule to reference it, e.g. "contract-allowlist".
	ID string
	// Description explains the rule in one sentence, for reports.
	Description string
	// Source names where the rule comes from, e.g. "design.md §3".
	Source string
	// Layer is which packages this rule applies to.
	Layer Layer
	// Semantics records whether this rule reads as an allowlist or a
	// denylist, for report hints; Forbids is the actual decision.
	Semantics Kind
	// Forbids reports whether importPath breaks this rule for a package in
	// Layer. Never called for a stdlib import.
	Forbids func(importPath string) bool
}

// allowedContractImport reports whether importPath is one of the targets
// design.md §3 allows a contract package to import, besides stdlib and
// other contract packages (handled separately, since they need the
// relative-path check isContractRelPath, not a flat list).
func allowedContractImport(importPath string) bool {
	switch importPath {
	case rootModulePath + "/egopb",
		rootModulePath + "/internal/queue",
		rootModulePath + "/internal/syncmap",
		"github.com/google/uuid",
		"go.uber.org/atomic":
		return true
	}
	return hasPathOrSubpath(importPath, "google.golang.org/protobuf")
}

// DefaultRules returns the repository's current rule table (design.md §3
// plus the #107 scope additions). It returns a fresh slice on every call;
// Rule holds only funcs and strings, so callers may freely keep or discard
// the result without sharing mutable state.
func DefaultRules() []Rule {
	return []Rule{
		{
			ID:          "contract-allowlist",
			Description: "contract packages may import only stdlib, other contract packages, egopb, google.golang.org/protobuf/..., internal/queue, internal/syncmap, github.com/google/uuid and go.uber.org/atomic",
			Source:      "design.md §3",
			Layer:       ContractLayer,
			Semantics:   Allowlist,
			Forbids: func(importPath string) bool {
				if allowedContractImport(importPath) {
					return false
				}
				rel := stripRootModulePrefix(importPath)
				if isContractRelPath(rel) {
					return false
				}
				return true
			},
		},
		{
			ID:          "application-no-runtime",
			Description: "the migration application must not import the root package ego, internal/extensions or the GoAkt runtime",
			Source:      "#107 scope",
			Layer:       ApplicationLayer,
			Semantics:   Denylist,
			Forbids: func(importPath string) bool {
				if importPath == rootModulePath {
					return true
				}
				if hasPathOrSubpath(importPath, rootModulePath+"/internal/extensions") {
					return true
				}
				return hasPathOrSubpath(importPath, "github.com/tochemey/goakt/v4")
			},
		},
		{
			ID:          "external-adapter-no-runtime",
			Description: "nested adapter modules under publisher/ must not import the root package ego or the GoAkt runtime",
			Source:      "design.md §3",
			Layer:       ExternalAdapterLayer,
			Semantics:   Denylist,
			Forbids: func(importPath string) bool {
				if importPath == rootModulePath {
					return true
				}
				return hasPathOrSubpath(importPath, "github.com/tochemey/goakt/v4")
			},
		},
		{
			ID:          "no-cross-module-internal",
			Description: "a nested module must not import the root module's internal/ packages",
			Source:      "design.md §3",
			Layer:       AnyNestedModuleLayer,
			Semantics:   Denylist,
			Forbids: func(importPath string) bool {
				return hasPathOrSubpath(importPath, rootModulePath+"/internal")
			},
		},
	}
}

// stripRootModulePrefix returns importPath relative to the root module,
// or importPath unchanged if it is not a root-module import path (in
// which case it can never be a contract package either).
func stripRootModulePrefix(importPath string) string {
	const prefix = rootModulePath + "/"
	if len(importPath) <= len(prefix) || importPath[:len(prefix)] != prefix {
		return importPath
	}
	return importPath[len(prefix):]
}

// ruleByID looks up a Rule by ID, for the baseline validator and report
// formatter, which are handed a rule ID (from a Violation or a
// BaselineEntry) and need the full Rule back.
func ruleByID(ruleset []Rule, id string) (Rule, bool) {
	for _, r := range ruleset {
		if r.ID == id {
			return r, true
		}
	}
	return Rule{}, false
}

// unknownRuleErr formats the error ValidateBaseline returns for a baseline
// entry naming a rule ID that is not in ruleset.
func unknownRuleErr(entry BaselineEntry) error {
	return fmt.Errorf("baseline entry %s -> %s: unknown rule %q", entry.Importer, entry.Import, entry.Rule)
}
