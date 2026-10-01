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

package saga

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/mock"
	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/extension"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/eventstream"
	samplepb "github.com/getsyntegrity/ego/example/examplepb"
	"github.com/getsyntegrity/ego/internal/engine/enginetest"
	"github.com/getsyntegrity/ego/internal/engine/eventsource"
	"github.com/getsyntegrity/ego/internal/engine/protocol"
	"github.com/getsyntegrity/ego/internal/extensions"
	"github.com/getsyntegrity/ego/internal/goaktlog"
	"github.com/getsyntegrity/ego/persistence"
	runtimeport "github.com/getsyntegrity/ego/port/runtime"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
	"github.com/getsyntegrity/ego/testkit"
)

// sagaStoreMock stands in for persistence.EventsStore in the cases that drive
// PreStart and persistence failures. It forwards only the methods those cases
// reach to a go-specs controller; any other method panics on the nil embedded
// interface, so an unexpected call is loud.
type sagaStoreMock struct {
	persistence.EventsStore
	c *mock.Controller
}

func (m sagaStoreMock) Ping(ctx context.Context) error {
	return m.c.Method("Ping").Call(ctx).Err(0)
}

func (m sagaStoreMock) GetLatestEvent(ctx context.Context, scope persistence.Scope, id string) (*egopb.Event, error) {
	r := m.c.Method("GetLatestEvent").Call(ctx, scope, id)
	return mock.Value[*egopb.Event](r, 0), r.Err(1)
}

func (m sagaStoreMock) ReplayEvents(ctx context.Context, scope persistence.Scope, id string, from, to, limit uint64) ([]*egopb.Event, error) {
	r := m.c.Method("ReplayEvents").Call(ctx, scope, id, from, to, limit)
	return mock.Value[[]*egopb.Event](r, 0), r.Err(1)
}

func (m sagaStoreMock) WriteEvents(ctx context.Context, scope persistence.Scope, events []*egopb.Event, precondition persistence.WritePrecondition) error {
	return m.c.Method("WriteEvents").Call(ctx, scope, events, precondition).Err(0)
}

func TestSagaStatus_String(t *testing.T) {
	specs.Describe(t, "SagaStatus.String names each lifecycle status", func(s *specs.Spec) {
		type statusCase struct {
			status   runtimeport.SagaStatus
			expected string
		}
		specs.Table(s, []statusCase{
			{runtimeport.SagaRunning, "running"},
			{runtimeport.SagaCompleted, "completed"},
			{runtimeport.SagaCompensating, "compensating"},
			{runtimeport.SagaFailed, "failed"},
			{runtimeport.SagaStatus(99), "unknown"},
		}, func(c statusCase) string { return c.expected }, func(ctx *specs.Context, c statusCase) {
			ctx.Expect(c.status.String()).ToEqual(c.expected)
		})
	})
}

var errSagaBoom = errors.New("saga test: boom")

const (
	// signalTimeout bounds how long a case waits for something that must happen.
	signalTimeout = 3 * time.Second
	// pollEvery is the interval of every poll in this file.
	pollEvery = 10 * time.Millisecond
	// quietWindow is how long a case watches for something that must not happen.
	quietWindow = 500 * time.Millisecond
)

// sagaRig is a started in-process goakt actor system wired with an events
// store and an events stream. The system and the stream stop when the case
// ends, so a failed assertion no longer leaks a running system.
type sagaRig struct {
	system goakt.ActorSystem
	stream eventstream.Stream
}

// newSagaRig starts the actor system. extra adds more extensions, for example
// the tenancy marker.
func newSagaRig(ctx *specs.Context, store persistence.EventsStore, extra ...extension.Extension) *sagaRig {
	stream := eventstream.New()
	exts := append([]extension.Extension{extensions.NewEventsStore(store), extensions.NewEventsStream(stream)}, extra...)
	system, err := goakt.NewActorSystem("TestSystem",
		goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
		goakt.WithExtensions(exts...),
		goakt.WithActorInitMaxRetries(1))
	ctx.Expect(err).To(specs.BeNil())
	ctx.Expect(system.Start(context.Background())).To(specs.BeNil())
	ctx.Cleanup(func() {
		stream.Close()
		ctx.Expect(system.Stop(context.Background())).To(specs.BeNil())
	})
	return &sagaRig{system: system, stream: stream}
}

// newTestkitStore returns a connected in-memory events store that disconnects
// when the case ends.
func newTestkitStore(ctx *specs.Context) *testkit.EventStore {
	store := testkit.NewEventsStore()
	ctx.Expect(store.Connect(context.Background())).To(specs.BeNil())
	ctx.Cleanup(func() { _ = store.Disconnect(context.Background()) })
	return store
}

// spawnSaga spawns a saga actor and expects the spawn to succeed.
func (r *sagaRig) spawnSaga(ctx *specs.Context, id string, behavior *enginetest.CallbackSagaBehavior, cfg *extensions.SagaConfig, extra ...extension.Dependency) *goakt.PID {
	deps := append([]extension.Dependency{behavior, cfg}, extra...)
	pid, err := r.system.Spawn(context.Background(), id, New(), goakt.WithLongLived(), goakt.WithDependencies(deps...))
	ctx.Expect(err).To(specs.BeNil())
	ctx.Expect(pid).To(specs.Not(specs.BeNil()))
	return pid
}

// spawnReplyTarget spawns an actor that answers every message with reply.
func (r *sagaRig) spawnReplyTarget(ctx *specs.Context, id string, reply proto.Message) {
	_, err := r.system.Spawn(context.Background(), id, &enginetest.SimpleReplyActor{Reply: reply}, goakt.WithLongLived())
	ctx.Expect(err).To(specs.BeNil())
}

// expectSpawnFails expects PreStart to fail, so no PID comes back.
func expectSpawnFails(ctx *specs.Context, pid *goakt.PID, err error) {
	ctx.Expect(err).To(specs.Not(specs.BeNil()))
	ctx.Expect(pid).To(specs.BeNil())
}

// foreignEvent is an event written by some other entity, which a saga reacts to.
func foreignEvent(ctx *specs.Context) *egopb.Event {
	return newAnyEvent(ctx, uuid.NewString(), 1, &testpb.AccountCreated{AccountId: uuid.NewString()}, nil)
}

// mustAny wraps msg in an Any, reporting a failure through the spec.
func mustAny(ctx *specs.Context, msg proto.Message) *anypb.Any {
	a, err := anypb.New(msg)
	ctx.Expect(err).To(specs.BeNil())
	return a
}

// stateReply is the successful answer a target entity gives to a saga command.
func stateReply(ctx *specs.Context, targetID string) *egopb.CommandReply {
	return &egopb.CommandReply{Reply: &egopb.CommandReply_StateReply{StateReply: &egopb.StateReply{
		PersistenceId:  targetID,
		SequenceNumber: 1,
		State:          mustAny(ctx, &samplepb.Account{}),
	}}}
}

// rejectReply is the error answer a target entity gives to a saga command.
func rejectReply() *egopb.CommandReply {
	return &egopb.CommandReply{Reply: &egopb.CommandReply_ErrorReply{ErrorReply: &egopb.ErrorReply{Message: "entity rejected command"}}}
}

// bump counts one call. The saga runs behavior callbacks on its own goroutine,
// so every counter is atomic.
func bump(n *atomic.Int32) { n.Add(1) }

// awaitCalls waits until n reaches at least want.
func awaitCalls(ctx *specs.Context, n *atomic.Int32, want int32, timeout time.Duration) {
	ctx.Eventually(func() any { return n.Load() }, specs.BeGreaterThanOrEqual(want),
		specs.WithTimeout(timeout), specs.WithInterval(pollEvery))
}

// expectCallsStay checks that n keeps its value for the whole window.
func expectCallsStay(ctx *specs.Context, n *atomic.Int32, want int32, window time.Duration) {
	ctx.Consistently(func() any { return n.Load() }, specs.Equal(want),
		specs.WithTimeout(window), specs.WithInterval(2*pollEvery))
}

// expectStaysRunning checks that the actor survives the whole window.
func expectStaysRunning(ctx *specs.Context, pid *goakt.PID, window time.Duration) {
	ctx.Consistently(func() any { return pid.IsRunning() }, specs.BeTrue(),
		specs.WithTimeout(window), specs.WithInterval(2*pollEvery))
}

func TestSagaActor(t *testing.T) {
	specs.Describe(t, "Actor reacts to saga lifecycle, events and commands on a real in-process actor system", func(s *specs.Spec) {
		s.It("PreStart: missing behavior fails to start", func(ctx *specs.Context) {
			sagaID := uuid.NewString()

			ctrl := mock.NewController(ctx)
			// A saga with no behavior fails before it touches the store.
			ctrl.Method("Ping").Expect(mock.Any()).Never()
			ctrl.Method("GetLatestEvent").Expect(mock.Any(), persistence.Unscoped(), sagaID).Never()
			rig := newSagaRig(ctx, sagaStoreMock{c: ctrl})

			// Spawn with no behavior dependency
			pid, err := rig.system.Spawn(context.Background(), sagaID, New(), goakt.WithLongLived())

			expectSpawnFails(ctx, pid, err)
		})

		// Each row scripts the store and the behavior so that PreStart fails at
		// one step, and the spawn must come back with an error and no PID.
		type preStartFailure struct {
			name  string
			setup func(ctx *specs.Context, ctrl *mock.Controller, sagaID string) *enginetest.CallbackSagaBehavior
		}
		specs.Table(s, []preStartFailure{
			{"PreStart: events store ping failure", func(_ *specs.Context, ctrl *mock.Controller, sagaID string) *enginetest.CallbackSagaBehavior {
				ctrl.Method("Ping").Expect(mock.Any()).Return(errSagaBoom)
				return &enginetest.CallbackSagaBehavior{SagaID: sagaID}
			}},
			{"PreStart: GetLatestEvent failure", func(_ *specs.Context, ctrl *mock.Controller, sagaID string) *enginetest.CallbackSagaBehavior {
				ctrl.Method("Ping").Expect(mock.Any()).Return(nil)
				ctrl.Method("GetLatestEvent").Expect(mock.Any(), persistence.Unscoped(), sagaID).Return(nil, errSagaBoom)
				return &enginetest.CallbackSagaBehavior{SagaID: sagaID}
			}},
			{"PreStart: ReplayEvents failure", func(_ *specs.Context, ctrl *mock.Controller, sagaID string) *enginetest.CallbackSagaBehavior {
				latest := &egopb.Event{PersistenceId: sagaID, SequenceNumber: 3}
				ctrl.Method("Ping").Expect(mock.Any()).Return(nil)
				ctrl.Method("GetLatestEvent").Expect(mock.Any(), persistence.Unscoped(), sagaID).Return(latest, nil)
				ctrl.Method("ReplayEvents").Expect(mock.Any(), persistence.Unscoped(), sagaID, uint64(1), uint64(3), uint64(3)).Return(nil, errSagaBoom)
				return &enginetest.CallbackSagaBehavior{SagaID: sagaID}
			}},
			{"PreStart: UnmarshalNew failure during recovery", func(_ *specs.Context, ctrl *mock.Controller, sagaID string) *enginetest.CallbackSagaBehavior {
				// An event with an unresolvable type URL
				bad := &egopb.Event{
					PersistenceId:  sagaID,
					SequenceNumber: 1,
					Event:          &anypb.Any{TypeUrl: "type.googleapis.com/nonexistent.Type", Value: []byte("bad")},
				}
				latest := &egopb.Event{PersistenceId: sagaID, SequenceNumber: 1}
				ctrl.Method("Ping").Expect(mock.Any()).Return(nil)
				ctrl.Method("GetLatestEvent").Expect(mock.Any(), persistence.Unscoped(), sagaID).Return(latest, nil)
				ctrl.Method("ReplayEvents").Expect(mock.Any(), persistence.Unscoped(), sagaID, uint64(1), uint64(1), uint64(1)).Return([]*egopb.Event{bad}, nil)
				return &enginetest.CallbackSagaBehavior{SagaID: sagaID}
			}},
			{"PreStart: ApplyEvent failure during recovery", func(ctx *specs.Context, ctrl *mock.Controller, sagaID string) *enginetest.CallbackSagaBehavior {
				replayed := &egopb.Event{
					PersistenceId:  sagaID,
					SequenceNumber: 1,
					Event:          mustAny(ctx, &testpb.AccountCreated{AccountId: sagaID, AccountBalance: 100}),
				}
				latest := &egopb.Event{PersistenceId: sagaID, SequenceNumber: 1}
				ctrl.Method("Ping").Expect(mock.Any()).Return(nil)
				ctrl.Method("GetLatestEvent").Expect(mock.Any(), persistence.Unscoped(), sagaID).Return(latest, nil)
				ctrl.Method("ReplayEvents").Expect(mock.Any(), persistence.Unscoped(), sagaID, uint64(1), uint64(1), uint64(1)).Return([]*egopb.Event{replayed}, nil)
				return &enginetest.CallbackSagaBehavior{
					SagaID: sagaID,
					ApplyEventFn: func(_ context.Context, _ Event, _ State) (State, error) {
						return nil, errSagaBoom
					},
				}
			}},
		}, func(c preStartFailure) string { return c.name }, func(ctx *specs.Context, c preStartFailure) {
			sagaID := uuid.NewString()
			ctrl := mock.NewController(ctx)
			behavior := c.setup(ctx, ctrl, sagaID)
			rig := newSagaRig(ctx, sagaStoreMock{c: ctrl})

			pid, err := rig.system.Spawn(context.Background(), sagaID, New(),
				goakt.WithLongLived(),
				goakt.WithDependencies(behavior, extensions.NewSagaConfig(0)))

			expectSpawnFails(ctx, pid, err)
		})

		s.It("PreStart: happy path recovery with prior events", func(ctx *specs.Context) {
			sagaID := uuid.NewString()
			replayed := &egopb.Event{
				PersistenceId:  sagaID,
				SequenceNumber: 2,
				Event:          mustAny(ctx, &testpb.AccountCreated{AccountId: sagaID, AccountBalance: 100}),
			}
			latest := &egopb.Event{PersistenceId: sagaID, SequenceNumber: 2}
			ctrl := mock.NewController(ctx)
			ctrl.Method("Ping").Expect(mock.Any()).Return(nil)
			ctrl.Method("GetLatestEvent").Expect(mock.Any(), persistence.Unscoped(), sagaID).Return(latest, nil)
			ctrl.Method("ReplayEvents").Expect(mock.Any(), persistence.Unscoped(), sagaID, uint64(1), uint64(2), uint64(2)).Return([]*egopb.Event{replayed}, nil)
			rig := newSagaRig(ctx, sagaStoreMock{c: ctrl})

			var applied atomic.Int32
			behavior := &enginetest.CallbackSagaBehavior{
				SagaID: sagaID,
				ApplyEventFn: func(_ context.Context, _ Event, state State) (State, error) {
					bump(&applied)
					return state, nil
				},
			}
			rig.spawnSaga(ctx, sagaID, behavior, extensions.NewSagaConfig(0))

			// ApplyEvent must run while PreStart replays the stored event.
			awaitCalls(ctx, &applied, 1, 2*time.Second)
		})
	})

	t.Run("Receive: GetStateCommand returns current state", func(t *testing.T) {
		ctx := context.TODO()
		sagaID := uuid.NewString()

		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		defer eventStore.Disconnect(ctx) //nolint:errcheck

		stream := eventstream.New()
		defer stream.Close()

		actorSystem, err := goakt.NewActorSystem("TestSystem",
			goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(stream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		behavior := &enginetest.CallbackSagaBehavior{SagaID: sagaID}
		sagaCfg := extensions.NewSagaConfig(0)

		pid, err := actorSystem.Spawn(ctx, sagaID, New(),
			goakt.WithLongLived(),
			goakt.WithDependencies(behavior, sagaCfg))
		require.NoError(t, err)
		require.NotNil(t, pid)

		reply, err := goakt.Ask(ctx, pid, new(egopb.GetStateCommand), 3*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		commandReply, ok := reply.(*egopb.CommandReply)
		require.True(t, ok)
		stateReply := commandReply.GetReply().(*egopb.CommandReply_StateReply)
		require.NotNil(t, stateReply)
		assert.EqualValues(t, sagaID, stateReply.StateReply.GetPersistenceId())
		assert.EqualValues(t, 0, stateReply.StateReply.GetSequenceNumber())

		stream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("Receive: PostStart with timeout triggers compensation", func(t *testing.T) {
		ctx := context.TODO()
		sagaID := uuid.NewString()

		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		defer eventStore.Disconnect(ctx) //nolint:errcheck

		stream := eventstream.New()
		defer stream.Close()

		actorSystem, err := goakt.NewActorSystem("TestSystem",
			goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(stream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		compensated := make(chan struct{}, 1)
		behavior := &enginetest.CallbackSagaBehavior{
			SagaID: sagaID,
			CompensateFn: func(_ context.Context, _ State) ([]sagaCommand, error) {
				select {
				case compensated <- struct{}{}:
				default:
				}
				return nil, nil
			},
		}
		// 200ms timeout so the test runs quickly
		sagaCfg := extensions.NewSagaConfig(200 * time.Millisecond)

		pid, err := actorSystem.Spawn(ctx, sagaID, New(),
			goakt.WithLongLived(),
			goakt.WithDependencies(behavior, sagaCfg))
		require.NoError(t, err)
		require.NotNil(t, pid)

		select {
		case <-compensated:
		case <-time.After(3 * time.Second):
			t.Fatal("compensation was not triggered after timeout")
		}

		stream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("Receive: sagaTimeoutMsg when not running is no-op", func(t *testing.T) {
		ctx := context.TODO()
		sagaID := uuid.NewString()

		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		defer eventStore.Disconnect(ctx) //nolint:errcheck

		stream := eventstream.New()
		defer stream.Close()

		actorSystem, err := goakt.NewActorSystem("TestSystem",
			goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(stream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		compensateCalled := make(chan struct{}, 1)
		behavior := &enginetest.CallbackSagaBehavior{
			SagaID: sagaID,
			CompensateFn: func(_ context.Context, _ State) ([]sagaCommand, error) {
				select {
				case compensateCalled <- struct{}{}:
				default:
				}
				return nil, nil
			},
		}
		sagaCfg := extensions.NewSagaConfig(0)

		pid, err := actorSystem.Spawn(ctx, sagaID, New(),
			goakt.WithLongLived(),
			goakt.WithDependencies(behavior, sagaCfg))
		require.NoError(t, err)
		require.NotNil(t, pid)

		// Manually send sagaTimeoutMsg twice: first triggers compensation (status → Compensating),
		// second should be a no-op because status is no longer runtimeport.SagaRunning.
		require.NoError(t, goakt.Tell(ctx, pid, &sagaTimeoutMsg{}))

		// Drain first compensation signal
		select {
		case <-compensateCalled:
		case <-time.After(2 * time.Second):
			t.Fatal("first compensation not triggered")
		}

		// Second sagaTimeoutMsg – must not call Compensate again
		require.NoError(t, goakt.Tell(ctx, pid, &sagaTimeoutMsg{}))

		require.Never(t, func() bool {
			select {
			case <-compensateCalled:
				return true
			default:
				return false
			}
		}, 500*time.Millisecond, 20*time.Millisecond, "Compensate was called again but saga is no longer running")

		stream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("Receive: unknown message is unhandled", func(t *testing.T) {
		ctx := context.TODO()
		sagaID := uuid.NewString()

		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		defer eventStore.Disconnect(ctx) //nolint:errcheck

		stream := eventstream.New()
		defer stream.Close()

		actorSystem, err := goakt.NewActorSystem("TestSystem",
			goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(stream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		behavior := &enginetest.CallbackSagaBehavior{SagaID: sagaID}
		sagaCfg := extensions.NewSagaConfig(0)

		pid, err := actorSystem.Spawn(ctx, sagaID, New(),
			goakt.WithLongLived(),
			goakt.WithDependencies(behavior, sagaCfg))
		require.NoError(t, err)
		require.NotNil(t, pid)

		// Send an unknown message type – the actor should call ctx.Unhandled() without panicking.
		require.NoError(t, goakt.Tell(ctx, pid, new(emptypb.Empty)))

		// The actor is still alive: poll for the duration instead of a blind
		// sleep so a delayed crash is still caught.
		require.Never(t, func() bool { return !pid.IsRunning() }, 500*time.Millisecond, 20*time.Millisecond)

		stream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("consumeEvents: skips non-egopb-Event payloads", func(t *testing.T) {
		ctx := context.TODO()
		sagaID := uuid.NewString()

		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		defer eventStore.Disconnect(ctx) //nolint:errcheck

		stream := eventstream.New()
		defer stream.Close()

		actorSystem, err := goakt.NewActorSystem("TestSystem",
			goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(stream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		handleEventCalled := make(chan struct{}, 1)
		behavior := &enginetest.CallbackSagaBehavior{
			SagaID: sagaID,
			HandleEventFn: func(_ context.Context, _ Event, _ State) (*sagaAction, error) {
				handleEventCalled <- struct{}{}
				return &sagaAction{}, nil
			},
		}
		sagaCfg := extensions.NewSagaConfig(0)

		pid, err := actorSystem.Spawn(ctx, sagaID, New(),
			goakt.WithLongLived(),
			goakt.WithDependencies(behavior, sagaCfg))
		require.NoError(t, err)
		require.NotNil(t, pid)

		// Publish a non-*egopb.Event payload
		stream.Publish(protocol.EventsTopic, new(emptypb.Empty))

		require.Never(t, func() bool {
			select {
			case <-handleEventCalled:
				return true
			default:
				return false
			}
		}, 500*time.Millisecond, 20*time.Millisecond, "HandleEvent should not be called for non-Event payload")

		stream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("consumeEvents: skips own saga events", func(t *testing.T) {
		ctx := context.TODO()
		sagaID := uuid.NewString()

		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		defer eventStore.Disconnect(ctx) //nolint:errcheck

		stream := eventstream.New()
		defer stream.Close()

		actorSystem, err := goakt.NewActorSystem("TestSystem",
			goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(stream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		handleEventCalled := make(chan struct{}, 1)
		behavior := &enginetest.CallbackSagaBehavior{
			SagaID: sagaID,
			HandleEventFn: func(_ context.Context, _ Event, _ State) (*sagaAction, error) {
				handleEventCalled <- struct{}{}
				return &sagaAction{}, nil
			},
		}
		sagaCfg := extensions.NewSagaConfig(0)

		pid, err := actorSystem.Spawn(ctx, sagaID, New(),
			goakt.WithLongLived(),
			goakt.WithDependencies(behavior, sagaCfg))
		require.NoError(t, err)
		require.NotNil(t, pid)

		eventAny, _ := anypb.New(&testpb.AccountCreated{AccountId: sagaID})
		ownEvent := &egopb.Event{
			PersistenceId:  sagaID, // same as saga ID → should be skipped
			SequenceNumber: 1,
			Event:          eventAny,
		}
		topic := protocol.EventsTopic
		stream.Publish(topic, ownEvent)

		require.Never(t, func() bool {
			select {
			case <-handleEventCalled:
				return true
			default:
				return false
			}
		}, 500*time.Millisecond, 20*time.Millisecond, "HandleEvent should not be called for own saga events")

		stream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("consumeEvents: skips events when saga is not running", func(t *testing.T) {
		ctx := context.TODO()
		sagaID := uuid.NewString()

		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		defer eventStore.Disconnect(ctx) //nolint:errcheck

		stream := eventstream.New()
		defer stream.Close()

		actorSystem, err := goakt.NewActorSystem("TestSystem",
			goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(stream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		// the saga processes events on its own goroutine, so the counter must
		// be atomic for the test goroutine to read it safely
		var callCount atomic.Int32
		behavior := &enginetest.CallbackSagaBehavior{
			SagaID: sagaID,
			HandleEventFn: func(_ context.Context, _ Event, _ State) (*sagaAction, error) {
				callCount.Add(1)
				return &sagaAction{Complete: true}, nil
			},
		}
		sagaCfg := extensions.NewSagaConfig(0)

		pid, err := actorSystem.Spawn(ctx, sagaID, New(),
			goakt.WithLongLived(),
			goakt.WithDependencies(behavior, sagaCfg))
		require.NoError(t, err)
		require.NotNil(t, pid)

		eventAny, _ := anypb.New(&testpb.AccountCreated{AccountId: uuid.NewString()})
		domainEvent := &egopb.Event{
			PersistenceId:  uuid.NewString(),
			SequenceNumber: 1,
			Event:          eventAny,
		}
		topic := protocol.EventsTopic

		// First event: triggers Complete → saga status becomes runtimeport.SagaCompleted
		stream.Publish(topic, domainEvent)
		require.Eventually(t, func() bool { return callCount.Load() >= 1 }, 2*time.Second, 10*time.Millisecond,
			"HandleEvent was not called for the first event")

		countAfterFirst := callCount.Load()

		// Subsequent events should be ignored
		stream.Publish(topic, domainEvent)
		stream.Publish(topic, domainEvent)

		assert.Never(t, func() bool { return callCount.Load() != countAfterFirst }, 500*time.Millisecond, 20*time.Millisecond,
			"HandleEvent should not be called after saga completes")

		stream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("consumeEvents: UnmarshalNew error is logged and skipped", func(t *testing.T) {
		ctx := context.TODO()
		sagaID := uuid.NewString()

		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		defer eventStore.Disconnect(ctx) //nolint:errcheck

		stream := eventstream.New()
		defer stream.Close()

		actorSystem, err := goakt.NewActorSystem("TestSystem",
			goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(stream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		secondEventHandled := make(chan struct{}, 1)
		behavior := &enginetest.CallbackSagaBehavior{
			SagaID: sagaID,
			HandleEventFn: func(_ context.Context, _ Event, _ State) (*sagaAction, error) {
				select {
				case secondEventHandled <- struct{}{}:
				default:
				}
				return &sagaAction{}, nil
			},
		}
		sagaCfg := extensions.NewSagaConfig(0)

		pid, err := actorSystem.Spawn(ctx, sagaID, New(),
			goakt.WithLongLived(),
			goakt.WithDependencies(behavior, sagaCfg))
		require.NoError(t, err)
		require.NotNil(t, pid)

		topic := protocol.EventsTopic

		// First event: bad type URL → UnmarshalNew fails → logged and skipped
		badEvent := &egopb.Event{
			PersistenceId:  uuid.NewString(),
			SequenceNumber: 1,
			Event:          &anypb.Any{TypeUrl: "type.googleapis.com/nonexistent.Type", Value: []byte("bad")},
		}
		stream.Publish(topic, badEvent)

		// Second event: valid → HandleEvent is called, proving the saga continues
		goodEventAny, _ := anypb.New(&testpb.AccountCreated{AccountId: uuid.NewString()})
		goodEvent := &egopb.Event{
			PersistenceId:  uuid.NewString(),
			SequenceNumber: 1,
			Event:          goodEventAny,
		}
		stream.Publish(topic, goodEvent)

		select {
		case <-secondEventHandled:
		case <-time.After(2 * time.Second):
			t.Fatal("saga did not continue processing after UnmarshalNew error")
		}

		stream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("consumeEvents: HandleEvent error is logged and skipped", func(t *testing.T) {
		ctx := context.TODO()
		sagaID := uuid.NewString()

		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		defer eventStore.Disconnect(ctx) //nolint:errcheck

		stream := eventstream.New()
		defer stream.Close()

		actorSystem, err := goakt.NewActorSystem("TestSystem",
			goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(stream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		callCount := 0
		secondHandled := make(chan struct{}, 1)
		behavior := &enginetest.CallbackSagaBehavior{
			SagaID: sagaID,
			HandleEventFn: func(_ context.Context, _ Event, _ State) (*sagaAction, error) {
				callCount++
				if callCount == 1 {
					return nil, assert.AnError
				}
				select {
				case secondHandled <- struct{}{}:
				default:
				}
				return &sagaAction{}, nil
			},
		}
		sagaCfg := extensions.NewSagaConfig(0)

		pid, err := actorSystem.Spawn(ctx, sagaID, New(),
			goakt.WithLongLived(),
			goakt.WithDependencies(behavior, sagaCfg))
		require.NoError(t, err)
		require.NotNil(t, pid)

		topic := protocol.EventsTopic
		eventAny, _ := anypb.New(&testpb.AccountCreated{AccountId: uuid.NewString()})
		event := &egopb.Event{PersistenceId: uuid.NewString(), SequenceNumber: 1, Event: eventAny}

		stream.Publish(topic, event)
		stream.Publish(topic, event)

		select {
		case <-secondHandled:
		case <-time.After(2 * time.Second):
			t.Fatal("saga did not continue processing after HandleEvent error")
		}

		stream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("consumeEvents: HandleEvent returns nil action is no-op", func(t *testing.T) {
		ctx := context.TODO()
		sagaID := uuid.NewString()

		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		defer eventStore.Disconnect(ctx) //nolint:errcheck

		stream := eventstream.New()
		defer stream.Close()

		actorSystem, err := goakt.NewActorSystem("TestSystem",
			goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(stream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		handled := make(chan struct{}, 1)
		behavior := &enginetest.CallbackSagaBehavior{
			SagaID: sagaID,
			HandleEventFn: func(_ context.Context, _ Event, _ State) (*sagaAction, error) {
				handled <- struct{}{}
				return nil, nil // nil action
			},
		}
		sagaCfg := extensions.NewSagaConfig(0)

		pid, err := actorSystem.Spawn(ctx, sagaID, New(),
			goakt.WithLongLived(),
			goakt.WithDependencies(behavior, sagaCfg))
		require.NoError(t, err)
		require.NotNil(t, pid)

		topic := protocol.EventsTopic
		eventAny, _ := anypb.New(&testpb.AccountCreated{AccountId: uuid.NewString()})
		event := &egopb.Event{PersistenceId: uuid.NewString(), SequenceNumber: 1, Event: eventAny}
		stream.Publish(topic, event)

		select {
		case <-handled:
		case <-time.After(2 * time.Second):
			t.Fatal("HandleEvent was not called")
		}
		// Actor must still be alive
		require.Never(t, func() bool { return !pid.IsRunning() }, 300*time.Millisecond, 20*time.Millisecond)

		stream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("consumeEvents: HandleEvent Complete action marks saga completed", func(t *testing.T) {
		ctx := context.TODO()
		sagaID := uuid.NewString()

		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		defer eventStore.Disconnect(ctx) //nolint:errcheck

		stream := eventstream.New()
		defer stream.Close()

		actorSystem, err := goakt.NewActorSystem("TestSystem",
			goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(stream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		behavior := &enginetest.CallbackSagaBehavior{
			SagaID: sagaID,
			HandleEventFn: func(_ context.Context, _ Event, _ State) (*sagaAction, error) {
				return &sagaAction{Complete: true}, nil
			},
		}
		sagaCfg := extensions.NewSagaConfig(0)

		pid, err := actorSystem.Spawn(ctx, sagaID, New(),
			goakt.WithLongLived(),
			goakt.WithDependencies(behavior, sagaCfg))
		require.NoError(t, err)
		require.NotNil(t, pid)

		topic := protocol.EventsTopic
		eventAny, _ := anypb.New(&testpb.AccountCreated{AccountId: uuid.NewString()})
		event := &egopb.Event{PersistenceId: uuid.NewString(), SequenceNumber: 1, Event: eventAny}
		stream.Publish(topic, event)

		// Actor is still alive but status is Completed
		require.Never(t, func() bool { return !pid.IsRunning() }, 500*time.Millisecond, 20*time.Millisecond)

		stream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("consumeEvents: persistAndApplyEvents ApplyEvent error is logged", func(t *testing.T) {
		ctx := context.TODO()
		sagaID := uuid.NewString()

		ctrl := mock.NewController(t)
		eventStore := sagaStoreMock{c: ctrl}
		ctrl.Method("Ping").Expect(mock.Any()).Return(nil)
		ctrl.Method("GetLatestEvent").Expect(mock.Any(), persistence.Unscoped(), sagaID).Return(nil, nil)

		stream := eventstream.New()
		defer stream.Close()

		actorSystem, err := goakt.NewActorSystem("TestSystem",
			goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(stream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		handled := make(chan struct{}, 1)
		// the saga processes events on its own goroutine, so the counter must
		// be atomic for the test goroutine to read it safely
		var applyCallCount atomic.Int32
		behavior := &enginetest.CallbackSagaBehavior{
			SagaID: sagaID,
			HandleEventFn: func(_ context.Context, event Event, _ State) (*sagaAction, error) {
				handled <- struct{}{}
				return &sagaAction{Events: []Event{event}}, nil
			},
			ApplyEventFn: func(_ context.Context, _ Event, state State) (State, error) {
				applyCallCount.Add(1)
				return nil, assert.AnError
			},
		}
		sagaCfg := extensions.NewSagaConfig(0)

		pid, err := actorSystem.Spawn(ctx, sagaID, New(),
			goakt.WithLongLived(),
			goakt.WithDependencies(behavior, sagaCfg))
		require.NoError(t, err)
		require.NotNil(t, pid)

		topic := protocol.EventsTopic
		eventAny, _ := anypb.New(&testpb.AccountCreated{AccountId: uuid.NewString()})
		event := &egopb.Event{PersistenceId: uuid.NewString(), SequenceNumber: 1, Event: eventAny}
		stream.Publish(topic, event)

		select {
		case <-handled:
		case <-time.After(2 * time.Second):
			t.Fatal("HandleEvent was not called")
		}

		// ApplyEvent runs after HandleEvent returns, within the same actor
		// message turn, so wait for the actual condition instead of guessing
		// a settle time.
		require.Eventually(t, func() bool { return applyCallCount.Load() > 0 }, 2*time.Second, 10*time.Millisecond,
			"ApplyEvent was not called")

		// Actor must still be alive despite the error
		require.True(t, pid.IsRunning())

		stream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("consumeEvents: persistAndApplyEvents WriteEvents error is logged", func(t *testing.T) {
		ctx := context.TODO()
		sagaID := uuid.NewString()

		writeEventsCalled := make(chan struct{}, 1)
		ctrl := mock.NewController(t)
		eventStore := sagaStoreMock{c: ctrl}
		ctrl.Method("Ping").Expect(mock.Any()).Return(nil)
		ctrl.Method("GetLatestEvent").Expect(mock.Any(), persistence.Unscoped(), sagaID).Return(nil, nil)
		ctrl.Method("WriteEvents").Expect(mock.Any(), persistence.Unscoped(), mock.Any(), mock.Any()).
			Do(func([]any) []any {
				select {
				case writeEventsCalled <- struct{}{}:
				default:
				}
				return []any{assert.AnError}
			}).
			AnyTimes()

		stream := eventstream.New()
		defer stream.Close()

		actorSystem, err := goakt.NewActorSystem("TestSystem",
			goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(stream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		behavior := &enginetest.CallbackSagaBehavior{
			SagaID: sagaID,
			HandleEventFn: func(_ context.Context, event Event, _ State) (*sagaAction, error) {
				return &sagaAction{Events: []Event{event}}, nil
			},
		}
		sagaCfg := extensions.NewSagaConfig(0)

		pid, err := actorSystem.Spawn(ctx, sagaID, New(),
			goakt.WithLongLived(),
			goakt.WithDependencies(behavior, sagaCfg))
		require.NoError(t, err)
		require.NotNil(t, pid)

		topic := protocol.EventsTopic
		eventAny, _ := anypb.New(&testpb.AccountCreated{AccountId: uuid.NewString()})
		event := &egopb.Event{PersistenceId: uuid.NewString(), SequenceNumber: 1, Event: eventAny}
		stream.Publish(topic, event)

		select {
		case <-writeEventsCalled:
		case <-time.After(2 * time.Second):
			t.Fatal("WriteEvents was not called")
		}

		require.True(t, pid.IsRunning())

		stream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("consumeEvents: HandleEvent with events persists state", func(t *testing.T) {
		ctx := context.TODO()
		sagaID := uuid.NewString()

		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		defer eventStore.Disconnect(ctx) //nolint:errcheck

		stream := eventstream.New()
		defer stream.Close()

		actorSystem, err := goakt.NewActorSystem("TestSystem",
			goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(stream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		applied := make(chan struct{}, 1)
		behavior := &enginetest.CallbackSagaBehavior{
			SagaID:         sagaID,
			InitialStateFn: func() State { return &samplepb.Account{} },
			HandleEventFn: func(_ context.Context, event Event, _ State) (*sagaAction, error) {
				return &sagaAction{Events: []Event{event}}, nil
			},
			ApplyEventFn: func(_ context.Context, _ Event, _ State) (State, error) {
				select {
				case applied <- struct{}{}:
				default:
				}
				return &samplepb.Account{AccountBalance: 100}, nil
			},
		}
		sagaCfg := extensions.NewSagaConfig(0)

		pid, err := actorSystem.Spawn(ctx, sagaID, New(),
			goakt.WithLongLived(),
			goakt.WithDependencies(behavior, sagaCfg))
		require.NoError(t, err)
		require.NotNil(t, pid)

		topic := protocol.EventsTopic
		eventAny, _ := anypb.New(&testpb.AccountCreated{AccountId: uuid.NewString()})
		event := &egopb.Event{PersistenceId: uuid.NewString(), SequenceNumber: 1, Event: eventAny}
		stream.Publish(topic, event)

		select {
		case <-applied:
		case <-time.After(2 * time.Second):
			t.Fatal("ApplyEvent was not called")
		}

		// State should have been updated. No extra wait is needed here: Ask
		// is dispatched through the same actor mailbox that is still
		// finishing the event turn that sent the applied signal above, so it
		// is only serviced once that turn (including the state update) is
		// complete.
		reply, err := goakt.Ask(ctx, pid, new(egopb.GetStateCommand), 3*time.Second)
		require.NoError(t, err)
		commandReply := reply.(*egopb.CommandReply)
		stateReply := commandReply.GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, 1, stateReply.StateReply.GetSequenceNumber())

		stream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("compensate: behavior failure sets runtimeport.SagaFailed", func(t *testing.T) {
		ctx := context.TODO()
		sagaID := uuid.NewString()

		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		defer eventStore.Disconnect(ctx) //nolint:errcheck

		stream := eventstream.New()
		defer stream.Close()

		actorSystem, err := goakt.NewActorSystem("TestSystem",
			goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(stream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		behavior := &enginetest.CallbackSagaBehavior{
			SagaID: sagaID,
			HandleEventFn: func(_ context.Context, _ Event, _ State) (*sagaAction, error) {
				return &sagaAction{Compensate: true}, nil
			},
			CompensateFn: func(_ context.Context, _ State) ([]sagaCommand, error) {
				return nil, assert.AnError
			},
		}
		sagaCfg := extensions.NewSagaConfig(0)

		pid, err := actorSystem.Spawn(ctx, sagaID, New(),
			goakt.WithLongLived(),
			goakt.WithDependencies(behavior, sagaCfg))
		require.NoError(t, err)
		require.NotNil(t, pid)

		topic := protocol.EventsTopic
		eventAny, _ := anypb.New(&testpb.AccountCreated{AccountId: uuid.NewString()})
		event := &egopb.Event{PersistenceId: uuid.NewString(), SequenceNumber: 1, Event: eventAny}
		stream.Publish(topic, event)

		require.Never(t, func() bool { return !pid.IsRunning() }, 500*time.Millisecond, 20*time.Millisecond)

		stream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("compensate: command SendSync failure sets runtimeport.SagaFailed", func(t *testing.T) {
		ctx := context.TODO()
		sagaID := uuid.NewString()

		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		defer eventStore.Disconnect(ctx) //nolint:errcheck

		stream := eventstream.New()
		defer stream.Close()

		actorSystem, err := goakt.NewActorSystem("TestSystem",
			goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(stream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		behavior := &enginetest.CallbackSagaBehavior{
			SagaID: sagaID,
			HandleEventFn: func(_ context.Context, _ Event, _ State) (*sagaAction, error) {
				return &sagaAction{Compensate: true}, nil
			},
			CompensateFn: func(_ context.Context, _ State) ([]sagaCommand, error) {
				return []sagaCommand{
					{EntityID: "nonexistent-entity", Command: new(emptypb.Empty), Timeout: 500 * time.Millisecond},
				}, nil
			},
		}
		sagaCfg := extensions.NewSagaConfig(0)

		pid, err := actorSystem.Spawn(ctx, sagaID, New(),
			goakt.WithLongLived(),
			goakt.WithDependencies(behavior, sagaCfg))
		require.NoError(t, err)
		require.NotNil(t, pid)

		topic := protocol.EventsTopic
		eventAny, _ := anypb.New(&testpb.AccountCreated{AccountId: uuid.NewString()})
		event := &egopb.Event{PersistenceId: uuid.NewString(), SequenceNumber: 1, Event: eventAny}
		stream.Publish(topic, event)

		// The SendSync timeout is 500ms; wait through that plus margin,
		// polling for a crash instead of guessing a fixed settle time.
		require.Never(t, func() bool { return !pid.IsRunning() }, 2*time.Second, 20*time.Millisecond)

		stream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("compensate: successful compensation sets runtimeport.SagaCompleted", func(t *testing.T) {
		ctx := context.TODO()
		sagaID := uuid.NewString()
		targetID := uuid.NewString()

		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		defer eventStore.Disconnect(ctx) //nolint:errcheck

		stream := eventstream.New()
		defer stream.Close()

		actorSystem, err := goakt.NewActorSystem("TestSystem",
			goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(stream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		// Spawn a target actor that accepts compensation commands
		compensationReply := &egopb.CommandReply{
			Reply: &egopb.CommandReply_StateReply{
				StateReply: &egopb.StateReply{
					PersistenceId:  targetID,
					SequenceNumber: 1,
					State:          enginetest.MustAny(t, &samplepb.Account{}),
				},
			},
		}
		_, err = actorSystem.Spawn(ctx, targetID,
			&enginetest.SimpleReplyActor{Reply: compensationReply},
			goakt.WithLongLived())
		require.NoError(t, err)

		compensated := make(chan struct{}, 1)
		behavior := &enginetest.CallbackSagaBehavior{
			SagaID: sagaID,
			HandleEventFn: func(_ context.Context, _ Event, _ State) (*sagaAction, error) {
				return &sagaAction{Compensate: true}, nil
			},
			CompensateFn: func(_ context.Context, _ State) ([]sagaCommand, error) {
				return []sagaCommand{
					{EntityID: targetID, Command: new(emptypb.Empty), Timeout: 3 * time.Second},
				}, nil
			},
			ApplyEventFn: func(_ context.Context, _ Event, state State) (State, error) {
				select {
				case compensated <- struct{}{}:
				default:
				}
				return state, nil
			},
		}
		sagaCfg := extensions.NewSagaConfig(0)

		pid, err := actorSystem.Spawn(ctx, sagaID, New(),
			goakt.WithLongLived(),
			goakt.WithDependencies(behavior, sagaCfg))
		require.NoError(t, err)
		require.NotNil(t, pid)

		topic := protocol.EventsTopic
		eventAny, _ := anypb.New(&testpb.AccountCreated{AccountId: uuid.NewString()})
		event := &egopb.Event{PersistenceId: uuid.NewString(), SequenceNumber: 1, Event: eventAny}
		stream.Publish(topic, event)

		// compensate() (saga_actor.go) marks runtimeport.SagaCompleted directly on a
		// successful SendSync; it never calls ApplyEvent, so `compensated`
		// (wired for a different code path) cannot be awaited here. Poll
		// survival over the same window the original blind sleep used.
		require.Never(t, func() bool { return !pid.IsRunning() }, 2*time.Second, 20*time.Millisecond)

		stream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("sendCommand: SendSync error triggers HandleError with compensate", func(t *testing.T) {
		ctx := context.TODO()
		sagaID := uuid.NewString()

		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		defer eventStore.Disconnect(ctx) //nolint:errcheck

		stream := eventstream.New()
		defer stream.Close()

		actorSystem, err := goakt.NewActorSystem("TestSystem",
			goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(stream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		handleErrorCalled := make(chan struct{}, 1)
		behavior := &enginetest.CallbackSagaBehavior{
			SagaID: sagaID,
			HandleEventFn: func(_ context.Context, _ Event, _ State) (*sagaAction, error) {
				return &sagaAction{Commands: []sagaCommand{
					{EntityID: "nonexistent-entity", Command: new(emptypb.Empty), Timeout: 500 * time.Millisecond},
				}}, nil
			},
			HandleErrorFn: func(_ context.Context, _ string, _ error, _ State) (*sagaAction, error) {
				select {
				case handleErrorCalled <- struct{}{}:
				default:
				}
				return &sagaAction{Complete: true}, nil
			},
		}
		sagaCfg := extensions.NewSagaConfig(0)

		pid, err := actorSystem.Spawn(ctx, sagaID, New(),
			goakt.WithLongLived(),
			goakt.WithDependencies(behavior, sagaCfg))
		require.NoError(t, err)
		require.NotNil(t, pid)

		topic := protocol.EventsTopic
		eventAny, _ := anypb.New(&testpb.AccountCreated{AccountId: uuid.NewString()})
		event := &egopb.Event{PersistenceId: uuid.NewString(), SequenceNumber: 1, Event: eventAny}
		stream.Publish(topic, event)

		select {
		case <-handleErrorCalled:
		case <-time.After(3 * time.Second):
			t.Fatal("HandleError was not called after SendSync failure")
		}

		stream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("sendCommand: HandleError failure is logged", func(t *testing.T) {
		ctx := context.TODO()
		sagaID := uuid.NewString()

		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		defer eventStore.Disconnect(ctx) //nolint:errcheck

		stream := eventstream.New()
		defer stream.Close()

		actorSystem, err := goakt.NewActorSystem("TestSystem",
			goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(stream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		handleErrorCalled := make(chan struct{}, 1)
		behavior := &enginetest.CallbackSagaBehavior{
			SagaID: sagaID,
			HandleEventFn: func(_ context.Context, _ Event, _ State) (*sagaAction, error) {
				return &sagaAction{Commands: []sagaCommand{
					{EntityID: "nonexistent-entity", Command: new(emptypb.Empty), Timeout: 500 * time.Millisecond},
				}}, nil
			},
			HandleErrorFn: func(_ context.Context, _ string, _ error, _ State) (*sagaAction, error) {
				select {
				case handleErrorCalled <- struct{}{}:
				default:
				}
				return nil, assert.AnError
			},
		}
		sagaCfg := extensions.NewSagaConfig(0)

		pid, err := actorSystem.Spawn(ctx, sagaID, New(),
			goakt.WithLongLived(),
			goakt.WithDependencies(behavior, sagaCfg))
		require.NoError(t, err)
		require.NotNil(t, pid)

		topic := protocol.EventsTopic
		eventAny, _ := anypb.New(&testpb.AccountCreated{AccountId: uuid.NewString()})
		event := &egopb.Event{PersistenceId: uuid.NewString(), SequenceNumber: 1, Event: eventAny}
		stream.Publish(topic, event)

		select {
		case <-handleErrorCalled:
		case <-time.After(3 * time.Second):
			t.Fatal("HandleError was not called")
		}
		require.Never(t, func() bool { return !pid.IsRunning() }, 300*time.Millisecond, 20*time.Millisecond)

		stream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("sendCommand: unexpected reply type is logged", func(t *testing.T) {
		ctx := context.TODO()
		sagaID := uuid.NewString()
		targetID := uuid.NewString()

		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		defer eventStore.Disconnect(ctx) //nolint:errcheck

		stream := eventstream.New()
		defer stream.Close()

		actorSystem, err := goakt.NewActorSystem("TestSystem",
			goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(stream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		// Target responds with a non-CommandReply message
		_, err = actorSystem.Spawn(ctx, targetID,
			&enginetest.SimpleReplyActor{Reply: new(emptypb.Empty)},
			goakt.WithLongLived())
		require.NoError(t, err)

		commandSent := make(chan struct{}, 1)
		behavior := &enginetest.CallbackSagaBehavior{
			SagaID: sagaID,
			HandleEventFn: func(_ context.Context, _ Event, _ State) (*sagaAction, error) {
				select {
				case commandSent <- struct{}{}:
				default:
				}
				return &sagaAction{Commands: []sagaCommand{
					{EntityID: targetID, Command: new(emptypb.Empty), Timeout: 3 * time.Second},
				}}, nil
			},
		}
		sagaCfg := extensions.NewSagaConfig(0)

		pid, err := actorSystem.Spawn(ctx, sagaID, New(),
			goakt.WithLongLived(),
			goakt.WithDependencies(behavior, sagaCfg))
		require.NoError(t, err)
		require.NotNil(t, pid)

		topic := protocol.EventsTopic
		eventAny, _ := anypb.New(&testpb.AccountCreated{AccountId: uuid.NewString()})
		event := &egopb.Event{PersistenceId: uuid.NewString(), SequenceNumber: 1, Event: eventAny}
		stream.Publish(topic, event)

		select {
		case <-commandSent:
		case <-time.After(2 * time.Second):
			t.Fatal("command was not sent")
		}

		// Actor must still be running even though reply was unexpected
		require.Never(t, func() bool { return !pid.IsRunning() }, 500*time.Millisecond, 20*time.Millisecond)

		stream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("sendCommand: error reply triggers HandleError", func(t *testing.T) {
		ctx := context.TODO()
		sagaID := uuid.NewString()
		targetID := uuid.NewString()

		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		defer eventStore.Disconnect(ctx) //nolint:errcheck

		stream := eventstream.New()
		defer stream.Close()

		actorSystem, err := goakt.NewActorSystem("TestSystem",
			goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(stream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		// Target returns an error reply (parseCommandReply will return error)
		errorReply := &egopb.CommandReply{
			Reply: &egopb.CommandReply_ErrorReply{
				ErrorReply: &egopb.ErrorReply{Message: "entity rejected command"},
			},
		}
		_, err = actorSystem.Spawn(ctx, targetID,
			&enginetest.SimpleReplyActor{Reply: errorReply},
			goakt.WithLongLived())
		require.NoError(t, err)

		handleErrorCalled := make(chan struct{}, 1)
		behavior := &enginetest.CallbackSagaBehavior{
			SagaID: sagaID,
			HandleEventFn: func(_ context.Context, _ Event, _ State) (*sagaAction, error) {
				return &sagaAction{Commands: []sagaCommand{
					{EntityID: targetID, Command: new(emptypb.Empty), Timeout: 3 * time.Second},
				}}, nil
			},
			HandleErrorFn: func(_ context.Context, _ string, _ error, _ State) (*sagaAction, error) {
				select {
				case handleErrorCalled <- struct{}{}:
				default:
				}
				return &sagaAction{Complete: true}, nil
			},
		}
		sagaCfg := extensions.NewSagaConfig(0)

		pid, err := actorSystem.Spawn(ctx, sagaID, New(),
			goakt.WithLongLived(),
			goakt.WithDependencies(behavior, sagaCfg))
		require.NoError(t, err)
		require.NotNil(t, pid)

		topic := protocol.EventsTopic
		eventAny, _ := anypb.New(&testpb.AccountCreated{AccountId: uuid.NewString()})
		event := &egopb.Event{PersistenceId: uuid.NewString(), SequenceNumber: 1, Event: eventAny}
		stream.Publish(topic, event)

		select {
		case <-handleErrorCalled:
		case <-time.After(3 * time.Second):
			t.Fatal("HandleError was not called after error reply")
		}

		stream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("sendCommand: error reply HandleError failure is logged", func(t *testing.T) {
		ctx := context.TODO()
		sagaID := uuid.NewString()
		targetID := uuid.NewString()

		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		defer eventStore.Disconnect(ctx) //nolint:errcheck

		stream := eventstream.New()
		defer stream.Close()

		actorSystem, err := goakt.NewActorSystem("TestSystem",
			goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(stream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		errorReply := &egopb.CommandReply{
			Reply: &egopb.CommandReply_ErrorReply{
				ErrorReply: &egopb.ErrorReply{Message: "entity rejected command"},
			},
		}
		_, err = actorSystem.Spawn(ctx, targetID,
			&enginetest.SimpleReplyActor{Reply: errorReply},
			goakt.WithLongLived())
		require.NoError(t, err)

		handleErrorCalled := make(chan struct{}, 1)
		behavior := &enginetest.CallbackSagaBehavior{
			SagaID: sagaID,
			HandleEventFn: func(_ context.Context, _ Event, _ State) (*sagaAction, error) {
				return &sagaAction{Commands: []sagaCommand{
					{EntityID: targetID, Command: new(emptypb.Empty), Timeout: 3 * time.Second},
				}}, nil
			},
			HandleErrorFn: func(_ context.Context, _ string, _ error, _ State) (*sagaAction, error) {
				select {
				case handleErrorCalled <- struct{}{}:
				default:
				}
				return nil, assert.AnError
			},
		}
		sagaCfg := extensions.NewSagaConfig(0)

		pid, err := actorSystem.Spawn(ctx, sagaID, New(),
			goakt.WithLongLived(),
			goakt.WithDependencies(behavior, sagaCfg))
		require.NoError(t, err)
		require.NotNil(t, pid)

		topic := protocol.EventsTopic
		eventAny, _ := anypb.New(&testpb.AccountCreated{AccountId: uuid.NewString()})
		event := &egopb.Event{PersistenceId: uuid.NewString(), SequenceNumber: 1, Event: eventAny}
		stream.Publish(topic, event)

		select {
		case <-handleErrorCalled:
		case <-time.After(3 * time.Second):
			t.Fatal("HandleError was not called")
		}
		require.Never(t, func() bool { return !pid.IsRunning() }, 300*time.Millisecond, 20*time.Millisecond)

		stream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("sendCommand: HandleResult failure is logged", func(t *testing.T) {
		ctx := context.TODO()
		sagaID := uuid.NewString()
		targetID := uuid.NewString()

		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		defer eventStore.Disconnect(ctx) //nolint:errcheck

		stream := eventstream.New()
		defer stream.Close()

		actorSystem, err := goakt.NewActorSystem("TestSystem",
			goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(stream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		// Target returns a successful state reply
		successReply := &egopb.CommandReply{
			Reply: &egopb.CommandReply_StateReply{
				StateReply: &egopb.StateReply{
					PersistenceId:  targetID,
					SequenceNumber: 1,
					State:          enginetest.MustAny(t, &samplepb.Account{}),
				},
			},
		}
		_, err = actorSystem.Spawn(ctx, targetID,
			&enginetest.SimpleReplyActor{Reply: successReply},
			goakt.WithLongLived())
		require.NoError(t, err)

		handleResultCalled := make(chan struct{}, 1)
		behavior := &enginetest.CallbackSagaBehavior{
			SagaID: sagaID,
			HandleEventFn: func(_ context.Context, _ Event, _ State) (*sagaAction, error) {
				return &sagaAction{Commands: []sagaCommand{
					{EntityID: targetID, Command: new(emptypb.Empty), Timeout: 3 * time.Second},
				}}, nil
			},
			HandleResultFn: func(_ context.Context, _ string, _ State, _ State) (*sagaAction, error) {
				select {
				case handleResultCalled <- struct{}{}:
				default:
				}
				return nil, assert.AnError
			},
		}
		sagaCfg := extensions.NewSagaConfig(0)

		pid, err := actorSystem.Spawn(ctx, sagaID, New(),
			goakt.WithLongLived(),
			goakt.WithDependencies(behavior, sagaCfg))
		require.NoError(t, err)
		require.NotNil(t, pid)

		topic := protocol.EventsTopic
		eventAny, _ := anypb.New(&testpb.AccountCreated{AccountId: uuid.NewString()})
		event := &egopb.Event{PersistenceId: uuid.NewString(), SequenceNumber: 1, Event: eventAny}
		stream.Publish(topic, event)

		select {
		case <-handleResultCalled:
		case <-time.After(3 * time.Second):
			t.Fatal("HandleResult was not called")
		}
		require.Never(t, func() bool { return !pid.IsRunning() }, 300*time.Millisecond, 20*time.Millisecond)

		stream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("sendCommand: HandleResult success completes saga", func(t *testing.T) {
		ctx := context.TODO()
		sagaID := uuid.NewString()
		targetID := uuid.NewString()

		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		defer eventStore.Disconnect(ctx) //nolint:errcheck

		stream := eventstream.New()
		defer stream.Close()

		actorSystem, err := goakt.NewActorSystem("TestSystem",
			goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(stream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		successReply := &egopb.CommandReply{
			Reply: &egopb.CommandReply_StateReply{
				StateReply: &egopb.StateReply{
					PersistenceId:  targetID,
					SequenceNumber: 1,
					State:          enginetest.MustAny(t, &samplepb.Account{}),
				},
			},
		}
		_, err = actorSystem.Spawn(ctx, targetID,
			&enginetest.SimpleReplyActor{Reply: successReply},
			goakt.WithLongLived())
		require.NoError(t, err)

		handleResultCalled := make(chan struct{}, 1)
		behavior := &enginetest.CallbackSagaBehavior{
			SagaID: sagaID,
			HandleEventFn: func(_ context.Context, _ Event, _ State) (*sagaAction, error) {
				return &sagaAction{Commands: []sagaCommand{
					{EntityID: targetID, Command: new(emptypb.Empty), Timeout: 3 * time.Second},
				}}, nil
			},
			HandleResultFn: func(_ context.Context, _ string, _ State, _ State) (*sagaAction, error) {
				select {
				case handleResultCalled <- struct{}{}:
				default:
				}
				return &sagaAction{Complete: true}, nil
			},
		}
		sagaCfg := extensions.NewSagaConfig(0)

		pid, err := actorSystem.Spawn(ctx, sagaID, New(),
			goakt.WithLongLived(),
			goakt.WithDependencies(behavior, sagaCfg))
		require.NoError(t, err)
		require.NotNil(t, pid)

		topic := protocol.EventsTopic
		eventAny, _ := anypb.New(&testpb.AccountCreated{AccountId: uuid.NewString()})
		event := &egopb.Event{PersistenceId: uuid.NewString(), SequenceNumber: 1, Event: eventAny}
		stream.Publish(topic, event)

		select {
		case <-handleResultCalled:
		case <-time.After(3 * time.Second):
			t.Fatal("HandleResult was not called")
		}
		require.Never(t, func() bool { return !pid.IsRunning() }, 300*time.Millisecond, 20*time.Millisecond)

		stream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("sendCommand: default timeout when zero", func(t *testing.T) {
		ctx := context.TODO()
		sagaID := uuid.NewString()

		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		defer eventStore.Disconnect(ctx) //nolint:errcheck

		stream := eventstream.New()
		defer stream.Close()

		actorSystem, err := goakt.NewActorSystem("TestSystem",
			goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(stream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		handleErrorCalled := make(chan struct{}, 1)
		behavior := &enginetest.CallbackSagaBehavior{
			SagaID: sagaID,
			HandleEventFn: func(_ context.Context, _ Event, _ State) (*sagaAction, error) {
				// Timeout=0 should default to 5s in sendCommand
				return &sagaAction{Commands: []sagaCommand{
					{EntityID: "nonexistent-entity", Command: new(emptypb.Empty), Timeout: 0},
				}}, nil
			},
			HandleErrorFn: func(_ context.Context, _ string, _ error, _ State) (*sagaAction, error) {
				select {
				case handleErrorCalled <- struct{}{}:
				default:
				}
				return &sagaAction{Complete: true}, nil
			},
		}
		sagaCfg := extensions.NewSagaConfig(0)

		pid, err := actorSystem.Spawn(ctx, sagaID, New(),
			goakt.WithLongLived(),
			goakt.WithDependencies(behavior, sagaCfg))
		require.NoError(t, err)
		require.NotNil(t, pid)

		topic := protocol.EventsTopic
		eventAny, _ := anypb.New(&testpb.AccountCreated{AccountId: uuid.NewString()})
		event := &egopb.Event{PersistenceId: uuid.NewString(), SequenceNumber: 1, Event: eventAny}
		stream.Publish(topic, event)

		select {
		case <-handleErrorCalled:
		case <-time.After(10 * time.Second):
			t.Fatal("HandleError was not called (expected after 5s default timeout)")
		}

		stream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})
}

// TestSagaFailsClosed documents and proves the known #54 limitation: Actor
// dispatches commands via sendCommand/compensate using context.Background()
// (saga_actor.go, read-only in this change), which never carries a
// TenantContext. In tenant-aware mode this means a saga can never legitimately
// reach a domain handler on its own — it fails closed at the exact same
// pre-handler gate added to eventsource.Actor and DurableStateActor for T4-A
// (see TestEventSourcedActorTenancyGate, TestDurableStateActorTenancyGate).
//
// This is not a new mechanism: the saga's context.Background() dispatch is
// architecturally identical to the "artificial loss of TenantContext" case
// already covered directly against the actors. This test additionally proves
// it end-to-end, through a real Actor reacting to a real event and
// invoking a real tenant-aware eventsource.Actor via SendSync, exactly as
// production code would.
func TestSagaFailsClosed(t *testing.T) {
	t.Run("saga-dispatched command in tenant-aware mode is blocked before HandleCommand", func(t *testing.T) {
		ctx := context.TODO()
		sagaID := uuid.NewString()
		targetID := uuid.NewString()

		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		defer eventStore.Disconnect(ctx) //nolint:errcheck

		stream := eventstream.New()
		defer stream.Close()

		// extensions.NewTenancyMarker() puts the actor system in tenant-aware
		// mode, exactly as Engine.NewEngine does when a resolver is
		// registered via WithTenantResolver. No resolver is registered here
		// at all: the saga must never be able to reach one (see structural
		// invariant), and this test does not need one to prove the gate.
		actorSystem, err := goakt.NewActorSystem("TestSystem",
			goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(stream),
				extensions.NewTenancyMarker(),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))

		// The saga's real target: a genuine tenant-aware eventsource.Actor,
		// not a stub. If the gate ever regressed and let a saga-dispatched
		// command through, this probe would record it.
		//
		// This test spawns directly through actorSystem.Spawn, bypassing
		// Engine.Entity entirely, so it must supply the per-spawn
		// extensions.EntityTenantScope dependency itself — exactly what
		// Engine.Entity injects when given engine.WithTenant (TENANT-003 T4,
		// corrected). Without it, tenancy being active
		// (extensions.NewTenancyMarker() above) makes the target's own
		// PreStart fail closed with extensions.ErrEntityTenantScopeMissing before this
		// test ever reaches the saga-dispatch gate it means to prove.
		targetProbe := enginetest.NewTenancyProbeEventSourcedBehavior(targetID)
		_, err = actorSystem.Spawn(ctx, targetID, eventsource.New(),
			goakt.WithDependencies(targetProbe, extensions.NewEntityTenantScope("acme")), goakt.WithLongLived(), goakt.WithStashing())
		require.NoError(t, err)

		// Post-EGO-TENANT-002/PR3, Actor reconstructs a TenantContext from
		// each incoming event's own tenant metadata (SG2) and rejects the
		// event outright — before HandleEvent ever runs, so no sagaAction and
		// no command is ever produced — when that metadata is absent or
		// malformed (SG4). This is a strictly earlier and stronger form of
		// the structural invariant this test originally proved by relying on
		// the saga blindly dispatching via context.Background() and the
		// target entity's own tenancy gate catching it downstream: that
		// fallback path no longer exists because the saga never reaches
		// sendCommand for a tenant-less event in the first place.
		var handleEventCalls atomic.Int32
		behavior := &enginetest.CallbackSagaBehavior{
			SagaID: sagaID,
			HandleEventFn: func(_ context.Context, _ Event, _ State) (*sagaAction, error) {
				handleEventCalls.Add(1)
				return &sagaAction{Commands: []sagaCommand{
					{EntityID: targetID, Command: &testpb.CreateAccount{AccountBalance: 500}, Timeout: 3 * time.Second},
				}}, nil
			},
		}
		sagaCfg := extensions.NewSagaConfig(0)

		pid, err := actorSystem.Spawn(ctx, sagaID, New(),
			goakt.WithLongLived(),
			// Same reasoning as the target's spawn above: this saga is also
			// spawned directly through actorSystem.Spawn, so it needs its own
			// EntityTenantScope dependency to get past tenancy-active PreStart.
			goakt.WithDependencies(behavior, sagaCfg, extensions.NewEntityTenantScope("acme")))
		require.NoError(t, err)
		require.NotNil(t, pid)

		topic := protocol.EventsTopic
		eventAny, _ := anypb.New(&testpb.AccountCreated{AccountId: uuid.NewString()})
		event := &egopb.Event{PersistenceId: uuid.NewString(), SequenceNumber: 1, Event: eventAny}
		stream.Publish(topic, event)

		require.Never(t, func() bool { return handleEventCalls.Load() != 0 }, 2*time.Second, 20*time.Millisecond,
			"HandleEvent must never run for an event with no tenant metadata in tenant-aware mode")

		assert.Zero(t, targetProbe.InvocationCount(),
			"HandleCommand must never run for a command the saga could not have formed for a rejected event")

		latest, err := eventStore.GetLatestEvent(ctx, persistence.Unscoped(), targetID)
		require.NoError(t, err)
		assert.Nil(t, latest, "no event may be persisted when the gate blocks the saga's command")

		require.True(t, pid.IsRunning())

		stream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})
}
