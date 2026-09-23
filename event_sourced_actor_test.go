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
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/encryption"
	"github.com/pablogore/ego/v4/eventadapter"
	"github.com/pablogore/ego/v4/eventstream"
	"github.com/pablogore/ego/v4/internal/extensions"
	"github.com/pablogore/ego/v4/internal/pause"
	mockencryption "github.com/pablogore/ego/v4/mocks/encryption"
	mockadapter "github.com/pablogore/ego/v4/mocks/eventadapter"
	mocks "github.com/pablogore/ego/v4/mocks/persistence"
	"github.com/pablogore/ego/v4/persistence"
	"github.com/pablogore/ego/v4/tenancy"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
	"github.com/pablogore/ego/v4/testkit"
)

func TestEventSourcedActor(t *testing.T) {
	t.Run("with state reply", func(t *testing.T) {
		ctx := context.TODO()

		// create the event store
		eventStore := testkit.NewEventsStore()
		// create a persistence id
		persistenceID := uuid.NewString()
		// create the persistence behavior
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		// connect the event store
		err := eventStore.Connect(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		// create an instance of events stream
		eventStream := eventstream.New()

		// create an actor system
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
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
		actor := newEventSourcedActor()
		// spawn the actor
		pid, _ := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
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
		assert.True(t, proto.Equal(expected, resultingState))

		// disconnect the events store
		err = eventStore.Disconnect(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		// close the stream
		eventStream.Close()
		// stop the actor system
		err = actorSystem.Stop(ctx)
		assert.NoError(t, err)
	})
	t.Run("with error reply", func(t *testing.T) {
		ctx := context.TODO()

		// create the event store
		eventStore := testkit.NewEventsStore()
		// create a persistence id
		persistenceID := uuid.NewString()
		// create the persistence behavior
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		// connect the event store
		err := eventStore.Connect(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		// create an instance of events stream
		eventStream := eventstream.New()

		// create an actor system
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
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
		persistentActor := newEventSourcedActor()
		// spawn the actor
		pid, _ := actorSystem.Spawn(ctx, behavior.ID(), persistentActor, goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
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

		// disconnect the event store
		require.NoError(t, eventStore.Disconnect(ctx))
		// close the stream
		eventStream.Close()

		pause.For(time.Second)

		// stop the actor system
		err = actorSystem.Stop(ctx)
		assert.NoError(t, err)
	})
	t.Run("with unhandled command", func(t *testing.T) {
		ctx := context.TODO()

		// create the event store
		eventStore := testkit.NewEventsStore()
		// create a persistence id
		persistenceID := uuid.NewString()
		// create the persistence behavior
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		// connect the event store
		err := eventStore.Connect(ctx)
		require.NoError(t, err)

		// create an instance of events stream
		eventStream := eventstream.New()

		// create an actor system
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
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
		persistentActor := newEventSourcedActor()
		// spawn the actor
		pid, _ := actorSystem.Spawn(ctx, behavior.ID(), persistentActor, goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
		require.NotNil(t, pid)

		pause.For(time.Second)

		command := &testpb.TestSend{}
		// send the command to the actor
		reply, err := goakt.Ask(ctx, pid, command, time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)
		require.IsType(t, new(egopb.CommandReply), reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_ErrorReply), commandReply.GetReply())

		errorReply := commandReply.GetReply().(*egopb.CommandReply_ErrorReply)
		assert.Equal(t, "unhandled command", errorReply.ErrorReply.GetMessage())

		// disconnect from the event store
		require.NoError(t, eventStore.Disconnect(ctx))

		// close the stream
		eventStream.Close()
		// stop the actor system
		err = actorSystem.Stop(ctx)
		assert.NoError(t, err)
	})
	t.Run("with state recovery from event store", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))

		// create a persistence id
		persistenceID := uuid.NewString()
		// create the persistence behavior
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		// connect the event store
		err := eventStore.Connect(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		// create an instance of event stream
		eventStream := eventstream.New()

		// create an actor system
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
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
		persistentActor := newEventSourcedActor()
		// spawn the actor
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), persistentActor, goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
		require.NoError(t, err)
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

		// free resources
		assert.NoError(t, eventStore.Disconnect(ctx))
		// close the stream
		eventStream.Close()

		pause.For(time.Second)

		err = actorSystem.Stop(ctx)
		assert.NoError(t, err)
	})
	t.Run("with no event to persist", func(t *testing.T) {
		ctx := context.TODO()

		// create the event store
		eventStore := testkit.NewEventsStore()
		// create a persistence id
		persistenceID := uuid.NewString()
		// create the persistence behavior
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		// connect the event store
		err := eventStore.Connect(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		// create an instance of event stream
		eventStream := eventstream.New()

		// create an actor system
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
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
		actor := newEventSourcedActor()
		// spawn the actor
		pid, _ := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
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
		assert.EqualValues(t, 1, state.StateReply.GetSequenceNumber())

		// marshal the resulting state
		resultingState := new(testpb.Account)
		err = state.StateReply.GetState().UnmarshalTo(resultingState)
		require.NoError(t, err)

		// create the expected response
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
		assert.True(t, proto.Equal(expected, resultingState))

		// test no events to persist
		command = new(testpb.TestNoEvent)
		// send a command
		reply, err = goakt.Ask(ctx, pid, command, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)
		commandReply = reply.(*egopb.CommandReply)

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

		// disconnect from the event store
		assert.NoError(t, eventStore.Disconnect(ctx))
		// close the stream
		eventStream.Close()

		pause.For(time.Second)

		// stop the actor system
		err = actorSystem.Stop(ctx)
		assert.NoError(t, err)
	})
	t.Run("with unhandled event", func(t *testing.T) {
		ctx := context.TODO()

		// create the event store
		eventStore := testkit.NewEventsStore()
		// create a persistence id
		persistenceID := uuid.NewString()
		// create the persistence behavior
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		// connect the event store
		err := eventStore.Connect(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		// create an instance of events stream
		eventStream := eventstream.New()

		// create an actor system
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
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
		actor := newEventSourcedActor()
		// spawn the actor
		pid, _ := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
		require.NotNil(t, pid)

		pause.For(time.Second)

		// send the command to the actor
		reply, err := goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500.00}, 5*time.Second)
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

		reply, err = goakt.Ask(ctx, pid, new(emptypb.Empty), 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)
		require.IsType(t, new(egopb.CommandReply), reply)

		commandReply = reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_ErrorReply), commandReply.GetReply())

		errorReply := commandReply.GetReply().(*egopb.CommandReply_ErrorReply)
		assert.Equal(t, "unhandled event", errorReply.ErrorReply.GetMessage())

		// disconnect from the event store
		require.NoError(t, eventStore.Disconnect(ctx))

		pause.For(time.Second)

		// close the stream
		eventStream.Close()
		// stop the actor system
		err = actorSystem.Stop(ctx)
		assert.NoError(t, err)
	})
	t.Run("With events store ping failed", func(t *testing.T) {
		ctx := context.TODO()

		// create an instance of events stream
		eventStream := eventstream.New()

		// create a persistence id
		persistenceID := uuid.NewString()
		// create the persistence behavior
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		eventStore := new(mocks.EventsStore)
		eventStore.EXPECT().Ping(mock.Anything).Return(assert.AnError)

		// create an actor system
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
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
		actor := newEventSourcedActor()
		// spawn the actor
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithLongLived(), goakt.WithDependencies(behavior), goakt.WithStashing())
		require.Error(t, err)
		require.Nil(t, pid)

		// close the stream
		eventStream.Close()
		// stop the actor system
		err = actorSystem.Stop(ctx)
		assert.NoError(t, err)
	})
	t.Run("With events store GetLatestEvent failed", func(t *testing.T) {
		ctx := context.TODO()

		// create an instance of events stream
		eventStream := eventstream.New()

		// create a persistence id
		persistenceID := uuid.NewString()
		// create the persistence behavior
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		eventStore := new(mocks.EventsStore)
		eventStore.EXPECT().Ping(mock.Anything).Return(nil)
		eventStore.EXPECT().GetLatestEvent(mock.Anything, persistence.Unscoped(), persistenceID).Return(nil, assert.AnError)

		// create an actor system
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
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
		actor := newEventSourcedActor()
		// spawn the actor
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
		require.Error(t, err)
		require.Nil(t, pid)

		// close the stream
		eventStream.Close()
		// stop the actor system
		err = actorSystem.Stop(ctx)
		assert.NoError(t, err)
	})
	t.Run("With replay events failure during recovery", func(t *testing.T) {
		ctx := context.TODO()

		// create an instance of events stream
		eventStream := eventstream.New()

		// create a persistence id
		persistenceID := uuid.NewString()
		// create the persistence behavior
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		latestEvent := &egopb.Event{
			PersistenceId:  persistenceID,
			SequenceNumber: 1,
		}

		eventStore := new(mocks.EventsStore)
		eventStore.EXPECT().Ping(mock.Anything).Return(nil)
		eventStore.EXPECT().GetLatestEvent(mock.Anything, persistence.Unscoped(), persistenceID).Return(latestEvent, nil)
		eventStore.EXPECT().ReplayEvents(mock.Anything, persistence.Unscoped(), persistenceID, uint64(1), uint64(1), mock.AnythingOfType("uint64")).
			Return(nil, assert.AnError)

		// create an actor system
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
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
		actor := newEventSourcedActor()
		// spawn the actor
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
		require.Error(t, err)
		require.Nil(t, pid)

		// close the stream
		eventStream.Close()
		// stop the actor system
		err = actorSystem.Stop(ctx)
		assert.NoError(t, err)
	})
	t.Run("with snapshot store recovery", func(t *testing.T) {
		ctx := context.TODO()

		// create the event store
		eventStore := testkit.NewEventsStore()
		// create the snapshot store
		snapshotStore := testkit.NewSnapshotStore()
		// create a persistence id
		persistenceID := uuid.NewString()
		// create the persistence behavior
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		// connect the stores
		require.NoError(t, eventStore.Connect(ctx))
		require.NoError(t, snapshotStore.Connect(ctx))

		// pre-write a snapshot
		stateAny, err := anypb.New(&testpb.Account{AccountId: persistenceID, AccountBalance: 100})
		require.NoError(t, err)
		snapshot := &egopb.Snapshot{
			PersistenceId:  persistenceID,
			SequenceNumber: 1,
			State:          stateAny,
			Timestamp:      time.Now().Unix(),
		}
		require.NoError(t, snapshotStore.WriteSnapshot(ctx, persistence.Unscoped(), snapshot))

		// pre-write an event after the snapshot
		eventAny, err := anypb.New(&testpb.AccountCredited{AccountId: persistenceID, AccountBalance: 50})
		require.NoError(t, err)
		event := &egopb.Event{
			PersistenceId:  persistenceID,
			SequenceNumber: 2,
			Event:          eventAny,
			Timestamp:      time.Now().Unix(),
			Shard:          0,
		}
		require.NoError(t, eventStore.WriteEvents(ctx, persistence.Unscoped(), []*egopb.Event{event}, persistence.Unconditional()))

		// create an instance of events stream
		eventStream := eventstream.New()

		// create an actor system
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewSnapshotStore(snapshotStore),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		assert.NotNil(t, actorSystem)

		// start the actor system
		err = actorSystem.Start(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		// create the persistence actor using the behavior previously created
		actor := newEventSourcedActor()
		// spawn the actor
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		// fetch the current state
		reply, err := goakt.Ask(ctx, pid, &egopb.GetStateCommand{}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)
		require.IsType(t, new(egopb.CommandReply), reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		state := commandReply.GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, 2, state.StateReply.GetSequenceNumber())

		// marshal the resulting state
		resultingState := new(testpb.Account)
		err = state.StateReply.GetState().UnmarshalTo(resultingState)
		require.NoError(t, err)

		expected := &testpb.Account{
			AccountId:      persistenceID,
			AccountBalance: 150.00,
		}
		assert.True(t, proto.Equal(expected, resultingState))

		// free resources
		assert.NoError(t, eventStore.Disconnect(ctx))
		assert.NoError(t, snapshotStore.Disconnect(ctx))
		eventStream.Close()

		pause.For(time.Second)

		err = actorSystem.Stop(ctx)
		assert.NoError(t, err)
	})
	t.Run("with telemetry extension", func(t *testing.T) {
		ctx := context.TODO()

		// create the event store
		eventStore := testkit.NewEventsStore()
		// create a persistence id
		persistenceID := uuid.NewString()
		// create the persistence behavior
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		// connect the event store
		err := eventStore.Connect(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		// create an instance of events stream
		eventStream := eventstream.New()

		// create noop tracer and meter for telemetry
		noopTracer := tracenoop.NewTracerProvider().Tracer("test")
		noopMeter := noop.NewMeterProvider().Meter("test")

		// create an actor system with telemetry extension
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewTelemetryExtension(noopTracer, noopMeter),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		assert.NotNil(t, actorSystem)

		// start the actor system
		err = actorSystem.Start(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		// create the persistence actor using the behavior previously created
		actor := newEventSourcedActor()
		// spawn the actor
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
		require.NoError(t, err)
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

		// disconnect the event store
		assert.NoError(t, eventStore.Disconnect(ctx))
		// close the stream
		eventStream.Close()

		pause.For(time.Second)

		// stop the actor system (exercises PostStop metrics decrement)
		err = actorSystem.Stop(ctx)
		assert.NoError(t, err)
	})
	t.Run("with encryption during command processing", func(t *testing.T) {
		ctx := context.TODO()

		// create the event store
		eventStore := testkit.NewEventsStore()
		// create a persistence id
		persistenceID := uuid.NewString()
		// create the persistence behavior
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		// connect the event store
		err := eventStore.Connect(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		// create an instance of events stream
		eventStream := eventstream.New()

		// create a key store and encryptor
		keyStore := testkit.NewKeyStore()
		encryptor := encryption.NewAESEncryptor(keyStore)

		// create an actor system with encryptor extension
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewEncryptor(encryptor),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		assert.NotNil(t, actorSystem)

		// start the actor system
		err = actorSystem.Start(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		// create the persistence actor using the behavior previously created
		actor := newEventSourcedActor()
		// spawn the actor
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
		require.NoError(t, err)
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

		// disconnect the event store
		assert.NoError(t, eventStore.Disconnect(ctx))
		// close the stream
		eventStream.Close()

		pause.For(time.Second)

		// stop the actor system
		err = actorSystem.Stop(ctx)
		assert.NoError(t, err)
	})
	t.Run("with snapshot persistence on interval", func(t *testing.T) {
		ctx := context.TODO()

		// create the stores
		eventStore := testkit.NewEventsStore()
		snapshotStore := testkit.NewSnapshotStore()
		// create a persistence id
		persistenceID := uuid.NewString()
		// create the persistence behavior
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		// connect the stores
		require.NoError(t, eventStore.Connect(ctx))
		require.NoError(t, snapshotStore.Connect(ctx))

		// create an instance of events stream
		eventStream := eventstream.New()

		// create an actor system with snapshot store
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewSnapshotStore(snapshotStore),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		assert.NotNil(t, actorSystem)

		// start the actor system
		err = actorSystem.Start(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		// create entity config with snapshot interval of 1
		entityCfg := &extensions.EntityConfig{
			SnapshotInterval: 1,
		}

		// create the persistence actor using the behavior previously created
		actor := newEventSourcedActor()
		// spawn the actor with behavior and entity config dependencies
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior, entityCfg), goakt.WithLongLived(), goakt.WithStashing())
		require.NoError(t, err)
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
		assert.EqualValues(t, 1, state.StateReply.GetSequenceNumber())

		pause.For(time.Second)

		// verify snapshot was written
		snap, err := snapshotStore.GetLatestSnapshot(ctx, persistence.Unscoped(), persistenceID)
		require.NoError(t, err)
		require.NotNil(t, snap)
		assert.EqualValues(t, 1, snap.GetSequenceNumber())

		// free resources
		assert.NoError(t, eventStore.Disconnect(ctx))
		assert.NoError(t, snapshotStore.Disconnect(ctx))
		eventStream.Close()

		pause.For(time.Second)

		err = actorSystem.Stop(ctx)
		assert.NoError(t, err)
	})
	t.Run("with retention policy delete events on snapshot", func(t *testing.T) {
		ctx := context.TODO()

		// create the stores
		eventStore := testkit.NewEventsStore()
		snapshotStore := testkit.NewSnapshotStore()
		// create a persistence id
		persistenceID := uuid.NewString()
		// create the persistence behavior
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		// connect the stores
		require.NoError(t, eventStore.Connect(ctx))
		require.NoError(t, snapshotStore.Connect(ctx))

		// create an instance of events stream
		eventStream := eventstream.New()

		// create an actor system with snapshot store
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewSnapshotStore(snapshotStore),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		assert.NotNil(t, actorSystem)

		// start the actor system
		err = actorSystem.Start(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		// create entity config with snapshot interval of 2 and retention policy
		entityCfg := &extensions.EntityConfig{
			SnapshotInterval:       2,
			HasRetentionPolicy:     true,
			DeleteEventsOnSnapshot: true,
			EventsRetentionCount:   0,
		}

		// create the persistence actor using the behavior previously created
		actor := newEventSourcedActor()
		// spawn the actor with behavior and entity config dependencies
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior, entityCfg), goakt.WithLongLived(), goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		// send first command
		reply, err := goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500.00}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)
		require.IsType(t, new(egopb.CommandReply), reply)

		// send second command to hit snapshot interval
		reply, err = goakt.Ask(ctx, pid, &testpb.CreditAccount{AccountId: persistenceID, Balance: 100}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		state := commandReply.GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, 2, state.StateReply.GetSequenceNumber())

		pause.For(time.Second)

		// verify snapshot was written
		snap, err := snapshotStore.GetLatestSnapshot(ctx, persistence.Unscoped(), persistenceID)
		require.NoError(t, err)
		require.NotNil(t, snap)
		assert.EqualValues(t, 2, snap.GetSequenceNumber())

		// verify events were deleted (deleteUpTo = eventsCounter = 2 since EventsRetentionCount is 0)
		latestEvent, err := eventStore.GetLatestEvent(ctx, persistence.Unscoped(), persistenceID)
		require.NoError(t, err)
		assert.Nil(t, latestEvent)

		// free resources
		assert.NoError(t, eventStore.Disconnect(ctx))
		assert.NoError(t, snapshotStore.Disconnect(ctx))
		eventStream.Close()

		pause.For(time.Second)

		err = actorSystem.Stop(ctx)
		assert.NoError(t, err)
	})
	t.Run("with event adapters during recovery", func(t *testing.T) {
		ctx := context.TODO()

		// create the event store
		eventStore := testkit.NewEventsStore()
		// create a persistence id
		persistenceID := uuid.NewString()
		// create the persistence behavior
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		// connect the event store
		require.NoError(t, eventStore.Connect(ctx))

		// pre-write an event to the store
		eventAny, err := anypb.New(&testpb.AccountCreated{AccountId: persistenceID, AccountBalance: 100})
		require.NoError(t, err)
		event := &egopb.Event{
			PersistenceId:  persistenceID,
			SequenceNumber: 1,
			Event:          eventAny,
			Timestamp:      time.Now().Unix(),
			Shard:          0,
		}
		require.NoError(t, eventStore.WriteEvents(ctx, persistence.Unscoped(), []*egopb.Event{event}, persistence.Unconditional()))

		// create an instance of events stream
		eventStream := eventstream.New()

		// create a no-op event adapter that passes events through unchanged
		adapter := &noopEventAdapter{}

		// create an actor system with event adapters extension
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewEventAdapters([]eventadapter.EventAdapter{adapter}),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		assert.NotNil(t, actorSystem)

		// start the actor system
		err = actorSystem.Start(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		// create the persistence actor using the behavior previously created
		actor := newEventSourcedActor()
		// spawn the actor
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		// fetch the current state - should have recovered through the adapter chain
		reply, err := goakt.Ask(ctx, pid, &egopb.GetStateCommand{}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)
		require.IsType(t, new(egopb.CommandReply), reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		state := commandReply.GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, 1, state.StateReply.GetSequenceNumber())

		resultingState := new(testpb.Account)
		err = state.StateReply.GetState().UnmarshalTo(resultingState)
		require.NoError(t, err)

		expected := &testpb.Account{
			AccountId:      persistenceID,
			AccountBalance: 100,
		}
		assert.True(t, proto.Equal(expected, resultingState))

		// free resources
		assert.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()

		pause.For(time.Second)

		err = actorSystem.Stop(ctx)
		assert.NoError(t, err)
	})
	t.Run("with snapshot and encryption during recovery", func(t *testing.T) {
		ctx := context.TODO()

		// create the stores
		eventStore := testkit.NewEventsStore()
		snapshotStore := testkit.NewSnapshotStore()

		// create a persistence id
		persistenceID := uuid.NewString()
		// create the persistence behavior
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		// connect the stores
		require.NoError(t, eventStore.Connect(ctx))
		require.NoError(t, snapshotStore.Connect(ctx))

		// create an instance of events stream
		eventStream := eventstream.New()

		// create a key store and encryptor
		keyStore := testkit.NewKeyStore()
		encryptor := encryption.NewAESEncryptor(keyStore)

		// create entity config with snapshot interval of 2 (snapshot at event 2, event 3 has no snapshot)
		entityCfg := &extensions.EntityConfig{
			SnapshotInterval: 2,
		}

		// create an actor system with encryption and snapshot store
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewSnapshotStore(snapshotStore),
				extensions.NewEncryptor(encryptor),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		assert.NotNil(t, actorSystem)

		// start the actor system
		err = actorSystem.Start(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		// create the persistence actor using the behavior previously created
		actor := newEventSourcedActor()
		// spawn the actor with behavior and entity config
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior, entityCfg), goakt.WithLongLived(), goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		// send command 1 (event 1, no snapshot yet)
		reply, err := goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500.00}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		// send command 2 (event 2, snapshot taken at seq 2)
		reply, err = goakt.Ask(ctx, pid, &testpb.CreditAccount{AccountId: persistenceID, Balance: 200}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		// send command 3 (event 3, no snapshot - this encrypted event will need replay after snapshot)
		reply, err = goakt.Ask(ctx, pid, &testpb.CreditAccount{AccountId: persistenceID, Balance: 100}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		pause.For(time.Second)

		// stop the first actor system
		eventStream.Close()
		err = actorSystem.Stop(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		// create a new events stream
		eventStream2 := eventstream.New()

		// start a NEW actor system with the same stores and encryption
		actorSystem2, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream2),
				extensions.NewSnapshotStore(snapshotStore),
				extensions.NewEncryptor(encryptor),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		assert.NotNil(t, actorSystem2)

		err = actorSystem2.Start(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		// spawn the actor again - should recover from encrypted snapshot and events
		behavior2 := NewAccountEventSourcedBehavior(persistenceID)
		actor2 := newEventSourcedActor()
		pid2, err := actorSystem2.Spawn(ctx, behavior2.ID(), actor2, goakt.WithDependencies(behavior2, entityCfg), goakt.WithLongLived())
		require.NoError(t, err)
		require.NotNil(t, pid2)

		pause.For(time.Second)

		// fetch the current state - should have recovered
		reply, err = goakt.Ask(ctx, pid2, &egopb.GetStateCommand{}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)
		require.IsType(t, new(egopb.CommandReply), reply)

		commandReply = reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		state := commandReply.GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, 3, state.StateReply.GetSequenceNumber())

		resultingState := new(testpb.Account)
		err = state.StateReply.GetState().UnmarshalTo(resultingState)
		require.NoError(t, err)

		// 500 + 200 + 100 = 800
		expected := &testpb.Account{
			AccountId:      persistenceID,
			AccountBalance: 800.00,
		}
		assert.True(t, proto.Equal(expected, resultingState))

		// free resources
		assert.NoError(t, eventStore.Disconnect(ctx))
		assert.NoError(t, snapshotStore.Disconnect(ctx))
		eventStream2.Close()

		pause.For(time.Second)

		err = actorSystem2.Stop(ctx)
		assert.NoError(t, err)
	})
	t.Run("with encrypted event replay without snapshot store", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))

		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		eventStream := eventstream.New()
		keyStore := testkit.NewKeyStore()
		encryptor := encryption.NewAESEncryptor(keyStore)

		// first actor system: send commands with encryption (no snapshot store)
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewEncryptor(encryptor),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)
		pause.For(time.Second)

		reply, err := goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 300.00}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		reply, err = goakt.Ask(ctx, pid, &testpb.CreditAccount{AccountId: persistenceID, Balance: 150}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
		pause.For(time.Second)

		// second actor system: recover from encrypted events (no snapshot)
		eventStream2 := eventstream.New()
		actorSystem2, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream2),
				extensions.NewEncryptor(encryptor),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		require.NoError(t, actorSystem2.Start(ctx))
		pause.For(time.Second)

		behavior2 := NewAccountEventSourcedBehavior(persistenceID)
		actor2 := newEventSourcedActor()
		pid2, err := actorSystem2.Spawn(ctx, behavior2.ID(), actor2, goakt.WithDependencies(behavior2), goakt.WithLongLived())
		require.NoError(t, err)
		require.NotNil(t, pid2)
		pause.For(time.Second)

		reply, err = goakt.Ask(ctx, pid2, &egopb.GetStateCommand{}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		commandReply := reply.(*egopb.CommandReply)
		state := commandReply.GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, 2, state.StateReply.GetSequenceNumber())

		resultingState := new(testpb.Account)
		require.NoError(t, state.StateReply.GetState().UnmarshalTo(resultingState))

		expected := &testpb.Account{
			AccountId:      persistenceID,
			AccountBalance: 450.00,
		}
		assert.True(t, proto.Equal(expected, resultingState))

		assert.NoError(t, eventStore.Disconnect(ctx))
		eventStream2.Close()
		pause.For(time.Second)
		assert.NoError(t, actorSystem2.Stop(ctx))
	})
	t.Run("with retention policy delete snapshots on snapshot", func(t *testing.T) {
		ctx := context.TODO()

		// create the stores
		eventStore := testkit.NewEventsStore()
		snapshotStore := testkit.NewSnapshotStore()
		// create a persistence id
		persistenceID := uuid.NewString()
		// create the persistence behavior
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		// connect the stores
		require.NoError(t, eventStore.Connect(ctx))
		require.NoError(t, snapshotStore.Connect(ctx))

		// create an instance of events stream
		eventStream := eventstream.New()

		// create an actor system with snapshot store
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewSnapshotStore(snapshotStore),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		assert.NotNil(t, actorSystem)

		// start the actor system
		err = actorSystem.Start(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		// create entity config with snapshot interval of 2 and delete snapshots retention policy
		entityCfg := &extensions.EntityConfig{
			SnapshotInterval:          2,
			HasRetentionPolicy:        true,
			DeleteSnapshotsOnSnapshot: true,
		}

		// create the persistence actor using the behavior previously created
		actor := newEventSourcedActor()
		// spawn the actor with behavior and entity config dependencies
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior, entityCfg), goakt.WithLongLived(), goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		// send 4 commands: snapshots at events 2 and 4
		// command 1: create account
		reply, err := goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500.00}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		// command 2: credit (triggers first snapshot at seq 2)
		reply, err = goakt.Ask(ctx, pid, &testpb.CreditAccount{AccountId: persistenceID, Balance: 100}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		// command 3: credit
		reply, err = goakt.Ask(ctx, pid, &testpb.CreditAccount{AccountId: persistenceID, Balance: 50}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		// command 4: credit (triggers second snapshot at seq 4, should delete snapshot at seq 2)
		reply, err = goakt.Ask(ctx, pid, &testpb.CreditAccount{AccountId: persistenceID, Balance: 25}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		state := commandReply.GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, 4, state.StateReply.GetSequenceNumber())

		pause.For(time.Second)

		// verify latest snapshot exists at seq 4
		snap, err := snapshotStore.GetLatestSnapshot(ctx, persistence.Unscoped(), persistenceID)
		require.NoError(t, err)
		require.NotNil(t, snap)
		assert.EqualValues(t, 4, snap.GetSequenceNumber())

		// free resources
		assert.NoError(t, eventStore.Disconnect(ctx))
		assert.NoError(t, snapshotStore.Disconnect(ctx))
		eventStream.Close()

		pause.For(time.Second)

		err = actorSystem.Stop(ctx)
		assert.NoError(t, err)
	})
}

// TestEventSourcedActorTenancyGate exercises the T4-A pre-handler gate added
// to EventSourcedActor: when the actor system carries the tenancy marker
// (tenant-aware mode), HandleCommand must never run without a TenantContext
// already attached to the incoming ctx. The gate reuses tenancy.Require — a
// read-only check — and never calls a resolver itself, so these tests spawn
// the actor directly and dispatch through goakt.Ask with a plain context,
// deliberately bypassing Engine.SendCommand's resolve-and-attach step
// (mirroring how a saga or any other internal caller can reach the actor
// runtime without crossing the trust boundary; see TestSagaFailsClosed...
// in saga_test.go for the end-to-end demonstration).
func TestEventSourcedActorTenancyGate(t *testing.T) {
	t.Run("non-batched: missing TenantContext blocks HandleCommand and persistence", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		persistenceID := uuid.NewString()
		behavior := newTenancyProbeEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))

		eventStream := eventstream.New()

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewTenancyMarker(),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, extensions.NewEntityTenantScope("acme")), goakt.WithLongLived(), goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)
		pause.For(time.Second)

		// No TenantContext attached: this is exactly what a caller that
		// bypasses Engine.SendCommand (e.g. a saga's context.Background()
		// dispatch, documented in #54) looks like from the actor's side.
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

		scopeA, err := persistence.NewTenantScope("acme")
		require.NoError(t, err)
		latest, err := eventStore.GetLatestEvent(ctx, scopeA, persistenceID)
		require.NoError(t, err)
		assert.Nil(t, latest, "no event may be persisted when the gate blocks the command")

		require.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()
		pause.For(time.Second)
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("batched: missing TenantContext blocks HandleCommand before flushBatch is ever reached", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		persistenceID := uuid.NewString()
		behavior := newTenancyProbeEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))

		eventStream := eventstream.New()

		// A short flush window with a threshold that is never reached by a
		// single command: if the gate failed to block the command and
		// flushBatch ran, it would still take at least this long, giving the
		// assertion below a real window to catch a regression.
		entityCfg := &extensions.EntityConfig{
			BatchThreshold:   100,
			BatchFlushWindow: 200 * time.Millisecond,
		}

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewTenancyMarker(),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, entityCfg, extensions.NewEntityTenantScope("acme")),
			goakt.WithLongLived(),
			goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)
		pause.For(time.Second)

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

		// Give any wrongly-scheduled flush timer time to fire, then confirm
		// nothing was ever written: flushBatch's own context.Background()
		// call (T4-B, out of scope here) must never even be reached.
		pause.For(500 * time.Millisecond)
		scopeA, err := persistence.NewTenantScope("acme")
		require.NoError(t, err)
		latest, err := eventStore.GetLatestEvent(ctx, scopeA, persistenceID)
		require.NoError(t, err)
		assert.Nil(t, latest, "no event may be persisted when the gate blocks the command")

		require.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()
		pause.For(time.Second)
		require.NoError(t, actorSystem.Stop(ctx))
	})
}

// TestEventSourcedActorVerifyTenantForPersist unit-tests the T4-B defensive
// persistence invariant (design.md D4) in isolation, without a goakt actor
// system: verifyTenantForPersist must be a no-op in legacy mode, must fail
// closed via tenancy.Require when tenant-aware mode has no TenantContext
// attached, and must succeed without touching a resolver (the actor struct
// holds no resolver field at all — see the EventSourcedActor.tenantAware
// doc comment) when one is already attached.
func TestEventSourcedActorVerifyTenantForPersist(t *testing.T) {
	t.Run("legacy mode is always a no-op", func(t *testing.T) {
		entity := &EventSourcedActor{}
		assert.NoError(t, entity.verifyTenantForPersist(context.Background()))
	})

	t.Run("tenant-aware mode fails closed when no TenantContext is attached", func(t *testing.T) {
		entity := &EventSourcedActor{tenantAware: true}
		err := entity.verifyTenantForPersist(context.Background())
		assert.True(t, errors.Is(err, tenancy.ErrMissing))
	})

	t.Run("tenant-aware mode succeeds against an already-attached TenantContext", func(t *testing.T) {
		entity := &EventSourcedActor{tenantAware: true}
		tc, err := tenancy.NewTenantContext("acme")
		require.NoError(t, err)
		ctx, err := tenancy.Attach(context.Background(), tc)
		require.NoError(t, err)
		assert.NoError(t, entity.verifyTenantForPersist(ctx))
	})

	// Blocker 2 inheritance (EGO-TENANT-006 review fix): a resolver
	// returning the zero-value tenancy.TenantContext{} (Blocker 1) is now
	// rejected by tenancy.Attach itself (design.md Decision D8) before the
	// command ever reaches this actor, so ctx here ends up with nothing
	// attached — the same "missing" case this gate already covered. No
	// code change to verifyTenantForPersist was needed to inherit this
	// protection; it fails closed purely because tenancy.Require now
	// rejects malformed content, and Attach never let one through.
	t.Run("a resolver-invalid TenantContext never gets attached, so persistence still fails closed", func(t *testing.T) {
		entity := &EventSourcedActor{tenantAware: true}

		ctx, attachErr := tenancy.Attach(context.Background(), tenancy.TenantContext{})
		require.Error(t, attachErr)
		assert.True(t, errors.Is(attachErr, tenancy.ErrInvalid))

		err := entity.verifyTenantForPersist(ctx)
		assert.True(t, errors.Is(err, tenancy.ErrMissing))
	})
}

// TestEventSourcedActorBatchTenantHomogeneity exercises the T4-B batched
// invariant (design.md D4): every command merged into the same batchBuffer
// before a flush must belong to the same tenant. Both commands here carry a
// TenantContext already attached (T4-A is satisfied for both, so
// HandleCommand runs for each); the second is rejected purely on
// homogeneity grounds, at buffer-append time, before flushBatch's
// context.Background() Ask is ever reached — proving this check is
// independent from, and additional to, T4-A.
func TestEventSourcedActorBatchTenantHomogeneity(t *testing.T) {
	ctx := context.TODO()

	eventStore := testkit.NewEventsStore()
	persistenceID := uuid.NewString()
	behavior := newTenancyProbeEventSourcedBehavior(persistenceID)

	require.NoError(t, eventStore.Connect(ctx))

	eventStream := eventstream.New()

	// A high threshold that a single command never reaches, and a short
	// flush window: this test only cares about the append-time check, not
	// any flush behavior, but the first command's reply is still deferred
	// until its cycle flushes by timer, so the window must stay well under
	// the Ask timeouts used below.
	entityCfg := &extensions.EntityConfig{
		BatchThreshold:   100,
		BatchFlushWindow: time.Second,
	}

	actorSystem, err := goakt.NewActorSystem("TestActorSystem",
		goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
		goakt.WithExtensions(
			extensions.NewEventsStore(eventStore),
			extensions.NewEventsStream(eventStream),
			extensions.NewTenancyMarker(),
		),
		goakt.WithActorInitMaxRetries(3))
	require.NoError(t, err)
	require.NoError(t, actorSystem.Start(ctx))
	pause.For(time.Second)

	actor := newEventSourcedActor()
	pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
		goakt.WithDependencies(behavior, entityCfg, extensions.NewEntityTenantScope("acme")),
		goakt.WithLongLived(),
		goakt.WithStashing())
	require.NoError(t, err)
	require.NotNil(t, pid)
	pause.For(time.Second)

	tenantA, err := tenancy.NewTenantContext("acme")
	require.NoError(t, err)
	tenantB, err := tenancy.NewTenantContext("globex")
	require.NoError(t, err)

	ctxA, err := tenancy.Attach(ctx, tenantA)
	require.NoError(t, err)

	// The first command's reply is deferred (stashed) until its batch cycle
	// flushes by timer, since the threshold is never reached by one command
	// alone: send it in the background and let it run concurrently with the
	// second command below, exactly as it would for two real concurrent
	// callers sharing a batch cycle.
	firstDone := make(chan struct{})
	var firstReply any
	var firstErr error
	go func() {
		defer close(firstDone)
		firstReply, firstErr = goakt.Ask(ctxA, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
	}()

	// Give the first command time to be received, pass the pre-handler gate,
	// and be appended to the batch buffer (recording tenant A) before the
	// second command — for a different tenant — is sent into the same cycle.
	pause.For(200 * time.Millisecond)

	ctxB, err := tenancy.Attach(ctx, tenantB)
	require.NoError(t, err)
	reply, err := goakt.Ask(ctxB, pid, &testpb.CreateAccount{AccountBalance: 10}, 5*time.Second)
	require.NoError(t, err)
	commandReply, ok := reply.(*egopb.CommandReply)
	require.True(t, ok)
	errorReply, ok := commandReply.GetReply().(*egopb.CommandReply_ErrorReply)
	require.True(t, ok, "a second command for a different tenant in the same batch cycle must be rejected")

	wantErr := tenancy.VerifyUnchanged(tenantA, tenantB)
	assert.Equal(t, wantErr.Error(), errorReply.ErrorReply.GetMessage())

	<-firstDone
	require.NoError(t, firstErr)
	firstCommandReply, ok := firstReply.(*egopb.CommandReply)
	require.True(t, ok)
	require.IsType(t, new(egopb.CommandReply_StateReply), firstCommandReply.GetReply(),
		"the first command of the batch cycle must succeed once its own cycle flushes")

	// Exactly the first, tenant-A command's event was ever persisted: the
	// rejected tenant-B command never reached the buffer at all.
	scopeA, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)
	latest, err := eventStore.GetLatestEvent(ctx, scopeA, persistenceID)
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.EqualValues(t, 1, latest.GetSequenceNumber())

	require.NoError(t, eventStore.Disconnect(ctx))
	eventStream.Close()
	pause.For(time.Second)
	require.NoError(t, actorSystem.Stop(ctx))
}

// TestEventSourcedActorResetBatchDoesNotClearActorTenant covers design.md
// D6 (EGO-TENANT-002): actorTenant is scoped to the actor's full lifetime,
// not to one batch cycle, so resetBatch must NOT clear it — superseding the
// prior "batchTenant" behavior, where resetBatch cleared the per-cycle field
// and a new cycle's first command from any tenant was accepted regardless of
// which tenant the previous, already-flushed cycle belonged to. BatchThreshold
// is 1, so each command completes a full, self-contained batch cycle (buffer,
// flush, reply, resetBatch) before the next command is sent. The second
// command uses a DIFFERENT tenant than the first and must now be REJECTED:
// actorTenant, seeded by the first cycle's persist, survives resetBatch and
// is compared against every later command for this actor's entire lifetime.
func TestEventSourcedActorResetBatchDoesNotClearActorTenant(t *testing.T) {
	ctx := context.TODO()

	eventStore := testkit.NewEventsStore()
	persistenceID := uuid.NewString()
	behavior := newTenancyProbeEventSourcedBehavior(persistenceID)

	require.NoError(t, eventStore.Connect(ctx))

	eventStream := eventstream.New()

	entityCfg := &extensions.EntityConfig{
		BatchThreshold:   1,
		BatchFlushWindow: time.Second,
	}

	actorSystem, err := goakt.NewActorSystem("TestActorSystem",
		goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
		goakt.WithExtensions(
			extensions.NewEventsStore(eventStore),
			extensions.NewEventsStream(eventStream),
			extensions.NewTenancyMarker(),
		),
		goakt.WithActorInitMaxRetries(3))
	require.NoError(t, err)
	require.NoError(t, actorSystem.Start(ctx))
	pause.For(time.Second)

	actor := newEventSourcedActor()
	pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
		goakt.WithDependencies(behavior, entityCfg, extensions.NewEntityTenantScope("acme")),
		goakt.WithLongLived(),
		goakt.WithStashing())
	require.NoError(t, err)
	require.NotNil(t, pid)
	pause.For(time.Second)

	tenantA, err := tenancy.NewTenantContext("acme")
	require.NoError(t, err)
	tenantB, err := tenancy.NewTenantContext("globex")
	require.NoError(t, err)

	// First cycle: tenant A. BatchThreshold==1 drives this all the way
	// through flush, reply, and resetBatch before the Ask returns.
	ctxA, err := tenancy.Attach(ctx, tenantA)
	require.NoError(t, err)
	reply, err := goakt.Ask(ctxA, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
	require.NoError(t, err)
	commandReply, ok := reply.(*egopb.CommandReply)
	require.True(t, ok)
	require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply(),
		"the first cycle's only command must succeed")

	// Second cycle: tenant B, a brand new batch cycle. actorTenant (seeded
	// as tenant A by the first cycle's persist) is NOT cleared by
	// resetBatch, so this cross-tenant command must be rejected — even
	// though it is the first command of its own, freshly reset cycle.
	ctxB, err := tenancy.Attach(ctx, tenantB)
	require.NoError(t, err)
	reply, err = goakt.Ask(ctxB, pid, &testpb.CreateAccount{AccountBalance: 10}, 5*time.Second)
	require.NoError(t, err, "Ask itself must not fail; the rejection is carried in the CommandReply")
	commandReply, ok = reply.(*egopb.CommandReply)
	require.True(t, ok)
	errorReply, ok := commandReply.GetReply().(*egopb.CommandReply_ErrorReply)
	require.True(t, ok,
		"a different tenant's command in a brand new batch cycle must still be "+
			"rejected against actorTenant, seeded by the previous, already-flushed cycle")

	wantErr := tenancy.VerifyUnchanged(tenantA, tenantB)
	assert.Equal(t, wantErr.Error(), errorReply.ErrorReply.GetMessage(),
		"rejection must be the fail-closed tenant error VerifyUnchanged produces, not an invented error type")

	// A third command from the SAME tenant (A) that established actorTenant
	// must still succeed in its own brand new batch cycle: actorTenant
	// surviving resetBatch is a cross-tenant guard, not a "one cycle only"
	// restriction on the tenant that originally established it.
	reply, err = goakt.Ask(ctxA, pid, &testpb.CreateAccount{AccountBalance: 20}, 5*time.Second)
	require.NoError(t, err)
	commandReply, ok = reply.(*egopb.CommandReply)
	require.True(t, ok)
	require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply(),
		"the tenant that established actorTenant must still succeed across later batch cycles")

	require.NoError(t, eventStore.Disconnect(ctx))
	eventStream.Close()
	pause.For(time.Second)
	require.NoError(t, actorSystem.Stop(ctx))
}

// TestEventSourcedActorBatchTenantHomogeneity_ZeroEventCrossTenant is the
// RED-before-fix regression test for Blocker 3 (EGO-TENANT-006 adversarial
// review). Before the fix, processAndBatch's tenant-homogeneity check ran
// AFTER entity.behavior.HandleCommand — and, for a command producing zero
// events, only after a StateReply had already been built from
// entity.latestState() (== entity.batchState, i.e. tenant A's in-flight,
// unpersisted data) and sent back to tenant B. Tenant B's HandleCommand also
// ran against tenant A's batchState before any homogeneity check occurred at
// all. Neither the wrong-tenant state leak nor the wrong-tenant HandleCommand
// invocation is acceptable: the check must gate BEFORE HandleCommand runs,
// exactly like the T4-A pre-handler gate does for a missing tenant.
func TestEventSourcedActorBatchTenantHomogeneity_ZeroEventCrossTenant(t *testing.T) {
	ctx := context.TODO()

	eventStore := testkit.NewEventsStore()
	persistenceID := uuid.NewString()
	behavior := newTenancyProbeEventSourcedBehavior(persistenceID)

	require.NoError(t, eventStore.Connect(ctx))

	eventStream := eventstream.New()

	// A high threshold that tenant A's single command never reaches on its
	// own, so its batch cycle (and batchTenant) stays open when tenant B's
	// command arrives.
	entityCfg := &extensions.EntityConfig{
		BatchThreshold:   100,
		BatchFlushWindow: time.Second,
	}

	actorSystem, err := goakt.NewActorSystem("TestActorSystem",
		goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
		goakt.WithExtensions(
			extensions.NewEventsStore(eventStore),
			extensions.NewEventsStream(eventStream),
			extensions.NewTenancyMarker(),
		),
		goakt.WithActorInitMaxRetries(3))
	require.NoError(t, err)
	require.NoError(t, actorSystem.Start(ctx))
	pause.For(time.Second)

	actor := newEventSourcedActor()
	pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
		goakt.WithDependencies(behavior, entityCfg, extensions.NewEntityTenantScope("acme")),
		goakt.WithLongLived(),
		goakt.WithStashing())
	require.NoError(t, err)
	require.NotNil(t, pid)
	pause.For(time.Second)

	tenantA, err := tenancy.NewTenantContext("acme")
	require.NoError(t, err)
	tenantB, err := tenancy.NewTenantContext("globex")
	require.NoError(t, err)

	// Tenant A starts the batch: this command produces one event, so
	// batchTenant becomes A and the batch stays open (threshold not
	// reached). Its reply is stashed until the cycle flushes by timer, so
	// send it in the background exactly like the homogeneity test above.
	ctxA, err := tenancy.Attach(ctx, tenantA)
	require.NoError(t, err)
	firstDone := make(chan struct{})
	var firstReply any
	var firstErr error
	go func() {
		defer close(firstDone)
		firstReply, firstErr = goakt.Ask(ctxA, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
	}()

	// Give tenant A's command time to be received, pass the pre-handler
	// gate, run HandleCommand, and be appended to the batch (recording
	// batchTenant == A) before tenant B's command is sent into the same
	// open cycle.
	pause.For(200 * time.Millisecond)
	invocationsBeforeB := behavior.invocationCount()
	require.EqualValues(t, 1, invocationsBeforeB, "tenant A's command must have already run HandleCommand once")

	// Tenant B sends a command whose handler would produce zero events —
	// a genuine no-op, not an error — against the same actor/persistence
	// ID, while tenant A's batch is still open.
	ctxB, err := tenancy.Attach(ctx, tenantB)
	require.NoError(t, err)
	reply, err := goakt.Ask(ctxB, pid, &testpb.TestNoEvent{}, 5*time.Second)
	require.NoError(t, err, "Ask itself must not fail; the rejection is carried in the CommandReply")
	commandReply, ok := reply.(*egopb.CommandReply)
	require.True(t, ok)
	errorReply, ok := commandReply.GetReply().(*egopb.CommandReply_ErrorReply)
	require.True(t, ok, "a zero-event command for a different tenant than the open batch must still be rejected")

	wantErr := tenancy.VerifyUnchanged(tenantA, tenantB)
	assert.Equal(t, wantErr.Error(), errorReply.ErrorReply.GetMessage(),
		"rejection must be the fail-closed tenant error VerifyUnchanged produces, not an invented error type")

	// The decisive assertion: tenant B's HandleCommand must never have run.
	// Before the Blocker 3 fix, it did run (against tenant A's batchState)
	// before the zero-event early-return path replied — this proves the
	// gate now runs first.
	assert.EqualValues(t, invocationsBeforeB, behavior.invocationCount(),
		"HandleCommand must not execute for a cross-tenant command while a different tenant's batch is open, even if it would have produced zero events")

	// Tenant A's in-flight batch must be untouched by B's rejected attempt:
	// let A's cycle flush (by timer) and confirm it still succeeds and
	// persists exactly A's one event, uncontaminated by B's attempt.
	<-firstDone
	require.NoError(t, firstErr)
	firstCommandReply, ok := firstReply.(*egopb.CommandReply)
	require.True(t, ok)
	require.IsType(t, new(egopb.CommandReply_StateReply), firstCommandReply.GetReply(),
		"tenant A's batch must still succeed once its own cycle flushes, unaffected by tenant B's rejected attempt")

	scopeA, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)
	latest, err := eventStore.GetLatestEvent(ctx, scopeA, persistenceID)
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.EqualValues(t, 1, latest.GetSequenceNumber(),
		"exactly tenant A's one event was persisted; tenant B's rejected attempt contributed nothing")

	require.NoError(t, eventStore.Disconnect(ctx))
	eventStream.Close()
	pause.For(time.Second)
	require.NoError(t, actorSystem.Stop(ctx))
}

// TestEventSourcedActorBatchTenantHomogeneity_ZeroEventSameTenant is the
// regression companion to the cross-tenant test above: the SAME tenant
// sending a zero-event command into its own open batch must still succeed
// and still hit the len(events)==0 cached-state-reply path in
// processAndBatch — the Blocker 3 fix's pre-handler homogeneity check must
// not reject a same-tenant command.
func TestEventSourcedActorBatchTenantHomogeneity_ZeroEventSameTenant(t *testing.T) {
	ctx := context.TODO()

	eventStore := testkit.NewEventsStore()
	persistenceID := uuid.NewString()
	behavior := newTenancyProbeEventSourcedBehavior(persistenceID)

	require.NoError(t, eventStore.Connect(ctx))

	eventStream := eventstream.New()

	entityCfg := &extensions.EntityConfig{
		BatchThreshold:   100,
		BatchFlushWindow: time.Second,
	}

	actorSystem, err := goakt.NewActorSystem("TestActorSystem",
		goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
		goakt.WithExtensions(
			extensions.NewEventsStore(eventStore),
			extensions.NewEventsStream(eventStream),
			extensions.NewTenancyMarker(),
		),
		goakt.WithActorInitMaxRetries(3))
	require.NoError(t, err)
	require.NoError(t, actorSystem.Start(ctx))
	pause.For(time.Second)

	actor := newEventSourcedActor()
	pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
		goakt.WithDependencies(behavior, entityCfg, extensions.NewEntityTenantScope("acme")),
		goakt.WithLongLived(),
		goakt.WithStashing())
	require.NoError(t, err)
	require.NotNil(t, pid)
	pause.For(time.Second)

	tenantA, err := tenancy.NewTenantContext("acme")
	require.NoError(t, err)

	ctxA, err := tenancy.Attach(ctx, tenantA)
	require.NoError(t, err)
	firstDone := make(chan struct{})
	var firstReply any
	var firstErr error
	go func() {
		defer close(firstDone)
		firstReply, firstErr = goakt.Ask(ctxA, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
	}()

	pause.For(200 * time.Millisecond)

	// Same tenant, zero-event command, into the same still-open batch.
	reply, err := goakt.Ask(ctxA, pid, &testpb.TestNoEvent{}, 5*time.Second)
	require.NoError(t, err)
	commandReply, ok := reply.(*egopb.CommandReply)
	require.True(t, ok)
	require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply(),
		"a same-tenant zero-event command must still succeed via the cached-state-reply path")

	<-firstDone
	require.NoError(t, firstErr)
	firstCommandReply, ok := firstReply.(*egopb.CommandReply)
	require.True(t, ok)
	require.IsType(t, new(egopb.CommandReply_StateReply), firstCommandReply.GetReply())

	require.NoError(t, eventStore.Disconnect(ctx))
	eventStream.Close()
	pause.For(time.Second)
	require.NoError(t, actorSystem.Stop(ctx))
}

func TestEventSourcedActorErrorPaths(t *testing.T) {
	t.Run("with missing behavior fails to start", func(t *testing.T) {
		ctx := context.TODO()

		eventStream := eventstream.New()
		persistenceID := uuid.NewString()

		eventStore := new(mocks.EventsStore)
		eventStore.EXPECT().Ping(mock.Anything).Return(nil)
		eventStore.EXPECT().GetLatestEvent(mock.Anything, persistence.Unscoped(), persistenceID).Return(nil, nil)

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		// spawn with no behavior dependency
		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, persistenceID, actor, goakt.WithLongLived(), goakt.WithStashing())
		require.Error(t, err)
		require.Nil(t, pid)

		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("with snapshot store GetLatestSnapshot failure during recovery", func(t *testing.T) {
		ctx := context.TODO()

		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)
		eventStream := eventstream.New()

		eventStore := new(mocks.EventsStore)
		eventStore.EXPECT().Ping(mock.Anything).Return(nil)

		snapshotStore := new(mocks.SnapshotStore)
		snapshotStore.EXPECT().Ping(mock.Anything).Return(nil)
		snapshotStore.EXPECT().GetLatestSnapshot(mock.Anything, persistence.Unscoped(), persistenceID).Return(nil, assert.AnError)

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewSnapshotStore(snapshotStore),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
		require.Error(t, err)
		require.Nil(t, pid)

		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("with snapshot decryption failure during recovery", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		snapshotStore := testkit.NewSnapshotStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))
		require.NoError(t, snapshotStore.Connect(ctx))

		// write an "encrypted" snapshot with dummy ciphertext
		stateAny, err := anypb.New(&testpb.Account{AccountId: persistenceID, AccountBalance: 100})
		require.NoError(t, err)
		encryptedState := &anypb.Any{TypeUrl: stateAny.GetTypeUrl(), Value: []byte("fake-ciphertext")}
		snapshot := &egopb.Snapshot{
			PersistenceId:   persistenceID,
			SequenceNumber:  1,
			State:           encryptedState,
			Timestamp:       time.Now().Unix(),
			IsEncrypted:     true,
			EncryptionKeyId: "key-1",
		}
		require.NoError(t, snapshotStore.WriteSnapshot(ctx, persistence.Unscoped(), snapshot))

		eventStream := eventstream.New()

		encryptor := new(mockencryption.Encryptor)
		encryptor.EXPECT().Decrypt(mock.Anything, persistenceID, []byte("fake-ciphertext"), "key-1").Return(nil, assert.AnError)

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewSnapshotStore(snapshotStore),
				extensions.NewEncryptor(encryptor),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
		require.Error(t, err)
		require.Nil(t, pid)

		require.NoError(t, eventStore.Disconnect(ctx))
		require.NoError(t, snapshotStore.Disconnect(ctx))
		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("with snapshot unmarshal failure after decryption during recovery", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		snapshotStore := testkit.NewSnapshotStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))
		require.NoError(t, snapshotStore.Connect(ctx))

		stateAny, err := anypb.New(&testpb.Account{AccountId: persistenceID, AccountBalance: 100})
		require.NoError(t, err)
		encryptedState := &anypb.Any{TypeUrl: stateAny.GetTypeUrl(), Value: []byte("fake-ciphertext")}
		snapshot := &egopb.Snapshot{
			PersistenceId:   persistenceID,
			SequenceNumber:  1,
			State:           encryptedState,
			Timestamp:       time.Now().Unix(),
			IsEncrypted:     true,
			EncryptionKeyId: "key-1",
		}
		require.NoError(t, snapshotStore.WriteSnapshot(ctx, persistence.Unscoped(), snapshot))

		eventStream := eventstream.New()

		// return non-proto garbage bytes so proto.Unmarshal fails
		encryptor := new(mockencryption.Encryptor)
		encryptor.EXPECT().Decrypt(mock.Anything, persistenceID, []byte("fake-ciphertext"), "key-1").Return([]byte("not-valid-proto"), nil)

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewSnapshotStore(snapshotStore),
				extensions.NewEncryptor(encryptor),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
		require.Error(t, err)
		require.Nil(t, pid)

		require.NoError(t, eventStore.Disconnect(ctx))
		require.NoError(t, snapshotStore.Disconnect(ctx))
		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("with snapshot state type mismatch unmarshal failure during recovery", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		snapshotStore := testkit.NewSnapshotStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))
		require.NoError(t, snapshotStore.Connect(ctx))

		// write snapshot with incompatible state type (AccountCredited instead of Account)
		wrongState, err := anypb.New(&testpb.AccountCredited{AccountId: persistenceID, AccountBalance: 100})
		require.NoError(t, err)
		snapshot := &egopb.Snapshot{
			PersistenceId:  persistenceID,
			SequenceNumber: 1,
			State:          wrongState,
			Timestamp:      time.Now().Unix(),
		}
		require.NoError(t, snapshotStore.WriteSnapshot(ctx, persistence.Unscoped(), snapshot))

		eventStream := eventstream.New()

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewSnapshotStore(snapshotStore),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
		require.Error(t, err)
		require.Nil(t, pid)

		require.NoError(t, eventStore.Disconnect(ctx))
		require.NoError(t, snapshotStore.Disconnect(ctx))
		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("with event decryption failure during recovery", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))

		// write an "encrypted" event with dummy ciphertext
		eventAny, err := anypb.New(&testpb.AccountCreated{AccountId: persistenceID, AccountBalance: 100})
		require.NoError(t, err)
		encryptedEvent := &anypb.Any{TypeUrl: eventAny.GetTypeUrl(), Value: []byte("fake-cipher")}
		event := &egopb.Event{
			PersistenceId:   persistenceID,
			SequenceNumber:  1,
			Event:           encryptedEvent,
			Timestamp:       time.Now().Unix(),
			IsEncrypted:     true,
			EncryptionKeyId: "key-1",
		}
		require.NoError(t, eventStore.WriteEvents(ctx, persistence.Unscoped(), []*egopb.Event{event}, persistence.Unconditional()))

		eventStream := eventstream.New()

		encryptor := new(mockencryption.Encryptor)
		encryptor.EXPECT().Decrypt(mock.Anything, persistenceID, []byte("fake-cipher"), "key-1").Return(nil, assert.AnError)

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewEncryptor(encryptor),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
		require.Error(t, err)
		require.Nil(t, pid)

		require.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("with event unmarshal failure after decryption during recovery", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))

		eventAny, err := anypb.New(&testpb.AccountCreated{AccountId: persistenceID, AccountBalance: 100})
		require.NoError(t, err)
		encryptedEvent := &anypb.Any{TypeUrl: eventAny.GetTypeUrl(), Value: []byte("fake-cipher")}
		event := &egopb.Event{
			PersistenceId:   persistenceID,
			SequenceNumber:  1,
			Event:           encryptedEvent,
			Timestamp:       time.Now().Unix(),
			IsEncrypted:     true,
			EncryptionKeyId: "key-1",
		}
		require.NoError(t, eventStore.WriteEvents(ctx, persistence.Unscoped(), []*egopb.Event{event}, persistence.Unconditional()))

		eventStream := eventstream.New()

		// return garbage bytes so proto.Unmarshal of the Any fails
		encryptor := new(mockencryption.Encryptor)
		encryptor.EXPECT().Decrypt(mock.Anything, persistenceID, []byte("fake-cipher"), "key-1").Return([]byte("not-valid-proto"), nil)

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewEncryptor(encryptor),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
		require.Error(t, err)
		require.Nil(t, pid)

		require.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("with event adapter chain failure during recovery", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))

		eventAny, err := anypb.New(&testpb.AccountCreated{AccountId: persistenceID, AccountBalance: 100})
		require.NoError(t, err)
		event := &egopb.Event{
			PersistenceId:  persistenceID,
			SequenceNumber: 1,
			Event:          eventAny,
			Timestamp:      time.Now().Unix(),
		}
		require.NoError(t, eventStore.WriteEvents(ctx, persistence.Unscoped(), []*egopb.Event{event}, persistence.Unconditional()))

		eventStream := eventstream.New()

		adapter := new(mockadapter.EventAdapter)
		adapter.EXPECT().Adapt(mock.Anything, uint64(1)).Return(nil, assert.AnError)

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewEventAdapters([]eventadapter.EventAdapter{adapter}),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
		require.Error(t, err)
		require.Nil(t, pid)

		require.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("with event UnmarshalNew failure during recovery", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))

		// write an event with an unknown TypeUrl so UnmarshalNew fails
		event := &egopb.Event{
			PersistenceId:  persistenceID,
			SequenceNumber: 1,
			Event:          &anypb.Any{TypeUrl: "type.googleapis.com/unknown.TypeThatDoesNotExist", Value: []byte{}},
			Timestamp:      time.Now().Unix(),
		}
		require.NoError(t, eventStore.WriteEvents(ctx, persistence.Unscoped(), []*egopb.Event{event}, persistence.Unconditional()))

		eventStream := eventstream.New()

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
		require.Error(t, err)
		require.Nil(t, pid)

		require.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("with HandleEvent failure during recovery", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		persistenceID := uuid.NewString()
		eventStream := eventstream.New()

		require.NoError(t, eventStore.Connect(ctx))
		pause.For(time.Second)

		// pre-write an event that the behavior will fail to handle
		eventAny, err := anypb.New(&testpb.AccountCreated{AccountId: persistenceID, AccountBalance: 100})
		require.NoError(t, err)
		event := &egopb.Event{
			PersistenceId:  persistenceID,
			SequenceNumber: 1,
			Event:          eventAny,
			Timestamp:      time.Now().Unix(),
		}
		require.NoError(t, eventStore.WriteEvents(ctx, persistence.Unscoped(), []*egopb.Event{event}, persistence.Unconditional()))

		// use a behavior that returns an error from HandleEvent
		behavior := NewFailingHandleEventBehavior(persistenceID)

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
		require.Error(t, err)
		require.Nil(t, pid)

		require.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("with event encryption failure during command processing", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))
		pause.For(time.Second)

		eventStream := eventstream.New()

		encryptor := new(mockencryption.Encryptor)
		encryptor.EXPECT().Encrypt(mock.Anything, persistenceID, mock.Anything).Return(nil, "", assert.AnError)

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewEncryptor(encryptor),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		reply, err := goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500.00}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_ErrorReply), commandReply.GetReply())

		require.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("with snapshot encryption failure during command processing", func(t *testing.T) {
		// Snapshot encryption failures are logged by the snapshot writer child
		// actor but do not fail the command. Snapshots are an optimization for
		// faster recovery, not a correctness requirement. The command succeeds
		// with a state reply.
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		snapshotStore := testkit.NewSnapshotStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))
		require.NoError(t, snapshotStore.Connect(ctx))
		pause.For(time.Second)

		eventStream := eventstream.New()

		encryptor := new(mockencryption.Encryptor)
		// event encryption (parent) succeeds; snapshot encryption (child) fails
		encryptor.EXPECT().Encrypt(mock.Anything, persistenceID, mock.Anything).Return([]byte("ciphertext"), "key-1", nil).Once()
		encryptor.EXPECT().Encrypt(mock.Anything, persistenceID, mock.Anything).Return(nil, "", assert.AnError).Maybe()

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewSnapshotStore(snapshotStore),
				extensions.NewEncryptor(encryptor),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		entityCfg := &extensions.EntityConfig{SnapshotInterval: 1}
		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior, entityCfg), goakt.WithLongLived(), goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		reply, err := goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500.00}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		state := commandReply.GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, 1, state.StateReply.GetSequenceNumber())

		// allow time for the child snapshot writer to process
		pause.For(time.Second)

		// verify no snapshot was written since encryption failed
		snap, err := snapshotStore.GetLatestSnapshot(ctx, persistence.Unscoped(), persistenceID)
		require.NoError(t, err)
		assert.Nil(t, snap)

		require.NoError(t, eventStore.Disconnect(ctx))
		require.NoError(t, snapshotStore.Disconnect(ctx))
		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("with DeleteEvents error in retention policy does not crash", func(t *testing.T) {
		ctx := context.TODO()

		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)
		eventStream := eventstream.New()

		eventStore := new(mocks.EventsStore)
		eventStore.EXPECT().Ping(mock.Anything).Return(nil)
		eventStore.EXPECT().GetLatestEvent(mock.Anything, persistence.Unscoped(), persistenceID).Return(nil, nil)
		eventStore.EXPECT().WriteEvents(mock.Anything, persistence.Unscoped(), mock.Anything, mock.Anything).Return(nil)
		eventStore.EXPECT().DeleteEvents(mock.Anything, persistence.Unscoped(), persistenceID, uint64(2)).Return(assert.AnError)

		snapshotStore := new(mocks.SnapshotStore)
		snapshotStore.EXPECT().Ping(mock.Anything).Return(nil)
		snapshotStore.EXPECT().GetLatestSnapshot(mock.Anything, persistence.Unscoped(), persistenceID).Return(nil, nil)
		snapshotStore.EXPECT().WriteSnapshot(mock.Anything, persistence.Unscoped(), mock.Anything).Return(nil)

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewSnapshotStore(snapshotStore),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		entityCfg := &extensions.EntityConfig{
			SnapshotInterval:       2,
			HasRetentionPolicy:     true,
			DeleteEventsOnSnapshot: true,
			EventsRetentionCount:   0,
		}
		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior, entityCfg), goakt.WithLongLived(), goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		// first command
		reply, err := goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500.00}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)
		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		// second command triggers snapshot interval (2) and then DeleteEvents which errors
		reply, err = goakt.Ask(ctx, pid, &testpb.CreditAccount{AccountId: persistenceID, Balance: 100}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		// actor must still be alive: error is only logged
		commandReply = reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("with DeleteSnapshots error in retention policy does not crash", func(t *testing.T) {
		ctx := context.TODO()

		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)
		eventStream := eventstream.New()

		eventStore := new(mocks.EventsStore)
		eventStore.EXPECT().Ping(mock.Anything).Return(nil)
		eventStore.EXPECT().GetLatestEvent(mock.Anything, persistence.Unscoped(), persistenceID).Return(nil, nil)
		eventStore.EXPECT().WriteEvents(mock.Anything, persistence.Unscoped(), mock.Anything, mock.Anything).Return(nil).Times(4)

		snapshotStore := new(mocks.SnapshotStore)
		snapshotStore.EXPECT().Ping(mock.Anything).Return(nil)
		snapshotStore.EXPECT().GetLatestSnapshot(mock.Anything, persistence.Unscoped(), persistenceID).Return(nil, nil)
		snapshotStore.EXPECT().WriteSnapshot(mock.Anything, persistence.Unscoped(), mock.Anything).Return(nil).Times(2)
		snapshotStore.EXPECT().DeleteSnapshots(mock.Anything, persistence.Unscoped(), persistenceID, uint64(2)).Return(assert.AnError)

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewSnapshotStore(snapshotStore),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		entityCfg := &extensions.EntityConfig{
			SnapshotInterval:          2,
			HasRetentionPolicy:        true,
			DeleteSnapshotsOnSnapshot: true,
		}
		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior, entityCfg), goakt.WithLongLived(), goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		// commands 1 and 2: first snapshot at seq 2
		reply, err := goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500.00}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		reply, err = goakt.Ask(ctx, pid, &testpb.CreditAccount{AccountId: persistenceID, Balance: 100}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		// commands 3 and 4: second snapshot at seq 4 → DeleteSnapshots is called and errors
		reply, err = goakt.Ask(ctx, pid, &testpb.CreditAccount{AccountId: persistenceID, Balance: 50}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		reply, err = goakt.Ask(ctx, pid, &testpb.CreditAccount{AccountId: persistenceID, Balance: 25}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		// actor still alive after logged error
		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		state := commandReply.GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, 4, state.StateReply.GetSequenceNumber())

		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("with unhandled non-command message does not crash", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))
		pause.For(time.Second)

		eventStream := eventstream.New()

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior),
			goakt.WithLongLived(), goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		err = goakt.Tell(ctx, pid, new(egopb.NoReply))
		require.NoError(t, err)

		pause.For(500 * time.Millisecond)

		reply, err := goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		require.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("with persistEvents write failure shuts down actor", func(t *testing.T) {
		ctx := context.TODO()

		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)
		eventStream := eventstream.New()

		eventStore := new(mocks.EventsStore)
		eventStore.EXPECT().Ping(mock.Anything).Return(nil)
		eventStore.EXPECT().GetLatestEvent(mock.Anything, persistence.Unscoped(), persistenceID).Return(nil, nil)
		eventStore.EXPECT().WriteEvents(mock.Anything, persistence.Unscoped(), mock.Anything, mock.Anything).Return(assert.AnError)

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior),
			goakt.WithLongLived(), goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		reply, err := goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_ErrorReply), commandReply.GetReply())

		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})
}

// noopEventAdapter is a no-op event adapter that passes events through unchanged
type noopEventAdapter struct{}

func (a *noopEventAdapter) Adapt(event *anypb.Any, _ uint64) (*anypb.Any, error) {
	return event, nil
}

// FailingHandleEventBehavior is a test behavior whose HandleEvent always returns an error.
type FailingHandleEventBehavior struct {
	id string
}

// NewFailingHandleEventBehavior creates a FailingHandleEventBehavior.
func NewFailingHandleEventBehavior(id string) *FailingHandleEventBehavior {
	return &FailingHandleEventBehavior{id: id}
}

func (f *FailingHandleEventBehavior) ID() string { return f.id }

func (f *FailingHandleEventBehavior) InitialState() State {
	return new(testpb.Account)
}

func (f *FailingHandleEventBehavior) HandleCommand(_ context.Context, command Command, _ State) ([]Event, error) {
	switch command.(type) {
	case *testpb.CreateAccount:
		return []Event{&testpb.AccountCreated{AccountId: f.id, AccountBalance: 100}}, nil
	}
	return nil, nil
}

func (f *FailingHandleEventBehavior) HandleEvent(_ context.Context, _ Event, _ State) (State, error) {
	return nil, assert.AnError
}

func (f *FailingHandleEventBehavior) MarshalBinary() ([]byte, error) {
	return proto.Marshal(&egopb.StateReply{PersistenceId: f.id})
}

func (f *FailingHandleEventBehavior) UnmarshalBinary(data []byte) error {
	msg := new(egopb.StateReply)
	if err := proto.Unmarshal(data, msg); err != nil {
		return err
	}
	f.id = msg.GetPersistenceId()
	return nil
}

// TestEventSourcedActorGetStateDuringPersist is the permanent regression
// suite for the P1 read-after-write consistency bug: GetStateCommand must
// never observe entity.currentState while a direct (non-batched) persist
// write is unsettled (phasePersisting/phaseDirectReplying), and it must
// never receive a persist failure addressed to a different command.
func TestEventSourcedActorGetStateDuringPersist(t *testing.T) {
	type asyncReply struct {
		reply any
		err   error
	}

	t.Run("read during persist waits for confirmation then returns new state", func(t *testing.T) {
		ctx := context.TODO()

		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)
		eventStream := eventstream.New()

		started := make(chan struct{})
		release := make(chan struct{})

		eventStore := new(mocks.EventsStore)
		eventStore.EXPECT().Ping(mock.Anything).Return(nil)
		eventStore.EXPECT().GetLatestEvent(mock.Anything, persistence.Unscoped(), persistenceID).Return(nil, nil)
		eventStore.EXPECT().WriteEvents(mock.Anything, persistence.Unscoped(), mock.Anything, mock.Anything).Return(nil).Once()
		eventStore.EXPECT().WriteEvents(mock.Anything, persistence.Unscoped(), mock.Anything, mock.Anything).
			Run(func(_ context.Context, _ persistence.Scope, _ []*egopb.Event, _ persistence.WritePrecondition) {
				close(started)
				<-release
			}).
			Return(nil).Once()

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		reply, err := goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
		require.NoError(t, err)
		createState := reply.(*egopb.CommandReply).GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, 1, createState.StateReply.GetSequenceNumber())

		creditDone := make(chan asyncReply, 1)
		go func() {
			r, e := goakt.Ask(ctx, pid, &testpb.CreditAccount{AccountId: persistenceID, Balance: 100}, 5*time.Second)
			creditDone <- asyncReply{r, e}
		}()

		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for the credit write to enter phasePersisting")
		}

		stateDone := make(chan asyncReply, 1)
		go func() {
			r, e := goakt.Ask(ctx, pid, &egopb.GetStateCommand{}, 5*time.Second)
			stateDone <- asyncReply{r, e}
		}()

		// give the GetStateCommand time to reach the mailbox and stash itself
		// behind the in-flight write before we let that write complete.
		pause.For(300 * time.Millisecond)

		select {
		case <-stateDone:
			t.Fatal("GetStateCommand replied before the in-flight persist write was confirmed")
		default:
		}

		close(release)

		var credit asyncReply
		select {
		case credit = <-creditDone:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for the credit command reply")
		}
		require.NoError(t, credit.err)
		creditState := credit.reply.(*egopb.CommandReply).GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, 2, creditState.StateReply.GetSequenceNumber())

		var got asyncReply
		select {
		case got = <-stateDone:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for the deferred GetStateCommand reply")
		}
		require.NoError(t, got.err)
		commandReply := got.reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())
		stateReply := commandReply.GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, 2, stateReply.StateReply.GetSequenceNumber())

		resultingState := new(testpb.Account)
		require.NoError(t, stateReply.StateReply.GetState().UnmarshalTo(resultingState))
		assert.Equal(t, 600.00, resultingState.GetAccountBalance())

		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("two concurrent commands preserve order and correct recipient", func(t *testing.T) {
		ctx := context.TODO()

		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)
		eventStream := eventstream.New()

		started := make(chan struct{})
		release := make(chan struct{})

		eventStore := new(mocks.EventsStore)
		eventStore.EXPECT().Ping(mock.Anything).Return(nil)
		eventStore.EXPECT().GetLatestEvent(mock.Anything, persistence.Unscoped(), persistenceID).Return(nil, nil)
		eventStore.EXPECT().WriteEvents(mock.Anything, persistence.Unscoped(), mock.Anything, mock.Anything).Return(nil).Once()
		eventStore.EXPECT().WriteEvents(mock.Anything, persistence.Unscoped(), mock.Anything, mock.Anything).
			Run(func(_ context.Context, _ persistence.Scope, _ []*egopb.Event, _ persistence.WritePrecondition) {
				close(started)
				<-release
			}).
			Return(nil).Once()
		eventStore.EXPECT().WriteEvents(mock.Anything, persistence.Unscoped(), mock.Anything, mock.Anything).Return(nil).Once()

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		_, err = goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
		require.NoError(t, err)

		aDone := make(chan asyncReply, 1)
		go func() {
			r, e := goakt.Ask(ctx, pid, &testpb.CreditAccount{AccountId: persistenceID, Balance: 100}, 5*time.Second)
			aDone <- asyncReply{r, e}
		}()

		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for command A to enter phasePersisting")
		}

		bDone := make(chan asyncReply, 1)
		go func() {
			r, e := goakt.Ask(ctx, pid, &testpb.CreditAccount{AccountId: persistenceID, Balance: 50}, 5*time.Second)
			bDone <- asyncReply{r, e}
		}()

		// give command B time to arrive and stash behind the in-flight write.
		pause.For(300 * time.Millisecond)
		close(release)

		var a, b asyncReply
		select {
		case a = <-aDone:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for command A's reply")
		}
		select {
		case b = <-bDone:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for command B's reply")
		}

		require.NoError(t, a.err)
		aState := a.reply.(*egopb.CommandReply).GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, 2, aState.StateReply.GetSequenceNumber())
		aAccount := new(testpb.Account)
		require.NoError(t, aState.StateReply.GetState().UnmarshalTo(aAccount))
		assert.Equal(t, 600.00, aAccount.GetAccountBalance())

		require.NoError(t, b.err)
		bState := b.reply.(*egopb.CommandReply).GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, 3, bState.StateReply.GetSequenceNumber())
		bAccount := new(testpb.Account)
		require.NoError(t, bState.StateReply.GetState().UnmarshalTo(bAccount))
		assert.Equal(t, 650.00, bAccount.GetAccountBalance())

		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("persistence error reaches the originating command only", func(t *testing.T) {
		ctx := context.TODO()

		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)
		eventStream := eventstream.New()

		started := make(chan struct{})
		release := make(chan struct{})

		eventStore := new(mocks.EventsStore)
		eventStore.EXPECT().Ping(mock.Anything).Return(nil)
		eventStore.EXPECT().GetLatestEvent(mock.Anything, persistence.Unscoped(), persistenceID).Return(nil, nil)
		eventStore.EXPECT().WriteEvents(mock.Anything, persistence.Unscoped(), mock.Anything, mock.Anything).Return(nil).Once()
		eventStore.EXPECT().WriteEvents(mock.Anything, persistence.Unscoped(), mock.Anything, mock.Anything).
			Run(func(_ context.Context, _ persistence.Scope, _ []*egopb.Event, _ persistence.WritePrecondition) {
				close(started)
				<-release
			}).
			Return(assert.AnError).Once()

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior), goakt.WithLongLived(), goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		_, err = goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
		require.NoError(t, err)

		creditDone := make(chan asyncReply, 1)
		go func() {
			r, e := goakt.Ask(ctx, pid, &testpb.CreditAccount{AccountId: persistenceID, Balance: 100}, 5*time.Second)
			creditDone <- asyncReply{r, e}
		}()

		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for the credit write to enter phasePersisting")
		}

		stateDone := make(chan asyncReply, 1)
		go func() {
			r, e := goakt.Ask(ctx, pid, &egopb.GetStateCommand{}, 5*time.Second)
			stateDone <- asyncReply{r, e}
		}()

		// give the deferred read time to arrive and stash behind the failing write.
		pause.For(300 * time.Millisecond)
		close(release)

		var credit asyncReply
		select {
		case credit = <-creditDone:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for the credit command reply")
		}
		require.NoError(t, credit.err)
		creditReply := credit.reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_ErrorReply), creditReply.GetReply())
		errorReply := creditReply.GetReply().(*egopb.CommandReply_ErrorReply)
		assert.Contains(t, errorReply.ErrorReply.GetMessage(), assert.AnError.Error())

		// The originating command's persist failure sets directShutdown, so the
		// actor stops itself (pre-existing fail-fast behavior, unrelated to this
		// fix) before it can dispatch the redelivered, deferred GetStateCommand.
		// The contract this test protects is narrower than "the deferred read
		// gets a normal reply": it must never receive the error reply meant for
		// the command that actually failed. An Ask timeout against a
		// now-stopped actor satisfies that (it is clearly not the mistaken
		// error), whereas an ErrorReply carrying assert.AnError's message would
		// prove the two commands' responses got cross-wired.
		var deferred asyncReply
		select {
		case deferred = <-stateDone:
		case <-time.After(8 * time.Second):
			t.Fatal("timed out waiting for the deferred GetStateCommand to settle")
		}
		if deferred.err == nil {
			deferredReply := deferred.reply.(*egopb.CommandReply)
			if errorReply, ok := deferredReply.GetReply().(*egopb.CommandReply_ErrorReply); ok {
				assert.NotContains(t, errorReply.ErrorReply.GetMessage(), assert.AnError.Error(),
					"deferred GetStateCommand must not receive the originating command's persist error")
			}
		}

		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})
}

func TestEventSourcedActorBatch(t *testing.T) {
	t.Run("sequential commands flush by timer", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))
		pause.For(time.Second)

		eventStream := eventstream.New()

		entityCfg := &extensions.EntityConfig{
			BatchThreshold:   100,
			BatchFlushWindow: 100 * time.Millisecond,
		}

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)

		err = actorSystem.Start(ctx)
		require.NoError(t, err)
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, entityCfg),
			goakt.WithLongLived(),
			goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		reply, err := goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		state := commandReply.GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, 1, state.StateReply.GetSequenceNumber())

		resultingState := new(testpb.Account)
		require.NoError(t, state.StateReply.GetState().UnmarshalTo(resultingState))
		assert.EqualValues(t, 500, resultingState.GetAccountBalance())

		reply, err = goakt.Ask(ctx, pid, &testpb.CreditAccount{AccountId: persistenceID, Balance: 250}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		commandReply = reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		state = commandReply.GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, 2, state.StateReply.GetSequenceNumber())

		resultingState = new(testpb.Account)
		require.NoError(t, state.StateReply.GetState().UnmarshalTo(resultingState))
		assert.EqualValues(t, 750, resultingState.GetAccountBalance())

		require.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()
		pause.For(time.Second)
		require.NoError(t, actorSystem.Stop(ctx))
	})
	t.Run("concurrent commands flush by threshold", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))
		pause.For(time.Second)

		eventStream := eventstream.New()

		entityCfg := &extensions.EntityConfig{
			BatchThreshold:   2,
			BatchFlushWindow: 10 * time.Second,
		}

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)

		err = actorSystem.Start(ctx)
		require.NoError(t, err)
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, entityCfg),
			goakt.WithLongLived(),
			goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		var wg sync.WaitGroup
		replies := make([]any, 2)
		errs := make([]error, 2)

		wg.Add(2)
		go func() {
			defer wg.Done()
			replies[0], errs[0] = goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500}, 10*time.Second)
		}()
		go func() {
			defer wg.Done()
			replies[1], errs[1] = goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 300}, 10*time.Second)
		}()

		wg.Wait()

		require.NoError(t, errs[0])
		require.NoError(t, errs[1])

		for i, r := range replies {
			cr := r.(*egopb.CommandReply)
			require.IsType(t, new(egopb.CommandReply_StateReply), cr.GetReply(), "reply %d should be state reply", i)
		}

		stateReply, err := goakt.Ask(ctx, pid, &egopb.GetStateCommand{}, 5*time.Second)
		require.NoError(t, err)

		cr := stateReply.(*egopb.CommandReply)
		sr := cr.GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, 2, sr.StateReply.GetSequenceNumber())

		require.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()
		pause.For(time.Second)
		require.NoError(t, actorSystem.Stop(ctx))
	})
	t.Run("no-event command replies immediately in batch mode", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))
		pause.For(time.Second)

		eventStream := eventstream.New()

		entityCfg := &extensions.EntityConfig{
			BatchThreshold:   100,
			BatchFlushWindow: 100 * time.Millisecond,
		}

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)

		err = actorSystem.Start(ctx)
		require.NoError(t, err)
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, entityCfg),
			goakt.WithLongLived(),
			goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		reply, err := goakt.Ask(ctx, pid, &testpb.TestNoEvent{}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		state := commandReply.GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, 0, state.StateReply.GetSequenceNumber())

		require.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()
		pause.For(time.Second)
		require.NoError(t, actorSystem.Stop(ctx))
	})
	t.Run("error command replies immediately in batch mode", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))
		pause.For(time.Second)

		eventStream := eventstream.New()

		entityCfg := &extensions.EntityConfig{
			BatchThreshold:   100,
			BatchFlushWindow: 100 * time.Millisecond,
		}

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)

		err = actorSystem.Start(ctx)
		require.NoError(t, err)
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, entityCfg),
			goakt.WithLongLived(),
			goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		reply, err := goakt.Ask(ctx, pid, &testpb.CreditAccount{AccountId: "wrong-id", Balance: 100}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_ErrorReply), commandReply.GetReply())

		errorReply := commandReply.GetReply().(*egopb.CommandReply_ErrorReply)
		assert.Equal(t, "command sent to the wrong entity", errorReply.ErrorReply.GetMessage())

		require.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()
		pause.For(time.Second)
		require.NoError(t, actorSystem.Stop(ctx))
	})
	t.Run("batch with snapshot boundary crossing", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		snapshotStore := testkit.NewSnapshotStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))
		require.NoError(t, snapshotStore.Connect(ctx))
		pause.For(time.Second)

		eventStream := eventstream.New()

		entityCfg := &extensions.EntityConfig{
			SnapshotInterval: 2,
			BatchThreshold:   100,
			BatchFlushWindow: 100 * time.Millisecond,
		}

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewSnapshotStore(snapshotStore),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)

		err = actorSystem.Start(ctx)
		require.NoError(t, err)
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, entityCfg),
			goakt.WithLongLived(),
			goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		reply, err := goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
		require.NoError(t, err)
		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		reply, err = goakt.Ask(ctx, pid, &testpb.CreditAccount{AccountId: persistenceID, Balance: 100}, 5*time.Second)
		require.NoError(t, err)
		commandReply = reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		state := commandReply.GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, 2, state.StateReply.GetSequenceNumber())

		pause.For(time.Second)

		snap, err := snapshotStore.GetLatestSnapshot(ctx, persistence.Unscoped(), persistenceID)
		require.NoError(t, err)
		require.NotNil(t, snap)
		assert.EqualValues(t, 2, snap.GetSequenceNumber())

		require.NoError(t, eventStore.Disconnect(ctx))
		require.NoError(t, snapshotStore.Disconnect(ctx))
		eventStream.Close()
		pause.For(time.Second)
		require.NoError(t, actorSystem.Stop(ctx))
	})
	t.Run("commands arriving during flush are stashed and processed", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))
		pause.For(time.Second)

		eventStream := eventstream.New()

		entityCfg := &extensions.EntityConfig{
			BatchThreshold:   1,
			BatchFlushWindow: time.Second,
		}

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)

		err = actorSystem.Start(ctx)
		require.NoError(t, err)
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, entityCfg),
			goakt.WithLongLived(),
			goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		var wg sync.WaitGroup
		const numCommands = 5
		replies := make([]any, numCommands)
		errs := make([]error, numCommands)

		reply, err := goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 100}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		wg.Add(numCommands)
		for i := range numCommands {
			go func(idx int) {
				defer wg.Done()
				replies[idx], errs[idx] = goakt.Ask(ctx, pid,
					&testpb.CreditAccount{AccountId: persistenceID, Balance: 10},
					10*time.Second)
			}(i)
		}

		wg.Wait()

		for i := range numCommands {
			require.NoError(t, errs[i], "command %d failed", i)
			cr := replies[i].(*egopb.CommandReply)
			require.IsType(t, new(egopb.CommandReply_StateReply), cr.GetReply(), "command %d should be state reply", i)
		}

		stateReply, err := goakt.Ask(ctx, pid, &egopb.GetStateCommand{}, 5*time.Second)
		require.NoError(t, err)

		cr := stateReply.(*egopb.CommandReply)
		sr := cr.GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, numCommands+1, sr.StateReply.GetSequenceNumber())

		resultingState := new(testpb.Account)
		require.NoError(t, sr.StateReply.GetState().UnmarshalTo(resultingState))
		assert.EqualValues(t, 100+float64(numCommands)*10, resultingState.GetAccountBalance())

		require.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()
		pause.For(time.Second)
		require.NoError(t, actorSystem.Stop(ctx))
	})
	t.Run("batch persist failure returns error replies and shuts down actor", func(t *testing.T) {
		ctx := context.TODO()

		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		eventStore := new(mocks.EventsStore)
		eventStore.EXPECT().Ping(mock.Anything).Return(nil)
		eventStore.EXPECT().GetLatestEvent(mock.Anything, persistence.Unscoped(), persistenceID).Return(nil, nil)
		eventStore.EXPECT().WriteEvents(mock.Anything, persistence.Unscoped(), mock.Anything, mock.Anything).Return(assert.AnError)

		eventStream := eventstream.New()

		entityCfg := &extensions.EntityConfig{
			BatchThreshold:   1,
			BatchFlushWindow: time.Second,
		}

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, entityCfg),
			goakt.WithLongLived(),
			goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		reply, err := goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_ErrorReply), commandReply.GetReply())

		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})
	t.Run("batch persist failure with telemetry records metrics and ends spans", func(t *testing.T) {
		ctx := context.TODO()

		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		eventStore := new(mocks.EventsStore)
		eventStore.EXPECT().Ping(mock.Anything).Return(nil)
		eventStore.EXPECT().GetLatestEvent(mock.Anything, persistence.Unscoped(), persistenceID).Return(nil, nil)
		eventStore.EXPECT().WriteEvents(mock.Anything, persistence.Unscoped(), mock.Anything, mock.Anything).Return(assert.AnError)

		eventStream := eventstream.New()

		noopTracer := tracenoop.NewTracerProvider().Tracer("test")
		noopMeter := noop.NewMeterProvider().Meter("test")

		entityCfg := &extensions.EntityConfig{
			BatchThreshold:   1,
			BatchFlushWindow: time.Second,
		}

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewTelemetryExtension(noopTracer, noopMeter),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, entityCfg),
			goakt.WithLongLived(),
			goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		reply, err := goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_ErrorReply), commandReply.GetReply())

		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})
	t.Run("batch mode with encryption failure in processAndBatch", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))
		pause.For(time.Second)

		eventStream := eventstream.New()

		encryptor := new(mockencryption.Encryptor)
		encryptor.EXPECT().Encrypt(mock.Anything, persistenceID, mock.Anything).Return(nil, "", assert.AnError)

		entityCfg := &extensions.EntityConfig{
			BatchThreshold:   100,
			BatchFlushWindow: 100 * time.Millisecond,
		}

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewEncryptor(encryptor),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, entityCfg),
			goakt.WithLongLived(),
			goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		reply, err := goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_ErrorReply), commandReply.GetReply())

		require.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})
	t.Run("batch mode with telemetry traces commands", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))
		pause.For(time.Second)

		eventStream := eventstream.New()

		noopTracer := tracenoop.NewTracerProvider().Tracer("test")
		noopMeter := noop.NewMeterProvider().Meter("test")

		entityCfg := &extensions.EntityConfig{
			BatchThreshold:   100,
			BatchFlushWindow: 100 * time.Millisecond,
		}

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewTelemetryExtension(noopTracer, noopMeter),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, entityCfg),
			goakt.WithLongLived(),
			goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		reply, err := goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		state := commandReply.GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, 1, state.StateReply.GetSequenceNumber())

		require.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()
		pause.For(time.Second)
		require.NoError(t, actorSystem.Stop(ctx))
	})
	t.Run("batch mode with telemetry handles no-event command", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))
		pause.For(time.Second)

		eventStream := eventstream.New()

		noopTracer := tracenoop.NewTracerProvider().Tracer("test")
		noopMeter := noop.NewMeterProvider().Meter("test")

		entityCfg := &extensions.EntityConfig{
			BatchThreshold:   100,
			BatchFlushWindow: 100 * time.Millisecond,
		}

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewTelemetryExtension(noopTracer, noopMeter),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, entityCfg),
			goakt.WithLongLived(),
			goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		reply, err := goakt.Ask(ctx, pid, &testpb.TestNoEvent{}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		require.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()
		pause.For(time.Second)
		require.NoError(t, actorSystem.Stop(ctx))
	})
	t.Run("batch mode with telemetry handles error command", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))
		pause.For(time.Second)

		eventStream := eventstream.New()

		noopTracer := tracenoop.NewTracerProvider().Tracer("test")
		noopMeter := noop.NewMeterProvider().Meter("test")

		entityCfg := &extensions.EntityConfig{
			BatchThreshold:   100,
			BatchFlushWindow: 100 * time.Millisecond,
		}

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewTelemetryExtension(noopTracer, noopMeter),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, entityCfg),
			goakt.WithLongLived(),
			goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		reply, err := goakt.Ask(ctx, pid, &testpb.CreditAccount{AccountId: "wrong-id", Balance: 100}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_ErrorReply), commandReply.GetReply())

		require.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()
		pause.For(time.Second)
		require.NoError(t, actorSystem.Stop(ctx))
	})
	t.Run("batch mode with default flush window when only threshold is set", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))
		pause.For(time.Second)

		eventStream := eventstream.New()

		entityCfg := &extensions.EntityConfig{
			BatchThreshold: 100,
		}

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, entityCfg),
			goakt.WithLongLived(),
			goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		reply, err := goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		state := commandReply.GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, 1, state.StateReply.GetSequenceNumber())

		require.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()
		pause.For(time.Second)
		require.NoError(t, actorSystem.Stop(ctx))
	})
	t.Run("unhandled non-command message in batch mode", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))
		pause.For(time.Second)

		eventStream := eventstream.New()

		entityCfg := &extensions.EntityConfig{
			BatchThreshold:   100,
			BatchFlushWindow: 100 * time.Millisecond,
		}

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, entityCfg),
			goakt.WithLongLived(),
			goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		err = goakt.Tell(ctx, pid, new(egopb.NoReply))
		require.NoError(t, err)

		pause.For(500 * time.Millisecond)

		reply, err := goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		require.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()
		pause.For(time.Second)
		require.NoError(t, actorSystem.Stop(ctx))
	})
	t.Run("batch mode with encryption failure and telemetry ends span", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))
		pause.For(time.Second)

		eventStream := eventstream.New()

		noopTracer := tracenoop.NewTracerProvider().Tracer("test")
		noopMeter := noop.NewMeterProvider().Meter("test")

		encryptor := new(mockencryption.Encryptor)
		encryptor.EXPECT().Encrypt(mock.Anything, persistenceID, mock.Anything).Return(nil, "", assert.AnError)

		entityCfg := &extensions.EntityConfig{
			BatchThreshold:   100,
			BatchFlushWindow: 100 * time.Millisecond,
		}

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewEncryptor(encryptor),
				extensions.NewTelemetryExtension(noopTracer, noopMeter),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, entityCfg),
			goakt.WithLongLived(),
			goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		reply, err := goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_ErrorReply), commandReply.GetReply())

		require.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})
	t.Run("multiple commands batch with telemetry covers reply span and timer dedup", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))
		pause.For(time.Second)

		eventStream := eventstream.New()

		noopTracer := tracenoop.NewTracerProvider().Tracer("test")
		noopMeter := noop.NewMeterProvider().Meter("test")

		entityCfg := &extensions.EntityConfig{
			BatchThreshold:   3,
			BatchFlushWindow: 10 * time.Second,
		}

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewTelemetryExtension(noopTracer, noopMeter),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, entityCfg),
			goakt.WithLongLived(),
			goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		var wg sync.WaitGroup
		replies := make([]any, 3)
		errs := make([]error, 3)

		wg.Add(3)
		go func() {
			defer wg.Done()
			replies[0], errs[0] = goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500}, 10*time.Second)
		}()
		go func() {
			defer wg.Done()
			replies[1], errs[1] = goakt.Ask(ctx, pid, &testpb.CreditAccount{AccountId: persistenceID, Balance: 100}, 10*time.Second)
		}()
		go func() {
			defer wg.Done()
			replies[2], errs[2] = goakt.Ask(ctx, pid, &testpb.CreditAccount{AccountId: persistenceID, Balance: 50}, 10*time.Second)
		}()

		wg.Wait()

		for i, e := range errs {
			require.NoError(t, e, "command %d failed", i)
			cr := replies[i].(*egopb.CommandReply)
			require.IsType(t, new(egopb.CommandReply_StateReply), cr.GetReply(), "command %d should be state reply", i)
		}

		stateReply, err := goakt.Ask(ctx, pid, &egopb.GetStateCommand{}, 5*time.Second)
		require.NoError(t, err)

		cr := stateReply.(*egopb.CommandReply)
		sr := cr.GetReply().(*egopb.CommandReply_StateReply)
		assert.EqualValues(t, 3, sr.StateReply.GetSequenceNumber())

		require.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()
		pause.For(time.Second)
		require.NoError(t, actorSystem.Stop(ctx))
	})
	t.Run("batch with snapshot and telemetry", func(t *testing.T) {
		ctx := context.TODO()

		eventStore := testkit.NewEventsStore()
		snapshotStore := testkit.NewSnapshotStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))
		require.NoError(t, snapshotStore.Connect(ctx))
		pause.For(time.Second)

		eventStream := eventstream.New()

		noopTracer := tracenoop.NewTracerProvider().Tracer("test")
		noopMeter := noop.NewMeterProvider().Meter("test")

		entityCfg := &extensions.EntityConfig{
			SnapshotInterval: 2,
			BatchThreshold:   2,
			BatchFlushWindow: 10 * time.Second,
		}

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewSnapshotStore(snapshotStore),
				extensions.NewTelemetryExtension(noopTracer, noopMeter),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, entityCfg),
			goakt.WithLongLived(),
			goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		var wg sync.WaitGroup
		replies := make([]any, 2)
		errs := make([]error, 2)

		wg.Add(2)
		go func() {
			defer wg.Done()
			replies[0], errs[0] = goakt.Ask(ctx, pid, &testpb.CreateAccount{AccountBalance: 500}, 10*time.Second)
		}()
		go func() {
			defer wg.Done()
			replies[1], errs[1] = goakt.Ask(ctx, pid, &testpb.CreditAccount{AccountId: persistenceID, Balance: 100}, 10*time.Second)
		}()

		wg.Wait()

		require.NoError(t, errs[0])
		require.NoError(t, errs[1])

		for i, r := range replies {
			cr := r.(*egopb.CommandReply)
			require.IsType(t, new(egopb.CommandReply_StateReply), cr.GetReply(), "reply %d should be state reply", i)
		}

		pause.For(time.Second)

		snap, err := snapshotStore.GetLatestSnapshot(ctx, persistence.Unscoped(), persistenceID)
		require.NoError(t, err)
		require.NotNil(t, snap)
		assert.EqualValues(t, 2, snap.GetSequenceNumber())

		require.NoError(t, eventStore.Disconnect(ctx))
		require.NoError(t, snapshotStore.Disconnect(ctx))
		eventStream.Close()
		pause.For(time.Second)
		require.NoError(t, actorSystem.Stop(ctx))
	})
	// Test: batch trace spans are properly connected
	//
	// Verifies end-to-end trace context propagation through the batch processing
	// pipeline when multiple commands are processed as a single batch.
	//
	// Setup:
	//   - A real TracerProvider with an InMemoryExporter captures all emitted spans.
	//   - The global TextMapPropagator is configured with W3C TraceContext + Baggage,
	//     mirroring what Engine.Start does for the GoAkt context propagator
	//     (otelContextPropagator) to inject/extract trace context across actor boundaries.
	//   - Batch threshold is set to 3 with a long flush window so the batch flushes
	//     only when the threshold is reached (not by timer).
	//   - A parent span ("test.batch.parent") is created and its context is passed
	//     through goakt.Ask to the actor, simulating an inbound traced request.
	//
	// Assertions:
	//   - Exactly 3 "ego.command" spans are produced (one per batched command).
	//   - Every command span shares the same TraceID as the parent span, proving
	//     trace context flows from the caller through GoAkt into processAndBatch.
	//   - Every command span's Parent.SpanID equals the parent span's SpanID,
	//     proving direct parent-child linkage.
	//   - Every command span has a non-zero EndTime, proving the span lifecycle
	//     completes after replyFromBatch sends the pre-computed reply.
	//   - Every command span carries the ego.persistence_id and ego.command_type
	//     attributes set by processAndBatch.
	//   - All command spans have distinct SpanIDs (no accidental reuse).
	t.Run("batch trace spans are properly connected", func(t *testing.T) {
		ctx := context.TODO()

		// Set up the global text map propagator the same way the Engine does.
		// This is required for the GoAkt context propagator (otelContextPropagator)
		// to correctly inject/extract trace context across actor boundaries.
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))

		// Create an in-memory span exporter so we can inspect recorded spans.
		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(exporter),
			sdktrace.WithSampler(sdktrace.AlwaysSample()),
		)
		defer func() { _ = tp.Shutdown(ctx) }()

		tracer := tp.Tracer("ego-test")
		meter := noop.NewMeterProvider().Meter("test")

		eventStore := testkit.NewEventsStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))
		pause.For(time.Second)

		eventStream := eventstream.New()

		entityCfg := &extensions.EntityConfig{
			BatchThreshold:   3,
			BatchFlushWindow: 10 * time.Second,
		}

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewTelemetryExtension(tracer, meter),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, entityCfg),
			goakt.WithLongLived(),
			goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		// Create a parent span to verify that child spans are properly linked.
		parentCtx, parentSpan := tracer.Start(ctx, "test.batch.parent")

		var wg sync.WaitGroup
		replies := make([]any, 3)
		errs := make([]error, 3)

		wg.Add(3)
		go func() {
			defer wg.Done()
			replies[0], errs[0] = goakt.Ask(parentCtx, pid, &testpb.CreateAccount{AccountBalance: 500}, 10*time.Second)
		}()
		go func() {
			defer wg.Done()
			replies[1], errs[1] = goakt.Ask(parentCtx, pid, &testpb.CreditAccount{AccountId: persistenceID, Balance: 100}, 10*time.Second)
		}()
		go func() {
			defer wg.Done()
			replies[2], errs[2] = goakt.Ask(parentCtx, pid, &testpb.CreditAccount{AccountId: persistenceID, Balance: 50}, 10*time.Second)
		}()

		wg.Wait()
		parentSpan.End()

		for i, e := range errs {
			require.NoError(t, e, "command %d failed", i)
			cr := replies[i].(*egopb.CommandReply)
			require.IsType(t, new(egopb.CommandReply_StateReply), cr.GetReply(), "command %d should be state reply", i)
		}

		// Force-flush so all ended spans are exported.
		require.NoError(t, tp.ForceFlush(ctx))

		spans := exporter.GetSpans()

		// Collect "ego.command" spans (one per batched command).
		var commandSpans []tracetest.SpanStub
		var parentStub *tracetest.SpanStub
		for i := range spans {
			switch spans[i].Name {
			case "ego.command":
				commandSpans = append(commandSpans, spans[i])
			case "test.batch.parent":
				parentStub = &spans[i]
			}
		}

		require.NotNil(t, parentStub, "parent span should be exported")
		require.Len(t, commandSpans, 3, "each command in the batch should produce an ego.command span")

		parentTraceID := parentStub.SpanContext.TraceID()
		parentSpanID := parentStub.SpanContext.SpanID()

		for i, cs := range commandSpans {
			// Assert: all command spans belong to the same trace as the parent.
			assert.Equal(t, parentTraceID, cs.SpanContext.TraceID(),
				"command span %d should share the parent trace ID", i)

			// Assert: each command span is a direct child of the parent span.
			assert.Equal(t, parentSpanID, cs.Parent.SpanID(),
				"command span %d should be a child of the parent span", i)

			// Assert: the span has been ended (EndTime is set) after replyFromBatch.
			assert.False(t, cs.EndTime.IsZero(),
				"command span %d should be ended", i)

			// Assert: required observability attributes are present.
			attrMap := make(map[string]string)
			for _, attr := range cs.Attributes {
				attrMap[string(attr.Key)] = attr.Value.AsString()
			}
			assert.Equal(t, persistenceID, attrMap["ego.persistence_id"],
				"command span %d should have ego.persistence_id attribute", i)
			assert.NotEmpty(t, attrMap["ego.command_type"],
				"command span %d should have ego.command_type attribute", i)
		}

		// Assert: each command span has a unique span ID (no accidental reuse).
		spanIDs := make(map[trace.SpanID]struct{})
		for _, cs := range commandSpans {
			_, exists := spanIDs[cs.SpanContext.SpanID()]
			assert.False(t, exists, "command spans should have unique span IDs")
			spanIDs[cs.SpanContext.SpanID()] = struct{}{}
		}

		require.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()
		pause.For(time.Second)
		require.NoError(t, actorSystem.Stop(ctx))
	})
	// Test: batch trace spans are ended on error
	//
	// Verifies that when HandleCommand returns an error the "ego.command" span
	// is still created and properly ended, rather than being leaked.
	//
	// Setup:
	//   - Real TracerProvider + InMemoryExporter + global TextMapPropagator.
	//   - A single CreditAccount command is sent with a wrong account ID,
	//     causing HandleCommand to return an error inside processAndBatch.
	//   - The parent span ("test.error.parent") provides the trace context.
	//
	// Assertions:
	//   - Exactly 1 "ego.command" span is produced despite the error.
	//   - The span has a non-zero EndTime, proving processAndBatch called
	//     span.End() on the error path before sendErrorReply.
	//   - The span shares the parent's TraceID (trace context propagated).
	//   - The span's Parent.SpanID equals the parent span's SpanID
	//     (direct parent-child linkage preserved even on failure).
	t.Run("batch trace spans are ended on error", func(t *testing.T) {
		ctx := context.TODO()

		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))

		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(exporter),
			sdktrace.WithSampler(sdktrace.AlwaysSample()),
		)
		defer func() { _ = tp.Shutdown(ctx) }()

		tracer := tp.Tracer("ego-test")
		meter := noop.NewMeterProvider().Meter("test")

		eventStore := testkit.NewEventsStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))
		pause.For(time.Second)

		eventStream := eventstream.New()

		entityCfg := &extensions.EntityConfig{
			BatchThreshold:   100,
			BatchFlushWindow: 100 * time.Millisecond,
		}

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewTelemetryExtension(tracer, meter),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, entityCfg),
			goakt.WithLongLived(),
			goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		parentCtx, parentSpan := tracer.Start(ctx, "test.error.parent")

		// Send a command that will fail (CreditAccount on non-existent account).
		reply, err := goakt.Ask(parentCtx, pid, &testpb.CreditAccount{AccountId: "wrong-id", Balance: 100}, 5*time.Second)
		parentSpan.End()

		require.NoError(t, err)
		require.NotNil(t, reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_ErrorReply), commandReply.GetReply())

		require.NoError(t, tp.ForceFlush(ctx))

		spans := exporter.GetSpans()

		var commandSpans []tracetest.SpanStub
		for i := range spans {
			if spans[i].Name == "ego.command" {
				commandSpans = append(commandSpans, spans[i])
			}
		}

		// Assert: a span is still produced even though the command failed.
		require.Len(t, commandSpans, 1, "error command should still produce a span")

		cs := commandSpans[0]

		// Assert: the span was ended on the error path in processAndBatch.
		assert.False(t, cs.EndTime.IsZero(), "error span should be ended")

		parentStub := findSpan(spans, "test.error.parent")
		require.NotNil(t, parentStub)

		// Assert: trace context propagated through GoAkt into the actor.
		assert.Equal(t, parentStub.SpanContext.TraceID(), cs.SpanContext.TraceID(),
			"error span should share the parent trace ID")

		// Assert: direct parent-child linkage preserved on the error path.
		assert.Equal(t, parentStub.SpanContext.SpanID(), cs.Parent.SpanID(),
			"error span should be a child of the parent span")

		require.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()
		pause.For(time.Second)
		require.NoError(t, actorSystem.Stop(ctx))
	})
	// Test: batch trace spans with no-event command are ended immediately
	//
	// Verifies that when HandleCommand returns zero events the "ego.command"
	// span is ended immediately inside processAndBatch — it must not be
	// deferred to the batch flush cycle, because the command is answered
	// inline without entering the batch buffer.
	//
	// Setup:
	//   - Real TracerProvider + InMemoryExporter + global TextMapPropagator.
	//   - A TestNoEvent command is sent, which the behavior handles by
	//     returning an empty event slice.
	//   - The parent span ("test.noevent.parent") provides the trace context.
	//
	// Assertions:
	//   - Exactly 1 "ego.command" span is produced for the no-event command.
	//   - The span has a non-zero EndTime, proving processAndBatch called
	//     span.End() immediately when len(events) == 0.
	//   - The span shares the parent's TraceID (trace context propagated).
	//   - The span's Parent.SpanID equals the parent span's SpanID
	//     (direct parent-child linkage).
	t.Run("batch trace spans with no-event command are ended immediately", func(t *testing.T) {
		ctx := context.TODO()

		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))

		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(exporter),
			sdktrace.WithSampler(sdktrace.AlwaysSample()),
		)
		defer func() { _ = tp.Shutdown(ctx) }()

		tracer := tp.Tracer("ego-test")
		meter := noop.NewMeterProvider().Meter("test")

		eventStore := testkit.NewEventsStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))
		pause.For(time.Second)

		eventStream := eventstream.New()

		entityCfg := &extensions.EntityConfig{
			BatchThreshold:   100,
			BatchFlushWindow: 100 * time.Millisecond,
		}

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewTelemetryExtension(tracer, meter),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, entityCfg),
			goakt.WithLongLived(),
			goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		parentCtx, parentSpan := tracer.Start(ctx, "test.noevent.parent")

		reply, err := goakt.Ask(parentCtx, pid, &testpb.TestNoEvent{}, 5*time.Second)
		parentSpan.End()

		require.NoError(t, err)
		require.NotNil(t, reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		require.NoError(t, tp.ForceFlush(ctx))

		spans := exporter.GetSpans()

		var commandSpans []tracetest.SpanStub
		for i := range spans {
			if spans[i].Name == "ego.command" {
				commandSpans = append(commandSpans, spans[i])
			}
		}

		// Assert: a span is produced even for a no-event command.
		require.Len(t, commandSpans, 1, "no-event command should produce a span")

		cs := commandSpans[0]

		// Assert: the span was ended immediately (not deferred to batch flush).
		assert.False(t, cs.EndTime.IsZero(), "no-event span should be ended immediately")

		parentStub := findSpan(spans, "test.noevent.parent")
		require.NotNil(t, parentStub)

		// Assert: trace context propagated through GoAkt into the actor.
		assert.Equal(t, parentStub.SpanContext.TraceID(), cs.SpanContext.TraceID(),
			"no-event span should share the parent trace ID")

		// Assert: direct parent-child linkage.
		assert.Equal(t, parentStub.SpanContext.SpanID(), cs.Parent.SpanID(),
			"no-event span should be a child of the parent span")

		require.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()
		pause.For(time.Second)
		require.NoError(t, actorSystem.Stop(ctx))
	})
	// Test: batch trace spans with encryption failure are ended
	//
	// Verifies that when buildEnvelopes fails due to an encryption error the
	// "ego.command" span is still ended, preventing span leaks on the
	// encryption-failure path inside processAndBatch.
	//
	// Setup:
	//   - Real TracerProvider + InMemoryExporter + global TextMapPropagator.
	//   - A mock Encryptor is wired to return an error for any Encrypt call.
	//   - A CreateAccount command (which produces events) triggers buildEnvelopes,
	//     which calls the encryptor and fails.
	//   - The parent span ("test.encrypt.parent") provides the trace context.
	//
	// Assertions:
	//   - Exactly 1 "ego.command" span is produced despite the encryption failure.
	//   - The span has a non-zero EndTime, proving processAndBatch called
	//     span.End() on the buildEnvelopes error path before sendErrorReply.
	//   - The span shares the parent's TraceID (trace context propagated).
	//   - The span's Parent.SpanID equals the parent span's SpanID
	//     (direct parent-child linkage preserved on encryption failure).
	t.Run("batch trace spans with encryption failure are ended", func(t *testing.T) {
		ctx := context.TODO()

		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))

		exporter := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSyncer(exporter),
			sdktrace.WithSampler(sdktrace.AlwaysSample()),
		)
		defer func() { _ = tp.Shutdown(ctx) }()

		tracer := tp.Tracer("ego-test")
		meter := noop.NewMeterProvider().Meter("test")

		eventStore := testkit.NewEventsStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountEventSourcedBehavior(persistenceID)

		require.NoError(t, eventStore.Connect(ctx))
		pause.For(time.Second)

		eventStream := eventstream.New()

		encryptor := new(mockencryption.Encryptor)
		encryptor.EXPECT().Encrypt(mock.Anything, persistenceID, mock.Anything).Return(nil, "", assert.AnError)

		entityCfg := &extensions.EntityConfig{
			BatchThreshold:   100,
			BatchFlushWindow: 100 * time.Millisecond,
		}

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewEncryptor(encryptor),
				extensions.NewTelemetryExtension(tracer, meter),
			),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)

		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newEventSourcedActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, entityCfg),
			goakt.WithLongLived(),
			goakt.WithStashing())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		parentCtx, parentSpan := tracer.Start(ctx, "test.encrypt.parent")

		reply, err := goakt.Ask(parentCtx, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
		parentSpan.End()

		require.NoError(t, err)
		require.NotNil(t, reply)

		commandReply := reply.(*egopb.CommandReply)
		require.IsType(t, new(egopb.CommandReply_ErrorReply), commandReply.GetReply())

		require.NoError(t, tp.ForceFlush(ctx))

		spans := exporter.GetSpans()

		var commandSpans []tracetest.SpanStub
		for i := range spans {
			if spans[i].Name == "ego.command" {
				commandSpans = append(commandSpans, spans[i])
			}
		}

		// Assert: a span is produced despite the encryption failure.
		require.Len(t, commandSpans, 1, "encryption-failure command should produce a span")

		cs := commandSpans[0]

		// Assert: the span was ended on the buildEnvelopes error path.
		assert.False(t, cs.EndTime.IsZero(), "encryption-failure span should be ended")

		parentStub := findSpan(spans, "test.encrypt.parent")
		require.NotNil(t, parentStub)

		// Assert: trace context propagated through GoAkt into the actor.
		assert.Equal(t, parentStub.SpanContext.TraceID(), cs.SpanContext.TraceID(),
			"encryption-failure span should share the parent trace ID")

		// Assert: direct parent-child linkage preserved on encryption failure.
		assert.Equal(t, parentStub.SpanContext.SpanID(), cs.Parent.SpanID(),
			"encryption-failure span should be a child of the parent span")

		require.NoError(t, eventStore.Disconnect(ctx))
		eventStream.Close()
		require.NoError(t, actorSystem.Stop(ctx))
	})
}

// findSpan returns the first SpanStub with the given name, or nil.
func findSpan(spans tracetest.SpanStubs, name string) *tracetest.SpanStub {
	for i := range spans {
		if spans[i].Name == name {
			return &spans[i]
		}
	}
	return nil
}
