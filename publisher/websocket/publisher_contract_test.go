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

package websocket

import (
	"github.com/getsyntegrity/urd/port/publishing"
)

// The publishers implement the contracts from port/publishing directly, with
// no dependency on package `engine` or the GoAkt runtime it pulls in. The
// historical compatibility check against the `engine` aliases (ADR
// ego-arch-001, S1 criterion 3) still exists, but it lives in the separate,
// unreleased test/compat module (ADR ego-arch-006, slice S1; docs/ci.md,
// "Compatibility checks: the test/compat module"), precisely so that this
// module's tests never need to import `engine` (#122).
//
// The check that Publish on a closed publisher returns
// publishing.ErrPublisherNotStarted used to live here. It is now PT-1 of
// port/publishing/publishingtest, run in conformance_test.go against a
// publisher that really was connected and closed. Together with
// test/compat's TestUrdSentinelIsThePublishingSentinel, which checks that
// engine.ErrPublisherNotStarted is this same error value, it still proves the
// historical check that the error also matches engine.ErrPublisherNotStarted
// (ADR ego-arch-006, §6 S1).
var (
	_ publishing.EventPublisher = (*EventsPublisher)(nil)
	_ publishing.StatePublisher = (*DurableStatePublisher)(nil)
)
