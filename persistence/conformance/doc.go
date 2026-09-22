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

// Package conformance is a store-agnostic, cross-tenant isolation
// conformance suite for the three store interfaces in package persistence:
// EventsStore, StateStore, and SnapshotStore. It is EGO-TENANT-003 (T3)'s
// deliverable: the deep isolation matrix the issue's acceptance criteria
// call "tests cross-tenant direct-store/conformance independientes del
// mailbox del actor" — direct-store tests that never construct an actor or
// a mailbox, proving isolation is a property of the store itself.
//
// # Usage
//
// An adapter author — in this repository (see testkit/conformance_test.go)
// or in an external module (e.g. a postgres-backed store) — wires this
// suite into their own test:
//
//	func TestPostgresEventsStoreConformance(t *testing.T) {
//	    conformance.RunEventsStoreConformance(t, func(t *testing.T) persistence.EventsStore {
//	        return newPostgresEventsStore(t) // connects to a fresh schema/table
//	    })
//	}
//
// newStore MUST return a FRESH, EMPTY store on every call. The suite calls
// it once per subtest, never once for the whole suite, so that one
// subtest's writes can never leak into another's. RunEventsStoreConformance,
// RunStateStoreConformance, and RunSnapshotStoreConformance each call
// Connect/Disconnect around every subtest exactly as a real caller would;
// when Connect reports the store is unreachable (for example, an external
// adapter's CI has no database configured), that subtest is skipped with a
// clear message rather than silently counted as a passing isolation proof.
//
// Passing the suites applicable to the store kinds an adapter implements is
// the evidence of EGO-TENANT-003 compliance — see
// openspec/changes/ego-tenant-003/specs/persistence-tenant-isolation/spec.md
// for the normative requirements this suite exists to demonstrate.
//
// # Self-checking
//
// A conformance suite that passes against a store with no real isolation is
// worse than no suite at all. This package guards against silently
// degrading into an always-passing formality: EventsStoreChecks,
// StateStoreChecks, and SnapshotStoreChecks are the exact named checks
// RunEventsStoreConformance/RunStateStoreConformance/
// RunSnapshotStoreConformance run as `go test` subtests, and
// CaptureEventsStoreChecks/CaptureStateStoreChecks/CaptureSnapshotStoreChecks
// run those same checks without a *testing.T, capturing pass/fail instead of
// letting a failure abort the caller's own test. testkit/conformance_test.go's
// TestConformanceCatchesNonIsolatingStore is the permanent regression test
// that uses this to prove, against a deliberately non-isolating store, that
// the suite actually fails when it should.
package conformance
