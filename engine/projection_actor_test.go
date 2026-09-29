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
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/supervisor"
	"go.uber.org/atomic"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/internal/extensions"
	"github.com/getsyntegrity/ego/persistence"
	"github.com/getsyntegrity/ego/projection"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
	"github.com/getsyntegrity/ego/testkit"
)

func TestProjectionActorRunnerFailure(t *testing.T) {
	t.Run("recovers from transient store failure without restarting", func(t *testing.T) {
		ctx := context.TODO()
		logger := newLoggerAdapter(DiscardLogger)

		projectionName := "db-writer"
		persistenceID := uuid.NewString()
		shardNumber := uint64(9)

		journalStore := testkit.NewEventsStore()
		require.NoError(t, journalStore.Connect(ctx))

		// fail the first ShardOffsets round trip, then recover
		eventsStore := &flakyEventsStore{EventsStore: journalStore, failures: atomic.NewInt32(1)}

		offsetStore := testkit.NewOffsetStore()
		require.NoError(t, offsetStore.Connect(ctx))

		handler := projection.NewDiscardHandler()

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(logger),
			goakt.WithExtensions(
				extensions.NewEventsStore(eventsStore),
				extensions.NewOffsetStore(offsetStore),
				extensions.NewProjectionExtension(map[string]*projection.Options{
					projectionName: {Handler: handler, BufferSize: 500, PullInterval: 100 * time.Millisecond, Recovery: projection.NewRecovery()},
				})),
			goakt.WithActorInitMaxRetries(3))

		require.NoError(t, err)
		require.NotNil(t, actorSystem)

		require.NoError(t, actorSystem.Start(ctx))

		// persist events before the projection pulls for the first time
		event, err := anypb.New(&testpb.AccountCredited{})
		require.NoError(t, err)

		count := 10
		timestamp := timestamppb.Now()
		journals := make([]*egopb.Event, count)
		for i := range count {
			journals[i] = &egopb.Event{
				PersistenceId:  persistenceID,
				SequenceNumber: uint64(i + 1),
				IsDeleted:      false,
				Event:          event,
				Timestamp:      timestamp.AsTime().Unix(),
				Shard:          shardNumber,
			}
		}

		require.NoError(t, journalStore.WriteEvents(ctx, persistence.Unscoped(), journals, persistence.Unconditional()))

		// spawn the projection the way StartProjection does in standalone mode
		actor := NewProjectionActor()
		pid, err := actorSystem.Spawn(ctx, projectionName, actor,
			goakt.WithLongLived(),
			goakt.WithSupervisor(newProjectionSupervisor()))
		require.NoError(t, err)
		require.NotNil(t, pid)

		// the first pull fails; the runner retries in place with backoff and
		// replays the stalled backlog once the store recovers, with no actor
		// restart involved
		projectionID := &egopb.ProjectionId{
			ProjectionName: projectionName,
			ShardNumber:    shardNumber,
		}

		require.Eventually(t, func() bool {
			actual, err := offsetStore.GetCurrentOffset(ctx, projectionID)
			return err == nil && actual.GetValue() == journals[count-1].GetTimestamp()
		}, 10*time.Second, 100*time.Millisecond)

		require.True(t, pid.IsRunning())
		require.Zero(t, pid.RestartCount())

		// free resources
		require.NoError(t, actorSystem.Stop(ctx))
		require.NoError(t, journalStore.Disconnect(ctx))
		require.NoError(t, offsetStore.Disconnect(ctx))
	})
	t.Run("stops on unprocessable event", func(t *testing.T) {
		ctx := context.TODO()
		logger := newLoggerAdapter(DiscardLogger)

		projectionName := "db-writer"
		persistenceID := uuid.NewString()
		shardNumber := uint64(9)

		journalStore := testkit.NewEventsStore()
		require.NoError(t, journalStore.Connect(ctx))

		offsetStore := testkit.NewOffsetStore()
		require.NoError(t, offsetStore.Connect(ctx))

		// failingProjectionHandler always fails and the default recovery policy is Fail
		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(logger),
			goakt.WithExtensions(
				extensions.NewEventsStore(journalStore),
				extensions.NewOffsetStore(offsetStore),
				extensions.NewProjectionExtension(map[string]*projection.Options{
					projectionName: {Handler: failingProjectionHandler{}, BufferSize: 500, PullInterval: 100 * time.Millisecond, Recovery: projection.NewRecovery()},
				})),
			goakt.WithActorInitMaxRetries(3))

		require.NoError(t, err)
		require.NotNil(t, actorSystem)

		require.NoError(t, actorSystem.Start(ctx))

		event, err := anypb.New(&testpb.AccountCredited{})
		require.NoError(t, err)

		journals := []*egopb.Event{
			{
				PersistenceId:  persistenceID,
				SequenceNumber: 1,
				IsDeleted:      false,
				Event:          event,
				Timestamp:      timestamppb.Now().AsTime().Unix(),
				Shard:          shardNumber,
			},
		}

		require.NoError(t, journalStore.WriteEvents(ctx, persistence.Unscoped(), journals, persistence.Unconditional()))

		actor := NewProjectionActor()
		pid, err := actorSystem.Spawn(ctx, projectionName, actor,
			goakt.WithLongLived(),
			goakt.WithSupervisor(newProjectionSupervisor()))
		require.NoError(t, err)
		require.NotNil(t, pid)

		// the unprocessable event escalates to the actor and supervision
		// stops it, making the failure visible instead of leaving a
		// healthy-looking actor with a dead runner
		require.Eventually(t, func() bool {
			return !pid.IsRunning()
		}, 10*time.Second, 100*time.Millisecond)

		// free resources
		require.NoError(t, actorSystem.Stop(ctx))
		require.NoError(t, journalStore.Disconnect(ctx))
		require.NoError(t, offsetStore.Disconnect(ctx))
	})
}

func TestProjectionSupervisorContract(t *testing.T) {
	t.Run("keys the stop directive by the engine error type name", func(t *testing.T) {
		// goakt ships directive rules to peer nodes by type name with
		// singleton spawns: the name must not change across versions.
		var rule *supervisor.DirectiveRule
		for _, candidate := range newProjectionSupervisor().Rules() {
			if candidate.ErrorType == "engine.projectionRunnerError" {
				rule = &candidate
			}
		}
		require.NotNil(t, rule)
		assert.Equal(t, supervisor.StopDirective, rule.Directive)
	})
	t.Run("stops on the error the runner failure is escalated with", func(t *testing.T) {
		cause := errors.New("damn")
		err := &projectionRunnerError{err: cause}

		directive, ok := newProjectionSupervisor().Directive(err)
		require.True(t, ok)
		assert.Equal(t, supervisor.StopDirective, directive)
		assert.EqualError(t, err, "damn")
		assert.ErrorIs(t, err, cause)
	})
}

// failingProjectionHandler always fails to handle an event.
type failingProjectionHandler struct{}

var _ projection.Handler = failingProjectionHandler{}

func (failingProjectionHandler) Handle(context.Context, string, *anypb.Any, uint64) error {
	return errors.New("damn")
}

// flakyEventsStore delegates to the wrapped events store but fails ShardOffsets
// a configured number of times to simulate a transient store outage.
type flakyEventsStore struct {
	persistence.EventsStore
	failures *atomic.Int32
}

func (x *flakyEventsStore) ShardOffsets(ctx context.Context) (map[uint64]int64, error) {
	if x.failures.Sub(1) >= 0 {
		return nil, errors.New("shard offsets round trip failed")
	}

	return x.EventsStore.ShardOffsets(ctx)
}
