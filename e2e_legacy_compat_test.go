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
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablogore/ego/v4/command"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
	"github.com/pablogore/ego/v4/testkit"
)

// This file is task 5.5's e2e proof for AC8 ("Legacy Caller Compatibility"):
// a legacy command carrying no ExpectedRevision metadata behaves
// unconditionally end-to-end through BOTH EventSourcedActor+real
// testkit.EventsStore AND DurableStateActor+real testkit.StateStore -- never
// producing concurrency_conflict, regardless of concurrent dispatch -- and
// ExpectedRevision present as 0 maps to ExpectGenesis() for both actor
// types. TestLegacyCompatEventSourcedAndDurableStateNeverConflict is one
// test artifact composing both actor types deliberately: no existing test
// demonstrates this cross-actor-type guarantee together (the closest
// existing coverage, TestEventSourcedLegacyCommandIsUnconditional and the
// DurableState genesis tests in durable_state_actor_expected_revision_test.go,
// each exercise only one actor type in isolation).
func TestLegacyCompatEventSourcedAndDurableStateNeverConflict(t *testing.T) {
	t.Run("EventSourcedActor: legacy commands never conflict under concurrent dispatch", func(t *testing.T) {
		ctx := context.Background()
		store := testkit.NewEventsStore()
		require.NoError(t, store.Connect(ctx))
		t.Cleanup(func() { _ = store.Disconnect(ctx) })

		engine := newTestEngine(t, "e2e-legacy-es", store, WithLogger(DiscardLogger))
		require.NoError(t, engine.Start(ctx))

		entityID := uuid.NewString()
		require.NoError(t, engine.Entity(ctx, NewEventSourcedEntity(entityID)))

		// The legacy entry point: SendCommand never carries ExpectedRevision,
		// so every write it issues must resolve to Unconditional() (D4/D8).
		state, revision, err := engine.SendCommand(ctx, entityID, &testpb.CreateAccount{AccountBalance: 100}, time.Minute)
		require.NoError(t, err)
		acct, ok := state.(*testpb.Account)
		require.True(t, ok)
		assert.EqualValues(t, 100, acct.GetAccountBalance())
		assert.EqualValues(t, 1, revision)

		// Multiple legacy commands dispatched concurrently against the same
		// entity: Unconditional() never declares an expectation that can go
		// stale, so none of these may ever surface concurrency_conflict,
		// regardless of the concurrent dispatch racing through the mailbox.
		const n = 10
		var wg sync.WaitGroup
		results := make([]command.Result, n)
		wg.Add(n)
		for i := 0; i < n; i++ {
			go func(i int) {
				defer wg.Done()
				// dispatchWithMetadata with zero MetadataOptions declares no
				// ExpectedRevision -- the same absence contract as the legacy
				// SendCommand path exercises above.
				results[i] = dispatchWithMetadata(t, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 1})
			}(i)
		}
		wg.Wait()

		for i, result := range results {
			require.Equalf(t, command.OutcomeSuccess, result.Outcome(), "legacy command %d must never be rejected", i)
			if failure, hasFailure := result.Failure(); hasFailure {
				if code, hasCode := failure.Code(); hasCode {
					assert.NotEqual(t, command.CodeConcurrencyConflict, code)
				}
			}
		}
	})

	t.Run("DurableStateActor: legacy commands never conflict under concurrent dispatch", func(t *testing.T) {
		ctx := context.Background()
		store := testkit.NewDurableStore()
		require.NoError(t, store.Connect(ctx))
		t.Cleanup(func() { _ = store.Disconnect(ctx) })

		engine := newTestEngine(t, "e2e-legacy-ds", nil, WithLogger(DiscardLogger), WithStateStore(store))
		require.NoError(t, engine.Start(ctx))

		entityID := uuid.NewString()
		require.NoError(t, engine.DurableStateEntity(ctx, NewAccountDurableStateBehavior(entityID)))

		// dispatchWithMetadata with zero MetadataOptions declares no
		// ExpectedRevision, matching the legacy caller contract for
		// DurableStateActor as well.
		created := dispatchWithMetadata(t, engine, entityID, &testpb.CreateAccount{AccountBalance: 100})
		require.Equal(t, command.OutcomeSuccess, created.Outcome())
		require.EqualValues(t, 1, created.Revision())

		const n = 10
		var wg sync.WaitGroup
		results := make([]command.Result, n)
		wg.Add(n)
		for i := 0; i < n; i++ {
			go func(i int) {
				defer wg.Done()
				results[i] = dispatchWithMetadata(t, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 1})
			}(i)
		}
		wg.Wait()

		for i, result := range results {
			require.Equalf(t, command.OutcomeSuccess, result.Outcome(), "legacy command %d must never be rejected", i)
			if failure, hasFailure := result.Failure(); hasFailure {
				if code, hasCode := failure.Code(); hasCode {
					assert.NotEqual(t, command.CodeConcurrencyConflict, code)
				}
			}
		}
	})

	t.Run("EventSourcedActor: ExpectedRevision=0 maps to ExpectGenesis()", func(t *testing.T) {
		ctx := context.Background()
		store := testkit.NewEventsStore()
		require.NoError(t, store.Connect(ctx))
		t.Cleanup(func() { _ = store.Disconnect(ctx) })

		engine := newTestEngine(t, "e2e-legacy-es-genesis", store, WithLogger(DiscardLogger))
		require.NoError(t, engine.Start(ctx))

		entityID := uuid.NewString()
		require.NoError(t, engine.Entity(ctx, NewEventSourcedEntity(entityID)))

		result := dispatchWithMetadata(t, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
		require.Equal(t, command.OutcomeSuccess, result.Outcome())
		assert.EqualValues(t, 1, result.Revision())

		// A second genesis declaration against the now-existing aggregate must
		// be rejected as a conflict, proving 0 was read as ExpectGenesis(),
		// never as Unconditional() nor silently ignored.
		conflict := dispatchWithMetadata(t, engine, entityID, &testpb.CreateAccount{AccountBalance: 999}, command.WithExpectedRevision(0))
		require.Equal(t, command.OutcomeRejected, conflict.Outcome())
		failure, ok := conflict.Failure()
		require.True(t, ok)
		code, hasCode := failure.Code()
		require.True(t, hasCode)
		assert.Equal(t, command.CodeConcurrencyConflict, code)
	})

	t.Run("DurableStateActor: ExpectedRevision=0 maps to ExpectGenesis()", func(t *testing.T) {
		ctx := context.Background()
		store := testkit.NewDurableStore()
		require.NoError(t, store.Connect(ctx))
		t.Cleanup(func() { _ = store.Disconnect(ctx) })

		engine := newTestEngine(t, "e2e-legacy-ds-genesis", nil, WithLogger(DiscardLogger), WithStateStore(store))
		require.NoError(t, engine.Start(ctx))

		entityID := uuid.NewString()
		require.NoError(t, engine.DurableStateEntity(ctx, NewAccountDurableStateBehavior(entityID)))

		result := dispatchWithMetadata(t, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
		require.Equal(t, command.OutcomeSuccess, result.Outcome())
		assert.EqualValues(t, 1, result.Revision())

		conflict := dispatchWithMetadata(t, engine, entityID, &testpb.CreateAccount{AccountBalance: 999}, command.WithExpectedRevision(0))
		require.Equal(t, command.OutcomeRejected, conflict.Outcome())
		failure, ok := conflict.Failure()
		require.True(t, ok)
		code, hasCode := failure.Code()
		require.True(t, hasCode)
		assert.Equal(t, command.CodeConcurrencyConflict, code)
	})
}
