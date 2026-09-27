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

package selector

import (
	"strings"
	"testing"
)

// moduleFixtureOpts returns Options wired for the fixtureGraph's two
// satellite modules (kafka-like "publisher/kafka" and "publisher/nats"),
// used across the module-selection tests below. kafka imports the root
// module's "command" package; nats imports nothing from the root module,
// so it is only ever selected by a full gate or by its own changed files.
func moduleFixtureOpts(extraChanged ...string) Options {
	const mod = "github.com/x/mod"
	opts := satelliteOpts("publisher/kafka", "publisher/nats")
	opts.Modules = []Module{
		{Dir: "publisher/kafka", Imports: []string{mod + "/command"}},
		{Dir: "publisher/nats", Imports: nil},
	}
	return opts
}

func moduleDirs(sel []ModuleSelection) []string {
	dirs := make([]string, 0, len(sel))
	for _, m := range sel {
		dirs = append(dirs, m.Dir)
	}
	return dirs
}

func TestSelect_Modules_ChangedFileSelectsOnlyThatModule(t *testing.T) {
	g := fixtureGraph()
	res := Select(g, []string{"publisher/kafka/producer.go"}, moduleFixtureOpts())

	assertSameSet(t, moduleDirs(res.Modules), []string{"publisher/kafka"})
	if res.Modules[0].Reason != "changed files in publisher/kafka" {
		t.Fatalf("Reason = %q, want %q", res.Modules[0].Reason, "changed files in publisher/kafka")
	}
}

func TestSelect_Modules_SatelliteChangeIsReportedAsModuleLane(t *testing.T) {
	g := fixtureGraph()
	res := Select(g, []string{"publisher/kafka/producer.go"}, moduleFixtureOpts())

	want := "publisher/kafka/producer.go is inside nested module publisher/kafka (verified by the nested module lane)"
	if res.Changed[0].Reason != want {
		t.Fatalf("Changed[0].Reason = %q, want %q", res.Changed[0].Reason, want)
	}
	summary := BuildSummary(res)
	if strings.Contains(summary, "not covered") {
		t.Fatalf("summary still claims nested module changes are not covered:\n%s", summary)
	}
	if !strings.Contains(summary, "verified by the nested module lane") {
		t.Fatalf("summary does not point at the nested module lane:\n%s", summary)
	}
}

func TestSelect_Modules_DocsOnlyIsNone(t *testing.T) {
	g := fixtureGraph()
	res := Select(g, []string{"README.md"}, moduleFixtureOpts())

	if len(res.Modules) != 0 {
		t.Fatalf("Modules = %v, want none", res.Modules)
	}
}

func TestSelect_Modules_RootLeafChangeNotImportedByAnyModuleSelectsNoneAndKeepsRootFastLane(t *testing.T) {
	g := fixtureGraph()
	res := Select(g, []string{"internal/pause/x.go"}, moduleFixtureOpts())

	if res.Mode != ModeAffected {
		t.Fatalf("Mode = %s, want %s (root fast lane must be unaffected)", res.Mode, ModeAffected)
	}
	if len(res.Modules) != 0 {
		t.Fatalf("Modules = %v, want none: neither kafka nor nats imports internal/pause or the root package", res.Modules)
	}
}

func TestSelect_Modules_RootChangeImportedByModuleSelectsIt(t *testing.T) {
	g := fixtureGraph()
	res := Select(g, []string{"command/x.go"}, moduleFixtureOpts())

	if res.Mode != ModeAffected {
		t.Fatalf("Mode = %s, want %s (root fast lane must be unaffected)", res.Mode, ModeAffected)
	}
	assertSameSet(t, moduleDirs(res.Modules), []string{"publisher/kafka"})
	want := "imports affected root package github.com/x/mod/command"
	if res.Modules[0].Reason != want {
		t.Fatalf("Reason = %q, want %q", res.Modules[0].Reason, want)
	}
}

func TestSelect_Modules_AllSelectsEveryModule(t *testing.T) {
	g := fixtureGraph()
	res := Select(g, nil, func() Options {
		o := moduleFixtureOpts()
		o.All = true
		return o
	}())

	assertSameSet(t, moduleDirs(res.Modules), []string{"publisher/kafka", "publisher/nats"})
	for _, m := range res.Modules {
		if m.Reason != "full gate: -all requested" {
			t.Fatalf("Reason = %q, want the -all full-gate reason", m.Reason)
		}
	}
}

func TestSelect_Modules_RootGoModChangeSelectsEveryModule(t *testing.T) {
	g := fixtureGraph()
	res := Select(g, []string{"go.mod"}, moduleFixtureOpts())

	assertSameSet(t, moduleDirs(res.Modules), []string{"publisher/kafka", "publisher/nats"})
}

func TestSelect_Modules_CIPathChangeSelectsEveryModule(t *testing.T) {
	g := fixtureGraph()
	for _, path := range []string{
		".github/workflows/pull_request.yml",
		"scripts/ci/go-test.sh",
		"internal/cmd/ciselect/main.go",
		".golangci.yml",
	} {
		res := Select(g, []string{path}, moduleFixtureOpts())
		assertSameSet(t, moduleDirs(res.Modules), []string{"publisher/kafka", "publisher/nats"})
	}
}
