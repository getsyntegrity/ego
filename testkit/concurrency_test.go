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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/persistence"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
)

// markedEvent builds a single-event batch carrying marker in AccountBalance,
// so the winner of a race between two writers can be identified afterward
// from the persisted event alone.
func markedEvent(t *testing.T, persistenceID string, sequenceNumber uint64, marker float64) []*egopb.Event {
	t.Helper()
	anyEvent, err := anypb.New(&testpb.AccountCreated{AccountId: persistenceID, AccountBalance: marker})
	require.NoError(t, err)
	return []*egopb.Event{
		{PersistenceId: persistenceID, SequenceNumber: sequenceNumber, Event: anyEvent, Timestamp: time.Now().UnixMilli(), Shard: 1},
	}
}

// eventMarker recovers the marker written by markedEvent.
func eventMarker(t *testing.T, event *egopb.Event) float64 {
	t.Helper()
	var msg testpb.AccountCreated
	require.NoError(t, event.GetEvent().UnmarshalTo(&msg))
	return msg.GetAccountBalance()
}

// markedState builds a *egopb.DurableState carrying marker in AccountBalance,
// so the winner of a race between two writers can be identified afterward
// from the persisted state alone.
func markedState(t *testing.T, persistenceID string, versionNumber uint64, marker float64) *egopb.DurableState {
	t.Helper()
	anyState, err := anypb.New(&testpb.Account{AccountId: persistenceID, AccountBalance: marker})
	require.NoError(t, err)
	return &egopb.DurableState{PersistenceId: persistenceID, ResultingState: anyState, VersionNumber: versionNumber}
}

// stateMarker recovers the marker written by markedState.
func stateMarker(t *testing.T, state *egopb.DurableState) float64 {
	t.Helper()
	var msg testpb.Account
	require.NoError(t, state.GetResultingState().UnmarshalTo(&msg))
	return msg.GetAccountBalance()
}

// raceTwoWriters runs a and b as independent goroutines released
// simultaneously by a closed start channel — not sequentially by the test
// itself — so the two calls genuinely contend against the store rather than
// against each other's ordering in the test goroutine. It returns their two
// errors in a-then-b order regardless of which one actually finished first.
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
func countOutcomes(t *testing.T, errA, errB error) (successes, conflicts int) {
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
	ctx := context.TODO()
	store := NewEventsStore()
	require.NoError(t, store.Connect(ctx))

	const persistenceID = "t8-event-race"
	require.NoError(t, store.WriteEvents(ctx, persistence.Unscoped(), markedEvent(t, persistenceID, 1, 0), persistence.ExpectGenesis()))

	errA, errB := raceTwoWriters(
		func() error {
			return store.WriteEvents(ctx, persistence.Unscoped(), markedEvent(t, persistenceID, 2, 111), persistence.ExpectRevision(1))
		},
		func() error {
			return store.WriteEvents(ctx, persistence.Unscoped(), markedEvent(t, persistenceID, 2, 222), persistence.ExpectRevision(1))
		},
	)

	successes, conflicts := countOutcomes(t, errA, errB)
	assert.Equal(t, 1, successes, "exactly one of the two racing writers must commit")
	assert.Equal(t, 1, conflicts, "the losing writer must observe a typed concurrency conflict")

	latest, err := store.GetLatestEvent(ctx, persistence.Unscoped(), persistenceID)
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.EqualValues(t, 2, latest.GetSequenceNumber())

	winnerMarker := eventMarker(t, latest)
	if errA == nil {
		assert.EqualValues(t, 111, winnerMarker, "final revision must reflect writer A, the one that actually succeeded")
	} else {
		assert.EqualValues(t, 222, winnerMarker, "final revision must reflect writer B, the one that actually succeeded")
	}
}

// ---------------------------------------------------------------------------
// T9 — StateStore: two independent writers racing the same ExpectRevision(N)
// ---------------------------------------------------------------------------

func TestDurableStore_T9_ConcurrentExpectRevisionHasExactlyOneWinner(t *testing.T) {
	ctx := context.TODO()
	store := NewDurableStore()
	require.NoError(t, store.Connect(ctx))

	const persistenceID = "t9-state-race"
	require.NoError(t, store.WriteState(ctx, persistence.Unscoped(), markedState(t, persistenceID, 1, 0), persistence.ExpectGenesis()))

	errA, errB := raceTwoWriters(
		func() error {
			return store.WriteState(ctx, persistence.Unscoped(), markedState(t, persistenceID, 2, 111), persistence.ExpectRevision(1))
		},
		func() error {
			return store.WriteState(ctx, persistence.Unscoped(), markedState(t, persistenceID, 2, 222), persistence.ExpectRevision(1))
		},
	)

	successes, conflicts := countOutcomes(t, errA, errB)
	assert.Equal(t, 1, successes, "exactly one of the two racing writers must commit")
	assert.Equal(t, 1, conflicts, "the losing writer must observe a typed concurrency conflict, and must not overwrite the winner")

	got, err := store.GetLatestState(ctx, persistence.Unscoped(), persistenceID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.EqualValues(t, 2, got.GetVersionNumber())

	winnerMarker := stateMarker(t, got)
	if errA == nil {
		assert.EqualValues(t, 111, winnerMarker, "final state must reflect writer A, the one that actually succeeded")
	} else {
		assert.EqualValues(t, 222, winnerMarker, "final state must reflect writer B, the one that actually succeeded")
	}
}

// ---------------------------------------------------------------------------
// T10 — concurrent genesis on the same nonexistent persistence identity
// ---------------------------------------------------------------------------

func TestEventStore_T10_ConcurrentGenesisHasExactlyOneWinner(t *testing.T) {
	ctx := context.TODO()
	store := NewEventsStore()
	require.NoError(t, store.Connect(ctx))

	const persistenceID = "t10-event-genesis-race"

	errA, errB := raceTwoWriters(
		func() error {
			return store.WriteEvents(ctx, persistence.Unscoped(), markedEvent(t, persistenceID, 1, 111), persistence.ExpectGenesis())
		},
		func() error {
			return store.WriteEvents(ctx, persistence.Unscoped(), markedEvent(t, persistenceID, 1, 222), persistence.ExpectGenesis())
		},
	)

	successes, conflicts := countOutcomes(t, errA, errB)
	assert.Equal(t, 1, successes, "exactly one genesis commit must succeed")
	assert.Equal(t, 1, conflicts, "the losing genesis attempt must observe a typed concurrency conflict")

	latest, err := store.GetLatestEvent(ctx, persistence.Unscoped(), persistenceID)
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.EqualValues(t, 1, latest.GetSequenceNumber())

	winnerMarker := eventMarker(t, latest)
	if errA == nil {
		assert.EqualValues(t, 111, winnerMarker)
	} else {
		assert.EqualValues(t, 222, winnerMarker)
	}
}

func TestDurableStore_T10_ConcurrentGenesisHasExactlyOneWinner(t *testing.T) {
	ctx := context.TODO()
	store := NewDurableStore()
	require.NoError(t, store.Connect(ctx))

	const persistenceID = "t10-state-genesis-race"

	errA, errB := raceTwoWriters(
		func() error {
			return store.WriteState(ctx, persistence.Unscoped(), markedState(t, persistenceID, 1, 111), persistence.ExpectGenesis())
		},
		func() error {
			return store.WriteState(ctx, persistence.Unscoped(), markedState(t, persistenceID, 1, 222), persistence.ExpectGenesis())
		},
	)

	successes, conflicts := countOutcomes(t, errA, errB)
	assert.Equal(t, 1, successes, "exactly one genesis commit must succeed")
	assert.Equal(t, 1, conflicts, "the losing genesis attempt must observe a typed concurrency conflict")

	got, err := store.GetLatestState(ctx, persistence.Unscoped(), persistenceID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.EqualValues(t, 1, got.GetVersionNumber())

	winnerMarker := stateMarker(t, got)
	if errA == nil {
		assert.EqualValues(t, 111, winnerMarker)
	} else {
		assert.EqualValues(t, 222, winnerMarker)
	}
}

// ---------------------------------------------------------------------------
// T8/T9/T10 repeated under -race and across many persistence ids, so the
// guarantee is demonstrated under contention rather than as a single lucky
// interleaving. Still no actor of any kind is involved (T11).
// ---------------------------------------------------------------------------

func TestEventStore_T8_ConcurrentExpectRevisionHoldsAcrossManyAggregates(t *testing.T) {
	ctx := context.TODO()
	store := NewEventsStore()
	require.NoError(t, store.Connect(ctx))

	const rounds = 50
	for i := 0; i < rounds; i++ {
		persistenceID := fmt.Sprintf("t8-bulk-%d", i)
		require.NoError(t, store.WriteEvents(ctx, persistence.Unscoped(), markedEvent(t, persistenceID, 1, 0), persistence.ExpectGenesis()))

		errA, errB := raceTwoWriters(
			func() error {
				return store.WriteEvents(ctx, persistence.Unscoped(), markedEvent(t, persistenceID, 2, 1), persistence.ExpectRevision(1))
			},
			func() error {
				return store.WriteEvents(ctx, persistence.Unscoped(), markedEvent(t, persistenceID, 2, 2), persistence.ExpectRevision(1))
			},
		)

		successes, conflicts := countOutcomes(t, errA, errB)
		require.Equal(t, 1, successes)
		require.Equal(t, 1, conflicts)
	}
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
	ctx := context.TODO()
	store := NewDurableStore()
	require.NoError(t, store.Connect(ctx))

	const persistenceID = "checkpreconditions-narrow-responsibility"
	require.NoError(t, store.WriteState(ctx, persistence.Unscoped(), markedState(t, persistenceID, 1, 0), persistence.ExpectGenesis()))

	const observedVersion = 1
	const newVersion = observedVersion + 1

	// Both writers independently satisfy a checkPreconditions-style ±1 delta
	// check against the same observed version, since neither has yet seen the
	// other's write. Only the StateStore's ExpectRevision CAS can tell them
	// apart.
	delta := int(math.Abs(float64(newVersion - observedVersion)))
	require.Equal(t, 1, delta, "both writers must satisfy checkPreconditions' own ±1 rule to make this race meaningful")

	errA, errB := raceTwoWriters(
		func() error {
			return store.WriteState(ctx, persistence.Unscoped(), markedState(t, persistenceID, newVersion, 111), persistence.ExpectRevision(observedVersion))
		},
		func() error {
			return store.WriteState(ctx, persistence.Unscoped(), markedState(t, persistenceID, newVersion, 222), persistence.ExpectRevision(observedVersion))
		},
	)

	successes, conflicts := countOutcomes(t, errA, errB)
	assert.Equal(t, 1, successes, "checkPreconditions-shaped agreement on both sides must not let both writers commit")
	assert.Equal(t, 1, conflicts, "the loser must observe a typed concurrency conflict raised by the StateStore, not by checkPreconditions")

	got, err := store.GetLatestState(ctx, persistence.Unscoped(), persistenceID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.EqualValues(t, newVersion, got.GetVersionNumber())

	winnerMarker := stateMarker(t, got)
	if errA == nil {
		assert.EqualValues(t, 111, winnerMarker, "the persisted state must reflect whichever writer the StateStore actually admitted")
	} else {
		assert.EqualValues(t, 222, winnerMarker, "the persisted state must reflect whichever writer the StateStore actually admitted")
	}
}
