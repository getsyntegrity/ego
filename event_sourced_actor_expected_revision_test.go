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
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/pablogore/ego/v4/command"
	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/persistence"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
	"github.com/pablogore/ego/v4/testkit"
)

// -----------------------------------------------------------------------
// Unit tests for the pure helpers introduced for design.md D4/D9/D10
// (EGO-WRITE-004 PR3). These exercise the mapping/decision logic in
// isolation from the actor runtime; the end-to-end scenarios further down
// prove the same logic wired correctly into the real command.Result path.
// -----------------------------------------------------------------------

func TestPreconditionFromRevisionMapsPerD4(t *testing.T) {
	tests := []struct {
		name        string
		revision    uint64
		hasRevision bool
		want        persistence.WritePrecondition
	}{
		{name: "absent is unconditional", revision: 0, hasRevision: false, want: persistence.Unconditional()},
		{name: "zero is genesis, not absence", revision: 0, hasRevision: true, want: persistence.ExpectGenesis()},
		{name: "positive revision is an exact expectation", revision: 42, hasRevision: true, want: persistence.ExpectRevision(42)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := preconditionFromRevision(tc.revision, tc.hasRevision)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestShouldStayAliveAfterConflict(t *testing.T) {
	t.Run("non-conflict error never stays alive", func(t *testing.T) {
		entity := &EventSourcedActor{eventsCounter: 3}
		assert.False(t, entity.shouldStayAliveAfterConflict(errors.New("boom")))
	})

	t.Run("actual revision matches in-memory counter: provably in sync, stays alive", func(t *testing.T) {
		entity := &EventSourcedActor{eventsCounter: 3}
		conflictErr := persistence.NewConflictError("entity-1", persistence.ExpectRevision(5), persistence.WithActualRevision(3))
		assert.True(t, entity.shouldStayAliveAfterConflict(conflictErr))
	})

	t.Run("actual revision diverges from in-memory counter: not provably in sync, shuts down", func(t *testing.T) {
		entity := &EventSourcedActor{eventsCounter: 3}
		conflictErr := persistence.NewConflictError("entity-1", persistence.ExpectRevision(5), persistence.WithActualRevision(7))
		assert.False(t, entity.shouldStayAliveAfterConflict(conflictErr))
	})

	t.Run("conflict without an actual revision cannot be proven in sync", func(t *testing.T) {
		entity := &EventSourcedActor{eventsCounter: 3}
		conflictErr := persistence.NewConflictError("entity-1", persistence.ExpectRevision(5))
		assert.False(t, entity.shouldStayAliveAfterConflict(conflictErr))
	})
}

func TestResolveBatchPrecondition(t *testing.T) {
	t.Run("no admitted command declared a revision: unconditional", func(t *testing.T) {
		entity := &EventSourcedActor{batchHasPrecondition: false, batchBase: 7}
		assert.Equal(t, persistence.Unconditional(), entity.resolveBatchPrecondition())
	})

	t.Run("declared, base is empty store: genesis", func(t *testing.T) {
		entity := &EventSourcedActor{batchHasPrecondition: true, batchBase: 0}
		assert.Equal(t, persistence.ExpectGenesis(), entity.resolveBatchPrecondition())
	})

	t.Run("declared, non-empty base: exact revision", func(t *testing.T) {
		entity := &EventSourcedActor{batchHasPrecondition: true, batchBase: 4}
		assert.Equal(t, persistence.ExpectRevision(4), entity.resolveBatchPrecondition())
	})
}

// -----------------------------------------------------------------------
// End-to-end scenarios, dispatched through the real Engine/actor/persistence
// stack and verified through command.Result (design.md D1-D10). ES-stale and
// ES-genesis-conflict are the two scenarios required to be proven this way,
// not merely at the actor/message level; the rest are included for the same
// reason since the machinery is already in place.
// -----------------------------------------------------------------------

// dispatchWithMetadata wraps payload in a command.Envelope carrying opts and
// sends it through engine.Dispatch, returning the resulting command.Result.
// It is a test-only convenience over the canonical entry point real callers
// use to declare an ExpectedRevision (design.md D5); SendCommand (the legacy
// path exercised separately by TestEventSourcedLegacyCommandIsUnconditional)
// never carries one.
func dispatchWithMetadata(t *testing.T, engine *Engine, entityID string, payload proto.Message, opts ...command.MetadataOption) command.Result {
	t.Helper()
	md, err := command.NewMetadata(command.OperationID(uuid.NewString()), opts...)
	require.NoError(t, err)
	env, err := command.NewEnvelope(payload, md)
	require.NoError(t, err)
	result, err := engine.Dispatch(context.Background(), entityID, env, time.Minute)
	require.NoError(t, err)
	return result
}

// ES-success: expected revision matches the aggregate's current revision.
func TestEventSourcedExpectedRevisionSuccessMatchesCurrent(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "ES-success", store, WithLogger(DiscardLogger))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	require.NoError(t, engine.Entity(ctx, NewEventSourcedEntity(entityID)))

	result := dispatchWithMetadata(t, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
	require.Equal(t, command.OutcomeSuccess, result.Outcome())
	assert.EqualValues(t, 1, result.Revision())

	result = dispatchWithMetadata(t, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(1))
	require.Equal(t, command.OutcomeSuccess, result.Outcome())
	assert.EqualValues(t, 2, result.Revision())

	state, ok := result.State()
	require.True(t, ok)
	acct, ok := state.(*testpb.Account)
	require.True(t, ok)
	assert.EqualValues(t, 750, acct.GetAccountBalance())
}

// ES-stale: expected revision is behind the aggregate's current revision.
// Mandatorily verified end-to-end through command.Result (not just at the
// actor/message level): OutcomeRejected, Failure.Code() ==
// CodeConcurrencyConflict, and errors.As recovers the underlying
// *persistence.ConflictError with the store's real actual/expected revisions.
func TestEventSourcedExpectedRevisionStaleIsConcurrencyConflict(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "ES-stale", store, WithLogger(DiscardLogger))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	require.NoError(t, engine.Entity(ctx, NewEventSourcedEntity(entityID)))

	created := dispatchWithMetadata(t, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
	require.Equal(t, command.OutcomeSuccess, created.Outcome())
	require.EqualValues(t, 1, created.Revision())

	result := dispatchWithMetadata(t, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(99))

	require.Equal(t, command.OutcomeRejected, result.Outcome())
	failure, ok := result.Failure()
	require.True(t, ok)
	code, hasCode := failure.Code()
	require.True(t, hasCode)
	assert.Equal(t, command.CodeConcurrencyConflict, code)
	require.True(t, errors.Is(result.Err(), command.ErrRejected))

	var conflict *persistence.ConflictError
	require.True(t, errors.As(result.Err(), &conflict))
	actual, ok := conflict.ActualRevision()
	require.True(t, ok)
	assert.EqualValues(t, 1, actual)
	assert.Equal(t, persistence.ExpectRevision(99), conflict.Expected())
}

// ES-genesis-success: expected revision 0 (genesis) on a brand-new aggregate.
func TestEventSourcedExpectedRevisionGenesisSucceedsOnNewAggregate(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "ES-genesis-success", store, WithLogger(DiscardLogger))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	require.NoError(t, engine.Entity(ctx, NewEventSourcedEntity(entityID)))

	result := dispatchWithMetadata(t, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
	require.Equal(t, command.OutcomeSuccess, result.Outcome())
	assert.EqualValues(t, 1, result.Revision())
}

// ES-genesis-conflict: expected revision 0 (genesis) against an aggregate
// that already has committed events. Mandatorily verified end-to-end
// through command.Result, same contract as ES-stale.
func TestEventSourcedExpectedRevisionGenesisConflictsOnExistingAggregate(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "ES-genesis-conflict", store, WithLogger(DiscardLogger))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	require.NoError(t, engine.Entity(ctx, NewEventSourcedEntity(entityID)))

	created := dispatchWithMetadata(t, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
	require.Equal(t, command.OutcomeSuccess, created.Outcome())
	require.EqualValues(t, 1, created.Revision())

	result := dispatchWithMetadata(t, engine, entityID, &testpb.CreateAccount{AccountBalance: 999}, command.WithExpectedRevision(0))

	require.Equal(t, command.OutcomeRejected, result.Outcome())
	failure, ok := result.Failure()
	require.True(t, ok)
	code, hasCode := failure.Code()
	require.True(t, hasCode)
	assert.Equal(t, command.CodeConcurrencyConflict, code)

	var conflict *persistence.ConflictError
	require.True(t, errors.As(result.Err(), &conflict))
	actual, ok := conflict.ActualRevision()
	require.True(t, ok)
	assert.EqualValues(t, 1, actual)
	assert.Equal(t, persistence.ExpectGenesis(), conflict.Expected())
}

// ES-legacy: a command sent through the legacy SendCommand entry point never
// declares an ExpectedRevision, so it must keep writing unconditionally
// exactly as before WRITE-004 (backward compatibility, design.md D4/D8).
func TestEventSourcedLegacyCommandIsUnconditional(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "ES-legacy", store, WithLogger(DiscardLogger))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	require.NoError(t, engine.Entity(ctx, NewEventSourcedEntity(entityID)))

	state, revision, err := engine.SendCommand(ctx, entityID, &testpb.CreateAccount{AccountBalance: 500}, time.Minute)
	require.NoError(t, err)
	acct, ok := state.(*testpb.Account)
	require.True(t, ok)
	assert.EqualValues(t, 500, acct.GetAccountBalance())
	assert.EqualValues(t, 1, revision)

	state, revision, err = engine.SendCommand(ctx, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, time.Minute)
	require.NoError(t, err)
	acct, ok = state.(*testpb.Account)
	require.True(t, ok)
	assert.EqualValues(t, 750, acct.GetAccountBalance())
	assert.EqualValues(t, 2, revision)
}

// preconditionSpyEventsStore wraps a real testkit.EventStore, recording every
// WritePrecondition WriteEvents actually receives so ES-propagation can
// assert on the exact value that crossed the whole Dispatch -> carrier ->
// actor -> persistence path, not merely infer it from the write's outcome.
type preconditionSpyEventsStore struct {
	*testkit.EventStore
	mu            sync.Mutex
	preconditions []persistence.WritePrecondition
}

func (s *preconditionSpyEventsStore) WriteEvents(ctx context.Context, events []*egopb.Event, precondition persistence.WritePrecondition) error {
	s.mu.Lock()
	s.preconditions = append(s.preconditions, precondition)
	s.mu.Unlock()
	return s.EventStore.WriteEvents(ctx, events, precondition)
}

// ES-propagation: proves the D4 ExpectedRevision -> WritePrecondition mapping
// is exactly what reaches persistence.EventsStore.WriteEvents, for all three
// cases (absent, genesis, exact revision) - not just that the command
// eventually succeeds or fails.
func TestEventSourcedExpectedRevisionPropagatesToPersistencePrecondition(t *testing.T) {
	ctx := context.Background()
	underlying := testkit.NewEventsStore()
	require.NoError(t, underlying.Connect(ctx))
	t.Cleanup(func() { _ = underlying.Disconnect(ctx) })
	spy := &preconditionSpyEventsStore{EventStore: underlying}

	engine := newTestEngine(t, "ES-propagation", spy, WithLogger(DiscardLogger))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	require.NoError(t, engine.Entity(ctx, NewEventSourcedEntity(entityID)))

	result := dispatchWithMetadata(t, engine, entityID, &testpb.CreateAccount{AccountBalance: 500})
	require.Equal(t, command.OutcomeSuccess, result.Outcome())

	result = dispatchWithMetadata(t, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(1))
	require.Equal(t, command.OutcomeSuccess, result.Outcome())

	result = dispatchWithMetadata(t, engine, entityID, &testpb.CreateAccount{AccountBalance: 1}, command.WithExpectedRevision(0))
	require.Equal(t, command.OutcomeRejected, result.Outcome())

	require.Len(t, spy.preconditions, 3)
	assert.Equal(t, persistence.Unconditional(), spy.preconditions[0])
	assert.Equal(t, persistence.ExpectRevision(1), spy.preconditions[1])
	assert.Equal(t, persistence.ExpectGenesis(), spy.preconditions[2])
}

// ES-conflict-state: after a rejected conflicting write, the actor must
// remain functionally consistent - a subsequent command declaring the
// correct expected revision succeeds against the true, uncorrupted state.
// This is the case design.md D10 names as "provably in sync" (the failed
// write's declared actual revision equals what this actor already holds in
// memory), so the actor stays alive rather than being torn down.
func TestEventSourcedActorStaysConsistentAfterConflict(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "ES-conflict-state", store, WithLogger(DiscardLogger))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	require.NoError(t, engine.Entity(ctx, NewEventSourcedEntity(entityID)))

	created := dispatchWithMetadata(t, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
	require.Equal(t, command.OutcomeSuccess, created.Outcome())
	require.EqualValues(t, 1, created.Revision())

	conflicted := dispatchWithMetadata(t, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(99))
	require.Equal(t, command.OutcomeRejected, conflicted.Outcome())

	result := dispatchWithMetadata(t, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(1))
	require.Equal(t, command.OutcomeSuccess, result.Outcome())
	assert.EqualValues(t, 2, result.Revision())

	state, ok := result.State()
	require.True(t, ok)
	acct, ok := state.(*testpb.Account)
	require.True(t, ok)
	assert.EqualValues(t, 750, acct.GetAccountBalance())
}

// TestEventSourcedBatchedExpectedRevisionSuccessAndConflict exercises D9's
// batched-path wiring (resolveBatchPrecondition, batchBase/batchHasPrecondition
// seeding, and handleBatchPersistResponse's D10 lifecycle) end-to-end through
// command.Result, using a threshold that flushes every single command's batch
// on its own so the assertions stay deterministic without needing concurrent
// callers to exercise the multi-command admission gate.
func TestEventSourcedBatchedExpectedRevisionSuccessAndConflict(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "ES-batched", store, WithLogger(DiscardLogger))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	require.NoError(t, engine.Entity(ctx, NewEventSourcedEntity(entityID), WithBatchThreshold(1)))

	created := dispatchWithMetadata(t, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
	require.Equal(t, command.OutcomeSuccess, created.Outcome())
	require.EqualValues(t, 1, created.Revision())

	conflicted := dispatchWithMetadata(t, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(99))
	require.Equal(t, command.OutcomeRejected, conflicted.Outcome())
	failure, ok := conflicted.Failure()
	require.True(t, ok)
	code, hasCode := failure.Code()
	require.True(t, hasCode)
	assert.Equal(t, command.CodeConcurrencyConflict, code)

	result := dispatchWithMetadata(t, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(1))
	require.Equal(t, command.OutcomeSuccess, result.Outcome())
	assert.EqualValues(t, 2, result.Revision())
}
