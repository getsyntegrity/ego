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

package persistence

import "strconv"

// preconditionMode enumerates the three valid WritePrecondition states.
// The zero value (preconditionUnspecified) is deliberately not one of them:
// it exists only to make the zero value of WritePrecondition invalid.
type preconditionMode uint8

const (
	preconditionUnspecified preconditionMode = iota
	preconditionUnconditional
	preconditionGenesis
	preconditionExactRevision
)

// WritePrecondition declares the condition a conditional write must satisfy
// against the persisted revision of a given persistenceID before EventsStore
// or StateStore commits it. It is a comparable value type constructed only
// through its named constructors: Unconditional, ExpectGenesis and
// ExpectRevision. The zero value is invalid and MUST be rejected with
// ErrInvalidPrecondition rather than treated as any of the three valid
// states.
//
// # Revision model mapping
//
// Three related but distinct concepts exist around a WritePrecondition:
//
//   - ExpectedRevision is the caller's declared intent (command metadata,
//     CONTRACT-EXPECTED-REVISION-v1): "commit only if the aggregate is still
//     at revision N".
//   - CurrentRevision is the owning actor's in-memory counter
//     (EventSourcedActor's eventsCounter, DurableStateActor's
//     currentVersion), populated from StorageRevision at recovery and
//     advanced only after a write is confirmed committed. It can be stale
//     relative to a concurrent writer's commit.
//   - StorageRevision is the adapter's own compare-and-swap value:
//     egopb.Event.SequenceNumber for EventsStore, egopb.DurableState.
//     VersionNumber for StateStore. A WritePrecondition's exact-revision
//     value is always evaluated against StorageRevision at the atomic
//     instant of commit — never against a locally cached CurrentRevision.
//
// ExpectRevision(0) is a distinct, meaningful value in its own right, even
// though command metadata's own mapping (ExpectedRevision absent →
// unconditional; =0 → genesis; =N>0 → exact revision N) never produces it:
// the persistence SPI does not collapse "no prior commit" and "exact
// revision zero" into the same sentinel.
//
// # Revision model mapping table (design.md D4)
//
//	Concept          | EventSourced                                    | DurableState
//	-----------------|-------------------------------------------------|---------------------------------------------
//	ExpectedRevision | Metadata.ExpectedRevision(); absent →            | identical translation
//	                 | Unconditional(), 0 → ExpectGenesis(), N>0 →      |
//	                 | ExpectRevision(N)                                |
//	CurrentRevision  | eventsCounter (batchCounter while a batch is     | currentVersion; advanced only inside
//	                 | open); advanced only by applyConfirmedState      | commitState after WriteState returns nil
//	                 | after a confirmed write                          |
//	StorageRevision  | highest committed egopb.Event.SequenceNumber     | committed egopb.DurableState.VersionNumber
//	                 | per persistenceID                                | per persistenceID
type WritePrecondition struct {
	mode     preconditionMode
	revision uint64
}

// Unconditional returns a WritePrecondition that performs no revision check:
// the write commits regardless of the persisted revision. This is the
// legacy, pre-WRITE-004 behavior.
func Unconditional() WritePrecondition {
	return WritePrecondition{mode: preconditionUnconditional}
}

// ExpectGenesis returns a WritePrecondition that requires no prior commit to
// exist yet for the target persistenceID.
func ExpectGenesis() WritePrecondition {
	return WritePrecondition{mode: preconditionGenesis}
}

// ExpectRevision returns a WritePrecondition that requires the persisted
// revision for the target persistenceID to be exactly revision.
func ExpectRevision(revision uint64) WritePrecondition {
	return WritePrecondition{mode: preconditionExactRevision, revision: revision}
}

// IsUnconditional reports whether p performs no revision check.
func (p WritePrecondition) IsUnconditional() bool {
	return p.mode == preconditionUnconditional
}

// IsGenesis reports whether p requires no prior commit to exist.
func (p WritePrecondition) IsGenesis() bool {
	return p.mode == preconditionGenesis
}

// Revision reports the exact revision p requires, and whether p is in fact
// an exact-revision precondition. ok is false for Unconditional, Genesis and
// the invalid zero value.
func (p WritePrecondition) Revision() (revision uint64, ok bool) {
	if p.mode != preconditionExactRevision {
		return 0, false
	}
	return p.revision, true
}

// Valid reports whether p is one of the three constructible states. It is
// false only for the zero value of WritePrecondition.
func (p WritePrecondition) Valid() bool {
	return p.mode != preconditionUnspecified
}

// String renders p using the canonical conflict-grammar token
// (unconditional|genesis|N), for use in ConflictError.Error() and log
// messages.
func (p WritePrecondition) String() string {
	switch p.mode {
	case preconditionUnconditional:
		return "unconditional"
	case preconditionGenesis:
		return "genesis"
	case preconditionExactRevision:
		return strconv.FormatUint(p.revision, 10)
	default:
		return "unspecified"
	}
}
