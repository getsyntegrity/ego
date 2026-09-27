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

// Package adaptertest is the conformance suite every adapter can run in its
// own tests: publishers, stores and future adapters. It checks that an
// adapter's descriptor tells the truth and that the adapter follows the
// lifecycle rules L1–L4 of ego-arch-004 design §D4. Publisher behavior is
// checked separately by port/publishing/publishingtest, and store data
// semantics by persistence/conformance.
//
// # The checks
//
//   - AT-1: a declared descriptor is stable across calls, has a Name, lists
//     Target.Port among its Ports, and its capabilities agree with what the
//     value implements, in both directions. The suite checks adapter.CapStart
//     (through adapter.StarterOf) and adapter.CapReady (through
//     adapter.PingerOf) itself; CapReady is implied by the store ports and is
//     not required to be declared there. Every other capability is checked
//     through Target.Capabilities; a declared capability with no entry there
//     fails with "no check supplied".
//   - AT-2 (L1): acquiring a Target.FailStart value returns an error, within
//     the operation timeout.
//   - AT-3 (L2): release twice, release without acquire, and release after
//     a failed acquire all return nil.
//   - AT-4 (L3): release returns within a short deadline while the backend
//     is stalled by Target.Stall.
//   - AT-5 (L4): Ping succeeds after acquire, for an adapter that implements
//     adapter.Pinger.
//
// Acquire and release depend on Target.Ownership: an Owned adapter (a
// publisher) is acquired by Start when adapter.StarterOf finds one and
// released by Close; a Borrowed adapter (a store) is acquired by Connect and
// released by Disconnect.
//
// # Skipped and not exercised
//
// A check is skipped only when a Target factory returns an error matching
// ErrUnreachable (errors.Is), so a CI job without a live backend can skip
// honestly. Any other factory error, or a nil or typed-nil value, fails the
// check: a misconfigured adapter cannot hide behind a skip.
//
// A check the suite cannot run is reported as "not exercised", never as
// passed, and it has no subtest of its own: Run logs it and lists it in its
// summary. That happens when a hook is missing (AT-2 and the failed-acquire
// case of AT-3 without FailStart, AT-4 without Stall), when an Owned adapter
// acquires in its constructor (it has no Start, so there is no value left
// after a failed acquire), when the adapter does not implement
// adapter.Pinger (AT-5), and when the value is undeclared (AT-1). As
// everywhere in the adapter SPI, a nil or typed-nil value is undeclared;
// the suite rejects one from New before any check runs.
//
// # Self-checking
//
// Capture runs the same checks without failing the calling test and returns
// their results, so a suite's own tests can prove that each check fails
// against a deliberately broken adapter. The package's tests do exactly
// that.
//
// This package imports only the standard library and port/adapter, so a
// nested adapter module can run it without pulling in the runtime.
package adaptertest
