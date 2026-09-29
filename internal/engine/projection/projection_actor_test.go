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

package projection

import (
	"context"
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
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/encryption"
	"github.com/getsyntegrity/ego/eventadapter"
	"github.com/getsyntegrity/ego/internal/engine/enginetest"
	"github.com/getsyntegrity/ego/internal/extensions"
	"github.com/getsyntegrity/ego/internal/goaktlog"
	"github.com/getsyntegrity/ego/internal/pause"
	mocksoffsetstore "github.com/getsyntegrity/ego/mocks/offsetstore"
	mockseventstore "github.com/getsyntegrity/ego/mocks/persistence"
	"github.com/getsyntegrity/ego/persistence"
	egoprojection "github.com/getsyntegrity/ego/projection"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
	"github.com/getsyntegrity/ego/testkit"
)

func TestProjection(t *testing.T) {
	t.Run("With happy path", func(t *testing.T) {
		ctx := context.TODO()
		logger := goaktlog.New(enginetest.DiscardLogger)

		projectionName := "db-writer"
		persistenceID := uuid.NewString()
		shardNumber := uint64(9)

		// set up the event store
		journalStore := testkit.NewEventsStore()
		assert.NotNil(t, journalStore)
		require.NoError(t, journalStore.Connect(ctx))

		// set up the offset store
		offsetStore := testkit.NewOffsetStore()
		assert.NotNil(t, offsetStore)
		require.NoError(t, offsetStore.Connect(ctx))

		handler := egoprojection.NewDiscardHandler()

		// create an actor system
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(logger),
			goakt.WithExtensions(
				extensions.NewEventsStore(journalStore),
				extensions.NewOffsetStore(offsetStore),
				extensions.NewProjectionExtension(map[string]*egoprojection.Options{
					"db-writer": {Handler: handler, BufferSize: 500, PullInterval: time.Second, Recovery: egoprojection.NewRecovery()},
				})),
			goakt.WithActorInitMaxRetries(3))

		require.NoError(t, err)
		require.NotNil(t, actorSystem)

		// start the actor system
		err = actorSystem.Start(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		// create the actor
		actor := New()
		// spawn the actor
		pid, err := actorSystem.Spawn(ctx, projectionName, actor, goakt.WithLongLived())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		// persist some events
		event, err := anypb.New(&testpb.AccountCredited{})
		assert.NoError(t, err)

		count := 10
		timestamp := timestamppb.Now()
		journals := make([]*egopb.Event, count)
		for i := range count {
			seqNr := i + 1
			journals[i] = &egopb.Event{
				PersistenceId:  persistenceID,
				SequenceNumber: uint64(seqNr),
				IsDeleted:      false,
				Event:          event,
				Timestamp:      timestamp.AsTime().Unix(),
				Shard:          shardNumber,
			}
		}

		require.NoError(t, journalStore.WriteEvents(ctx, persistence.Unscoped(), journals, persistence.Unconditional()))

		// wait for the data to be persisted by the database since this an eventual consistency case
		pause.For(2 * time.Second)

		// create the projection id
		projectionID := &egopb.ProjectionId{
			ProjectionName: projectionName,
			ShardNumber:    shardNumber,
		}

		// let us grab the current offset
		actual, err := offsetStore.GetCurrentOffset(ctx, projectionID)
		require.NoError(t, err)
		require.NotNil(t, actual)
		require.EqualValues(t, journals[9].GetTimestamp(), actual.GetValue())

		// free resources
		require.NoError(t, journalStore.Disconnect(ctx))
		require.NoError(t, offsetStore.Disconnect(ctx))
		require.NoError(t, actorSystem.Stop(ctx))
	})
	t.Run("With unhandled message result in deadletter", func(t *testing.T) {
		ctx := context.TODO()
		logger := goaktlog.New(enginetest.DiscardLogger)

		projectionName := "db-writer"

		// set up the event store
		journalStore := testkit.NewEventsStore()
		assert.NotNil(t, journalStore)
		require.NoError(t, journalStore.Connect(ctx))

		// set up the offset store
		offsetStore := testkit.NewOffsetStore()
		assert.NotNil(t, offsetStore)
		require.NoError(t, offsetStore.Connect(ctx))

		handler := egoprojection.NewDiscardHandler()

		// create an actor system
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(logger),
			goakt.WithExtensions(
				extensions.NewEventsStore(journalStore),
				extensions.NewOffsetStore(offsetStore),
				extensions.NewProjectionExtension(map[string]*egoprojection.Options{
					"db-writer": {Handler: handler, BufferSize: 500, PullInterval: time.Second, Recovery: egoprojection.NewRecovery()},
				})),
			goakt.WithActorInitMaxRetries(3))

		require.NoError(t, err)
		assert.NotNil(t, actorSystem)

		// start the actor system
		err = actorSystem.Start(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		// create the actor
		actor := New()
		// spawn the actor
		pid, err := actorSystem.Spawn(ctx, projectionName, actor, goakt.WithLongLived())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(time.Second)

		message := &testpb.CreateAccount{}
		// send a message to the actor
		err = goakt.Tell(ctx, pid, message)
		require.NoError(t, err)

		pause.For(time.Second)
		metric := pid.Metric(ctx)
		require.EqualValues(t, 1, metric.DeadlettersCount())

		// free resources
		require.NoError(t, journalStore.Disconnect(ctx))
		require.NoError(t, offsetStore.Disconnect(ctx))
		assert.NoError(t, actorSystem.Stop(ctx))
	})
	t.Run("With dead letter handler", func(t *testing.T) {
		ctx := context.TODO()
		logger := goaktlog.New(enginetest.DiscardLogger)

		projectionName := "db-writer"
		persistenceID := uuid.NewString()
		shardNumber := uint64(9)

		journalStore := testkit.NewEventsStore()
		assert.NotNil(t, journalStore)
		require.NoError(t, journalStore.Connect(ctx))

		offsetStore := testkit.NewOffsetStore()
		assert.NotNil(t, offsetStore)
		require.NoError(t, offsetStore.Connect(ctx))

		handler := egoprojection.NewDiscardHandler()
		deadLetterHandler := egoprojection.NewDiscardDeadLetterHandler()

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(logger),
			goakt.WithExtensions(
				extensions.NewEventsStore(journalStore),
				extensions.NewOffsetStore(offsetStore),
				extensions.NewProjectionExtension(map[string]*egoprojection.Options{
					"db-writer": {Handler: handler, BufferSize: 500, PullInterval: time.Second, Recovery: egoprojection.NewRecovery(), DeadLetterHandler: deadLetterHandler},
				})),
			goakt.WithActorInitMaxRetries(3))

		require.NoError(t, err)
		require.NotNil(t, actorSystem)

		err = actorSystem.Start(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		// persist some events
		event, err := anypb.New(&testpb.AccountCredited{})
		assert.NoError(t, err)

		count := 10
		timestamp := timestamppb.Now()
		journals := make([]*egopb.Event, count)
		for i := range count {
			seqNr := i + 1
			journals[i] = &egopb.Event{
				PersistenceId:  persistenceID,
				SequenceNumber: uint64(seqNr),
				IsDeleted:      false,
				Event:          event,
				Timestamp:      timestamp.AsTime().Unix(),
				Shard:          shardNumber,
			}
		}

		require.NoError(t, journalStore.WriteEvents(ctx, persistence.Unscoped(), journals, persistence.Unconditional()))

		actor := New()
		pid, err := actorSystem.Spawn(ctx, projectionName, actor, goakt.WithLongLived())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(2 * time.Second)

		// free resources
		require.NoError(t, actorSystem.Stop(ctx))
		require.NoError(t, journalStore.Disconnect(ctx))
		require.NoError(t, offsetStore.Disconnect(ctx))
	})
	t.Run("With event adapters extension", func(t *testing.T) {
		ctx := context.TODO()
		logger := goaktlog.New(enginetest.DiscardLogger)

		projectionName := "db-writer"
		persistenceID := uuid.NewString()
		shardNumber := uint64(9)

		journalStore := testkit.NewEventsStore()
		assert.NotNil(t, journalStore)
		require.NoError(t, journalStore.Connect(ctx))

		offsetStore := testkit.NewOffsetStore()
		assert.NotNil(t, offsetStore)
		require.NoError(t, offsetStore.Connect(ctx))

		handler := egoprojection.NewDiscardHandler()

		// create a passthrough event adapter
		adapter := passthroughEventAdapter{}

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(logger),
			goakt.WithExtensions(
				extensions.NewEventsStore(journalStore),
				extensions.NewOffsetStore(offsetStore),
				extensions.NewProjectionExtension(map[string]*egoprojection.Options{
					"db-writer": {Handler: handler, BufferSize: 500, PullInterval: time.Second, Recovery: egoprojection.NewRecovery()},
				}),
				extensions.NewEventAdapters([]eventadapter.EventAdapter{adapter})),
			goakt.WithActorInitMaxRetries(3))

		require.NoError(t, err)
		require.NotNil(t, actorSystem)

		err = actorSystem.Start(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		// persist some events
		event, err := anypb.New(&testpb.AccountCredited{})
		assert.NoError(t, err)

		count := 10
		timestamp := timestamppb.Now()
		journals := make([]*egopb.Event, count)
		for i := range count {
			seqNr := i + 1
			journals[i] = &egopb.Event{
				PersistenceId:  persistenceID,
				SequenceNumber: uint64(seqNr),
				IsDeleted:      false,
				Event:          event,
				Timestamp:      timestamp.AsTime().Unix(),
				Shard:          shardNumber,
			}
		}

		require.NoError(t, journalStore.WriteEvents(ctx, persistence.Unscoped(), journals, persistence.Unconditional()))

		actor := New()
		pid, err := actorSystem.Spawn(ctx, projectionName, actor, goakt.WithLongLived())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(2 * time.Second)

		// free resources
		require.NoError(t, actorSystem.Stop(ctx))
		require.NoError(t, journalStore.Disconnect(ctx))
		require.NoError(t, offsetStore.Disconnect(ctx))
	})
	t.Run("With encryptor extension", func(t *testing.T) {
		ctx := context.TODO()
		logger := goaktlog.New(enginetest.DiscardLogger)

		projectionName := "db-writer"
		persistenceID := uuid.NewString()
		shardNumber := uint64(9)

		journalStore := testkit.NewEventsStore()
		assert.NotNil(t, journalStore)
		require.NoError(t, journalStore.Connect(ctx))

		offsetStore := testkit.NewOffsetStore()
		assert.NotNil(t, offsetStore)
		require.NoError(t, offsetStore.Connect(ctx))

		handler := egoprojection.NewDiscardHandler()

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(logger),
			goakt.WithExtensions(
				extensions.NewEventsStore(journalStore),
				extensions.NewOffsetStore(offsetStore),
				extensions.NewProjectionExtension(map[string]*egoprojection.Options{
					"db-writer": {Handler: handler, BufferSize: 500, PullInterval: time.Second, Recovery: egoprojection.NewRecovery()},
				}),
				extensions.NewEncryptor(encryption.NewAESEncryptor(testkit.NewKeyStore()))),
			goakt.WithActorInitMaxRetries(3))

		require.NoError(t, err)
		require.NotNil(t, actorSystem)

		err = actorSystem.Start(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		// persist some events
		event, err := anypb.New(&testpb.AccountCredited{})
		assert.NoError(t, err)

		count := 10
		timestamp := timestamppb.Now()
		journals := make([]*egopb.Event, count)
		for i := range count {
			seqNr := i + 1
			journals[i] = &egopb.Event{
				PersistenceId:  persistenceID,
				SequenceNumber: uint64(seqNr),
				IsDeleted:      false,
				Event:          event,
				Timestamp:      timestamp.AsTime().Unix(),
				Shard:          shardNumber,
			}
		}

		require.NoError(t, journalStore.WriteEvents(ctx, persistence.Unscoped(), journals, persistence.Unconditional()))

		actor := New()
		pid, err := actorSystem.Spawn(ctx, projectionName, actor, goakt.WithLongLived())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(2 * time.Second)

		// free resources
		require.NoError(t, actorSystem.Stop(ctx))
		require.NoError(t, journalStore.Disconnect(ctx))
		require.NoError(t, offsetStore.Disconnect(ctx))
	})
	t.Run("With telemetry extension", func(t *testing.T) {
		ctx := context.TODO()
		logger := goaktlog.New(enginetest.DiscardLogger)

		projectionName := "db-writer"
		persistenceID := uuid.NewString()
		shardNumber := uint64(9)

		journalStore := testkit.NewEventsStore()
		assert.NotNil(t, journalStore)
		require.NoError(t, journalStore.Connect(ctx))

		offsetStore := testkit.NewOffsetStore()
		assert.NotNil(t, offsetStore)
		require.NoError(t, offsetStore.Connect(ctx))

		handler := egoprojection.NewDiscardHandler()

		noopTracer := tracenoop.NewTracerProvider().Tracer("test")
		noopMeter := noop.NewMeterProvider().Meter("test")

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(logger),
			goakt.WithExtensions(
				extensions.NewEventsStore(journalStore),
				extensions.NewOffsetStore(offsetStore),
				extensions.NewProjectionExtension(map[string]*egoprojection.Options{
					"db-writer": {Handler: handler, BufferSize: 500, PullInterval: time.Second, Recovery: egoprojection.NewRecovery()},
				}),
				extensions.NewTelemetryExtension(noopTracer, noopMeter)),
			goakt.WithActorInitMaxRetries(3))

		require.NoError(t, err)
		require.NotNil(t, actorSystem)

		err = actorSystem.Start(ctx)
		require.NoError(t, err)

		pause.For(time.Second)

		// persist some events
		event, err := anypb.New(&testpb.AccountCredited{})
		assert.NoError(t, err)

		count := 10
		timestamp := timestamppb.Now()
		journals := make([]*egopb.Event, count)
		for i := range count {
			seqNr := i + 1
			journals[i] = &egopb.Event{
				PersistenceId:  persistenceID,
				SequenceNumber: uint64(seqNr),
				IsDeleted:      false,
				Event:          event,
				Timestamp:      timestamp.AsTime().Unix(),
				Shard:          shardNumber,
			}
		}

		require.NoError(t, journalStore.WriteEvents(ctx, persistence.Unscoped(), journals, persistence.Unconditional()))

		actor := New()
		pid, err := actorSystem.Spawn(ctx, projectionName, actor, goakt.WithLongLived())
		require.NoError(t, err)
		require.NotNil(t, pid)

		pause.For(2 * time.Second)

		// free resources (stop exercises PostStop metrics path)
		require.NoError(t, actorSystem.Stop(ctx))
		require.NoError(t, journalStore.Disconnect(ctx))
		require.NoError(t, offsetStore.Disconnect(ctx))
	})
}

func TestProjectionActorPreStartFailure(t *testing.T) {
	t.Run("fails when runner Start returns an error", func(t *testing.T) {
		ctx := context.TODO()
		logger := goaktlog.New(enginetest.DiscardLogger)

		projectionName := "db-writer"
		resetAt := time.Now().UTC()

		// Ping succeeds so the store-connectivity retrier passes immediately.
		eventsStore := mockseventstore.NewEventsStore(t)
		eventsStore.EXPECT().Ping(mock.Anything).Return(nil).Maybe()

		// Ping succeeds but ResetOffset returns an error, causing preStart – and
		// therefore runner.Start – to fail.
		offsetStore := mocksoffsetstore.NewOffsetStore(t)
		offsetStore.EXPECT().Ping(mock.Anything).Return(nil).Maybe()
		offsetStore.EXPECT().ResetOffset(mock.Anything, mock.Anything, mock.Anything).
			Return(errors.New("reset offset failed"))

		handler := egoprojection.NewDiscardHandler()

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(logger),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventsStore),
				extensions.NewOffsetStore(offsetStore),
				extensions.NewProjectionExtension(map[string]*egoprojection.Options{
					"db-writer": {Handler: handler, BufferSize: 500, ResetOffset: resetAt, PullInterval: time.Second, Recovery: egoprojection.NewRecovery()},
				})),
			goakt.WithActorInitMaxRetries(1))

		require.NoError(t, err)
		require.NotNil(t, actorSystem)

		require.NoError(t, actorSystem.Start(ctx))

		actor := New()
		_, err = actorSystem.Spawn(ctx, projectionName, actor, goakt.WithLongLived())
		require.Error(t, err)

		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("returns an error instead of panicking when the event adapters extension is registered with an unexpected type", func(t *testing.T) {
		ctx := context.TODO()
		logger := goaktlog.New(enginetest.DiscardLogger)

		projectionName := "db-writer"

		eventsStore := mockseventstore.NewEventsStore(t)
		eventsStore.EXPECT().Ping(mock.Anything).Return(nil).Maybe()

		offsetStore := mocksoffsetstore.NewOffsetStore(t)
		offsetStore.EXPECT().Ping(mock.Anything).Return(nil).Maybe()

		handler := egoprojection.NewDiscardHandler()

		actorSystem, err := goakt.NewActorSystem("TestProjectionMistypedEventAdaptersSystem",
			goakt.WithLogger(logger),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventsStore),
				extensions.NewOffsetStore(offsetStore),
				extensions.NewProjectionExtension(map[string]*egoprojection.Options{
					projectionName: {Handler: handler, BufferSize: 500, PullInterval: time.Second, Recovery: egoprojection.NewRecovery()},
				}),
				&enginetest.MistypedExtension{Name: extensions.EventAdaptersExtensionID}),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NotNil(t, actorSystem)
		require.NoError(t, actorSystem.Start(ctx))

		actor := New()
		pid, err := actorSystem.Spawn(ctx, projectionName, actor, goakt.WithLongLived())
		require.Error(t, err)
		require.Nil(t, pid)
		assert.ErrorIs(t, err, extensions.ErrMissingRequiredExtensions)

		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("returns an error instead of panicking when the events stream extension is registered with an unexpected type", func(t *testing.T) {
		ctx := context.TODO()
		logger := goaktlog.New(enginetest.DiscardLogger)

		projectionName := "db-writer"

		eventsStore := mockseventstore.NewEventsStore(t)
		eventsStore.EXPECT().Ping(mock.Anything).Return(nil).Maybe()

		offsetStore := mocksoffsetstore.NewOffsetStore(t)
		offsetStore.EXPECT().Ping(mock.Anything).Return(nil).Maybe()

		handler := egoprojection.NewDiscardHandler()

		actorSystem, err := goakt.NewActorSystem("TestProjectionMistypedEventsStreamSystem",
			goakt.WithLogger(logger),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventsStore),
				extensions.NewOffsetStore(offsetStore),
				extensions.NewProjectionExtension(map[string]*egoprojection.Options{
					projectionName: {Handler: handler, BufferSize: 500, PullInterval: time.Second, Recovery: egoprojection.NewRecovery()},
				}),
				&enginetest.MistypedExtension{Name: extensions.EventsStreamExtensionID}),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NotNil(t, actorSystem)
		require.NoError(t, actorSystem.Start(ctx))

		actor := New()
		pid, err := actorSystem.Spawn(ctx, projectionName, actor, goakt.WithLongLived())
		require.Error(t, err)
		require.Nil(t, pid)
		assert.ErrorIs(t, err, extensions.ErrMissingRequiredExtensions)

		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("returns an error instead of panicking when the encryptor extension is registered with an unexpected type", func(t *testing.T) {
		ctx := context.TODO()
		logger := goaktlog.New(enginetest.DiscardLogger)

		projectionName := "db-writer"

		eventsStore := mockseventstore.NewEventsStore(t)
		eventsStore.EXPECT().Ping(mock.Anything).Return(nil).Maybe()

		offsetStore := mocksoffsetstore.NewOffsetStore(t)
		offsetStore.EXPECT().Ping(mock.Anything).Return(nil).Maybe()

		handler := egoprojection.NewDiscardHandler()

		actorSystem, err := goakt.NewActorSystem("TestProjectionMistypedEncryptorSystem",
			goakt.WithLogger(logger),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventsStore),
				extensions.NewOffsetStore(offsetStore),
				extensions.NewProjectionExtension(map[string]*egoprojection.Options{
					projectionName: {Handler: handler, BufferSize: 500, PullInterval: time.Second, Recovery: egoprojection.NewRecovery()},
				}),
				&enginetest.MistypedExtension{Name: extensions.EncryptorExtensionID}),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NotNil(t, actorSystem)
		require.NoError(t, actorSystem.Start(ctx))

		actor := New()
		pid, err := actorSystem.Spawn(ctx, projectionName, actor, goakt.WithLongLived())
		require.Error(t, err)
		require.Nil(t, pid)
		assert.ErrorIs(t, err, extensions.ErrMissingRequiredExtensions)

		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("returns an error instead of panicking when the telemetry extension is registered with an unexpected type", func(t *testing.T) {
		ctx := context.TODO()
		logger := goaktlog.New(enginetest.DiscardLogger)

		projectionName := "db-writer"

		eventsStore := mockseventstore.NewEventsStore(t)
		eventsStore.EXPECT().Ping(mock.Anything).Return(nil).Maybe()

		offsetStore := mocksoffsetstore.NewOffsetStore(t)
		offsetStore.EXPECT().Ping(mock.Anything).Return(nil).Maybe()

		handler := egoprojection.NewDiscardHandler()

		actorSystem, err := goakt.NewActorSystem("TestProjectionMistypedTelemetrySystem",
			goakt.WithLogger(logger),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventsStore),
				extensions.NewOffsetStore(offsetStore),
				extensions.NewProjectionExtension(map[string]*egoprojection.Options{
					projectionName: {Handler: handler, BufferSize: 500, PullInterval: time.Second, Recovery: egoprojection.NewRecovery()},
				}),
				&enginetest.MistypedExtension{Name: extensions.TelemetryExtensionID}),
			goakt.WithActorInitMaxRetries(1))
		require.NoError(t, err)
		require.NotNil(t, actorSystem)
		require.NoError(t, actorSystem.Start(ctx))

		actor := New()
		pid, err := actorSystem.Spawn(ctx, projectionName, actor, goakt.WithLongLived())
		require.Error(t, err)
		require.Nil(t, pid)
		assert.ErrorIs(t, err, extensions.ErrMissingRequiredExtensions)

		require.NoError(t, actorSystem.Stop(ctx))
	})
}

// passthroughEventAdapter is an event adapter that passes events through unchanged
type passthroughEventAdapter struct{}

func (p passthroughEventAdapter) Adapt(event *anypb.Any, _ uint64) (*anypb.Any, error) {
	return event, nil
}
