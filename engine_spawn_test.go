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
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	testpb "github.com/getsyntegrity/ego/v4/test/data/testpb"
	"github.com/getsyntegrity/ego/v4/testkit"
)

// TestEngineSpawnMethodsDomainOnlySingleNode spawns behaviors that implement
// only the port/behavior contracts through the public SpawnEventSourced,
// SpawnDurableState and SpawnSaga methods on a single node (#123 criterion
// 2). Each answers a command round trip, and an envelope-capable behavior
// still receives HandleEnvelope.
func TestEngineSpawnMethodsDomainOnlySingleNode(t *testing.T) {
	ctx := context.Background()

	t.Run("SpawnEventSourced, envelope-capable", func(t *testing.T) {
		store := testkit.NewEventsStore()
		require.NoError(t, store.Connect(ctx))
		t.Cleanup(func() { _ = store.Disconnect(ctx) })
		engine := newTestEngine(t, "SpawnEventSourced", store, WithLogger(DiscardLogger))
		require.NoError(t, engine.Start(ctx))

		b := &domainOnlyEventSourced{id: uuid.NewString()}
		require.NoError(t, engine.SpawnEventSourced(ctx, b))

		state, revision, err := engine.SendCommand(ctx, b.ID(), &testpb.CreateAccount{AccountBalance: 7}, time.Minute)
		require.NoError(t, err)
		assert.EqualValues(t, 1, revision)
		assert.EqualValues(t, 7, state.(*testpb.Account).GetAccountBalance())
		assert.Equal(t, 1, b.envelopeHits(), "an envelope-capable behavior still receives HandleEnvelope")
	})

	t.Run("SpawnDurableState", func(t *testing.T) {
		stateStore := testkit.NewDurableStore()
		require.NoError(t, stateStore.Connect(ctx))
		t.Cleanup(func() { _ = stateStore.Disconnect(ctx) })
		engine := newTestEngine(t, "SpawnDurableState", nil, WithLogger(DiscardLogger), WithStateStore(stateStore))
		require.NoError(t, engine.Start(ctx))

		b := &domainOnlyDurableState{id: uuid.NewString()}
		require.NoError(t, engine.SpawnDurableState(ctx, b))

		state, revision, err := engine.SendCommand(ctx, b.ID(), &testpb.CreateAccount{AccountBalance: 9}, time.Minute)
		require.NoError(t, err)
		assert.EqualValues(t, 1, revision)
		assert.EqualValues(t, 9, state.(*testpb.Account).GetAccountBalance())
	})

	t.Run("SpawnSaga", func(t *testing.T) {
		store := testkit.NewEventsStore()
		require.NoError(t, store.Connect(ctx))
		t.Cleanup(func() { _ = store.Disconnect(ctx) })
		engine := newTestEngine(t, "SpawnSaga", store, WithLogger(DiscardLogger))
		require.NoError(t, engine.Start(ctx))

		b := &domainOnlySaga{id: "saga-" + uuid.NewString()}
		require.NoError(t, engine.SpawnSaga(ctx, b, 0))

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			info, err := engine.SagaStatus(ctx, b.ID(), time.Second)
			require.NoError(c, err)
			require.NotNil(c, info)
			assert.Equal(c, b.ID(), info.ID)
		}, 10*time.Second, 50*time.Millisecond)
	})

	t.Run("not started", func(t *testing.T) {
		engine := newTestEngine(t, "SpawnNotStarted", testkit.NewEventsStore(), WithLogger(DiscardLogger))
		assert.ErrorIs(t, engine.SpawnEventSourced(ctx, &domainOnlyEventSourced{id: "a"}), ErrEngineNotStarted)
		assert.ErrorIs(t, engine.SpawnDurableState(ctx, &domainOnlyDurableState{id: "b"}), ErrEngineNotStarted)
		assert.ErrorIs(t, engine.SpawnSaga(ctx, &domainOnlySaga{id: "c"}, 0), ErrEngineNotStarted)
	})
}
