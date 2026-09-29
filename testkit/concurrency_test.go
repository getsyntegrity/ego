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
// from the persisted event alone.
func markedEvent(t testing.TB, persistenceID string, sequenceNumber uint64, marker float64) []*egopb.Event {
	t.Helper()
	anyEvent, err := anypb.New(&testpb.AccountCreated{AccountId: persistenceID, AccountBalance: marker})
	if err != nil {
		t.Fatalf("build marked payload: %v", err)
	}
	return []*egopb.Event{
		{PersistenceId: persistenceID, SequenceNumber: sequenceNumber, Event: anyEvent, Timestamp: time.Now().UnixMilli(), Shard: 1},
	}
}

// eventMarker recovers the marker written by markedEvent.
func eventMarker(t testing.TB, event *egopb.Event) float64 {
	t.Helper()
	var msg testpb.AccountCreated
	if err := event.GetEvent().UnmarshalTo(&msg); err != nil {
		t.Fatalf("unmarshal marked event: %v", err)
	}
	return msg.GetAccountBalance()
}

// markedState builds a *egopb.DurableState carrying marker in AccountBalance,
// so the winner of a race between two writers can be identified afterward
// from the persisted state alone.
func markedState(t testing.TB, persistenceID string, versionNumber uint64, marker float64) *egopb.DurableState {
	t.Helper()
	anyState, err := anypb.New(&testpb.Account{AccountId: persistenceID, AccountBalance: marker})
	if err != nil {
		t.Fatalf("build marked payload: %v", err)
	}
	return &egopb.DurableState{PersistenceId: persistenceID, ResultingState: anyState, VersionNumber: versionNumber}
}

// stateMarker recovers the marker written by markedState.
func stateMarker(t testing.TB, state *egopb.DurableState) float64 {
	t.Helper()
	var msg testpb.Account
	if err := state.GetResultingState().UnmarshalTo(&msg); err != nil {
		t.Fatalf("unmarshal marked state: %v", err)
	}
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

// countOutcomes classifies two conditional-write results into (successes,
// conflicts), failing the test if either error is a non-conflict failure.
func countOutcomes(t testing.TB, errA, errB error) (successes, conflicts int) {
	t.Helper()
	for _, err := range []error{errA, errB} {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, persistence.ErrConcurrencyConflict):
			conflicts++
		default:
			t.Fatalf("unexpected non-conflict error: %v", err)
		}
	}
	return successes, conflicts
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
			ctx.Expect(store.WriteEvents(bg, persistence.Unscoped(), markedEvent(ctx.T, persistenceID, 1, 0), persistence.ExpectGenesis())).To(specs.BeNil())

			eventA := markedEvent(ctx.T, persistenceID, 2, 111)
			eventB := markedEvent(ctx.T, persistenceID, 2, 222)
			errA, errB := raceTwoWriters(
				func() error {
					return store.WriteEvents(bg, persistence.Unscoped(), eventA, persistence.ExpectRevision(1))
				},
				func() error {
					return store.WriteEvents(bg, persistence.Unscoped(), eventB, persistence.ExpectRevision(1))
				},
			)

			successes, conflicts := countOutcomes(ctx.T, errA, errB)
			ctx.Expect(successes).ToEqual(1)
			ctx.Expect(conflicts).ToEqual(1)

			latest, err := store.GetLatestEvent(bg, persistence.Unscoped(), persistenceID)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(latest).To(specs.Not(specs.BeNil()))
			ctx.Expect(latest.GetSequenceNumber()).ToEqual(uint64(2))

			winnerMarker := eventMarker(ctx.T, latest)
			if errA == nil {
				ctx.Expect(winnerMarker).ToEqual(float64(111))
			} else {
				ctx.Expect(winnerMarker).ToEqual(float64(222))
			}
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
			ctx.Expect(store.WriteState(bg, persistence.Unscoped(), markedState(ctx.T, persistenceID, 1, 0), persistence.ExpectGenesis())).To(specs.BeNil())

			stateA := markedState(ctx.T, persistenceID, 2, 111)
			stateB := markedState(ctx.T, persistenceID, 2, 222)
			errA, errB := raceTwoWriters(
				func() error {
					return store.WriteState(bg, persistence.Unscoped(), stateA, persistence.ExpectRevision(1))
				},
				func() error {
					return store.WriteState(bg, persistence.Unscoped(), stateB, persistence.ExpectRevision(1))
				},
			)

			successes, conflicts := countOutcomes(ctx.T, errA, errB)
			ctx.Expect(successes).ToEqual(1)
			ctx.Expect(conflicts).ToEqual(1)

			got, err := store.GetLatestState(bg, persistence.Unscoped(), persistenceID)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).To(specs.Not(specs.BeNil()))
			ctx.Expect(got.GetVersionNumber()).ToEqual(uint64(2))

			winnerMarker := stateMarker(ctx.T, got)
			if errA == nil {
				ctx.Expect(winnerMarker).ToEqual(float64(111))
			} else {
				ctx.Expect(winnerMarker).ToEqual(float64(222))
			}
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

			eventA := markedEvent(ctx.T, persistenceID, 1, 111)
			eventB := markedEvent(ctx.T, persistenceID, 1, 222)
			errA, errB := raceTwoWriters(
				func() error {
					return store.WriteEvents(bg, persistence.Unscoped(), eventA, persistence.ExpectGenesis())
				},
				func() error {
					return store.WriteEvents(bg, persistence.Unscoped(), eventB, persistence.ExpectGenesis())
				},
			)

			successes, conflicts := countOutcomes(ctx.T, errA, errB)
			ctx.Expect(successes).ToEqual(1)
			ctx.Expect(conflicts).ToEqual(1)

			latest, err := store.GetLatestEvent(bg, persistence.Unscoped(), persistenceID)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(latest).To(specs.Not(specs.BeNil()))
			ctx.Expect(latest.GetSequenceNumber()).ToEqual(uint64(1))

			winnerMarker := eventMarker(ctx.T, latest)
			if errA == nil {
				ctx.Expect(winnerMarker).ToEqual(float64(111))
			} else {
				ctx.Expect(winnerMarker).ToEqual(float64(222))
			}
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

			stateA := markedState(ctx.T, persistenceID, 1, 111)
			stateB := markedState(ctx.T, persistenceID, 1, 222)
			errA, errB := raceTwoWriters(
				func() error {
					return store.WriteState(bg, persistence.Unscoped(), stateA, persistence.ExpectGenesis())
				},
				func() error {
					return store.WriteState(bg, persistence.Unscoped(), stateB, persistence.ExpectGenesis())
				},
			)

			successes, conflicts := countOutcomes(ctx.T, errA, errB)
			ctx.Expect(successes).ToEqual(1)
			ctx.Expect(conflicts).ToEqual(1)

			got, err := store.GetLatestState(bg, persistence.Unscoped(), persistenceID)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).To(specs.Not(specs.BeNil()))
			ctx.Expect(got.GetVersionNumber()).ToEqual(uint64(1))

			winnerMarker := stateMarker(ctx.T, got)
			if errA == nil {
				ctx.Expect(winnerMarker).ToEqual(float64(111))
			} else {
				ctx.Expect(winnerMarker).ToEqual(float64(222))
			}
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
				ctx.Expect(store.WriteEvents(bg, persistence.Unscoped(), markedEvent(ctx.T, persistenceID, 1, 0), persistence.ExpectGenesis())).To(specs.BeNil())

				eventA := markedEvent(ctx.T, persistenceID, 2, 1)
				eventB := markedEvent(ctx.T, persistenceID, 2, 2)
				errA, errB := raceTwoWriters(
					func() error {
						return store.WriteEvents(bg, persistence.Unscoped(), eventA, persistence.ExpectRevision(1))
					},
					func() error {
						return store.WriteEvents(bg, persistence.Unscoped(), eventB, persistence.ExpectRevision(1))
					},
				)

				successes, conflicts := countOutcomes(ctx.T, errA, errB)
				ctx.Expect(successes).ToEqual(1)
				ctx.Expect(conflicts).ToEqual(1)
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
			ctx.Expect(store.WriteState(bg, persistence.Unscoped(), markedState(ctx.T, persistenceID, 1, 0), persistence.ExpectGenesis())).To(specs.BeNil())

			const observedVersion = 1
			const newVersion = observedVersion + 1

			// Both writers independently satisfy a checkPreconditions-style ±1 delta
			// check against the same observed version, since neither has yet seen the
			// other's write. Only the StateStore's ExpectRevision CAS can tell them
			// apart.
			delta := int(math.Abs(float64(newVersion - observedVersion)))
			ctx.Expect(delta).ToEqual(1)

			stateA := markedState(ctx.T, persistenceID, newVersion, 111)
			stateB := markedState(ctx.T, persistenceID, newVersion, 222)
			errA, errB := raceTwoWriters(
				func() error {
					return store.WriteState(bg, persistence.Unscoped(), stateA, persistence.ExpectRevision(observedVersion))
				},
				func() error {
					return store.WriteState(bg, persistence.Unscoped(), stateB, persistence.ExpectRevision(observedVersion))
				},
			)

			successes, conflicts := countOutcomes(ctx.T, errA, errB)
			ctx.Expect(successes).ToEqual(1)
			ctx.Expect(conflicts).ToEqual(1)

			got, err := store.GetLatestState(bg, persistence.Unscoped(), persistenceID)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(got).To(specs.Not(specs.BeNil()))
			ctx.Expect(got.GetVersionNumber()).ToEqual(uint64(newVersion))

			winnerMarker := stateMarker(ctx.T, got)
			if errA == nil {
				ctx.Expect(winnerMarker).ToEqual(float64(111))
			} else {
				ctx.Expect(winnerMarker).ToEqual(float64(222))
			}
		})
	})
}
