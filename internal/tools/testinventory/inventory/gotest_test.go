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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseRunFixture(t *testing.T) RunData {
	t.Helper()
	f, err := os.Open("../testdata/gotest/run.json")
	require.NoError(t, err)
	defer f.Close()
	run, err := ParseTestJSON(f)
	require.NoError(t, err)
	return run
}

func TestParseTestJSONRecordsTopLevelTestsAndSubtests(t *testing.T) {
	run := parseRunFixture(t)
	got := run.Tests[RunKey{Package: "example.com/fixture/sample", Name: "TestPureAdd"}]
	require.NotNil(t, got)
	assert.Equal(t, StatusPass, got.Status)
	assert.InDelta(t, 0.5, got.ElapsedS, 1e-9)
	assert.Equal(t, []Subtest{
		{Name: "TestPureAdd/one", Status: StatusPass, ElapsedS: 0.25},
		{Name: "TestPureAdd/two", Status: StatusPass},
	}, got.Subtests)
}

func TestParseTestJSONKeepsTheSkipCause(t *testing.T) {
	run := parseRunFixture(t)
	got := run.Tests[RunKey{Package: "example.com/fixture/sample", Name: "TestSkipsWithoutDSN"}]
	require.NotNil(t, got)
	assert.Equal(t, StatusSkip, got.Status)
	assert.Equal(t, "SAMPLE_POSTGRES_DSN is not set", got.Note)
}

func TestParseTestJSONKeepsTheFailureLineAndRoundsTime(t *testing.T) {
	run := parseRunFixture(t)
	got := run.Tests[RunKey{Package: "example.com/fixture/sample", Name: "TestBroken"}]
	require.NotNil(t, got)
	assert.Equal(t, StatusFail, got.Status)
	assert.Equal(t, "expected 1, got 2", got.Note)
	assert.InDelta(t, 1.235, got.ElapsedS, 1e-9)
}

func TestParseTestJSONRecordsPackages(t *testing.T) {
	run := parseRunFixture(t)
	assert.Equal(t, PackageRun{Package: "example.com/fixture/sample", Status: StatusFail, ElapsedS: 2.5}, run.Packages["example.com/fixture/sample"])
	other := run.Packages["example.com/fixture/other"]
	assert.Equal(t, StatusFail, other.Status)
	assert.Equal(t, "build failed", other.Note)
}

func TestParseTestJSONRejectsGarbage(t *testing.T) {
	_, err := ParseTestJSON(strings.NewReader("not json\n"))
	assert.Error(t, err)
}

func TestRunModuleEnumeratesSubtestsOfARealModule(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go tool")
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/go.mod", []byte("module example.com/tiny\n\ngo 1.22\n"), 0o600))
	src := `package tiny
import "testing"
func TestA(t *testing.T) { t.Run("x", func(t *testing.T) {}); t.Run("y", func(t *testing.T) { t.Skip("nope") }) }
func TestB(t *testing.T) { t.Skip("later") }
`
	require.NoError(t, os.WriteFile(dir+"/tiny_test.go", []byte(src), 0o600))

	run, err := RunModule(t.Context(), Module{Path: "example.com/tiny", Dir: dir, Rel: "."})
	require.NoError(t, err)
	a := run.Tests[RunKey{Package: "example.com/tiny", Name: "TestA"}]
	require.NotNil(t, a)
	assert.Equal(t, StatusPass, a.Status)
	require.Len(t, a.Subtests, 2)
	assert.Equal(t, StatusSkip, a.Subtests[1].Status)
	assert.Equal(t, "nope", a.Subtests[1].Note)
	assert.Equal(t, "later", run.Tests[RunKey{Package: "example.com/tiny", Name: "TestB"}].Note)
	assert.Equal(t, StatusPass, run.Packages["example.com/tiny"].Status)
}
