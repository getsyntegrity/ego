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
	"sync"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"

	"github.com/getsyntegrity/ego/command"
	"github.com/getsyntegrity/ego/persistence"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
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
//     EventsStore yield exactly one commit and one conflict, so the store's
//     own CAS -- not caller/mailbox ordering -- is shown to be what decides
//     the winner.

// TestEventSourcedIntegrationExactRevisionCommitsAndAdvancesStore proves an
// exact-match ExpectedRevision commits end-to-end and the real store's
// persisted revision advances accordingly.
func TestEventSourcedIntegrationExactRevisionCommitsAndAdvancesStore(t *testing.T) {
	specs.Describe(t, "an exact-match ExpectedRevision on a real event sourced actor", func(s *specs.Spec) {
		s.It("commits and advances the store's persisted revision", func(ctx *specs.Context) {
			bg := context.Background()
			store := connectedEventsStore(ctx)
			engine := startEngine(ctx, "ES-integration-exact", store, WithLogger(DiscardLogger))

			entityID := uuid.NewString()
			ctx.Expect(engine.Entity(bg, NewEventSourcedEntity(entityID))).To(specs.BeNil())

			created := dispatch(ctx, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
			expectSuccess(ctx, created)
			specs.ExpectT(ctx, created.Revision()).ToEqual(1)

			latest, err := store.GetLatestEvent(bg, persistence.Unscoped(), entityID)
			ctx.Expect(err).To(specs.BeNil())
			specs.ExpectT(ctx, latest.GetSequenceNumber()).ToEqual(1)

			// A second exact-match command: ExpectedRevision matches the real
			// persisted revision (1), so it commits and advances the store to 2.
			result := dispatch(ctx, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(1))
			expectSuccess(ctx, result)
			specs.ExpectT(ctx, result.Revision()).ToEqual(2)

			latest, err = store.GetLatestEvent(bg, persistence.Unscoped(), entityID)
			ctx.Expect(err).To(specs.BeNil())
			specs.ExpectT(ctx, latest.GetSequenceNumber()).ToEqual(2)

			specs.ExpectT(ctx, accountOf(ctx, result).GetAccountBalance()).ToEqual(750)
		})
	})
}

// TestEventSourcedIntegrationStaleRevisionRejectedStoreUnchanged proves a
// stale ExpectedRevision is rejected at the real store -- not merely
// observed by the actor's own bookkeeping -- and that the store's
// persisted revision is left unchanged by the rejected write.
func TestEventSourcedIntegrationStaleRevisionRejectedStoreUnchanged(t *testing.T) {
	specs.Describe(t, "a stale ExpectedRevision on a real event sourced actor", func(s *specs.Spec) {
		s.It("is rejected at the store and leaves the persisted revision unchanged", func(ctx *specs.Context) {
			bg := context.Background()
			store := connectedEventsStore(ctx)
			engine := startEngine(ctx, "ES-integration-stale", store, WithLogger(DiscardLogger))

			entityID := uuid.NewString()
			ctx.Expect(engine.Entity(bg, NewEventSourcedEntity(entityID))).To(specs.BeNil())

			created := dispatch(ctx, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
			expectSuccess(ctx, created)
			specs.ExpectT(ctx, created.Revision()).ToEqual(1)

			latest, err := store.GetLatestEvent(bg, persistence.Unscoped(), entityID)
			ctx.Expect(err).To(specs.BeNil())
			specs.ExpectT(ctx, latest.GetSequenceNumber()).ToEqual(1)

			// A stale command: ExpectedRevision does not match the real persisted
			// revision (1).
			result := dispatch(ctx, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(7))
			expectConcurrencyConflict(ctx, result)

			// The store must be unchanged by the rejected write.
			latest, err = store.GetLatestEvent(bg, persistence.Unscoped(), entityID)
			ctx.Expect(err).To(specs.BeNil())
			specs.ExpectT(ctx, latest.GetSequenceNumber()).ToEqual(1)
		})
	})
}

// TestEventSourcedIntegrationConcurrentGenesisYieldsExactlyOneCommit is the
// architectural-gate proof: two independent EventSourcedActor instances
// (separate Engine/goakt actor systems, mirroring
// TestDurableStateConcurrentGenesisWritersYieldExactlyOneCommit's pattern),
// both declaring ExpectedRevision=0 against the same persistence ID with no
// prior record, race concurrently against one shared testkit.EventsStore.
// Exactly one commits, exactly one is rejected with concurrency_conflict.
// The store's own CompareAndSwap/LoadOrStore path -- not an external lock,
// not mailbox/caller ordering -- is what serializes the two writers,
// satisfying the spec's "not merely observed in isolation" requirement.
func TestEventSourcedIntegrationConcurrentGenesisYieldsExactlyOneCommit(t *testing.T) {
	specs.Describe(t, "two event sourced actors racing the genesis revision on one store", func(s *specs.Spec) {
		s.It("commits exactly one and rejects the other with a concurrency conflict", func(ctx *specs.Context) {
			bg := context.Background()
			store := connectedEventsStore(ctx)
			entityID := uuid.NewString()

			engineA := startEngine(ctx, "ES-race-a", store, WithLogger(DiscardLogger))
			ctx.Expect(engineA.Entity(bg, NewEventSourcedEntity(entityID))).To(specs.BeNil())

			engineB := startEngine(ctx, "ES-race-b", store, WithLogger(DiscardLogger))
			ctx.Expect(engineB.Entity(bg, NewEventSourcedEntity(entityID))).To(specs.BeNil())

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

			latest, err := store.GetLatestEvent(bg, persistence.Unscoped(), entityID)
			ctx.Expect(err).To(specs.BeNil())
			specs.ExpectT(ctx, latest.GetSequenceNumber()).ToEqual(1)
		})
	})
}
