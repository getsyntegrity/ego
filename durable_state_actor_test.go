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
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"
	"go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/eventstream"
	"github.com/pablogore/ego/v4/internal/extensions"
	"github.com/pablogore/ego/v4/internal/pause"
	mocks "github.com/pablogore/ego/v4/mocks/persistence"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
	"github.com/pablogore/ego/v4/tenancy"
	"github.com/pablogore/ego/v4/testkit"
)

func TestDurableStateBehavior(t *testing.T) {
	t.Run("with state reply", func(t *testing.T) {
		ctx := context.TODO()

		durableStore := testkit.NewDurableStore()

		persistenceID := uuid.NewString()
		behavior := NewAccountDurableStateBehavior(persistenceID)
		err := durableStore.Connect(ctx)
		require.NoError(t, err)

		// create an instance of events stream
		eventStream := eventstream.New()

		// create an actor system
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewDurableStateStore(durableStore),
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		assert.NotNil(t, actorSystem)

		// start the actor system
		err = actorSystem.Start(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		actor := newDurableStateActor()
		pid, _ := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived())
		require.NotNil(t, pid)

		pause.For(time.Second)

		var command proto.Message

		command = &testpb.CreateAccount{AccountBalance: 500.00}
		// send the command to the actor
		reply, err := goakt.Ask(ctx, pid, command, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)
		require.IsType(t, new(egopb.CommandReply), reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		state := commandReply.GetReply().(*egopb.CommandReply_StateReply)
		require.EqualValues(t, 1, state.StateReply.GetSequenceNumber())

		// marshal the resulting state
		resultingState := new(testpb.Account)
		err = state.StateReply.GetState().UnmarshalTo(resultingState)
		require.NoError(t, err)

		expected := &testpb.Account{
			AccountId:      persistenceID,
			AccountBalance: 500.00,
		}
		require.True(t, proto.Equal(expected, resultingState))

		// send another command to credit the balance
		command = &testpb.CreditAccount{
			AccountId: persistenceID,
			Balance:   250,
		}
		reply, err = goakt.Ask(ctx, pid, command, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)
		require.IsType(t, new(egopb.CommandReply), reply)

		commandReply = reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		state = commandReply.GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, 2, state.StateReply.GetSequenceNumber())

		// marshal the resulting state
		resultingState = new(testpb.Account)
		err = state.StateReply.GetState().UnmarshalTo(resultingState)
		require.NoError(t, err)

		expected = &testpb.Account{
			AccountId:      persistenceID,
			AccountBalance: 750.00,
		}
		require.True(t, proto.Equal(expected, resultingState))

		// stop the actor system
		err = actorSystem.Stop(ctx)
		require.NoError(t, err)

		err = durableStore.Disconnect(ctx)
		require.NoError(t, err)

		pause.For(time.Second)
		eventStream.Close()
	})
	t.Run("with error reply", func(t *testing.T) {
		ctx := context.TODO()

		durableStore := testkit.NewDurableStore()
		// create a persistence id
		persistenceID := uuid.NewString()
		// create the persistence behavior
		behavior := NewAccountDurableStateBehavior(persistenceID)

		err := durableStore.Connect(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		// create an instance of events stream
		eventStream := eventstream.New()

		// create an actor system
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewDurableStateStore(durableStore),
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		assert.NotNil(t, actorSystem)

		// start the actor system
		err = actorSystem.Start(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		// create the persistence actor using the behavior previously created
		persistentActor := newDurableStateActor()
		// spawn the actor
		pid, _ := actorSystem.Spawn(ctx, behavior.ID(), persistentActor, goakt.WithDependencies(behavior), goakt.WithLongLived())
		require.NotNil(t, pid)

		pause.For(time.Second)

		var command proto.Message

		command = &testpb.CreateAccount{AccountBalance: 500.00}
		// send the command to the actor
		reply, err := goakt.Ask(ctx, pid, command, time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)
		require.IsType(t, new(egopb.CommandReply), reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		state := commandReply.GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, 1, state.StateReply.GetSequenceNumber())

		// marshal the resulting state
		resultingState := new(testpb.Account)
		err = state.StateReply.GetState().UnmarshalTo(resultingState)
		require.NoError(t, err)

		expected := &testpb.Account{
			AccountId:      persistenceID,
			AccountBalance: 500.00,
		}
		assert.True(t, proto.Equal(expected, resultingState))

		// send another command to credit the balance
		command = &testpb.CreditAccount{
			AccountId: "different-id",
			Balance:   250,
		}
		reply, err = goakt.Ask(ctx, pid, command, time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)
		require.IsType(t, new(egopb.CommandReply), reply)

		commandReply = reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_ErrorReply), commandReply.GetReply())

		errorReply := commandReply.GetReply().(*egopb.CommandReply_ErrorReply)
		assert.Equal(t, "command sent to the wrong entity", errorReply.ErrorReply.GetMessage())

		err = actorSystem.Stop(ctx)
		require.NoError(t, err)

		err = durableStore.Disconnect(ctx)
		require.NoError(t, err)

		pause.For(time.Second)
		eventStream.Close()
	})
	t.Run("with state recovery from state store", func(t *testing.T) {
		ctx := context.TODO()

		durableStore := testkit.NewDurableStore()
		require.NoError(t, durableStore.Connect(ctx))

		pause.For(time.Second)

		persistenceID := uuid.NewString()
		behavior := NewAccountDurableStateBehavior(persistenceID)

		err := durableStore.Connect(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		eventStream := eventstream.New()

		// create an actor system
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewDurableStateStore(durableStore),
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		assert.NotNil(t, actorSystem)

		// start the actor system
		err = actorSystem.Start(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		persistentActor := newDurableStateActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), persistentActor, goakt.WithDependencies(behavior), goakt.WithLongLived())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		var command proto.Message

		command = &testpb.CreateAccount{AccountBalance: 500.00}

		reply, err := goakt.Ask(ctx, pid, command, time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)
		require.IsType(t, new(egopb.CommandReply), reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		state := commandReply.GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, 1, state.StateReply.GetSequenceNumber())

		// marshal the resulting state
		resultingState := new(testpb.Account)
		err = state.StateReply.GetState().UnmarshalTo(resultingState)
		require.NoError(t, err)

		expected := &testpb.Account{
			AccountId:      persistenceID,
			AccountBalance: 500.00,
		}
		assert.True(t, proto.Equal(expected, resultingState))

		// send another command to credit the balance
		command = &testpb.CreditAccount{
			AccountId: persistenceID,
			Balance:   250,
		}
		reply, err = goakt.Ask(ctx, pid, command, time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)
		require.IsType(t, new(egopb.CommandReply), reply)

		commandReply = reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		state = commandReply.GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, 2, state.StateReply.GetSequenceNumber())

		// marshal the resulting state
		resultingState = new(testpb.Account)
		err = state.StateReply.GetState().UnmarshalTo(resultingState)
		require.NoError(t, err)

		expected = &testpb.Account{
			AccountId:      persistenceID,
			AccountBalance: 750.00,
		}

		assert.True(t, proto.Equal(expected, resultingState))
		// wait a while
		pause.For(time.Second)

		// restart the actor
		pid, err = actorSystem.ReSpawn(ctx, behavior.ID())
		require.NoError(t, err)

		pause.For(time.Second)

		// fetch the current state
		command = &egopb.GetStateCommand{}
		reply, err = goakt.Ask(ctx, pid, command, time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)
		require.IsType(t, new(egopb.CommandReply), reply)

		commandReply = reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		resultingState = new(testpb.Account)
		err = state.StateReply.GetState().UnmarshalTo(resultingState)
		require.NoError(t, err)
		expected = &testpb.Account{
			AccountId:      persistenceID,
			AccountBalance: 750.00,
		}
		assert.True(t, proto.Equal(expected, resultingState))

		err = actorSystem.Stop(ctx)
		assert.NoError(t, err)

		pause.For(time.Second)

		// free resources
		assert.NoError(t, durableStore.Disconnect(ctx))
		eventStream.Close()
	})
	t.Run("with state recovery from state store failure", func(t *testing.T) {
		ctx := context.TODO()
		pause.For(time.Second)

		persistenceID := uuid.NewString()
		behavior := NewAccountDurableStateBehavior(persistenceID)

		eventStream := eventstream.New()

		durableStore := new(mocks.StateStore)
		durableStore.EXPECT().Ping(mock.Anything).Return(nil)
		durableStore.EXPECT().GetLatestState(mock.Anything, behavior.ID()).Return(nil, assert.AnError)

		// create an actor system
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewDurableStateStore(durableStore),
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		assert.NotNil(t, actorSystem)

		// start the actor system
		err = actorSystem.Start(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		persistentActor := newDurableStateActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), persistentActor, goakt.WithDependencies(behavior), goakt.WithLongLived())
		require.Error(t, err)
		require.Nil(t, pid)

		pause.For(time.Second)

		err = actorSystem.Stop(ctx)
		assert.NoError(t, err)

		pause.For(time.Second)
		eventStream.Close()
		durableStore.AssertExpectations(t)
	})
	t.Run("with state recovery from state store with initial parsing failure", func(t *testing.T) {
		ctx := context.TODO()

		persistenceID := uuid.NewString()
		behavior := NewAccountDurableStateBehavior(persistenceID)

		eventStream := eventstream.New()

		latestState := &egopb.DurableState{
			ResultingState: &anypb.Any{
				TypeUrl: "invalid-type-url",
				Value:   []byte("invalid-value"),
			},
		}
		durableStore := new(mocks.StateStore)
		durableStore.EXPECT().Ping(mock.Anything).Return(nil)
		durableStore.EXPECT().GetLatestState(mock.Anything, behavior.ID()).Return(latestState, nil)

		// create an actor system
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewDurableStateStore(durableStore),
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		assert.NotNil(t, actorSystem)

		// start the actor system
		err = actorSystem.Start(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		persistentActor := newDurableStateActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), persistentActor, goakt.WithDependencies(behavior), goakt.WithLongLived())
		require.Error(t, err)
		require.Nil(t, pid)

		pause.For(time.Second)

		err = actorSystem.Stop(ctx)
		assert.NoError(t, err)

		pause.For(time.Second)

		eventStream.Close()
		durableStore.AssertExpectations(t)
	})
	t.Run("with telemetry extension", func(t *testing.T) {
		ctx := context.TODO()

		durableStore := testkit.NewDurableStore()
		require.NoError(t, durableStore.Connect(ctx))

		persistenceID := uuid.NewString()
		behavior := NewAccountDurableStateBehavior(persistenceID)

		eventStream := eventstream.New()

		noopTracer := tracenoop.NewTracerProvider().Tracer("test")
		noopMeter := noop.NewMeterProvider().Meter("test")

		// create an actor system with telemetry extension
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewDurableStateStore(durableStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewTelemetryExtension(noopTracer, noopMeter),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		require.NotNil(t, actorSystem)

		err = actorSystem.Start(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		actor := newDurableStateActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		command := &testpb.CreateAccount{AccountBalance: 500.00}
		reply, err := goakt.Ask(ctx, pid, command, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)
		require.IsType(t, new(egopb.CommandReply), reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		state := commandReply.GetReply().(*egopb.CommandReply_StateReply)
		require.EqualValues(t, 1, state.StateReply.GetSequenceNumber())

		// stop the actor system (exercises PostStop metrics path)
		err = actorSystem.Stop(ctx)
		require.NoError(t, err)

		err = durableStore.Disconnect(ctx)
		require.NoError(t, err)

		pause.For(time.Second)
		eventStream.Close()
	})
	t.Run("with mismatched state types from HandleCommand", func(t *testing.T) {
		ctx := context.TODO()

		durableStore := testkit.NewDurableStore()
		require.NoError(t, durableStore.Connect(ctx))

		persistenceID := uuid.NewString()
		behavior := &badStateDurableStateBehavior{id: persistenceID}

		eventStream := eventstream.New()

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewDurableStateStore(durableStore),
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		require.NotNil(t, actorSystem)

		err = actorSystem.Start(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		actor := newDurableStateActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		command := &testpb.CreateAccount{AccountBalance: 500.00}
		reply, err := goakt.Ask(ctx, pid, command, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)
		require.IsType(t, new(egopb.CommandReply), reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_ErrorReply), commandReply.GetReply())

		errorReply := commandReply.GetReply().(*egopb.CommandReply_ErrorReply)
		assert.Contains(t, errorReply.ErrorReply.GetMessage(), "mismatch state types")

		err = actorSystem.Stop(ctx)
		require.NoError(t, err)

		err = durableStore.Disconnect(ctx)
		require.NoError(t, err)

		pause.For(time.Second)
		eventStream.Close()
	})
	t.Run("with invalid version increment from HandleCommand", func(t *testing.T) {
		ctx := context.TODO()

		durableStore := testkit.NewDurableStore()
		require.NoError(t, durableStore.Connect(ctx))

		persistenceID := uuid.NewString()
		behavior := &badVersionDurableStateBehavior{id: persistenceID}

		eventStream := eventstream.New()

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewDurableStateStore(durableStore),
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		require.NotNil(t, actorSystem)

		err = actorSystem.Start(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		actor := newDurableStateActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		command := &testpb.CreateAccount{AccountBalance: 500.00}
		reply, err := goakt.Ask(ctx, pid, command, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)
		require.IsType(t, new(egopb.CommandReply), reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_ErrorReply), commandReply.GetReply())

		errorReply := commandReply.GetReply().(*egopb.CommandReply_ErrorReply)
		assert.Contains(t, errorReply.ErrorReply.GetMessage(), "received version")

		err = actorSystem.Stop(ctx)
		require.NoError(t, err)

		err = durableStore.Disconnect(ctx)
		require.NoError(t, err)

		pause.For(time.Second)
		eventStream.Close()
	})
}

// badStateDurableStateBehavior returns a different state type from HandleCommand than InitialState
type badStateDurableStateBehavior struct {
	id string
}

var _ DurableStateBehavior = (*badStateDurableStateBehavior)(nil)

func (x *badStateDurableStateBehavior) ID() string {
	return x.id
}

func (x *badStateDurableStateBehavior) InitialState() State {
	return new(testpb.Account)
}

func (x *badStateDurableStateBehavior) HandleCommand(_ context.Context, _ Command, priorVersion uint64, _ State) (State, uint64, error) {
	// return a different protobuf type than InitialState
	return &testpb.AccountCreated{
		AccountId:      x.id,
		AccountBalance: 500.00,
	}, priorVersion + 1, nil
}

func (x *badStateDurableStateBehavior) MarshalBinary() ([]byte, error) {
	return json.Marshal(struct {
		ID string `json:"id"`
	}{ID: x.id})
}

func (x *badStateDurableStateBehavior) UnmarshalBinary(data []byte) error {
	aux := struct {
		ID string `json:"id"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	x.id = aux.ID
	return nil
}

// badVersionDurableStateBehavior returns an invalid version increment from HandleCommand
type badVersionDurableStateBehavior struct {
	id string
}

var _ DurableStateBehavior = (*badVersionDurableStateBehavior)(nil)

func (x *badVersionDurableStateBehavior) ID() string {
	return x.id
}

func (x *badVersionDurableStateBehavior) InitialState() State {
	return new(testpb.Account)
}

func (x *badVersionDurableStateBehavior) HandleCommand(_ context.Context, _ Command, priorVersion uint64, _ State) (State, uint64, error) {
	return &testpb.Account{
		AccountId:      x.id,
		AccountBalance: 500.00,
	}, priorVersion + 5, nil // +5 instead of +1
}

func (x *badVersionDurableStateBehavior) MarshalBinary() ([]byte, error) {
	return json.Marshal(struct {
		ID string `json:"id"`
	}{ID: x.id})
}

// TestDurableStateActorTenancyGate exercises the T4-A pre-handler gate added
// to DurableStateActor: when the actor system carries the tenancy marker
// (tenant-aware mode), HandleCommand must never run without a TenantContext
// already attached to the incoming ctx. Like its EventSourcedActor
// counterpart, this spawns the actor directly and dispatches through
// goakt.Ask with a plain context, deliberately bypassing
// Engine.SendCommand's resolve-and-attach step.
func TestDurableStateActorTenancyGate(t *testing.T) {
	t.Run("missing TenantContext blocks HandleCommand and persistence", func(t *testing.T) {
		ctx := context.TODO()

		durableStore := testkit.NewDurableStore()
		persistenceID := uuid.NewString()
		behavior := newTenancyProbeDurableStateBehavior(persistenceID)
		require.NoError(t, durableStore.Connect(ctx))

		eventStream := eventstream.New()

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewDurableStateStore(durableStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewTenancyMarker(),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newDurableStateActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived())
		require.NoError(t, err)
		require.NotNil(t, pid)
		pause.For(time.Second)

		// No TenantContext attached: mirrors a caller that bypasses
		// Engine.SendCommand entirely.
		reply, err := goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		commandReply, ok := reply.(*egopb.CommandReply)
		require.True(t, ok)
		errorReply, ok := commandReply.GetReply().(*egopb.CommandReply_ErrorReply)
		require.True(t, ok, "expected an error reply because no TenantContext was attached")

		_, wantErr := tenancy.Require(context.Background())
		assert.Equal(t, wantErr.Error(), errorReply.ErrorReply.GetMessage())

		assert.Zero(t, behavior.invocationCount(), "HandleCommand must never run without an attached TenantContext")

		latest, err := durableStore.GetLatestState(ctx, persistenceID)
		require.NoError(t, err)
		assert.Nil(t, latest, "no state may be persisted when the gate blocks the command")

		require.NoError(t, durableStore.Disconnect(ctx))
		eventStream.Close()
		pause.For(time.Second)
		require.NoError(t, actorSystem.Stop(ctx))
	})
}

// TestDurableStateActorVerifyTenantForPersist unit-tests the T4-B defensive
// persistence invariant (design.md D4) in isolation, without a goakt actor
// system: verifyTenantForPersist must be a no-op in legacy mode, must fail
// closed via tenancy.Require when tenant-aware mode has no TenantContext
// attached, and must succeed without touching a resolver (the actor struct
// holds no resolver field at all — see DurableStateActor.tenantAware's doc
// comment) when one is already attached.
func TestDurableStateActorVerifyTenantForPersist(t *testing.T) {
	t.Run("legacy mode is always a no-op", func(t *testing.T) {
		entity := &DurableStateActor{}
		assert.NoError(t, entity.verifyTenantForPersist(context.Background()))
	})

	t.Run("tenant-aware mode fails closed when no TenantContext is attached", func(t *testing.T) {
		entity := &DurableStateActor{tenantAware: true}
		err := entity.verifyTenantForPersist(context.Background())
		assert.True(t, errors.Is(err, tenancy.ErrMissing))
	})

	t.Run("tenant-aware mode succeeds against an already-attached TenantContext", func(t *testing.T) {
		entity := &DurableStateActor{tenantAware: true}
		tc, err := tenancy.NewTenantContext("acme")
		require.NoError(t, err)
		ctx, err := tenancy.Attach(context.Background(), tc)
		require.NoError(t, err)
		assert.NoError(t, entity.verifyTenantForPersist(ctx))
	})
}

// TestDurableStateActorTenancyWritePath is the end-to-end counterpart of
// TestDurableStateActorVerifyTenantForPersist: with a TenantContext properly
// attached (as Engine.SendCommand would do at the trust boundary), the
// command must succeed all the way through persistStateAndPublish, and
// HandleCommand must observe the exact same TenantContext instance that was
// attached — proving the T4-B gate reads it back rather than re-resolving
// it (there is no resolver reachable from the actor to re-resolve with in
// the first place).
func TestDurableStateActorTenancyWritePath(t *testing.T) {
	ctx := context.TODO()

	durableStore := testkit.NewDurableStore()
	persistenceID := uuid.NewString()
	behavior := newTenancyProbeDurableStateBehavior(persistenceID)
	require.NoError(t, durableStore.Connect(ctx))

	eventStream := eventstream.New()

	actorSystem, err := goakt.NewActorSystem("TestActorSystem",
		goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
		goakt.WithExtensions(
			extensions.NewDurableStateStore(durableStore),
			extensions.NewEventsStream(eventStream),
			extensions.NewTenancyMarker(),
		),
		goakt.WithActorInitMaxRetries(3))
	require.NoError(t, err)
	require.NoError(t, actorSystem.Start(ctx))
	pause.For(time.Second)

	actor := newDurableStateActor()
	pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived())
	require.NoError(t, err)
	require.NotNil(t, pid)
	pause.For(time.Second)

	tenant, err := tenancy.NewTenantContext("acme")
	require.NoError(t, err)
	tenantCtx, err := tenancy.Attach(ctx, tenant)
	require.NoError(t, err)

	reply, err := goakt.Ask(tenantCtx, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
	require.NoError(t, err)
	require.NotNil(t, reply)

	commandReply, ok := reply.(*egopb.CommandReply)
	require.True(t, ok)
	require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply(),
		"a command with a TenantContext already attached must succeed through persistStateAndPublish")

	assert.EqualValues(t, 1, behavior.invocationCount())
	observed, ok := behavior.observedTenant()
	require.True(t, ok)
	assert.Equal(t, tenant, observed, "HandleCommand must observe the exact TenantContext attached at the trust boundary")

	latest, err := durableStore.GetLatestState(ctx, persistenceID)
	require.NoError(t, err)
	require.NotNil(t, latest, "the state must be persisted once the tenant is confirmed present")

	require.NoError(t, durableStore.Disconnect(ctx))
	eventStream.Close()
	pause.For(time.Second)
	require.NoError(t, actorSystem.Stop(ctx))
}

func (x *badVersionDurableStateBehavior) UnmarshalBinary(data []byte) error {
	aux := struct {
		ID string `json:"id"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	x.id = aux.ID
	return nil
}
