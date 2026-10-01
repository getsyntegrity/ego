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
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"github.com/getsyntegrity/ego/command"
	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/persistence"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
	"github.com/getsyntegrity/ego/testkit"
)

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
//
// Spec files should call dispatchG2 instead. This variant takes a plain
// *testing.T for the test files that are not on go-specs yet, and goes away
// with them.
func dispatchWithMetadata(t *testing.T, engine *Engine, entityID string, payload proto.Message, opts ...command.MetadataOption) command.Result {
	t.Helper()
	md, err := command.NewMetadata(command.OperationID(uuid.NewString()), opts...)
	if err != nil {
		t.Fatalf("building command metadata: %v", err)
	}
	env, err := command.NewEnvelope(payload, md)
	if err != nil {
		t.Fatalf("building command envelope: %v", err)
	}
	result, err := engine.Dispatch(context.Background(), entityID, env, time.Minute)
	if err != nil {
		t.Fatalf("dispatching command: %v", err)
	}
	return result
}

// ES-success: expected revision matches the aggregate's current revision.
func TestEventSourcedExpectedRevisionSuccessMatchesCurrent(t *testing.T) {
	specs.Describe(t, "an ExpectedRevision that matches the aggregate's current revision", func(s *specs.Spec) {
		s.It("commits each command and advances the revision", func(ctx *specs.Context) {
			store := connectedEventsStoreG2(ctx)
			engine := startEngineG2(ctx, "ES-success", store, WithLogger(DiscardLogger))

			entityID := uuid.NewString()
			ctx.Expect(engine.Entity(context.Background(), NewEventSourcedEntity(entityID))).To(specs.BeNil())

			result := dispatchG2(ctx, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
			expectSuccessG2(ctx, result)
			specs.ExpectT(ctx, result.Revision()).ToEqual(1)

			result = dispatchG2(ctx, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(1))
			expectSuccessG2(ctx, result)
			specs.ExpectT(ctx, result.Revision()).ToEqual(2)
			specs.ExpectT(ctx, accountOfG2(ctx, result).GetAccountBalance()).ToEqual(750)
		})
	})
}

// ES-stale: expected revision is behind the aggregate's current revision.
// Mandatorily verified end-to-end through command.Result (not just at the
// actor/message level): OutcomeRejected, Failure.Code() ==
// CodeConcurrencyConflict, and errors.As recovers the underlying
// *persistence.ConflictError with the store's real actual/expected revisions.
func TestEventSourcedExpectedRevisionStaleIsConcurrencyConflict(t *testing.T) {
	specs.Describe(t, "an ExpectedRevision that is behind the aggregate's current revision", func(s *specs.Spec) {
		s.It("is rejected as a concurrency conflict carrying the real revisions", func(ctx *specs.Context) {
			store := connectedEventsStoreG2(ctx)
			engine := startEngineG2(ctx, "ES-stale", store, WithLogger(DiscardLogger))

			entityID := uuid.NewString()
			ctx.Expect(engine.Entity(context.Background(), NewEventSourcedEntity(entityID))).To(specs.BeNil())

			created := dispatchG2(ctx, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
			expectSuccessG2(ctx, created)
			specs.ExpectT(ctx, created.Revision()).ToEqual(1)

			result := dispatchG2(ctx, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(99))

			expectConcurrencyConflictG2(ctx, result)
			conflict := conflictErrorG2(ctx, result)
			ctx.Expect(result.Err()).To(specs.MatchError(command.ErrRejected))
			actual, ok := conflict.ActualRevision()
			ctx.Expect(ok).To(specs.BeTrue())
			specs.ExpectT(ctx, actual).ToEqual(1)
			ctx.Expect(conflict.Expected()).ToEqual(persistence.ExpectRevision(99))
		})
	})
}

// ES-genesis-success: expected revision 0 (genesis) on a brand-new aggregate.
func TestEventSourcedExpectedRevisionGenesisSucceedsOnNewAggregate(t *testing.T) {
	specs.Describe(t, "the genesis ExpectedRevision on a brand-new aggregate", func(s *specs.Spec) {
		s.It("commits the first event", func(ctx *specs.Context) {
			store := connectedEventsStoreG2(ctx)
			engine := startEngineG2(ctx, "ES-genesis-success", store, WithLogger(DiscardLogger))

			entityID := uuid.NewString()
			ctx.Expect(engine.Entity(context.Background(), NewEventSourcedEntity(entityID))).To(specs.BeNil())

			result := dispatchG2(ctx, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
			expectSuccessG2(ctx, result)
			specs.ExpectT(ctx, result.Revision()).ToEqual(1)
		})
	})
}

// ES-genesis-conflict: expected revision 0 (genesis) against an aggregate
// that already has committed events. Mandatorily verified end-to-end
// through command.Result, same contract as ES-stale.
func TestEventSourcedExpectedRevisionGenesisConflictsOnExistingAggregate(t *testing.T) {
	specs.Describe(t, "the genesis ExpectedRevision on an aggregate that already has events", func(s *specs.Spec) {
		s.It("is rejected as a concurrency conflict carrying the real revisions", func(ctx *specs.Context) {
			store := connectedEventsStoreG2(ctx)
			engine := startEngineG2(ctx, "ES-genesis-conflict", store, WithLogger(DiscardLogger))

			entityID := uuid.NewString()
			ctx.Expect(engine.Entity(context.Background(), NewEventSourcedEntity(entityID))).To(specs.BeNil())

			created := dispatchG2(ctx, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
			expectSuccessG2(ctx, created)
			specs.ExpectT(ctx, created.Revision()).ToEqual(1)

			result := dispatchG2(ctx, engine, entityID, &testpb.CreateAccount{AccountBalance: 999}, command.WithExpectedRevision(0))

			expectConcurrencyConflictG2(ctx, result)
			conflict := conflictErrorG2(ctx, result)
			actual, ok := conflict.ActualRevision()
			ctx.Expect(ok).To(specs.BeTrue())
			specs.ExpectT(ctx, actual).ToEqual(1)
			ctx.Expect(conflict.Expected()).ToEqual(persistence.ExpectGenesis())
		})
	})
}

// ES-legacy: a command sent through the legacy SendCommand entry point never
// declares an ExpectedRevision, so it must keep writing unconditionally
// exactly as before WRITE-004 (backward compatibility, design.md D4/D8).
func TestEventSourcedLegacyCommandIsUnconditional(t *testing.T) {
	specs.Describe(t, "a command sent through the legacy SendCommand entry point", func(s *specs.Spec) {
		s.It("keeps writing unconditionally", func(ctx *specs.Context) {
			bg := context.Background()
			store := connectedEventsStoreG2(ctx)
			engine := startEngineG2(ctx, "ES-legacy", store, WithLogger(DiscardLogger))

			entityID := uuid.NewString()
			ctx.Expect(engine.Entity(bg, NewEventSourcedEntity(entityID))).To(specs.BeNil())

			state, revision, err := engine.SendCommand(bg, entityID, &testpb.CreateAccount{AccountBalance: 500}, time.Minute)
			ctx.Expect(err).To(specs.BeNil())
			acct, ok := state.(*testpb.Account)
			ctx.Expect(ok).To(specs.BeTrue())
			specs.ExpectT(ctx, acct.GetAccountBalance()).ToEqual(500)
			specs.ExpectT(ctx, revision).ToEqual(1)

			state, revision, err = engine.SendCommand(bg, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, time.Minute)
			ctx.Expect(err).To(specs.BeNil())
			acct, ok = state.(*testpb.Account)
			ctx.Expect(ok).To(specs.BeTrue())
			specs.ExpectT(ctx, acct.GetAccountBalance()).ToEqual(750)
			specs.ExpectT(ctx, revision).ToEqual(2)
		})
	})
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

func (s *preconditionSpyEventsStore) WriteEvents(ctx context.Context, scope persistence.Scope, events []*egopb.Event, precondition persistence.WritePrecondition) error {
	s.mu.Lock()
	s.preconditions = append(s.preconditions, precondition)
	s.mu.Unlock()
	return s.EventStore.WriteEvents(ctx, scope, events, precondition)
}

// recorded returns a copy of the preconditions seen so far, in call order.
func (s *preconditionSpyEventsStore) recorded() []persistence.WritePrecondition {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]persistence.WritePrecondition(nil), s.preconditions...)
}

// ES-propagation: proves the D4 ExpectedRevision -> WritePrecondition mapping
// is exactly what reaches persistence.EventsStore.WriteEvents, for all three
// cases (absent, genesis, exact revision) - not just that the command
// eventually succeeds or fails.
func TestEventSourcedExpectedRevisionPropagatesToPersistencePrecondition(t *testing.T) {
	specs.Describe(t, "the ExpectedRevision of a dispatched command", func(s *specs.Spec) {
		s.It("reaches WriteEvents as the matching write precondition", func(ctx *specs.Context) {
			underlying := connectedEventsStoreG2(ctx)
			spy := &preconditionSpyEventsStore{EventStore: underlying}

			engine := startEngineG2(ctx, "ES-propagation", spy, WithLogger(DiscardLogger))

			entityID := uuid.NewString()
			ctx.Expect(engine.Entity(context.Background(), NewEventSourcedEntity(entityID))).To(specs.BeNil())

			result := dispatchG2(ctx, engine, entityID, &testpb.CreateAccount{AccountBalance: 500})
			expectSuccessG2(ctx, result)

			result = dispatchG2(ctx, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(1))
			expectSuccessG2(ctx, result)

			result = dispatchG2(ctx, engine, entityID, &testpb.CreateAccount{AccountBalance: 1}, command.WithExpectedRevision(0))
			ctx.Expect(result.Outcome()).ToEqual(command.OutcomeRejected)

			ctx.Expect(spy.recorded()).ToEqual([]persistence.WritePrecondition{
				persistence.Unconditional(),
				persistence.ExpectRevision(1),
				persistence.ExpectGenesis(),
			})
		})
	})
}

// ES-conflict-state: after a rejected conflicting write, the actor must
// remain functionally consistent - a subsequent command declaring the
// correct expected revision succeeds against the true, uncorrupted state.
// This is the case design.md D10 names as "provably in sync" (the failed
// write's declared actual revision equals what this actor already holds in
// memory), so the actor stays alive rather than being torn down.
func TestEventSourcedActorStaysConsistentAfterConflict(t *testing.T) {
	specs.Describe(t, "an event sourced actor after a rejected conflicting write", func(s *specs.Spec) {
		s.It("accepts the next command that declares the correct revision", func(ctx *specs.Context) {
			store := connectedEventsStoreG2(ctx)
			engine := startEngineG2(ctx, "ES-conflict-state", store, WithLogger(DiscardLogger))

			entityID := uuid.NewString()
			ctx.Expect(engine.Entity(context.Background(), NewEventSourcedEntity(entityID))).To(specs.BeNil())

			created := dispatchG2(ctx, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
			expectSuccessG2(ctx, created)
			specs.ExpectT(ctx, created.Revision()).ToEqual(1)

			conflicted := dispatchG2(ctx, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(99))
			ctx.Expect(conflicted.Outcome()).ToEqual(command.OutcomeRejected)

			result := dispatchG2(ctx, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(1))
			expectSuccessG2(ctx, result)
			specs.ExpectT(ctx, result.Revision()).ToEqual(2)
			specs.ExpectT(ctx, accountOfG2(ctx, result).GetAccountBalance()).ToEqual(750)
		})
	})
}

// TestEventSourcedBatchedExpectedRevisionSuccessAndConflict exercises D9's
// batched-path wiring (resolveBatchPrecondition, batchBase/batchHasPrecondition
// seeding, and handleBatchPersistResponse's D10 lifecycle) end-to-end through
// command.Result, using a threshold that flushes every single command's batch
// on its own so the assertions stay deterministic without needing concurrent
// callers to exercise the multi-command admission gate.
func TestEventSourcedBatchedExpectedRevisionSuccessAndConflict(t *testing.T) {
	specs.Describe(t, "an ExpectedRevision on a batched event sourced actor", func(s *specs.Spec) {
		s.It("commits the matching command and rejects the conflicting one", func(ctx *specs.Context) {
			store := connectedEventsStoreG2(ctx)
			engine := startEngineG2(ctx, "ES-batched", store, WithLogger(DiscardLogger))

			entityID := uuid.NewString()
			ctx.Expect(engine.Entity(context.Background(), NewEventSourcedEntity(entityID), WithBatchThreshold(1))).To(specs.BeNil())

			created := dispatchG2(ctx, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
			expectSuccessG2(ctx, created)
			specs.ExpectT(ctx, created.Revision()).ToEqual(1)

			conflicted := dispatchG2(ctx, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(99))
			expectConcurrencyConflictG2(ctx, conflicted)

			result := dispatchG2(ctx, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(1))
			expectSuccessG2(ctx, result)
			specs.ExpectT(ctx, result.Revision()).ToEqual(2)
		})
	})
}

// -----------------------------------------------------------------------
// Task 3.9: the handler's received arguments must contain no
// ExpectedRevision value. Mirrors PR4's
// TestDurableStateHandlerShapeUnchangedByExpectedRevision, but adapted to
// EventSourcedBehavior's actual signature: HandleCommand(ctx, cmd,
// priorState) has no priorVersion parameter at all (behavior.go), so
// there is no version-shaped slot ExpectedRevision could leak into. The
// meaningful, still-adapted proof is that priorState observed by the
// handler always reflects the actor's real, store-confirmed state and
// never anything derived from the command's declared ExpectedRevision --
// including when a wildly bogus ExpectedRevision (unrelated to the real
// revision) is declared, which later causes the write itself to be
// rejected downstream as a concurrency_conflict but must have no bearing
// on what the handler already saw, since extraction happens only after
// dispatchToBehavior returns (design.md D9 step 1, spec: "ExpectedRevision
// Extracted for the Persist Request Only").
// -----------------------------------------------------------------------

// revisionProbeEventSourcedBehavior records every HandleCommand
// invocation's observed priorState balance, so task 3.9 can assert what
// the handler actually received, independent of what ExpectedRevision the
// dispatched command's metadata carried.
type revisionProbeEventSourcedBehavior struct {
	id string

	mu            sync.Mutex
	priorBalances []float64
}

var _ EventSourcedBehavior = (*revisionProbeEventSourcedBehavior)(nil)

func newRevisionProbeEventSourcedBehavior(id string) *revisionProbeEventSourcedBehavior {
	return &revisionProbeEventSourcedBehavior{id: id}
}

func (x *revisionProbeEventSourcedBehavior) ID() string {
	return x.id
}

func (x *revisionProbeEventSourcedBehavior) InitialState() State {
	return new(testpb.Account)
}

func (x *revisionProbeEventSourcedBehavior) HandleCommand(_ context.Context, cmd Command, priorState State) ([]Event, error) {
	account, _ := priorState.(*testpb.Account)
	x.mu.Lock()
	x.priorBalances = append(x.priorBalances, account.GetAccountBalance())
	x.mu.Unlock()

	switch c := cmd.(type) {
	case *testpb.CreateAccount:
		return []Event{
			&testpb.AccountCreated{AccountId: x.id, AccountBalance: c.GetAccountBalance()},
		}, nil
	case *testpb.CreditAccount:
		return []Event{
			&testpb.AccountCredited{AccountId: c.GetAccountId(), AccountBalance: c.GetBalance()},
		}, nil
	default:
		return nil, errors.New("unhandled command")
	}
}

func (x *revisionProbeEventSourcedBehavior) HandleEvent(_ context.Context, event Event, priorState State) (State, error) {
	switch evt := event.(type) {
	case *testpb.AccountCreated:
		return &testpb.Account{AccountId: evt.GetAccountId(), AccountBalance: evt.GetAccountBalance()}, nil
	case *testpb.AccountCredited:
		account := priorState.(*testpb.Account)
		return &testpb.Account{
			AccountId:      account.GetAccountId(),
			AccountBalance: account.GetAccountBalance() + evt.GetAccountBalance(),
		}, nil
	default:
		return nil, errors.New("unhandled event")
	}
}

func (x *revisionProbeEventSourcedBehavior) MarshalBinary() ([]byte, error) {
	return json.Marshal(struct {
		ID string `json:"id"`
	}{ID: x.id})
}

func (x *revisionProbeEventSourcedBehavior) UnmarshalBinary(data []byte) error {
	aux := struct {
		ID string `json:"id"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	x.id = aux.ID
	return nil
}

// observedPriorBalances returns every priorState balance HandleCommand has
// been called with so far, in call order.
func (x *revisionProbeEventSourcedBehavior) observedPriorBalances() []float64 {
	x.mu.Lock()
	defer x.mu.Unlock()
	return append([]float64(nil), x.priorBalances...)
}

func TestEventSourcedHandlerArgumentsNeverCarryExpectedRevision(t *testing.T) {
	specs.Describe(t, "the arguments an event sourced handler receives", func(s *specs.Spec) {
		s.It("never carry the declared ExpectedRevision", func(ctx *specs.Context) {
			store := connectedEventsStoreG2(ctx)
			engine := startEngineG2(ctx, "ES-handler-shape", store, WithLogger(DiscardLogger))

			entityID := uuid.NewString()
			behavior := newRevisionProbeEventSourcedBehavior(entityID)
			ctx.Expect(engine.Entity(context.Background(), behavior)).To(specs.BeNil())

			created := dispatchG2(ctx, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
			expectSuccessG2(ctx, created)
			specs.ExpectT(ctx, created.Revision()).ToEqual(1)

			// A deliberately bogus ExpectedRevision, unrelated to the real revision
			// (1): if it ever leaked into priorState, the handler's observed
			// balance would read something other than the actor's real,
			// store-confirmed balance (500). The write itself is later rejected
			// downstream as a concurrency_conflict (proven by
			// TestEventSourcedExpectedRevisionStaleIsConcurrencyConflict), but that
			// must have no bearing on what the handler already saw.
			conflicted := dispatchG2(ctx, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(999999))
			ctx.Expect(conflicted.Outcome()).ToEqual(command.OutcomeRejected)

			// HandleCommand must still be invoked for the rejected command --
			// ExpectedRevision is extracted only after dispatchToBehavior returns.
			// The genesis call sees the initial zero-value state, not
			// ExpectedRevision=0 reinterpreted as anything else; the second call
			// sees the actor's real, store-confirmed balance (500), never anything
			// derived from the declared ExpectedRevision (999999).
			ctx.Expect(behavior.observedPriorBalances()).ToEqual([]float64{0, 500})
		})
	})
}
