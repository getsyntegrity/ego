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
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fixtureModule = Module{Path: "example.com/fixture", Dir: "../testdata/fixture", Rel: "."}

func scanFixture(t *testing.T) map[string]Test {
	t.Helper()
	scan, err := ScanModule(fixtureModule)
	require.NoError(t, err)
	byName := make(map[string]Test, len(scan.Tests))
	for _, tc := range scan.Tests {
		_, dup := byName[tc.Name]
		require.False(t, dup, "duplicate test %s", tc.Name)
		byName[tc.Name] = tc
	}
	return byName
}

func signalIDs(tc Test) []string {
	seen := map[string]bool{}
	for _, s := range tc.Signals {
		seen[s.ID] = true
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func TestScanFindsOnlyRealTestFunctions(t *testing.T) {
	got := scanFixture(t)
	names := make([]string, 0, len(got))
	for name := range got {
		names = append(names, name)
	}
	sort.Strings(names)
	assert.Equal(t, []string{
		"TestActorSystem", "TestActorTypesOnly", "TestCluster", "TestDials", "TestFromExternalPackage",
		"TestGoList", "TestGoListThroughVariable", "TestGoViaLookPath", "TestHTTPServer", "TestIntegrationInName",
		"TestKafkaTypesOnly", "TestKafkaWriter", "TestListens", "TestNATSConnect", "TestOtherBinary",
		"TestParallelFlag", "TestParsesSource", "TestPureAdd", "TestRecorderOnly", "TestSQLOpen",
		"TestSkipOnOptionalFlag", "TestSkipsThroughHelper", "TestSkipsWithoutDSN", "TestSleeps", "TestStartsThroughAMethod",
		"TestTempDir", "TestTestkit", "TestUnresolvedWait", "TestUsesPgx", "TestWaitInHelper",
	}, names)
}

func TestScanRecordsLocation(t *testing.T) {
	tc := scanFixture(t)["TestFromExternalPackage"]
	assert.Equal(t, "example.com/fixture/sample", tc.Package)
	assert.Equal(t, "example.com/fixture", tc.Module)
	assert.Equal(t, "sample/external_test.go", tc.File)
	assert.Positive(t, tc.Line)
}

func TestScanSignals(t *testing.T) {
	want := map[string][]string{
		"TestPureAdd":               nil,
		"TestIntegrationInName":     nil,
		"TestListens":               {"net.listen"},
		"TestDials":                 {"net.dial"},
		"TestHTTPServer":            {"http.test-server"},
		"TestRecorderOnly":          nil,
		"TestGoList":                {"process.go-toolchain"},
		"TestGoListThroughVariable": {"process.go-toolchain"},
		"TestGoViaLookPath":         {"process.go-toolchain"},
		"TestOtherBinary":           {"process.exec"},
		"TestParsesSource":          {"source.inspect"},
		"TestSQLOpen":               {"db.sql"},
		"TestUsesPgx":               {"db.postgres"},
		"TestSkipsWithoutDSN":       {"env.external-endpoint", "skip.env"},
		"TestSkipsThroughHelper":    {"db.sql", "env.external-endpoint", "skip.env"},
		"TestSkipOnOptionalFlag":    {"skip.env"},
		"TestActorSystem":           {"actor.system"},
		"TestTestkit":               {"actor.system"},
		"TestCluster":               {"cluster"},
		"TestActorTypesOnly":        nil,
		"TestStartsThroughAMethod":  {"lifecycle.start"},
		"TestSleeps":                {"wait.pause", "wait.sleep"},
		"TestUnresolvedWait":        {"wait.sleep"},
		"TestWaitInHelper":          {"wait.sleep"},
		"TestTempDir":               {"fs.io", "fs.tempdir"},
		"TestParallelFlag":          {"concurrency.parallel"},
		"TestNATSConnect":           {"broker.nats"},
		"TestKafkaWriter":           {"broker.kafka"},
		"TestKafkaTypesOnly":        nil,
		"TestFromExternalPackage":   nil,
	}
	got := scanFixture(t)
	for name, ids := range want {
		t.Run(name, func(t *testing.T) {
			tc, ok := got[name]
			require.True(t, ok)
			assert.ElementsMatch(t, ids, signalIDs(tc))
		})
	}
}

func TestScanResolvesOneHelperLevelAndNamesIt(t *testing.T) {
	tc := scanFixture(t)["TestSkipsThroughHelper"]
	for _, s := range tc.Signals {
		assert.Equal(t, "openDB", s.Via, "signal %s", s.ID)
	}
}

func TestScanEnvSkipNamesTheVariable(t *testing.T) {
	tc := scanFixture(t)["TestSkipsWithoutDSN"]
	var details []string
	for _, s := range tc.Signals {
		if s.ID == "skip.env" {
			details = append(details, s.Detail)
		}
	}
	assert.Equal(t, []string{"SAMPLE_POSTGRES_DSN"}, details)
}

func TestScanFixedWaits(t *testing.T) {
	got := scanFixture(t)
	assert.EqualValues(t, 1250, got["TestSleeps"].FixedWaitMS, "50ms + 1s + a named constant of 200ms")
	assert.Zero(t, got["TestSleeps"].UnresolvedWaits)
	assert.Zero(t, got["TestUnresolvedWait"].FixedWaitMS)
	assert.Equal(t, 1, got["TestUnresolvedWait"].UnresolvedWaits)
	assert.EqualValues(t, 10, got["TestWaitInHelper"].FixedWaitMS, "helpers of helpers are not followed")
}

func TestScanIsDeterministic(t *testing.T) {
	first, err := ScanModule(fixtureModule)
	require.NoError(t, err)
	second, err := ScanModule(fixtureModule)
	require.NoError(t, err)
	assert.Equal(t, first, second)
	assert.True(t, sort.SliceIsSorted(first.Tests, func(i, j int) bool {
		if first.Tests[i].Package != first.Tests[j].Package {
			return first.Tests[i].Package < first.Tests[j].Package
		}
		return first.Tests[i].Name < first.Tests[j].Name
	}))
}
