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

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/getsyntegrity/urd/command"
	"github.com/getsyntegrity/urd/egopb"
	"github.com/getsyntegrity/urd/internal/engine/enginetest"
	testpb "github.com/getsyntegrity/urd/internal/testpb"
	"github.com/getsyntegrity/urd/persistence"
	"github.com/getsyntegrity/urd/testkit"
)

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

// durableStateRevisionRig is a started engine over a connected in-memory state
// store with one account entity spawned on it, which most cases here start from.
type durableStateRevisionRig struct {
	store    *testkit.DurableStore
	engine   *Engine
	entityID string
}

// newDurableStateRevisionRig starts an engine named name over a fresh state
// store and spawns the standard account behavior on it.
func newDurableStateRevisionRig(ctx *specs.Context, name string) durableStateRevisionRig {
	store := connectedDurableStore(ctx)
	engine := startEngine(ctx, name, nil, WithLogger(DiscardLogger), WithStateStore(store))
	entityID := uuid.NewString()
	ctx.Expect(engine.DurableStateEntity(context.Background(), NewAccountDurableStateBehavior(entityID))).To(specs.BeNil())
	return durableStateRevisionRig{store: store, engine: engine, entityID: entityID}
}

// createAccount commits the genesis command (balance 500, revision 1).
func (r durableStateRevisionRig) createAccount(ctx *specs.Context) {
	created := dispatch(ctx, r.engine, r.entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
	expectSuccess(ctx, created)
	specs.ExpectT(ctx, created.Revision()).ToEqual(1)
}

// credit dispatches a credit declaring expectedRevision.
func (r durableStateRevisionRig) credit(ctx *specs.Context, balance float64, expectedRevision uint64) command.Result {
	return dispatch(ctx, r.engine, r.entityID, &testpb.CreditAccount{AccountId: r.entityID, Balance: balance}, command.WithExpectedRevision(expectedRevision))
}

// storedVersion returns the persisted version number of the entity.
func (r durableStateRevisionRig) storedVersion(ctx *specs.Context) uint64 {
	durable, err := r.store.GetLatestState(context.Background(), persistence.Unscoped(), r.entityID)
	ctx.Expect(err).To(specs.BeNil())
	return durable.GetVersionNumber()
}

// DS-handler-shape (tasks 4.8/4.9): a command carrying ExpectedRevision=N
// must reach HandleCommand with the same (ctx, cmd, priorVersion, priorState)
// shape it always had, and N must never appear as priorVersion (or anywhere
// else in the handler's arguments) — it only travels to the conditional
// write step.
func TestDurableStateHandlerShapeUnchangedByExpectedRevision(t *testing.T) {
	specs.Describe(t, "the arguments a durable state handler receives", func(s *specs.Spec) {
		s.It("never carry the declared ExpectedRevision", func(ctx *specs.Context) {
			store := connectedDurableStore(ctx)
			engine := startEngine(ctx, "DS-handler-shape", nil, WithLogger(DiscardLogger), WithStateStore(store))

			entityID := uuid.NewString()
			behavior := newRevisionProbeDurableStateBehavior(entityID)
			ctx.Expect(engine.DurableStateEntity(context.Background(), behavior)).To(specs.BeNil())

			created := dispatch(ctx, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
			expectSuccess(ctx, created)

			// A second call declaring an ExpectedRevision of its own: if N ever
			// leaked into the handler as priorVersion, the recorded value would
			// differ from the actor's real version.
			credited := dispatch(ctx, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(1))
			expectSuccess(ctx, credited)

			// The genesis call sees priorVersion=0, not ExpectedRevision=0
			// reinterpreted as anything else; the second call sees the actor's
			// real currentVersion (1), never the declared ExpectedRevision.
			ctx.Expect(behavior.observedPriorVersions()).ToEqual([]uint64{0, 1})
		})
	})
}

// DS-exact-match (task 4.10): a command whose ExpectedRevision matches the
// persisted revision commits, and the new state lands at N+1.
func TestDurableStateExpectedRevisionExactMatchCommits(t *testing.T) {
	specs.Describe(t, "a durable state command whose ExpectedRevision matches the persisted revision", func(s *specs.Spec) {
		s.It("commits and the new state lands at N+1", func(ctx *specs.Context) {
			rig := newDurableStateRevisionRig(ctx, "DS-exact-match")
			rig.createAccount(ctx)

			result := rig.credit(ctx, 250, 1)
			expectSuccess(ctx, result)
			specs.ExpectT(ctx, result.Revision()).ToEqual(2)
			specs.ExpectT(ctx, rig.storedVersion(ctx)).ToEqual(2)
		})
	})
}

// DS-stale-store-not-cache (task 4.11): a second, independent actor instance
// (its own Engine/actor system) commits a further write against the same
// persistence ID and store, advancing StorageRevision past what the first
// actor's own in-memory currentVersion still reads. The first actor's next
// command, declaring ExpectedRevision equal to its own (now stale)
// currentVersion, must be rejected against the real store, not accepted
// because its local cache still agrees.
func TestDurableStateStaleExpectedRevisionRejectedAtStoreNotCache(t *testing.T) {
	specs.Describe(t, "an actor whose in-memory version is behind the store", func(s *specs.Spec) {
		s.It("rejects a command that matches its own stale cache", func(ctx *specs.Context) {
			bg := context.Background()
			store := connectedDurableStore(ctx)
			entityID := uuid.NewString()

			engineA := startEngine(ctx, "DS-stale-a", nil, WithLogger(DiscardLogger), WithStateStore(store))
			ctx.Expect(engineA.DurableStateEntity(bg, NewAccountDurableStateBehavior(entityID))).To(specs.BeNil())

			created := dispatch(ctx, engineA, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
			expectSuccess(ctx, created)
			specs.ExpectT(ctx, created.Revision()).ToEqual(1)

			// A second, independent actor instance for the same persistence ID,
			// against the same underlying store, advances StorageRevision to 2
			// without engineA's actor ever observing it.
			engineB := startEngine(ctx, "DS-stale-b", nil, WithLogger(DiscardLogger), WithStateStore(store))
			ctx.Expect(engineB.DurableStateEntity(bg, NewAccountDurableStateBehavior(entityID))).To(specs.BeNil())

			advanced := dispatch(ctx, engineB, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 100}, command.WithExpectedRevision(1))
			expectSuccess(ctx, advanced)
			specs.ExpectT(ctx, advanced.Revision()).ToEqual(2)

			// engineA's actor still believes currentVersion==1; it declares
			// ExpectedRevision=1 to match its own stale cache, but the real store is
			// already at revision 2.
			result := dispatch(ctx, engineA, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(1))
			expectConcurrencyConflict(ctx, result)
			conflict := conflictError(ctx, result)
			actual, ok := conflict.ActualRevision()
			ctx.Expect(ok).To(specs.BeTrue())
			specs.ExpectT(ctx, actual).ToEqual(2)
		})
	})
}

// DS-genesis-race (task 4.12): two independent DurableStateActor instances
// (separate Engine/actor systems), both declaring ExpectedRevision=0 against
// the same persistence ID with no prior record, race concurrently. Exactly
// one commits, exactly one is rejected with concurrency_conflict. The store's
// CompareAndSwap/LoadOrStore path, not an external lock, is what serializes
// the two writers.
func TestDurableStateConcurrentGenesisWritersYieldExactlyOneCommit(t *testing.T) {
	specs.Describe(t, "two durable state actors racing the genesis revision on one store", func(s *specs.Spec) {
		s.It("commits exactly one and rejects the other with a concurrency conflict", func(ctx *specs.Context) {
			bg := context.Background()
			store := connectedDurableStore(ctx)
			entityID := uuid.NewString()

			engineA := startEngine(ctx, "DS-race-a", nil, WithLogger(DiscardLogger), WithStateStore(store))
			ctx.Expect(engineA.DurableStateEntity(bg, NewAccountDurableStateBehavior(entityID))).To(specs.BeNil())

			engineB := startEngine(ctx, "DS-race-b", nil, WithLogger(DiscardLogger), WithStateStore(store))
			ctx.Expect(engineB.DurableStateEntity(bg, NewAccountDurableStateBehavior(entityID))).To(specs.BeNil())

			var wg sync.WaitGroup
			results := make([]command.Result, 2)
			wg.Add(2)
			ctx.Go(func(ctx *specs.Context) {
				defer wg.Done()
				results[0] = dispatch(ctx, engineA, entityID, &testpb.CreateAccount{AccountBalance: 100}, command.WithExpectedRevision(0))
			})
			ctx.Go(func(ctx *specs.Context) {
				defer wg.Done()
				results[1] = dispatch(ctx, engineB, entityID, &testpb.CreateAccount{AccountBalance: 200}, command.WithExpectedRevision(0))
			})
			wg.Wait()

			outcomes := []command.Outcome{results[0].Outcome(), results[1].Outcome()}
			ctx.Expect(outcomes).To(specs.ContainTheSameElementsAs([]command.Outcome{command.OutcomeSuccess, command.OutcomeRejected}))
			for _, result := range results {
				if result.Outcome() == command.OutcomeRejected {
					expectConcurrencyConflict(ctx, result)
				}
			}

			durable, err := store.GetLatestState(bg, persistence.Unscoped(), entityID)
			ctx.Expect(err).To(specs.BeNil())
			specs.ExpectT(ctx, durable.GetVersionNumber()).ToEqual(1)
		})
	})
}

// DS-both-checks-independent (task 4.13): the handler returns a version
// adjacent to the actor's currentVersion (checkPreconditions passes) but the
// declared ExpectedRevision no longer matches the persisted revision. The
// command must still fail with concurrency_conflict — checkPreconditions
// passing is not sufficient for the write to succeed.
func TestDurableStateCheckPreconditionsPassesYetExpectedRevisionConflicts(t *testing.T) {
	specs.Describe(t, "a handler version that passes checkPreconditions", func(s *specs.Spec) {
		s.It("still conflicts when the declared ExpectedRevision no longer matches", func(ctx *specs.Context) {
			rig := newDurableStateRevisionRig(ctx, "DS-both-checks")
			rig.createAccount(ctx)

			// priorVersion=1, handler returns priorVersion+1=2: adjacent, so
			// checkPreconditions passes. ExpectedRevision=99 does not match the
			// persisted revision (1), so the conditional write still rejects.
			result := rig.credit(ctx, 250, 99)
			expectConcurrencyConflict(ctx, result)
		})
	})
}

// DS-non-adjacent-not-conflict (task 4.14): a handler-produced non-adjacent
// version is rejected by checkPreconditions, a failure class distinct from
// concurrency_conflict — its Failure.Code() must not read
// "concurrency_conflict".
func TestDurableStateNonAdjacentVersionIsNeverConcurrencyConflict(t *testing.T) {
	specs.Describe(t, "a handler-produced non-adjacent version", func(s *specs.Spec) {
		s.It("fails without the concurrency_conflict code", func(ctx *specs.Context) {
			store := connectedDurableStore(ctx)
			engine := startEngine(ctx, "DS-non-adjacent", nil, WithLogger(DiscardLogger), WithStateStore(store))

			entityID := uuid.NewString()
			ctx.Expect(engine.DurableStateEntity(context.Background(), enginetest.NewBadVersionDurableStateBehavior(entityID))).To(specs.BeNil())

			result := dispatch(ctx, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
			ctx.Expect(result.Outcome()).To(specs.NotEqual(command.OutcomeSuccess))

			failure, ok := result.Failure()
			ctx.Expect(ok).To(specs.BeTrue())
			if code, hasCode := failure.Code(); hasCode {
				ctx.Expect(code).To(specs.NotEqual(command.CodeConcurrencyConflict))
			}
		})
	})
}

// DS-no-partial-commit (task 4.15): on conflict, the actor's in-memory
// state/version are left exactly as they were before the rejected command —
// proven here by a follow-up command declaring the actor's true
// (pre-conflict) revision, which must still succeed against the
// uncorrupted state.
func TestDurableStateNoPartialCommitOnConflict(t *testing.T) {
	specs.Describe(t, "a durable state actor after a rejected conflicting write", func(s *specs.Spec) {
		s.It("keeps its state and the stored revision untouched", func(ctx *specs.Context) {
			rig := newDurableStateRevisionRig(ctx, "DS-no-partial-commit")
			rig.createAccount(ctx)

			conflicted := rig.credit(ctx, 999, 99)
			ctx.Expect(conflicted.Outcome()).ToEqual(command.OutcomeRejected)

			// The rejected write's 999-credit must never have been applied, and the
			// stored revision must still read 1 — proving both the in-memory state
			// and the durable record are untouched by the conflicting attempt.
			specs.ExpectT(ctx, rig.storedVersion(ctx)).ToEqual(1)

			// The follow-up balance is the earlier 500 plus this +250 only.
			result := rig.credit(ctx, 250, 1)
			expectSuccess(ctx, result)
			specs.ExpectT(ctx, result.Revision()).ToEqual(2)
			specs.ExpectT(ctx, accountOf(ctx, result).GetAccountBalance()).ToEqual(750)
		})
	})
}

// DS-conflict-result-shape (task 4.16): the caller's command.Result on a
// conflict reports OutcomeRejected and Failure.Code()==("concurrency_conflict", true).
func TestDurableStateConflictResultShape(t *testing.T) {
	specs.Describe(t, "the command result of a conflicting durable state write", func(s *specs.Spec) {
		s.It("reports a rejected outcome with the concurrency_conflict code", func(ctx *specs.Context) {
			rig := newDurableStateRevisionRig(ctx, "DS-conflict-shape")
			rig.createAccount(ctx)

			result := dispatch(ctx, rig.engine, rig.entityID, &testpb.CreateAccount{AccountBalance: 999}, command.WithExpectedRevision(0))
			ctx.Expect(result.Err()).To(specs.MatchError(command.ErrRejected))

			expectConcurrencyConflict(ctx, result)
			conflict := conflictError(ctx, result)
			ctx.Expect(conflict.Expected()).ToEqual(persistence.ExpectGenesis())
		})
	})
}

// DS-storage-revision-authority (task 4.17): when the actor's currentVersion
// has fallen behind the store's real StorageRevision because another
// process wrote directly to the store (bypassing this actor entirely), the
// conditional write is still evaluated against StorageRevision and rejects,
// even though nothing about the actor's own in-memory bookkeeping ever
// changed.
func TestDurableStateConditionalWriteEvaluatedAgainstStorageRevision(t *testing.T) {
	specs.Describe(t, "a durable state actor whose version fell behind the store", func(s *specs.Spec) {
		s.It("evaluates the conditional write against the real StorageRevision", func(ctx *specs.Context) {
			rig := newDurableStateRevisionRig(ctx, "DS-storage-authority")
			rig.createAccount(ctx)

			// Another process writes directly to the store, bypassing this actor
			// entirely (no dispatch, no command) — this actor's currentVersion still
			// reads 1, but the real StorageRevision is now 2.
			ctx.Expect(writeDirectDurableState(context.Background(), rig.store, rig.entityID, 2, &testpb.Account{AccountId: rig.entityID, AccountBalance: 999})).To(specs.BeNil())

			result := rig.credit(ctx, 250, 1)
			expectConcurrencyConflict(ctx, result)
			conflict := conflictError(ctx, result)
			actual, ok := conflict.ActualRevision()
			ctx.Expect(ok).To(specs.BeTrue())
			specs.ExpectT(ctx, actual).ToEqual(2)
		})
	})
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
