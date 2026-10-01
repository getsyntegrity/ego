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
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"
	goakt "github.com/tochemey/goakt/v4/actor"

	"github.com/getsyntegrity/ego/egopb"
	samplepb "github.com/getsyntegrity/ego/example/examplepb"
	"github.com/getsyntegrity/ego/internal/engine/enginetest"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
)

// errSagaCompensationG4 is the failure a saga's compensation returns when the
// test only needs it to fail.
var errSagaCompensationG4 = errors.New("compensation failed")

// sagaStatusRigG4 is a started engine plus the case it belongs to, with the
// helpers the saga status specs share.
type sagaStatusRigG4 struct {
	ctx    *specs.Context
	engine *Engine
}

// newSagaStatusRigG4 starts an engine with an in-memory events store.
func newSagaStatusRigG4(ctx *specs.Context, name string) sagaStatusRigG4 {
	engine := newTestEngine(ctx.T, name, connectedEventsStore(ctx), WithLogger(DiscardLogger))
	ctx.Expect(engine.Start(context.Background())).To(specs.BeNil())
	return sagaStatusRigG4{ctx: ctx, engine: engine}
}

// spawnSagaReactingTo spawns a saga that answers action for the
// AccountCreated event of entityID and ignores every other event.
func (r sagaStatusRigG4) spawnSagaReactingTo(entityID string, action func() *SagaAction,
	compensate func(context.Context, State) ([]SagaCommand, error)) string {
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
	r.ctx.Expect(r.engine.SpawnSaga(context.Background(), saga, 0)).To(specs.BeNil())
	return sagaID
}

// createAccount spawns entityID and sends it the command that emits the
// AccountCreated event the sagas above react to.
func (r sagaStatusRigG4) createAccount(entityID string) {
	bg := context.Background()
	r.ctx.Expect(r.engine.Entity(bg, NewAccountEventSourcedBehavior(entityID))).To(specs.BeNil())
	_, _, err := r.engine.SendCommand(bg, entityID, &testpb.CreateAccount{AccountBalance: 500}, time.Minute)
	r.ctx.Expect(err).To(specs.BeNil())
}

// status reads the saga's reported lifecycle status. A failed query reads as an
// out-of-range status, so a poll keeps going until the saga answers.
func (r sagaStatusRigG4) status(sagaID string) any {
	info, err := r.engine.SagaStatus(context.Background(), sagaID, 5*time.Second)
	if err != nil || info == nil {
		return SagaStatus(-1)
	}
	return info.Status
}

// expectEventualStatus polls until the saga reports want, then checks that
// SagaInfo still carries the saga ID and state.
func (r sagaStatusRigG4) expectEventualStatus(sagaID string, want SagaStatus) {
	r.ctx.Eventually(func() any { return r.status(sagaID) }, specs.Equal(want),
		specs.WithTimeout(waitTimeout), specs.WithInterval(20*time.Millisecond))

	info, err := r.engine.SagaStatus(context.Background(), sagaID, 5*time.Second)
	r.ctx.Expect(err).To(specs.BeNil())
	r.ctx.Expect(info.ID).To(specs.Equal(sagaID))
	// SagaInfo.State must still carry the saga state
	r.ctx.Expect(info.State).To(specs.Not(specs.BeNil()))
}

// TestEngineSagaStatusReportsLifecycleStatus covers #153: Engine.SagaStatus
// must report the saga actor's real lifecycle status in SagaInfo.Status, not
// the SagaRunning zero value, while SagaInfo.State keeps carrying the saga
// state.
//
// Every saga below reacts only to the AccountCreated event of its own
// entity, which the test triggers with SendCommand; the status is then
// polled with ctx.Eventually under a bounded timeout, so the test waits
// for the saga to process the event instead of sleeping.
//
// SagaCompensating is not observable through Engine.SagaStatus: the actor
// runs the whole compensation (behavior.Compensate plus every compensation
// command) inside one message, so a status query queued behind it is only
// answered once the status has already moved on to SagaCompleted or
// SagaFailed. Its wire mapping is covered by
// TestEngineSagaStatusMapsWireStatus instead.
func TestEngineSagaStatusReportsLifecycleStatus(t *testing.T) {
	specs.Describe(t, "Engine.SagaStatus reports the saga actor's lifecycle status", func(s *specs.Spec) {
		s.It("a saga that has not finished reports SagaRunning", func(ctx *specs.Context) {
			rig := newSagaStatusRigG4(ctx, "SagaStatusRunning")
			entityID := uuid.NewString()
			// A non-terminal action: the saga reacts but neither completes nor
			// compensates, so it must keep reporting SagaRunning.
			var reacted atomic.Bool
			sagaID := rig.spawnSagaReactingTo(entityID, func() *SagaAction {
				reacted.Store(true)
				return &SagaAction{}
			}, nil)
			rig.createAccount(entityID)

			// The actor answers the status query only after the message that
			// handled the event, so the status read below already reflects it.
			ctx.Eventually(func() any { return reacted.Load() }, specs.BeTrue(), specs.WithTimeout(waitTimeout))

			info, err := rig.engine.SagaStatus(context.Background(), sagaID, 5*time.Second)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(info.Status).To(specs.Equal(SagaRunning))
			ctx.Expect(info.State).To(specs.Not(specs.BeNil()))
		})

		s.It("a saga whose action completes reports SagaCompleted", func(ctx *specs.Context) {
			rig := newSagaStatusRigG4(ctx, "SagaStatusCompleted")
			entityID := uuid.NewString()
			sagaID := rig.spawnSagaReactingTo(entityID, func() *SagaAction { return &SagaAction{Complete: true} }, nil)
			rig.createAccount(entityID)

			rig.expectEventualStatus(sagaID, SagaCompleted)
		})

		s.It("a saga whose compensation fails reports SagaFailed", func(ctx *specs.Context) {
			rig := newSagaStatusRigG4(ctx, "SagaStatusFailed")
			entityID := uuid.NewString()
			sagaID := rig.spawnSagaReactingTo(entityID,
				func() *SagaAction { return &SagaAction{Compensate: true} },
				func(context.Context, State) ([]SagaCommand, error) { return nil, errSagaCompensationG4 })
			rig.createAccount(entityID)

			rig.expectEventualStatus(sagaID, SagaFailed)
		})

		s.It("a saga whose compensation succeeds reports SagaCompleted", func(ctx *specs.Context) {
			rig := newSagaStatusRigG4(ctx, "SagaStatusCompensated")
			entityID := uuid.NewString()
			sagaID := rig.spawnSagaReactingTo(entityID,
				func() *SagaAction { return &SagaAction{Compensate: true} },
				func(context.Context, State) ([]SagaCommand, error) { return nil, nil })
			rig.createAccount(entityID)

			rig.expectEventualStatus(sagaID, SagaCompleted)
		})
	})
}

// wireStatusCaseG4 maps one lifecycle status of the wire reply to the status
// Engine.SagaStatus reports.
type wireStatusCaseG4 struct {
	wire egopb.SagaLifecycleStatus
	want SagaStatus
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
	specs.Describe(t, "Engine.SagaStatus maps the wire lifecycle status of a StateReply", func(s *specs.Spec) {
		specs.Table(s, []wireStatusCaseG4{
			{egopb.SagaLifecycleStatus_SAGA_LIFECYCLE_STATUS_NONE, SagaRunning},
			{egopb.SagaLifecycleStatus_SAGA_LIFECYCLE_STATUS_RUNNING, SagaRunning},
			{egopb.SagaLifecycleStatus_SAGA_LIFECYCLE_STATUS_COMPLETED, SagaCompleted},
			{egopb.SagaLifecycleStatus_SAGA_LIFECYCLE_STATUS_COMPENSATING, SagaCompensating},
			{egopb.SagaLifecycleStatus_SAGA_LIFECYCLE_STATUS_FAILED, SagaFailed},
		}, func(c wireStatusCaseG4) string { return c.wire.String() }, func(ctx *specs.Context, c wireStatusCaseG4) {
			bg := context.Background()
			rig := newSagaStatusRigG4(ctx, "SagaStatusWire")

			sagaID := "saga-wire-" + uuid.NewString()
			reply := &egopb.CommandReply{
				Reply: &egopb.CommandReply_StateReply{
					StateReply: &egopb.StateReply{
						PersistenceId: sagaID,
						State:         enginetest.MustAny(ctx.T, &samplepb.Account{AccountId: sagaID}),
						SagaStatus:    c.wire,
					},
				},
			}
			_, err := rig.engine.ActorSystem().Spawn(bg, sagaID, &enginetest.SimpleReplyActor{Reply: reply}, goakt.WithLongLived())
			ctx.Expect(err).To(specs.BeNil())

			info, err := rig.engine.SagaStatus(bg, sagaID, 5*time.Second)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(info.Status).To(specs.Equal(c.want))
			ctx.Expect(info.ID).To(specs.Equal(sagaID))
			// SagaInfo.State must still carry the replied state
			account, ok := info.State.(*samplepb.Account)
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(account.GetAccountId()).To(specs.Equal(sagaID))
		})
	})
}
