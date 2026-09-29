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
	"testing"

	"github.com/stretchr/testify/assert"
)

func renderFixture() Inventory {
	mk := func(pkg, name, file string, lane Lane, dest string, sigs ...string) Entry {
		return Entry{
			Test:           Test{Module: "example.com/x", Package: "example.com/x/" + pkg, Name: name, File: file, Signals: sig(sigs...)},
			Classification: Classification{Lane: lane, Destination: dest, Reason: "r"},
			LeavesPR:       lane.LeavesPR(),
		}
	}
	tests := []Entry{
		mk("engine", "TestEngineIntegrationInMemory", "engine/a_test.go", LaneComponent, DestPR, SigActorSystem),
		mk("engine", "TestPlain", "engine/a_test.go", LaneUnit, DestPR),
		mk("engine", "TestClusterOfTwo", "engine/b_test.go", LaneIntegration, "#212", SigCluster),
		mk("store", "TestE2EOverPostgres", "store/pg_test.go", LaneIntegration, "#211", SigDBSQL),
		mk("store", "TestHTTPFake", "store/http_test.go", LaneComponent, DestPR, SigHTTPTestServer),
	}
	tests[3].Run = &RunResult{Status: StatusSkip, Note: "EGO_POSTGRES_DSN is not set", Subtests: []Subtest{{Name: "TestE2EOverPostgres/a", Status: StatusSkip}}}
	tests[1].Run = &RunResult{Status: StatusPass, Subtests: []Subtest{{Name: "TestPlain/x", Status: StatusPass}, {Name: "TestPlain/y", Status: StatusPass}}}
	return Inventory{
		Header:  Header{Commit: "0de4249", GoVersion: "go1.26.6", GOOS: "linux", GOARCH: "amd64", CPUs: 8, RunRecorded: true},
		Modules: []ModuleInfo{{Path: "example.com/x", Dir: "."}},
		Packages: []PackageRun{
			{Package: "example.com/x/engine", Status: StatusPass, ElapsedS: 12.5},
			{Package: "example.com/x/store", Status: StatusPass, ElapsedS: 0.2},
		},
		Tests: tests,
	}
}

func TestRenderMarkdownCountsPerModuleAndLane(t *testing.T) {
	md := RenderMarkdown(renderFixture())
	assert.Contains(t, md, "| Module | unit | component | integration | architecture | example | Total |")
	assert.Contains(t, md, "| `.` | 1 | 2 | 2 | 0 | 0 | 5 |")
	assert.Contains(t, md, "| **Total** | 1 | 2 | 2 | 0 | 0 | 5 |")
}

func TestRenderMarkdownListsTestsLeavingThePRPerDestination(t *testing.T) {
	md := RenderMarkdown(renderFixture())
	assert.Contains(t, md, "#212")
	assert.Contains(t, md, "`TestClusterOfTwo`")
	assert.Contains(t, md, "#211")
	assert.Contains(t, md, "`TestE2EOverPostgres`")
	assert.NotContains(t, md, "| `TestPlain`", "tests that stay are not in the leaving list")
}

func TestRenderMarkdownCallsOutInMemoryTestsNamedIntegration(t *testing.T) {
	md := RenderMarkdown(renderFixture())
	assert.Contains(t, md, "named like integration or end-to-end but not integration")
	assert.Contains(t, md, "`TestEngineIntegrationInMemory`")
	assert.Contains(t, md, "`TestE2EOverPostgres` | integration")
}

func TestRenderMarkdownListsSkipsWithCauseAndMixedFiles(t *testing.T) {
	md := RenderMarkdown(renderFixture())
	assert.Contains(t, md, "EGO_POSTGRES_DSN is not set")
	assert.Contains(t, md, "`engine/a_test.go`")
	assert.NotContains(t, md, "`engine/b_test.go` |", "a single-lane file is not mixed")
	assert.Contains(t, md, "Subtests recorded: 3")
}

func TestRenderMarkdownIsDeterministic(t *testing.T) {
	assert.Equal(t, RenderMarkdown(renderFixture()), RenderMarkdown(renderFixture()))
}

func TestRenderMarkdownWithoutRunSaysSo(t *testing.T) {
	inv := renderFixture()
	inv.Header.RunRecorded = false
	assert.Contains(t, RenderMarkdown(inv), "No run is recorded")
}

func TestRenderMarkdownQueuesUnitTestsThatStartSomethingUntilReviewed(t *testing.T) {
	inv := renderFixture()
	inv.Tests = append(inv.Tests,
		Entry{Test: Test{Package: "example.com/x/app", Name: "TestStartsApp", File: "app/a_test.go", Signals: []Signal{{ID: SigLifecycle, Detail: "Start"}}},
			Classification: Classification{Lane: LaneUnit, Destination: DestPR}},
		Entry{Test: Test{Package: "example.com/x/app", Name: "TestStartsFake", File: "app/a_test.go", Signals: []Signal{{ID: SigLifecycle, Detail: "Start"}}},
			Classification: Classification{Lane: LaneUnit, Destination: DestPR, Reason: "override"}, Override: "Start on a fake store, no actors"},
	)
	md := RenderMarkdown(inv)
	assert.Contains(t, md, "Review queue (1)")
	assert.Contains(t, md, "`TestStartsApp`")
	assert.NotContains(t, md, "- `app`: `TestStartsApp`, `TestStartsFake`")
}
