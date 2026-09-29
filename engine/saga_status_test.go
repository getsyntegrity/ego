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
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"

	"github.com/getsyntegrity/ego/egopb"
	samplepb "github.com/getsyntegrity/ego/example/examplepb"
	"github.com/getsyntegrity/ego/internal/engine/enginetest"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
	"github.com/getsyntegrity/ego/testkit"
)

// TestEngineSagaStatusReportsLifecycleStatus covers #153: Engine.SagaStatus
// must report the saga actor's real lifecycle status in SagaInfo.Status, not
// the SagaRunning zero value, while SagaInfo.State keeps carrying the saga
// state.
//
// Every saga below reacts only to the AccountCreated event of its own
// entity, which the test triggers with SendCommand; the status is then
// polled with require.Eventually under a bounded timeout, so the test waits
// for the saga to process the event instead of sleeping.
//
// SagaCompensating is not observable through Engine.SagaStatus: the actor
// runs the whole compensation (behavior.Compensate plus every compensation
// command) inside one message, so a status query queued behind it is only
// answered once the status has already moved on to SagaCompleted or
// SagaFailed. Its wire mapping is covered by
// TestEngineSagaStatusMapsWireStatus instead.
func TestEngineSagaStatusReportsLifecycleStatus(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "SagaStatusLifecycle", store, WithLogger(DiscardLogger))
	require.NoError(t, engine.Start(ctx))
	t.Cleanup(func() { _ = engine.Stop(ctx) })

	// spawnSagaReactingTo spawns a saga that answers action for the
	// AccountCreated event of entityID and ignores every other event.
	spawnSagaReactingTo := func(t *testing.T, entityID string, action func() *SagaAction, compensate func(context.Context, State) ([]SagaCommand, error)) string {
		t.Helper()
		sagaID := "saga-" + uuid.NewString()
		saga := &enginetest.CallbackSagaBehavior{
			SagaID: sagaID,
			HandleEventFn: func(_ context.Context, event Event, _ State) (*SagaAction, error) {
				created, ok := event.(*testpb.AccountCreated)
				if !ok || created.GetAccountId() != entityID {
					return &SagaAction{}, nil
				}
				return action(), nil
			},
			CompensateFn: compensate,
		}
		require.NoError(t, engine.SpawnSaga(ctx, saga, 0))
		return sagaID
	}

	// createAccount spawns entityID and sends it the command that emits the
	// AccountCreated event the sagas above react to.
	createAccount := func(t *testing.T, entityID string) {
		t.Helper()
		require.NoError(t, engine.Entity(ctx, NewAccountEventSourcedBehavior(entityID)))
		_, _, err := engine.SendCommand(ctx, entityID, &testpb.CreateAccount{AccountBalance: 500}, time.Minute)
		require.NoError(t, err)
	}

	requireEventualStatus := func(t *testing.T, sagaID string, want SagaStatus) {
		t.Helper()
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			info, err := engine.SagaStatus(ctx, sagaID, 5*time.Second)
			if !assert.NoError(c, err) {
				return
			}
			assert.Equal(c, want, info.Status, "SagaInfo.Status")
			assert.Equal(c, sagaID, info.ID)
			assert.NotNil(c, info.State, "SagaInfo.State must still carry the saga state")
		}, 10*time.Second, 20*time.Millisecond, "saga %s never reported status %s", sagaID, want)
	}

	t.Run("a saga that has not finished reports SagaRunning", func(t *testing.T) {
		entityID := uuid.NewString()
		// A non-terminal action: the saga reacts but neither completes nor
		// compensates, so it must keep reporting SagaRunning.
		reacted := make(chan struct{})
		sagaID := spawnSagaReactingTo(t, entityID, func() *SagaAction {
			close(reacted)
			return &SagaAction{}
		}, nil)
		createAccount(t, entityID)

		// The actor answers the status query only after the message that
		// handled the event, so the status read below already reflects it.
		select {
		case <-reacted:
		case <-time.After(10 * time.Second):
			require.FailNow(t, "the saga never reacted to its triggering event")
		}

		info, err := engine.SagaStatus(ctx, sagaID, 5*time.Second)
		require.NoError(t, err)
		assert.Equal(t, SagaRunning, info.Status)
		assert.NotNil(t, info.State)
	})

	t.Run("a saga whose action completes reports SagaCompleted", func(t *testing.T) {
		entityID := uuid.NewString()
		sagaID := spawnSagaReactingTo(t, entityID, func() *SagaAction { return &SagaAction{Complete: true} }, nil)
		createAccount(t, entityID)

		requireEventualStatus(t, sagaID, SagaCompleted)
	})

	t.Run("a saga whose compensation fails reports SagaFailed", func(t *testing.T) {
		entityID := uuid.NewString()
		sagaID := spawnSagaReactingTo(t, entityID,
			func() *SagaAction { return &SagaAction{Compensate: true} },
			func(context.Context, State) ([]SagaCommand, error) { return nil, assert.AnError })
		createAccount(t, entityID)

		requireEventualStatus(t, sagaID, SagaFailed)
	})

	t.Run("a saga whose compensation succeeds reports SagaCompleted", func(t *testing.T) {
		entityID := uuid.NewString()
		sagaID := spawnSagaReactingTo(t, entityID,
			func() *SagaAction { return &SagaAction{Compensate: true} },
			func(context.Context, State) ([]SagaCommand, error) { return nil, nil })
		createAccount(t, entityID)

		requireEventualStatus(t, sagaID, SagaCompleted)
	})
}

// TestEngineSagaStatusMapsWireStatus covers the wire half of #153: whatever
// lifecycle status a saga actor reports in its StateReply, Engine.SagaStatus
// surfaces it in SagaInfo.Status. It drives Engine.SagaStatus against a stub
// actor answering a fixed StateReply, which is how SagaCompensating (never
// observable from a real saga actor, see
// TestEngineSagaStatusReportsLifecycleStatus) is covered. A reply without the
// field (SAGA_LIFECYCLE_STATUS_NONE, as a saga node built before it existed
// sends) keeps reading as SagaRunning.
func TestEngineSagaStatusMapsWireStatus(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "SagaStatusWire", store, WithLogger(DiscardLogger))
	require.NoError(t, engine.Start(ctx))
	t.Cleanup(func() { _ = engine.Stop(ctx) })

	cases := []struct {
		wire egopb.SagaLifecycleStatus
		want SagaStatus
	}{
		{egopb.SagaLifecycleStatus_SAGA_LIFECYCLE_STATUS_NONE, SagaRunning},
		{egopb.SagaLifecycleStatus_SAGA_LIFECYCLE_STATUS_RUNNING, SagaRunning},
		{egopb.SagaLifecycleStatus_SAGA_LIFECYCLE_STATUS_COMPLETED, SagaCompleted},
		{egopb.SagaLifecycleStatus_SAGA_LIFECYCLE_STATUS_COMPENSATING, SagaCompensating},
		{egopb.SagaLifecycleStatus_SAGA_LIFECYCLE_STATUS_FAILED, SagaFailed},
	}
	for _, tc := range cases {
		t.Run(tc.wire.String(), func(t *testing.T) {
			sagaID := "saga-wire-" + uuid.NewString()
			reply := &egopb.CommandReply{
				Reply: &egopb.CommandReply_StateReply{
					StateReply: &egopb.StateReply{
						PersistenceId: sagaID,
						State:         enginetest.MustAny(t, &samplepb.Account{AccountId: sagaID}),
						SagaStatus:    tc.wire,
					},
				},
			}
			_, err := engine.ActorSystem().Spawn(ctx, sagaID, &enginetest.SimpleReplyActor{Reply: reply}, goakt.WithLongLived())
			require.NoError(t, err)

			info, err := engine.SagaStatus(ctx, sagaID, 5*time.Second)
			require.NoError(t, err)
			assert.Equal(t, tc.want, info.Status)
			assert.Equal(t, sagaID, info.ID)
			account, ok := info.State.(*samplepb.Account)
			require.True(t, ok, "SagaInfo.State must still carry the replied state")
			assert.Equal(t, sagaID, account.GetAccountId())
		})
	}
}
