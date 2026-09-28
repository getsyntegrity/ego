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

import "testing"

// planDirs returns the directories of the modules a Result selects, root
// included, in plan order.
func planDirs(res Result) []string {
	var dirs []string
	for _, p := range res.Plan {
		if p.Selected {
			dirs = append(dirs, p.Dir)
		}
	}
	return dirs
}

// A change confined to one nested module verifies that module and whatever
// requires it, and never starts the root package lane.
func TestSelectionPolicy_NestedModuleOnlyChange(t *testing.T) {
	res := Select(fixtureGraph(), []string{"publisher/kafka/producer.go"}, moduleFixtureOpts())

	if res.Mode != ModeNone {
		t.Fatalf("Mode = %s, want %s: the root lane must not run", res.Mode, ModeNone)
	}
	if res.Global {
		t.Fatalf("Global = true, want false")
	}
	assertSameSet(t, planDirs(res), []string{"publisher/kafka"})
}

// A leaf root package selects only the root lane; a widely imported one also
// pulls in the nested module that imports it.
func TestSelectionPolicy_RootPackageChanges(t *testing.T) {
	tests := []struct {
		name        string
		changed     string
		wantModules []string
		wantDirs    []string
	}{
		{"leaf", "internal/pause/x.go", nil, []string{"."}},
		{"widely imported", "command/x.go", []string{"publisher/kafka"}, []string{".", "publisher/kafka"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := Select(fixtureGraph(), []string{tc.changed}, moduleFixtureOpts())
			if res.Mode != ModeAffected {
				t.Fatalf("Mode = %s, want %s (reasons=%v)", res.Mode, ModeAffected, res.Reasons)
			}
			assertSameSet(t, moduleDirs(res.Modules), tc.wantModules)
			assertSameSet(t, planDirs(res), tc.wantDirs)
		})
	}
}

// Files that only a tag push, a manual dispatch, a schedule or GitHub itself
// reads can never change how a module builds or tests, so they select nothing.
func TestSelectionPolicy_NoVerificationImpactGitHubFiles(t *testing.T) {
	paths := []string{
		".github/workflows/release.yml",
		".github/workflows/release-publishers.yml",
		".github/workflows/stale.yml",
		".github/CODEOWNERS",
		".github/ISSUE_TEMPLATE/bug_report.md",
		".github/ISSUE_TEMPLATE/config.yml",
	}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			res := Select(fixtureGraph(), []string{p}, moduleFixtureOpts())
			if res.Mode != ModeNone {
				t.Fatalf("Mode = %s, want %s (reasons=%v)", res.Mode, ModeNone, res.Reasons)
			}
			if res.Global {
				t.Fatalf("Global = true, want false")
			}
			if got := planDirs(res); len(got) != 0 {
				t.Fatalf("selected modules = %v, want none", got)
			}
			if res.Changed[0].Class != ClassNoTest {
				t.Fatalf("Class = %s, want %s", res.Changed[0].Class, ClassNoTest)
			}
		})
	}
}

// Anything under .github/ that is not explicitly allow-listed can change the
// verification of every module, so it keeps forcing the full suite. That
// includes files nobody has created yet and names that merely look like an
// allow-listed one.
func TestSelectionPolicy_VerificationImpactGitHubFilesForceFull(t *testing.T) {
	paths := []string{
		".github/workflows/build.yml",
		".github/workflows/pull_request.yml",
		".github/actions/setup/action.yml",
		".github/dependabot.yml",
		".github/brand-new-file.yml",
		".github/workflows/release.yml.bak",
		".github/workflows/release.yml/extra.yml",
		".github/ISSUE_TEMPLATE-evil/run.sh",
		".github/CODEOWNERS/nested",
	}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			g := fixtureGraph()
			res := Select(g, []string{p}, moduleFixtureOpts())
			assertFull(t, g, res)
			if !res.Global {
				t.Fatalf("Global = false, want true")
			}
			assertSameSet(t, planDirs(res), []string{".", "publisher/kafka", "publisher/nats"})
		})
	}
}

// A release-only file alongside a real change must not hide that change, and
// alongside a verification-impacting one must not weaken it.
func TestSelectionPolicy_MixedGitHubChanges(t *testing.T) {
	res := Select(fixtureGraph(), []string{".github/workflows/release.yml", "publisher/kafka/producer.go"}, moduleFixtureOpts())
	assertSameSet(t, planDirs(res), []string{"publisher/kafka"})

	g := fixtureGraph()
	res = Select(g, []string{".github/workflows/release.yml", ".github/workflows/build.yml"}, moduleFixtureOpts())
	assertFull(t, g, res)
	assertSameSet(t, planDirs(res), []string{".", "publisher/kafka", "publisher/nats"})
}

// A push to main or develop runs -all: root plus every nested module, once,
// through the module matrix.
func TestSelectionPolicy_AllVerifiesRootAndEveryNestedModule(t *testing.T) {
	g := fixtureGraph()
	opts := moduleFixtureOpts()
	opts.All = true
	res := Select(g, nil, opts)

	assertFull(t, g, res)
	if !res.Global {
		t.Fatalf("Global = false, want true")
	}
	assertSameSet(t, planDirs(res), []string{".", "publisher/kafka", "publisher/nats"})
}

// When the diff cannot be interpreted safely the selector must widen, never
// narrow: an empty list and an unrecognized path both run everything.
func TestSelectionPolicy_UndeterminedChangeFallsBackToFull(t *testing.T) {
	// An unrecognized path forces the root lane to full; nested modules
	// follow only through the requirement graph (kafka requires the root,
	// nats does not). An empty list is global.
	for name, tc := range map[string]struct {
		changed  []string
		wantDirs []string
	}{
		"empty list":       {nil, []string{".", "publisher/kafka", "publisher/nats"}},
		"unrecognized dir": {[]string{"mystery/thing.txt"}, []string{".", "publisher/kafka"}},
	} {
		t.Run(name, func(t *testing.T) {
			g := fixtureGraph()
			res := Select(g, tc.changed, moduleFixtureOpts())
			assertFull(t, g, res)
			assertSameSet(t, planDirs(res), tc.wantDirs)
		})
	}
}
