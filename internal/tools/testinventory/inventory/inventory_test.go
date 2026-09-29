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
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
}

func TestDiscoverModulesFindsNestedModulesAndSkipsFixtures(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/root\n\ngo 1.22\n")
	writeFile(t, filepath.Join(root, "publisher", "kafka", "go.mod"), "// comment\nmodule example.com/root/publisher/kafka\n")
	writeFile(t, filepath.Join(root, "pkg", "testdata", "fixture", "go.mod"), "module example.com/fixture\n")
	writeFile(t, filepath.Join(root, ".hidden", "go.mod"), "module example.com/hidden\n")

	mods, err := DiscoverModules(root)
	require.NoError(t, err)
	require.Len(t, mods, 2)
	assert.Equal(t, "example.com/root", mods[0].Path)
	assert.Equal(t, ".", mods[0].Rel)
	assert.Equal(t, "example.com/root/publisher/kafka", mods[1].Path)
	assert.Equal(t, "publisher/kafka", mods[1].Rel)
}

func TestScanModuleDoesNotDescendIntoNestedModules(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/root\n")
	writeFile(t, filepath.Join(root, "a_test.go"), "package root\nimport \"testing\"\nfunc TestRoot(t *testing.T) {}\n")
	writeFile(t, filepath.Join(root, "sub", "go.mod"), "module example.com/root/sub\n")
	writeFile(t, filepath.Join(root, "sub", "b_test.go"), "package sub\nimport \"testing\"\nfunc TestSub(t *testing.T) {}\n")

	mods, err := DiscoverModules(root)
	require.NoError(t, err)
	scans, err := ScanAll(mods)
	require.NoError(t, err)
	require.Len(t, scans, 2)
	require.Len(t, scans[0].Scan.Tests, 1)
	assert.Equal(t, "TestRoot", scans[0].Scan.Tests[0].Name)
	assert.Equal(t, "sub/b_test.go", scans[1].Scan.Tests[0].File)
	assert.Equal(t, "example.com/root/sub", scans[1].Scan.Tests[0].Package)
}

func sampleScans() []ModuleScan {
	return []ModuleScan{{
		Module: Module{Path: "example.com/x", Rel: "."},
		Scan: Scan{Tests: []Test{
			{Module: "example.com/x", Package: "example.com/x/a", Name: "TestUnit", File: "a/a_test.go"},
			{Module: "example.com/x", Package: "example.com/x/a", Name: "TestDB", File: "a/a_test.go", Signals: sig(SigDBSQL)},
		}},
	}}
}

func TestAssembleKeepsPreviousRunData(t *testing.T) {
	first, err := Assemble(sampleScans(), Overrides{}, nil)
	require.NoError(t, err)
	first.Header = Header{Commit: "abc", RunRecorded: true}
	first.Tests[0].Run = &RunResult{Status: StatusPass, ElapsedS: 0.1}
	first.Packages = []PackageRun{{Package: "example.com/x/a", Status: StatusPass, ElapsedS: 1}}

	second, err := Assemble(sampleScans(), Overrides{}, &first)
	require.NoError(t, err)
	assert.Equal(t, "abc", second.Header.Commit)
	require.NotNil(t, second.Tests[0].Run)
	assert.Equal(t, StatusPass, second.Tests[0].Run.Status)
	assert.Nil(t, second.Tests[1].Run)
	assert.Len(t, second.Packages, 1)
}

func TestAssembleSortsTestsAndListsModules(t *testing.T) {
	inv, err := Assemble(sampleScans(), Overrides{ExampleModules: []string{"."}}, nil)
	require.NoError(t, err)
	assert.Equal(t, []ModuleInfo{{Path: "example.com/x", Dir: ".", Example: true}}, inv.Modules)
	assert.Equal(t, "TestDB", inv.Tests[0].Name)
}

func TestInventoryFileRoundTripIsStable(t *testing.T) {
	inv, err := Assemble(sampleScans(), Overrides{}, nil)
	require.NoError(t, err)
	p := filepath.Join(t.TempDir(), "inventory.json")
	require.NoError(t, WriteInventory(p, inv))
	first, err := os.ReadFile(p)
	require.NoError(t, err)

	back, err := ReadInventory(p)
	require.NoError(t, err)
	require.NoError(t, WriteInventory(p, back))
	second, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, string(first), string(second))
	assert.Equal(t, byte('\n'), first[len(first)-1])
}

func freshAndCommitted(t *testing.T) (committed, fresh Inventory) {
	t.Helper()
	inv, err := Assemble(sampleScans(), Overrides{}, nil)
	require.NoError(t, err)
	fresh = inv
	fresh.Tests = slices.Clone(inv.Tests)
	return inv, fresh
}

func TestCheckIsCleanWhenNothingChanged(t *testing.T) {
	committed, fresh := freshAndCommitted(t)
	assert.Empty(t, Check(committed, fresh, RenderMarkdown(committed)))
}

func TestCheckReportsATestMissingFromTheInventory(t *testing.T) {
	committed, fresh := freshAndCommitted(t)
	committed.Tests = committed.Tests[:1]
	problems := Check(committed, fresh, RenderMarkdown(committed))
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0], "missing from inventory")
	assert.Contains(t, problems[0], "TestUnit")
}

func TestCheckReportsAStaleEntry(t *testing.T) {
	committed, fresh := freshAndCommitted(t)
	fresh.Tests = fresh.Tests[:1]
	problems := Check(committed, fresh, RenderMarkdown(committed))
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0], "no longer in the source")
}

func TestCheckReportsALaneThatChanged(t *testing.T) {
	committed, fresh := freshAndCommitted(t)
	committed.Tests[0].Lane = LaneUnit
	problems := Check(committed, fresh, RenderMarkdown(committed))
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0], "lane changed")
}

func TestCheckReportsUnclassifiedEntries(t *testing.T) {
	committed, fresh := freshAndCommitted(t)
	committed.Tests[0].Lane = ""
	problems := Check(committed, fresh, RenderMarkdown(committed))
	assert.Contains(t, problems[0], "unclassified")
}

func TestCheckReportsAStaleSummary(t *testing.T) {
	committed, fresh := freshAndCommitted(t)
	problems := Check(committed, fresh, "old summary")
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0], "inventory.md")
}

func TestCheckIgnoresLineShiftsAndRunData(t *testing.T) {
	committed, fresh := freshAndCommitted(t)
	fresh.Tests[0].Line = 999
	committed.Tests[0].Run = &RunResult{Status: StatusPass}
	assert.Empty(t, Check(committed, fresh, RenderMarkdown(committed)))
}
