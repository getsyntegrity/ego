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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func entryFor(dir, name string, sigs ...string) Entry {
	return Entry{
		Test:           Test{Package: "example.com/x/" + dir, Name: name, File: dir + "/a_test.go", Signals: sig(sigs...)},
		Classification: Classification{Lane: LaneUnit, Destination: DestPR, Reason: "no resource signal"},
	}
}

func TestApplyOverridesReclassifiesWithTheWrittenReason(t *testing.T) {
	entries := []Entry{entryFor("engine", "TestHelperStartsCluster")}
	ov := Overrides{Overrides: []Override{{
		Dir: "engine", Test: "TestHelperStartsCluster", Lane: LaneIntegration,
		Reason: "the helper starts a two node cluster through a method the scanner cannot follow",
	}}}
	got, err := ApplyOverrides(entries, ov)
	require.NoError(t, err)
	assert.Equal(t, LaneIntegration, got[0].Lane)
	assert.Equal(t, "#210", got[0].Destination, "defaults to the generic integration workflow")
	assert.Contains(t, got[0].Override, "two node cluster")
	assert.Equal(t, "no resource signal", entries[0].Reason, "input is not mutated")
}

func TestApplyOverridesCanPickTheDestination(t *testing.T) {
	entries := []Entry{entryFor("engine", "TestA")}
	ov := Overrides{Overrides: []Override{{Dir: "engine", Test: "TestA", Lane: LaneIntegration, Destination: "#212", Reason: "cluster"}}}
	got, err := ApplyOverrides(entries, ov)
	require.NoError(t, err)
	assert.Equal(t, "#212", got[0].Destination)
}

func TestApplyOverridesRejectsBadEntries(t *testing.T) {
	entries := []Entry{entryFor("engine", "TestA")}
	cases := map[string]Override{
		"missing test":   {Dir: "engine", Test: "TestGone", Lane: LaneUnit, Reason: "r"},
		"missing reason": {Dir: "engine", Test: "TestA", Lane: LaneUnit},
		"blank reason":   {Dir: "engine", Test: "TestA", Lane: LaneUnit, Reason: "  "},
		"invalid lane":   {Dir: "engine", Test: "TestA", Lane: "smoke", Reason: "r"},
		"wrong dir":      {Dir: "other", Test: "TestA", Lane: LaneUnit, Reason: "r"},
	}
	for name, o := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ApplyOverrides(entries, Overrides{Overrides: []Override{o}})
			assert.Error(t, err)
		})
	}
}

func TestApplyOverridesRejectsDuplicates(t *testing.T) {
	entries := []Entry{entryFor("engine", "TestA")}
	o := Override{Dir: "engine", Test: "TestA", Lane: LaneUnit, Reason: "r"}
	_, err := ApplyOverrides(entries, Overrides{Overrides: []Override{o, o}})
	assert.ErrorContains(t, err, "twice")
}

func TestLoadOverrides(t *testing.T) {
	p := filepath.Join(t.TempDir(), "o.json")
	require.NoError(t, os.WriteFile(p, []byte(`{"example_modules":["example/cluster"],"overrides":[{"dir":"a","test":"TestA","lane":"unit","reason":"r"}]}`), 0o600))
	ov, err := LoadOverrides(p)
	require.NoError(t, err)
	assert.Equal(t, []string{"example/cluster"}, ov.ExampleModules)
	assert.Equal(t, LaneUnit, ov.Overrides[0].Lane)
}

func TestLoadOverridesRejectsUnknownFields(t *testing.T) {
	p := filepath.Join(t.TempDir(), "o.json")
	require.NoError(t, os.WriteFile(p, []byte(`{"overides":[]}`), 0o600))
	_, err := LoadOverrides(p)
	assert.Error(t, err)
}

func TestBuildEntriesClassifiesAndMarksExampleModules(t *testing.T) {
	scans := []ModuleScan{
		{Module: Module{Path: "example.com/x", Rel: "."}, Scan: Scan{Tests: []Test{{Module: "example.com/x", Package: "example.com/x", Name: "TestA", File: "a_test.go", Signals: sig(SigActorSystem)}}}},
		{Module: Module{Path: "example.com/x/example/cluster", Rel: "example/cluster"}, Scan: Scan{Tests: []Test{{Module: "example.com/x/example/cluster", Package: "example.com/x/example/cluster", Name: "TestB", File: "example/cluster/b_test.go"}}}},
	}
	got := BuildEntries(scans, Overrides{ExampleModules: []string{"example/cluster"}})
	require.Len(t, got, 2)
	assert.Equal(t, LaneComponent, got[0].Lane)
	assert.Equal(t, LaneExample, got[1].Lane)
}

func TestMergeRunAttachesResultsAndReportsUnmatched(t *testing.T) {
	entries := []Entry{
		entryFor("sample", "TestPureAdd"),
		entryFor("sample", "TestSkipsWithoutDSN"),
		entryFor("sample", "TestNeverRan"),
	}
	for i := range entries {
		entries[i].Package = "example.com/fixture/sample"
	}
	run := parseRunFixture(t)
	unmatched := MergeRun(entries, run)
	require.NotNil(t, entries[0].Run)
	assert.Len(t, entries[0].Run.Subtests, 2)
	assert.Equal(t, StatusSkip, entries[1].Run.Status)
	assert.Nil(t, entries[2].Run)
	assert.Equal(t, []RunKey{
		{Package: "example.com/fixture/sample", Name: "TestBroken"},
		{Package: "example.com/fixture/sample", Name: "TestNotInSource"},
	}, unmatched)
}
