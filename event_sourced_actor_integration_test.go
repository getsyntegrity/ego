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

package ego

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablogore/ego/v4/command"
	"github.com/pablogore/ego/v4/persistence"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
	"github.com/pablogore/ego/v4/testkit"
)

// This file is task 3.17's integration proof (spec: "End-to-End Propagation
// Proof (AC6, T6)"): a real EventSourcedActor wired to a real Engine,
// dispatching through the real command/persistence stack against a real
// testkit.EventsStore (not a call-recording mock, not a stub that ignores
// the precondition). It mirrors PR4's durable_state_actor_integration_test.go
// pattern for the EventSourced side, closing the gap that file's own
// comment named as still open when PR4 landed.
//
// Three scenarios are required by the spec's own scenario list and are all
// proven here against real storage:
//   - exact-match ExpectedRevision commits and the store's revision
//     advances correctly;
//   - stale ExpectedRevision is rejected at the store (not merely the
//     actor's in-memory cache) with concurrency_conflict, store unchanged;
//   - two independent EventSourcedActor instances (separate Engine/actor
//     systems, following the same concurrency-proof pattern as
//     TestDurableStateConcurrentGenesisWritersYieldExactlyOneCommit) both
//     racing ExpectedRevision=0 (genesis) against one shared testkit
//     EventsStore yield exactly one commit and one conflict, proven under
//     -race so the store's own CAS -- not caller/mailbox ordering -- is
//     shown to be what decides the winner.

// TestEventSourcedIntegrationExactRevisionCommitsAndAdvancesStore proves an
// exact-match ExpectedRevision commits end-to-end and the real store's
// persisted revision advances accordingly.
func TestEventSourcedIntegrationExactRevisionCommitsAndAdvancesStore(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "ES-integration-exact", store, WithLogger(DiscardLogger))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	require.NoError(t, engine.Entity(ctx, NewEventSourcedEntity(entityID)))

	created := dispatchWithMetadata(t, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
	require.Equal(t, command.OutcomeSuccess, created.Outcome())
	require.EqualValues(t, 1, created.Revision())

	latest, err := store.GetLatestEvent(ctx, persistence.Unscoped(), entityID)
	require.NoError(t, err)
	require.NotNil(t, latest)
	require.EqualValues(t, 1, latest.GetSequenceNumber())

	// A second exact-match command: ExpectedRevision matches the real
	// persisted revision (1), so it commits and advances the store to 2.
	result := dispatchWithMetadata(t, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(1))
	require.Equal(t, command.OutcomeSuccess, result.Outcome())
	assert.EqualValues(t, 2, result.Revision())

	latest, err = store.GetLatestEvent(ctx, persistence.Unscoped(), entityID)
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.EqualValues(t, 2, latest.GetSequenceNumber())

	state, ok := result.State()
	require.True(t, ok)
	acct, ok := state.(*testpb.Account)
	require.True(t, ok)
	assert.EqualValues(t, 750, acct.GetAccountBalance())
}

// TestEventSourcedIntegrationStaleRevisionRejectedStoreUnchanged proves a
// stale ExpectedRevision is rejected at the real store -- not merely
// observed by the actor's own bookkeeping -- and that the store's
// persisted revision is left unchanged by the rejected write.
func TestEventSourcedIntegrationStaleRevisionRejectedStoreUnchanged(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "ES-integration-stale", store, WithLogger(DiscardLogger))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	require.NoError(t, engine.Entity(ctx, NewEventSourcedEntity(entityID)))

	created := dispatchWithMetadata(t, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
	require.Equal(t, command.OutcomeSuccess, created.Outcome())
	require.EqualValues(t, 1, created.Revision())

	latest, err := store.GetLatestEvent(ctx, persistence.Unscoped(), entityID)
	require.NoError(t, err)
	require.NotNil(t, latest)
	require.EqualValues(t, 1, latest.GetSequenceNumber())

	// A stale command: ExpectedRevision does not match the real persisted
	// revision (1).
	result := dispatchWithMetadata(t, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(7))
	require.Equal(t, command.OutcomeRejected, result.Outcome())
	failure, ok := result.Failure()
	require.True(t, ok)
	code, hasCode := failure.Code()
	require.True(t, hasCode)
	assert.Equal(t, command.CodeConcurrencyConflict, code)

	// The store must be unchanged by the rejected write.
	latest, err = store.GetLatestEvent(ctx, persistence.Unscoped(), entityID)
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.EqualValues(t, 1, latest.GetSequenceNumber())
}

// TestEventSourcedIntegrationConcurrentGenesisYieldsExactlyOneCommit is the
// architectural-gate proof: two independent EventSourcedActor instances
// (separate Engine/goakt actor systems, mirroring
// TestDurableStateConcurrentGenesisWritersYieldExactlyOneCommit's pattern),
// both declaring ExpectedRevision=0 against the same persistence ID with no
// prior record, race concurrently against one shared testkit.EventsStore.
// Exactly one commits, exactly one is rejected with concurrency_conflict.
// Run with -race to prove the store's own CompareAndSwap/LoadOrStore path
// -- not an external lock, not mailbox/caller ordering -- is what
// serializes the two writers, satisfying the spec's "not merely observed
// in isolation" requirement.
func TestEventSourcedIntegrationConcurrentGenesisYieldsExactlyOneCommit(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	entityID := uuid.NewString()

	engineA := newTestEngine(t, "ES-race-a", store, WithLogger(DiscardLogger))
	require.NoError(t, engineA.Start(ctx))
	require.NoError(t, engineA.Entity(ctx, NewEventSourcedEntity(entityID)))

	engineB := newTestEngine(t, "ES-race-b", store, WithLogger(DiscardLogger))
	require.NoError(t, engineB.Start(ctx))
	require.NoError(t, engineB.Entity(ctx, NewEventSourcedEntity(entityID)))

	var wg sync.WaitGroup
	results := make([]command.Result, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		results[0] = dispatchWithMetadata(t, engineA, entityID, &testpb.CreateAccount{AccountBalance: 100}, command.WithExpectedRevision(0))
	}()
	go func() {
		defer wg.Done()
		results[1] = dispatchWithMetadata(t, engineB, entityID, &testpb.CreateAccount{AccountBalance: 200}, command.WithExpectedRevision(0))
	}()
	wg.Wait()

	successes, conflicts := 0, 0
	for _, result := range results {
		switch result.Outcome() {
		case command.OutcomeSuccess:
			successes++
		case command.OutcomeRejected:
			failure, ok := result.Failure()
			require.True(t, ok)
			code, hasCode := failure.Code()
			require.True(t, hasCode)
			assert.Equal(t, command.CodeConcurrencyConflict, code)
			conflicts++
		default:
			t.Fatalf("unexpected outcome %v", result.Outcome())
		}
	}
	assert.Equal(t, 1, successes)
	assert.Equal(t, 1, conflicts)

	latest, err := store.GetLatestEvent(ctx, persistence.Unscoped(), entityID)
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.EqualValues(t, 1, latest.GetSequenceNumber())
}
