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

func sig(ids ...string) []Signal {
	out := make([]Signal, len(ids))
	for i, id := range ids {
		out[i] = Signal{ID: id}
	}
	return out
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name    string
		signals []Signal
		example bool
		lane    Lane
		dest    string
	}{
		{"no signals is unit", nil, false, LaneUnit, DestPR},
		{"evidence only stays unit", sig("wait.sleep", "fs.tempdir", "fs.io", "concurrency.parallel", "skip.env"), false, LaneUnit, DestPR},
		{"actor system is component", sig("actor.system"), false, LaneComponent, DestPR},
		{"local http server is component", sig("http.test-server"), false, LaneComponent, DestPR},
		{"loopback listener is component", sig("net.listen"), false, LaneComponent, DestPR},
		{"sql is integration for #211", sig("db.sql", "actor.system"), false, LaneIntegration, "#211"},
		{"postgres is integration for #211", sig("db.postgres"), false, LaneIntegration, "#211"},
		{"external endpoint alone is integration", sig("env.external-endpoint"), false, LaneIntegration, "#210"},
		{"cluster is integration for #212", sig("cluster", "actor.system"), false, LaneIntegration, "#212"},
		{"broker is integration for #213", sig("broker.kafka"), false, LaneIntegration, "#213"},
		{"database wins over broker and cluster", sig("broker.nats", "cluster", "db.sql"), false, LaneIntegration, "#211"},
		{"broker wins over cluster", sig("cluster", "broker.pulsar"), false, LaneIntegration, "#213"},
		{"foreign process is integration", sig("process.exec"), false, LaneIntegration, "#210"},
		{"go toolchain is architecture", sig("process.go-toolchain"), false, LaneArchitecture, "#208"},
		{"source inspection is architecture", sig("source.inspect", "fs.io"), false, LaneArchitecture, "#208"},
		{"example module alone is example", nil, true, LaneExample, "#214"},
		{"example module with actors is example", sig("actor.system"), true, LaneExample, "#214"},
		{"integration beats the example module", sig("db.sql"), true, LaneIntegration, "#211"},
		{"architecture beats the example module", sig("process.go-toolchain"), true, LaneArchitecture, "#208"},
		{"integration beats architecture", sig("cluster", "process.go-toolchain"), false, LaneIntegration, "#212"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.signals, tc.example)
			assert.Equal(t, tc.lane, got.Lane)
			assert.Equal(t, tc.dest, got.Destination)
			assert.NotEmpty(t, got.Reason)
		})
	}
}

func TestClassifyReasonNamesTheDecidingSignal(t *testing.T) {
	assert.Contains(t, Classify(sig("db.sql", "actor.system"), false).Reason, "db.sql")
	assert.Contains(t, Classify(nil, true).Reason, "example module")
	assert.Contains(t, Classify(nil, false).Reason, "no resource signal")
}

func TestLeavesPR(t *testing.T) {
	assert.False(t, LaneUnit.LeavesPR())
	assert.False(t, LaneComponent.LeavesPR())
	assert.True(t, LaneIntegration.LeavesPR())
	assert.True(t, LaneArchitecture.LeavesPR())
	assert.True(t, LaneExample.LeavesPR())
}
