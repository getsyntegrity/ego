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

// Package inventory builds the test inventory described in docs/testing/lanes.md.
//
// It scans the Test functions of every Go module statically, classifies each into a
// lane from the resources it uses, merges the result of a real `go test -json` run
// (subtests, skips and timings), and checks that a committed inventory is fresh.
package inventory

// Module is one Go module of the repository.
type Module struct {
	// Path is the module path declared in go.mod.
	Path string
	// Dir is the directory holding go.mod, usable from the process working directory.
	Dir string
	// Rel is Dir relative to the repository root in slash form, "." for the root module.
	Rel string
}

// Signal is one observable resource use found in a test or in a helper it calls.
type Signal struct {
	ID string `json:"id"`
	// Via names the local helper function that produced the signal, empty when
	// the Test function itself did.
	Via string `json:"via,omitempty"`
	// Detail carries evidence such as an environment variable name.
	Detail string `json:"detail,omitempty"`
}

// Test is one Test function found in the source.
type Test struct {
	Module  string `json:"module"`
	Package string `json:"package"`
	Name    string `json:"name"`
	// File is the repository-relative path of the file declaring the test.
	File string `json:"file"`
	Line int    `json:"line"`

	Signals []Signal `json:"signals,omitempty"`
	// FixedWaitMS is the sum of the constant durations passed to time.Sleep and
	// pause.For, counted once per occurrence.
	FixedWaitMS     int64 `json:"fixed_wait_ms,omitempty"`
	UnresolvedWaits int   `json:"unresolved_waits,omitempty"`
}

// Scan is the static result for one module.
type Scan struct {
	Tests []Test
}

// Signal identifiers. docs/testing/lanes.md documents each of them.
const (
	SigDBSQL            = "db.sql"
	SigDBPostgres       = "db.postgres"
	SigExternalEndpoint = "env.external-endpoint"
	SigBrokerKafka      = "broker.kafka"
	SigBrokerNATS       = "broker.nats"
	SigBrokerPulsar     = "broker.pulsar"
	SigCluster          = "cluster"
	SigProcessExec      = "process.exec"
	SigGoToolchain      = "process.go-toolchain"
	SigSourceInspect    = "source.inspect"
	SigActorSystem      = "actor.system"
	SigNetListen        = "net.listen"
	SigNetDial          = "net.dial"
	SigHTTPTestServer   = "http.test-server"
	SigPortAlloc        = "net.port-alloc"
	SigSkipEnv          = "skip.env"
	SigWaitSleep        = "wait.sleep"
	SigWaitPause        = "wait.pause"
	SigFSTempDir        = "fs.tempdir"
	SigFSIO             = "fs.io"
	SigParallel         = "concurrency.parallel"
	SigLifecycle        = "lifecycle.start"
)
