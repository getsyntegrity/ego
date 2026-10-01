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

// This file proves EGO-WRITE-004's architectural gates T8, T9 and T10: that
// EventStore and DurableStore themselves — not caller-side serialization —
// are the sole authority deciding which of two racing conditional writers
// commits. Every test here drives the store directly with raw goroutines.
// None of them constructs an EventSourcedActor, a DurableStateActor, or any
// actor mailbox, which is exactly T11's requirement: the guarantee must be
// demonstrable without relying on, or being confused with, the accidental
// serialization a single actor's mailbox would otherwise provide.
package testkit

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/persistence"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
)

// markedEvent builds a single-event batch carrying marker in AccountBalance,
// so the winner of a race between two writers can be identified afterward
// from the persisted event alone. A payload that cannot be built fails the
// spec through ctx.
func markedEvent(ctx *specs.Context, persistenceID string, sequenceNumber uint64, marker float64) []*egopb.Event {
	ctx.T.Helper()
	anyEvent, err := anypb.New(&testpb.AccountCreated{AccountId: persistenceID, AccountBalance: marker})
	ctx.Expect(err).To(specs.BeNil())
	return []*egopb.Event{
		{PersistenceId: persistenceID, SequenceNumber: sequenceNumber, Event: anyEvent, Timestamp: time.Now().UnixMilli(), Shard: 1},
	}
}

// eventMarker recovers the marker written by markedEvent.
func eventMarker(ctx *specs.Context, event *egopb.Event) float64 {
	ctx.T.Helper()
	var msg testpb.AccountCreated
	ctx.Expect(event.GetEvent().UnmarshalTo(&msg)).To(specs.BeNil())
	return msg.GetAccountBalance()
}

// markedState builds a *egopb.DurableState carrying marker in AccountBalance,
// so the winner of a race between two writers can be identified afterward
// from the persisted state alone.
func markedState(ctx *specs.Context, persistenceID string, versionNumber uint64, marker float64) *egopb.DurableState {
	ctx.T.Helper()
	anyState, err := anypb.New(&testpb.Account{AccountId: persistenceID, AccountBalance: marker})
	ctx.Expect(err).To(specs.BeNil())
	return &egopb.DurableState{PersistenceId: persistenceID, ResultingState: anyState, VersionNumber: versionNumber}
}

// stateMarker recovers the marker written by markedState.
func stateMarker(ctx *specs.Context, state *egopb.DurableState) float64 {
	ctx.T.Helper()
	var msg testpb.Account
	ctx.Expect(state.GetResultingState().UnmarshalTo(&msg)).To(specs.BeNil())
	return msg.GetAccountBalance()
}

// raceTwoWriters runs a and b as independent goroutines released
// simultaneously by a closed start channel — not sequentially by the test
// itself — so the two calls genuinely contend against the store rather than
// against each other's ordering in the test goroutine. It returns their two
// errors in a-then-b order regardless of which one actually finished first.
//
// The goroutines only drive the two calls and never touch a spec context or a
// testing.T, so they stay raw goroutines; raceTwoWriters joins both before it
// returns, and the caller asserts on the collected errors. Callers must build
// every payload before calling it, since a and b run off the case goroutine.
func raceTwoWriters(a, b func() error) (errA, errB error) {
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		<-start
		errA = a()
	}()
	go func() {
		defer wg.Done()
		<-start
		errB = b()
	}()

	close(start)
	wg.Wait()
	return errA, errB
}

// outcomeOf names the result of one conditional write: a commit, a concurrency
// conflict, or any other failure (kept with its message so a spec failure shows
// what went wrong).
func outcomeOf(err error) string {
	switch {
	case err == nil:
		return "committed"
	case errors.Is(err, persistence.ErrConcurrencyConflict):
		return "conflict"
	default:
		return "unexpected error: " + err.Error()
	}
}

// expectOneWinnerOneConflict asserts that, of two racing conditional writes,
// exactly one committed and the other got a concurrency conflict, in either order.
func expectOneWinnerOneConflict(ctx *specs.Context, errA, errB error) {
	ctx.T.Helper()
	ctx.Expect([]string{outcomeOf(errA), outcomeOf(errB)}).To(specs.ContainTheSameElementsAs([]string{"committed", "conflict"}))
}

// winningMarker is the marker the persisted record must carry: a's when a
// committed, b's otherwise.
func winningMarker(errA error, markerA, markerB float64) float64 {
	if errA == nil {
		return markerA
	}
	return markerB
}

// ---------------------------------------------------------------------------
// T8 — EventStore: two independent writers racing the same ExpectRevision(N)
// ---------------------------------------------------------------------------

func TestEventStore_T8_ConcurrentExpectRevisionHasExactlyOneWinner(t *testing.T) {
	specs.Describe(t, "EventStore lets exactly one of two writers racing the same ExpectRevision commit", func(s *specs.Spec) {
		s.It("one writer commits, the loser gets a concurrency conflict and the winner's event is stored", func(ctx *specs.Context) {
			bg := context.TODO()
			store := NewEventsStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())

			const persistenceID = "t8-event-race"
			ctx.Expect(store.WriteEvents(bg, persistence.Unscoped(), markedEvent(ctx, persistenceID, 1, 0), persistence.ExpectGenesis())).To(specs.BeNil())

			eventA := markedEvent(ctx, persistenceID, 2, 111)
			eventB := markedEvent(ctx, persistenceID, 2, 222)
			errA, errB := raceTwoWriters(
				func() error {
					return store.WriteEvents(bg, persistence.Unscoped(), eventA, persistence.ExpectRevision(1))
				},
				func() error {
					return store.WriteEvents(bg, persistence.Unscoped(), eventB, persistence.ExpectRevision(1))
				},
			)

			expectOneWinnerOneConflict(ctx, errA, errB)

			latest, err := store.GetLatestEvent(bg, persistence.Unscoped(), persistenceID)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(latest).To(specs.Not(specs.BeNil()))
			ctx.Expect(latest.GetSequenceNumber()).ToEqual(uint64(2))

			ctx.Expect(eventMarker(ctx, latest)).ToEqual(winningMarker(errA, 111, 222))
		})
	})
}

// ---------------------------------------------------------------------------
// T9 — StateStore: two independent writers racing the same ExpectRevision(N)
// ---------------------------------------------------------------------------

func TestDurableStore_T9_ConcurrentExpectRevisionHasExactlyOneWinner(t *testing.T) {
	specs.Describe(t, "DurableStore lets exactly one of two writers racing the same ExpectRevision commit", func(s *specs.Spec) {
		s.It("one writer commits, the loser gets a concurrency conflict and the winner's state is stored", func(ctx *specs.Context) {
			bg := context.TODO()
			store := NewDurableStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())

			const persistenceID = "t9-state-race"
			ctx.Expect(store.WriteState(bg, persistence.Unscoped(), markedState(ctx, persistenceID, 1, 0), persistence.ExpectGenesis())).To(specs.BeNil())

			stateA := markedState(ctx, persistenceID, 2, 111)
			stateB := markedState(ctx, persistenceID, 2, 222)
			errA, errB := raceTwoWriters(
				func() error {
					return store.WriteState(bg, persistence.Unscoped(), stateA, persistence.ExpectRevision(1))
				},
				func() error {
					return store.WriteState(bg, persistence.Unscoped(), stateB, persistence.ExpectRevision(1))
				},
			)

			expectOneWinnerOneConflict(ctx, errA, errB)

			got, err := store.GetLatestState(bg, persistence.Unscoped(), persistenceID)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).To(specs.Not(specs.BeNil()))
			ctx.Expect(got.GetVersionNumber()).ToEqual(uint64(2))

			ctx.Expect(stateMarker(ctx, got)).ToEqual(winningMarker(errA, 111, 222))
		})
	})
}

// ---------------------------------------------------------------------------
// T10 — concurrent genesis on the same nonexistent persistence identity
// ---------------------------------------------------------------------------

func TestEventStore_T10_ConcurrentGenesisHasExactlyOneWinner(t *testing.T) {
	specs.Describe(t, "EventStore lets exactly one of two concurrent genesis writers commit", func(s *specs.Spec) {
		s.It("one genesis commits, the loser gets a concurrency conflict and the winner's event is stored", func(ctx *specs.Context) {
			bg := context.TODO()
			store := NewEventsStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())

			const persistenceID = "t10-event-genesis-race"

			eventA := markedEvent(ctx, persistenceID, 1, 111)
			eventB := markedEvent(ctx, persistenceID, 1, 222)
			errA, errB := raceTwoWriters(
				func() error {
					return store.WriteEvents(bg, persistence.Unscoped(), eventA, persistence.ExpectGenesis())
				},
				func() error {
					return store.WriteEvents(bg, persistence.Unscoped(), eventB, persistence.ExpectGenesis())
				},
			)

			expectOneWinnerOneConflict(ctx, errA, errB)

			latest, err := store.GetLatestEvent(bg, persistence.Unscoped(), persistenceID)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(latest).To(specs.Not(specs.BeNil()))
			ctx.Expect(latest.GetSequenceNumber()).ToEqual(uint64(1))

			ctx.Expect(eventMarker(ctx, latest)).ToEqual(winningMarker(errA, 111, 222))
		})
	})
}

func TestDurableStore_T10_ConcurrentGenesisHasExactlyOneWinner(t *testing.T) {
	specs.Describe(t, "DurableStore lets exactly one of two concurrent genesis writers commit", func(s *specs.Spec) {
		s.It("one genesis commits, the loser gets a concurrency conflict and the winner's state is stored", func(ctx *specs.Context) {
			bg := context.TODO()
			store := NewDurableStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())

			const persistenceID = "t10-state-genesis-race"

			stateA := markedState(ctx, persistenceID, 1, 111)
			stateB := markedState(ctx, persistenceID, 1, 222)
			errA, errB := raceTwoWriters(
				func() error {
					return store.WriteState(bg, persistence.Unscoped(), stateA, persistence.ExpectGenesis())
				},
				func() error {
					return store.WriteState(bg, persistence.Unscoped(), stateB, persistence.ExpectGenesis())
				},
			)

			expectOneWinnerOneConflict(ctx, errA, errB)

			got, err := store.GetLatestState(bg, persistence.Unscoped(), persistenceID)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).To(specs.Not(specs.BeNil()))
			ctx.Expect(got.GetVersionNumber()).ToEqual(uint64(1))

			ctx.Expect(stateMarker(ctx, got)).ToEqual(winningMarker(errA, 111, 222))
		})
	})
}

// ---------------------------------------------------------------------------
// T8/T9/T10 repeated across many persistence ids, so the guarantee is
// demonstrated under contention rather than as a single lucky interleaving.
// Still no actor of any kind is involved (T11).
// ---------------------------------------------------------------------------

func TestEventStore_T8_ConcurrentExpectRevisionHoldsAcrossManyAggregates(t *testing.T) {
	specs.Describe(t, "EventStore keeps a single ExpectRevision winner across many aggregates", func(s *specs.Spec) {
		s.It("every one of 50 raced aggregates has exactly one winner and one conflict", func(ctx *specs.Context) {
			bg := context.TODO()
			store := NewEventsStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())

			const rounds = 50
			for i := 0; i < rounds; i++ {
				persistenceID := fmt.Sprintf("t8-bulk-%d", i)
				ctx.Expect(store.WriteEvents(bg, persistence.Unscoped(), markedEvent(ctx, persistenceID, 1, 0), persistence.ExpectGenesis())).To(specs.BeNil())

				eventA := markedEvent(ctx, persistenceID, 2, 1)
				eventB := markedEvent(ctx, persistenceID, 2, 2)
				errA, errB := raceTwoWriters(
					func() error {
						return store.WriteEvents(bg, persistence.Unscoped(), eventA, persistence.ExpectRevision(1))
					},
					func() error {
						return store.WriteEvents(bg, persistence.Unscoped(), eventB, persistence.ExpectRevision(1))
					},
				)

				expectOneWinnerOneConflict(ctx, errA, errB)
			}
		})
	})
}

// ---------------------------------------------------------------------------
// 2.23 — DurableStateActor.checkPreconditions (an in-memory "does the new
// version differ from my currently cached version by exactly 1" sanity
// check) is a distinct, narrower responsibility than the StateStore's own
// conditional write. It never inspects the store, never takes a lock across
// the read-then-write window, and is not itself a concurrency guarantee.
//
// This test builds two writers that each independently observe the same
// stale version and each compute newVersion = observed+1 — exactly the
// shape checkPreconditions demands, and exactly the shape both writers would
// pass if checkPreconditions were the only gate. It shows that passing that
// narrow, in-memory check does not by itself prevent a StateStore-level
// conflict: a competing writer may already have advanced VersionNumber, and
// only the StateStore's real conditional write — not checkPreconditions —
// is what decides which of the two actually commits.
// ---------------------------------------------------------------------------

func TestDurableStore_CheckPreconditionsAloneDoesNotPreventStateStoreConflict(t *testing.T) {
	specs.Describe(t, "DurableStore's conditional write, not checkPreconditions, decides between two stale-version writers", func(s *specs.Spec) {
		s.It("both writers pass the plus-or-minus one rule yet only one commits", func(ctx *specs.Context) {
			bg := context.TODO()
			store := NewDurableStore()
			ctx.Expect(store.Connect(bg)).To(specs.BeNil())

			const persistenceID = "checkpreconditions-narrow-responsibility"
			ctx.Expect(store.WriteState(bg, persistence.Unscoped(), markedState(ctx, persistenceID, 1, 0), persistence.ExpectGenesis())).To(specs.BeNil())

			const observedVersion = 1
			const newVersion = observedVersion + 1

			// Both writers independently satisfy a checkPreconditions-style ±1 delta
			// check against the same observed version, since neither has yet seen the
			// other's write. Only the StateStore's ExpectRevision CAS can tell them
			// apart.
			delta := int(math.Abs(float64(newVersion - observedVersion)))
			ctx.Expect(delta).ToEqual(1)

			stateA := markedState(ctx, persistenceID, newVersion, 111)
			stateB := markedState(ctx, persistenceID, newVersion, 222)
			errA, errB := raceTwoWriters(
				func() error {
					return store.WriteState(bg, persistence.Unscoped(), stateA, persistence.ExpectRevision(observedVersion))
				},
				func() error {
					return store.WriteState(bg, persistence.Unscoped(), stateB, persistence.ExpectRevision(observedVersion))
				},
			)

			expectOneWinnerOneConflict(ctx, errA, errB)

			got, err := store.GetLatestState(bg, persistence.Unscoped(), persistenceID)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).To(specs.Not(specs.BeNil()))
			ctx.Expect(got.GetVersionNumber()).ToEqual(uint64(newVersion))

			ctx.Expect(stateMarker(ctx, got)).ToEqual(winningMarker(errA, 111, 222))
		})
	})
}
