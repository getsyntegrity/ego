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
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"

	"github.com/getsyntegrity/urd/command"
	testpb "github.com/getsyntegrity/urd/internal/testpb"
	"github.com/getsyntegrity/urd/persistence"
)

// TestDurableStateExpectedRevisionEndToEndPropagation is task 4.18's
// integration proof (spec: "End-to-End Propagation Proof (T7)"): a real
// DurableStateActor wired to a real Engine, dispatching through the real
// command/persistence stack against a real testkit.StateStore (not a
// call-recording mock, not a stub that ignores the precondition).
//
// Two commands, identical in every field except ExpectedRevision, are
// dispatched against the same persistence ID and the same store: the one
// matching the persisted revision commits and advances the real
// StorageRevision; the one that does not match is rejected with
// concurrency_conflict and leaves the persisted revision unchanged. This
// proves ExpectedRevision actually changes the real commit outcome, not
// merely that it is accepted and ignored.
func TestDurableStateExpectedRevisionEndToEndPropagation(t *testing.T) {
	specs.Describe(t, "a durable state actor wired to a real engine and store", func(s *specs.Spec) {
		s.It("commits the matching ExpectedRevision and rejects the other one, leaving the store untouched", func(ctx *specs.Context) {
			bg := context.Background()
			store := connectedDurableStore(ctx)
			engine := startEngine(ctx, "DS-integration-propagation", nil, WithLogger(DiscardLogger), WithStateStore(store))

			entityID := uuid.NewString()
			ctx.Expect(engine.DurableStateEntity(bg, NewAccountDurableStateBehavior(entityID))).To(specs.BeNil())

			created := dispatch(ctx, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
			expectSuccess(ctx, created)
			specs.ExpectT(ctx, created.Revision()).ToEqual(1)

			durable, err := store.GetLatestState(bg, persistence.Unscoped(), entityID)
			ctx.Expect(err).To(specs.BeNil())
			specs.ExpectT(ctx, durable.GetVersionNumber()).ToEqual(1)

			// The non-matching command: same payload/entity, ExpectedRevision does
			// not match the real persisted revision (1).
			nonMatching := dispatch(ctx, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(7))
			expectConcurrencyConflict(ctx, nonMatching)
			conflict := conflictError(ctx, nonMatching)
			ctx.Expect(conflict.Expected()).ToEqual(persistence.ExpectRevision(7))
			actual, ok := conflict.ActualRevision()
			ctx.Expect(ok).To(specs.BeTrue())
			specs.ExpectT(ctx, actual).ToEqual(1)

			// The store must be unchanged by the rejected write.
			durable, err = store.GetLatestState(bg, persistence.Unscoped(), entityID)
			ctx.Expect(err).To(specs.BeNil())
			specs.ExpectT(ctx, durable.GetVersionNumber()).ToEqual(1)

			// The matching command: identical payload, ExpectedRevision now matches
			// the real persisted revision (1). It commits and advances the real
			// StorageRevision to 2.
			matching := dispatch(ctx, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(1))
			expectSuccess(ctx, matching)
			specs.ExpectT(ctx, matching.Revision()).ToEqual(2)
			specs.ExpectT(ctx, accountOf(ctx, matching).GetAccountBalance()).ToEqual(750)

			durable, err = store.GetLatestState(bg, persistence.Unscoped(), entityID)
			ctx.Expect(err).To(specs.BeNil())
			specs.ExpectT(ctx, durable.GetVersionNumber()).ToEqual(2)
		})
	})
}
