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
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablogore/ego/v4/command"
	"github.com/pablogore/ego/v4/persistence"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
	"github.com/pablogore/ego/v4/testkit"
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
	ctx := context.Background()
	store := testkit.NewDurableStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "DS-integration-propagation", nil, WithLogger(DiscardLogger), WithStateStore(store))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	require.NoError(t, engine.DurableStateEntity(ctx, NewAccountDurableStateBehavior(entityID)))

	created := dispatchWithMetadata(t, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
	require.Equal(t, command.OutcomeSuccess, created.Outcome())
	require.EqualValues(t, 1, created.Revision())

	durable, err := store.GetLatestState(ctx, persistence.Unscoped(), entityID)
	require.NoError(t, err)
	require.NotNil(t, durable)
	require.EqualValues(t, 1, durable.GetVersionNumber())

	// The non-matching command: same payload/entity, ExpectedRevision does
	// not match the real persisted revision (1).
	nonMatching := dispatchWithMetadata(t, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(7))
	require.Equal(t, command.OutcomeRejected, nonMatching.Outcome())
	failure, ok := nonMatching.Failure()
	require.True(t, ok)
	code, hasCode := failure.Code()
	require.True(t, hasCode)
	assert.Equal(t, command.CodeConcurrencyConflict, code)

	var conflict *persistence.ConflictError
	require.True(t, errors.As(nonMatching.Err(), &conflict))
	assert.Equal(t, persistence.ExpectRevision(7), conflict.Expected())
	actual, ok := conflict.ActualRevision()
	require.True(t, ok)
	assert.EqualValues(t, 1, actual)

	// The store must be unchanged by the rejected write.
	durable, err = store.GetLatestState(ctx, persistence.Unscoped(), entityID)
	require.NoError(t, err)
	require.NotNil(t, durable)
	assert.EqualValues(t, 1, durable.GetVersionNumber())

	// The matching command: identical payload, ExpectedRevision now matches
	// the real persisted revision (1). It commits and advances the real
	// StorageRevision to 2.
	matching := dispatchWithMetadata(t, engine, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 250}, command.WithExpectedRevision(1))
	require.Equal(t, command.OutcomeSuccess, matching.Outcome())
	assert.EqualValues(t, 2, matching.Revision())

	state, ok := matching.State()
	require.True(t, ok)
	acct, ok := state.(*testpb.Account)
	require.True(t, ok)
	assert.EqualValues(t, 750, acct.GetAccountBalance())

	durable, err = store.GetLatestState(ctx, persistence.Unscoped(), entityID)
	require.NoError(t, err)
	require.NotNil(t, durable)
	assert.EqualValues(t, 2, durable.GetVersionNumber())
}
