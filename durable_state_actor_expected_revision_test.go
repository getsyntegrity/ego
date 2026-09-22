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
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/pablogore/ego/v4/command"
	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/persistence"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
	"github.com/pablogore/ego/v4/testkit"
)

// -----------------------------------------------------------------------
// Unit test for the DurableStateActor-specific D10 helper (EGO-WRITE-004
// PR4, task 4.7). Mirrors TestShouldStayAliveAfterConflict in
// event_sourced_actor_expected_revision_test.go: same decision logic
// (provably in sync iff err is a *persistence.ConflictError whose
// ActualRevision is known and equals the actor's current in-memory
// version), inverted in effect (recover in place instead of shutting down).
// -----------------------------------------------------------------------

func TestProvablyInSyncAfterConflict(t *testing.T) {
	t.Run("non-conflict error is never provably in sync", func(t *testing.T) {
		entity := &DurableStateActor{currentVersion: 3}
		assert.False(t, entity.provablyInSyncAfterConflict(errors.New("boom")))
	})

	t.Run("actual revision matches in-memory version: provably in sync", func(t *testing.T) {
		entity := &DurableStateActor{currentVersion: 3}
		conflictErr := persistence.NewConflictError(persistence.Unscoped(), "entity-1", persistence.ExpectRevision(5), persistence.WithActualRevision(3))
		assert.True(t, entity.provablyInSyncAfterConflict(conflictErr))
	})

	t.Run("actual revision diverges from in-memory version: not provably in sync", func(t *testing.T) {
		entity := &DurableStateActor{currentVersion: 3}
		conflictErr := persistence.NewConflictError(persistence.Unscoped(), "entity-1", persistence.ExpectRevision(5), persistence.WithActualRevision(7))
		assert.False(t, entity.provablyInSyncAfterConflict(conflictErr))
	})

	t.Run("conflict without an actual revision cannot be proven in sync", func(t *testing.T) {
		entity := &DurableStateActor{currentVersion: 3}
		conflictErr := persistence.NewConflictError(persistence.Unscoped(), "entity-1", persistence.ExpectRevision(5))
		assert.False(t, entity.provablyInSyncAfterConflict(conflictErr))
	})
}

// -----------------------------------------------------------------------
// revisionProbeDurableStateBehavior records every HandleCommand invocation's
// priorVersion/priorState, so tasks 4.8/4.9 can assert what the handler
// actually received, independent of what ExpectedRevision the dispatched
// command's metadata carried.
// -----------------------------------------------------------------------

type revisionProbeDurableStateBehavior struct {
	id string

	mu            sync.Mutex
	priorVersions []uint64
}

var _ DurableStateBehavior = (*revisionProbeDurableStateBehavior)(nil)

func newRevisionProbeDurableStateBehavior(id string) *revisionProbeDurableStateBehavior {
	return &revisionProbeDurableStateBehavior{id: id}
}

func (x *revisionProbeDurableStateBehavior) ID() string {
	return x.id
}

func (x *revisionProbeDurableStateBehavior) InitialState() State {
	return new(testpb.Account)
}

// nolint
func (x *revisionProbeDurableStateBehavior) HandleCommand(_ context.Context, cmd Command, priorVersion uint64, priorState State) (State, uint64, error) {
	x.mu.Lock()
	x.priorVersions = append(x.priorVersions, priorVersion)
	x.mu.Unlock()

	switch c := cmd.(type) {
	case *testpb.CreateAccount:
		return &testpb.Account{AccountId: x.id, AccountBalance: c.GetAccountBalance()}, priorVersion + 1, nil
	case *testpb.CreditAccount:
		account := priorState.(*testpb.Account)
		return &testpb.Account{
			AccountId:      x.id,
			AccountBalance: account.GetAccountBalance() + c.GetBalance(),
		}, priorVersion + 1, nil
	default:
		return nil, 0, errors.New("unhandled command")
	}
}

func (x *revisionProbeDurableStateBehavior) MarshalBinary() ([]byte, error) {
	return json.Marshal(struct {
		ID string `json:"id"`
	}{ID: x.id})
}

func (x *revisionProbeDurableStateBehavior) UnmarshalBinary(data []byte) error {
	aux := struct {
		ID string `json:"id"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	x.id = aux.ID
	return nil
}

// observedPriorVersions returns every priorVersion HandleCommand has been
// called with so far, in call order.
func (x *revisionProbeDurableStateBehavior) observedPriorVersions() []uint64 {
	x.mu.Lock()
	defer x.mu.Unlock()
	return append([]uint64(nil), x.priorVersions...)
}

// DS-handler-shape (tasks 4.8/4.9): a command carrying ExpectedRevision=N
// must reach HandleCommand with the same (ctx, cmd, priorVersion, priorState)
// shape it always had, and N must never appear as priorVersion (or anywhere
// else in the handler's arguments) — it only travels to the conditional
// write step.
func TestDurableStateHandlerShapeUnchangedByExpectedRevision(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewDurableStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "DS-handler-shape", nil, WithLogger(DiscardLogger), WithStateStore(store))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	behavior := newRevisionProbeDurableStateBehavior(entityID)
	require.NoError(t, engine.DurableStateEntity(ctx, behavior))

	created := dispatchWithMetadata(t, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
	require.Equal(t, command.OutcomeSuccess, created.Outcome())

	// A deliberately large, unrelated-to-version ExpectedRevision: if N ever
	// leaked into the handler as priorVersion, this call's recorded
	// priorVersion would read 999999 instead of the actor's real version (1).
	credited := dispatchWithMetadata(t, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(1))
	require.Equal(t, command.OutcomeSuccess, credited.Outcome())

	priorVersions := behavior.observedPriorVersions()
	require.Len(t, priorVersions, 2)
	assert.EqualValues(t, 0, priorVersions[0], "genesis call must see priorVersion=0, not ExpectedRevision=0 reinterpreted as anything else")
	assert.EqualValues(t, 1, priorVersions[1], "second call must see the actor's real currentVersion (1), never the declared ExpectedRevision")
}

// DS-exact-match (task 4.10): a command whose ExpectedRevision matches the
// persisted revision commits, and the new state lands at N+1.
func TestDurableStateExpectedRevisionExactMatchCommits(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewDurableStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "DS-exact-match", nil, WithLogger(DiscardLogger), WithStateStore(store))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	require.NoError(t, engine.DurableStateEntity(ctx, NewAccountDurableStateBehavior(entityID)))

	created := dispatchWithMetadata(t, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
	require.Equal(t, command.OutcomeSuccess, created.Outcome())
	assert.EqualValues(t, 1, created.Revision())

	result := dispatchWithMetadata(t, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(1))
	require.Equal(t, command.OutcomeSuccess, result.Outcome())
	assert.EqualValues(t, 2, result.Revision())

	durable, err := store.GetLatestState(ctx, persistence.Unscoped(), entityID)
	require.NoError(t, err)
	require.NotNil(t, durable)
	assert.EqualValues(t, 2, durable.GetVersionNumber())
}

// DS-stale-store-not-cache (task 4.11): a second, independent actor instance
// (its own Engine/actor system) commits a further write against the same
// persistence ID and store, advancing StorageRevision past what the first
// actor's own in-memory currentVersion still reads. The first actor's next
// command, declaring ExpectedRevision equal to its own (now stale)
// currentVersion, must be rejected against the real store, not accepted
// because its local cache still agrees.
func TestDurableStateStaleExpectedRevisionRejectedAtStoreNotCache(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewDurableStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	entityID := uuid.NewString()

	engineA := newTestEngine(t, "DS-stale-a", nil, WithLogger(DiscardLogger), WithStateStore(store))
	require.NoError(t, engineA.Start(ctx))
	require.NoError(t, engineA.DurableStateEntity(ctx, NewAccountDurableStateBehavior(entityID)))

	created := dispatchWithMetadata(t, engineA, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
	require.Equal(t, command.OutcomeSuccess, created.Outcome())
	require.EqualValues(t, 1, created.Revision())

	// A second, independent actor instance for the same persistence ID,
	// against the same underlying store, advances StorageRevision to 2
	// without engineA's actor ever observing it.
	engineB := newTestEngine(t, "DS-stale-b", nil, WithLogger(DiscardLogger), WithStateStore(store))
	require.NoError(t, engineB.Start(ctx))
	require.NoError(t, engineB.DurableStateEntity(ctx, NewAccountDurableStateBehavior(entityID)))

	advanced := dispatchWithMetadata(t, engineB, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 100}, command.WithExpectedRevision(1))
	require.Equal(t, command.OutcomeSuccess, advanced.Outcome())
	require.EqualValues(t, 2, advanced.Revision())

	// engineA's actor still believes currentVersion==1; it declares
	// ExpectedRevision=1 to match its own stale cache, but the real store is
	// already at revision 2.
	result := dispatchWithMetadata(t, engineA, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(1))
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
	assert.EqualValues(t, 2, actual)
}

// DS-genesis-race (task 4.12): two independent DurableStateActor instances
// (separate Engine/actor systems), both declaring ExpectedRevision=0 against
// the same persistence ID with no prior record, race concurrently. Exactly
// one commits, exactly one is rejected with concurrency_conflict. Run with
// -race to prove the store's CompareAndSwap/LoadOrStore path, not an
// external lock, is what serializes the two writers.
func TestDurableStateConcurrentGenesisWritersYieldExactlyOneCommit(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewDurableStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	entityID := uuid.NewString()

	engineA := newTestEngine(t, "DS-race-a", nil, WithLogger(DiscardLogger), WithStateStore(store))
	require.NoError(t, engineA.Start(ctx))
	require.NoError(t, engineA.DurableStateEntity(ctx, NewAccountDurableStateBehavior(entityID)))

	engineB := newTestEngine(t, "DS-race-b", nil, WithLogger(DiscardLogger), WithStateStore(store))
	require.NoError(t, engineB.Start(ctx))
	require.NoError(t, engineB.DurableStateEntity(ctx, NewAccountDurableStateBehavior(entityID)))

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

	durable, err := store.GetLatestState(ctx, persistence.Unscoped(), entityID)
	require.NoError(t, err)
	require.NotNil(t, durable)
	assert.EqualValues(t, 1, durable.GetVersionNumber())
}

// DS-both-checks-independent (task 4.13): the handler returns a version
// adjacent to the actor's currentVersion (checkPreconditions passes) but the
// declared ExpectedRevision no longer matches the persisted revision. The
// command must still fail with concurrency_conflict — checkPreconditions
// passing is not sufficient for the write to succeed.
func TestDurableStateCheckPreconditionsPassesYetExpectedRevisionConflicts(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewDurableStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "DS-both-checks", nil, WithLogger(DiscardLogger), WithStateStore(store))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	require.NoError(t, engine.DurableStateEntity(ctx, NewAccountDurableStateBehavior(entityID)))

	created := dispatchWithMetadata(t, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
	require.Equal(t, command.OutcomeSuccess, created.Outcome())
	require.EqualValues(t, 1, created.Revision())

	// priorVersion=1, handler returns priorVersion+1=2: adjacent, so
	// checkPreconditions passes. ExpectedRevision=99 does not match the
	// persisted revision (1), so the conditional write still rejects.
	result := dispatchWithMetadata(t, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(99))
	require.Equal(t, command.OutcomeRejected, result.Outcome())
	failure, ok := result.Failure()
	require.True(t, ok)
	code, hasCode := failure.Code()
	require.True(t, hasCode)
	assert.Equal(t, command.CodeConcurrencyConflict, code)
}

// DS-non-adjacent-not-conflict (task 4.14): a handler-produced non-adjacent
// version is rejected by checkPreconditions, a failure class distinct from
// concurrency_conflict — its Failure.Code() must not read
// "concurrency_conflict".
func TestDurableStateNonAdjacentVersionIsNeverConcurrencyConflict(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewDurableStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "DS-non-adjacent", nil, WithLogger(DiscardLogger), WithStateStore(store))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	require.NoError(t, engine.DurableStateEntity(ctx, &badVersionDurableStateBehavior{id: entityID}))

	result := dispatchWithMetadata(t, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
	require.NotEqual(t, command.OutcomeSuccess, result.Outcome())

	failure, ok := result.Failure()
	require.True(t, ok)
	code, hasCode := failure.Code()
	if hasCode {
		assert.NotEqual(t, command.CodeConcurrencyConflict, code)
	}
}

// DS-no-partial-commit (task 4.15): on conflict, the actor's in-memory
// state/version are left exactly as they were before the rejected command —
// proven here by a follow-up command declaring the actor's true
// (pre-conflict) revision, which must still succeed against the
// uncorrupted state.
func TestDurableStateNoPartialCommitOnConflict(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewDurableStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "DS-no-partial-commit", nil, WithLogger(DiscardLogger), WithStateStore(store))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	require.NoError(t, engine.DurableStateEntity(ctx, NewAccountDurableStateBehavior(entityID)))

	created := dispatchWithMetadata(t, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
	require.Equal(t, command.OutcomeSuccess, created.Outcome())
	require.EqualValues(t, 1, created.Revision())

	conflicted := dispatchWithMetadata(t, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 999}, command.WithExpectedRevision(99))
	require.Equal(t, command.OutcomeRejected, conflicted.Outcome())

	// The rejected write's 999-credit must never have been applied, and the
	// stored revision must still read 1 — proving both the in-memory state
	// and the durable record are untouched by the conflicting attempt.
	durable, err := store.GetLatestState(ctx, persistence.Unscoped(), entityID)
	require.NoError(t, err)
	require.NotNil(t, durable)
	assert.EqualValues(t, 1, durable.GetVersionNumber())

	result := dispatchWithMetadata(t, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(1))
	require.Equal(t, command.OutcomeSuccess, result.Outcome())
	assert.EqualValues(t, 2, result.Revision())

	state, ok := result.State()
	require.True(t, ok)
	acct, ok := state.(*testpb.Account)
	require.True(t, ok)
	assert.EqualValues(t, 750, acct.GetAccountBalance(), "the rejected +999 credit must not have landed, only the earlier 500 plus this +250")
}

// DS-conflict-result-shape (task 4.16): the caller's command.Result on a
// conflict reports OutcomeRejected and Failure.Code()==("concurrency_conflict", true).
func TestDurableStateConflictResultShape(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewDurableStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "DS-conflict-shape", nil, WithLogger(DiscardLogger), WithStateStore(store))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	require.NoError(t, engine.DurableStateEntity(ctx, NewAccountDurableStateBehavior(entityID)))

	created := dispatchWithMetadata(t, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
	require.Equal(t, command.OutcomeSuccess, created.Outcome())

	result := dispatchWithMetadata(t, engine, entityID, &testpb.CreateAccount{AccountBalance: 999}, command.WithExpectedRevision(0))
	require.Equal(t, command.OutcomeRejected, result.Outcome())
	require.True(t, errors.Is(result.Err(), command.ErrRejected))

	failure, ok := result.Failure()
	require.True(t, ok)
	code, hasCode := failure.Code()
	require.True(t, hasCode)
	assert.Equal(t, command.CodeConcurrencyConflict, code)

	var conflict *persistence.ConflictError
	require.True(t, errors.As(result.Err(), &conflict))
	assert.Equal(t, persistence.ExpectGenesis(), conflict.Expected())
}

// DS-storage-revision-authority (task 4.17): when the actor's currentVersion
// has fallen behind the store's real StorageRevision because another
// process wrote directly to the store (bypassing this actor entirely), the
// conditional write is still evaluated against StorageRevision and rejects,
// even though nothing about the actor's own in-memory bookkeeping ever
// changed.
func TestDurableStateConditionalWriteEvaluatedAgainstStorageRevision(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewDurableStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "DS-storage-authority", nil, WithLogger(DiscardLogger), WithStateStore(store))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	require.NoError(t, engine.DurableStateEntity(ctx, NewAccountDurableStateBehavior(entityID)))

	created := dispatchWithMetadata(t, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
	require.Equal(t, command.OutcomeSuccess, created.Outcome())
	require.EqualValues(t, 1, created.Revision())

	// Another process writes directly to the store, bypassing this actor
	// entirely (no dispatch, no command) — this actor's currentVersion still
	// reads 1, but the real StorageRevision is now 2.
	require.NoError(t, writeDirectDurableState(ctx, store, entityID, 2, &testpb.Account{AccountId: entityID, AccountBalance: 999}))

	result := dispatchWithMetadata(t, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(1))
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
	assert.EqualValues(t, 2, actual)
}

// writeDirectDurableState writes state directly to store, unconditionally,
// simulating a writer that bypasses the DurableStateActor entirely (e.g.
// another process or node). Used only to construct "actor's currentVersion
// has fallen behind StorageRevision" scenarios.
func writeDirectDurableState(ctx context.Context, store *testkit.DurableStore, persistenceID string, version uint64, state State) error {
	stateAny, err := anypb.New(state)
	if err != nil {
		return err
	}
	return store.WriteState(ctx, persistence.Unscoped(), &egopb.DurableState{
		PersistenceId:  persistenceID,
		VersionNumber:  version,
		ResultingState: stateAny,
	}, persistence.Unconditional())
}
