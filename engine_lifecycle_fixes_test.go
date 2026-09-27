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
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"

	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/testkit"
)

// failingCloseEventPublisher is an EventPublisher whose Close always fails
// and counts how many times it was called.
type failingCloseEventPublisher struct {
	id     string
	closes atomic.Int32
}

var _ EventPublisher = (*failingCloseEventPublisher)(nil)

func (p *failingCloseEventPublisher) ID() string { return p.id }

func (p *failingCloseEventPublisher) Publish(context.Context, *egopb.Event) error { return nil }

func (p *failingCloseEventPublisher) Close(context.Context) error {
	p.closes.Add(1)
	return errors.New("close failed: " + p.id)
}

// countingStatePublisher is a StatePublisher that counts Close calls.
type countingStatePublisher struct {
	id     string
	closes atomic.Int32
}

var _ StatePublisher = (*countingStatePublisher)(nil)

func (p *countingStatePublisher) ID() string { return p.id }

func (p *countingStatePublisher) Publish(context.Context, *egopb.DurableState) error { return nil }

func (p *countingStatePublisher) Close(context.Context) error {
	p.closes.Add(1)
	return nil
}

// TestEngineStopAttemptsEveryStep is the #126 defect 1 regression: a
// publisher whose Close fails must not stop Engine.Stop from closing the
// other publishers and the event stream, and the errors are joined.
func TestEngineStopAttemptsEveryStep(t *testing.T) {
	ctx := context.Background()
	engine := newTestEngine(t, "stop-every-step-"+uuid.NewString(), testkit.NewEventsStore())
	require.NoError(t, engine.Start(ctx))

	// Both event publishers fail on Close, so whichever one the map yields
	// first, the other is closed only if Stop carries on after the error.
	first := &failingCloseEventPublisher{id: "events-1"}
	second := &failingCloseEventPublisher{id: "events-2"}
	states := &countingStatePublisher{id: "states-1"}
	require.NoError(t, engine.AddEventPublishers(first, second))
	require.NoError(t, engine.AddStatePublishers(states))
	require.Positive(t, engine.eventStream.SubscribersCount(eventsTopic))

	err := engine.Stop(ctx)
	require.Error(t, err)
	assert.ErrorContains(t, err, "close failed: events-1")
	assert.ErrorContains(t, err, "close failed: events-2")

	assert.EqualValues(t, 1, first.closes.Load(), "first event publisher closed once")
	assert.EqualValues(t, 1, second.closes.Load(), "second event publisher closed once")
	assert.EqualValues(t, 1, states.closes.Load(), "state publisher closed")
	assert.Zero(t, engine.eventStream.SubscribersCount(eventsTopic), "event stream closed")
	assert.Zero(t, engine.eventStream.SubscribersCount(statesTopic), "event stream closed")
	assert.Zero(t, engine.eventsStreams.Len())
	assert.Zero(t, engine.statesStreams.Len())
	assert.False(t, engine.Started())
	assert.True(t, engine.ActorSystem() == nil, "actor system reference detached")

	// A second Stop has nothing left to do and closes nothing twice.
	require.NoError(t, engine.Stop(ctx))
	assert.EqualValues(t, 1, first.closes.Load())
	assert.EqualValues(t, 1, second.closes.Load())
}

// newStateOnlyEngine builds a started engine with a durable-state store and
// no events store, which NewConfig(nil, ...) allows.
func newStateOnlyEngine(t *testing.T) *Engine {
	t.Helper()
	ctx := context.Background()
	stateStore := testkit.NewDurableStore()
	require.NoError(t, stateStore.Connect(ctx))
	t.Cleanup(func() { _ = stateStore.Disconnect(ctx) })

	cfg := NewConfig(nil, WithStateStore(stateStore))
	sys, err := goakt.NewActorSystem("state-only-"+uuid.NewString(), cfg.GoaktOptions()...)
	require.NoError(t, err)
	require.NoError(t, sys.Start(ctx))
	engine, err := NewEngine(sys, cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = engine.Stop(context.Background())
		_ = sys.Stop(context.Background())
	})
	require.NoError(t, engine.Start(ctx))
	return engine
}

// TestSpawnWithoutEventsStore is the #126 defect 2 regression: spawning an
// event-sourced entity or a saga on an engine without an events store
// returns ErrEventsStoreRequired and spawns nothing.
func TestSpawnWithoutEventsStore(t *testing.T) {
	ctx := context.Background()

	t.Run("event-sourced entity", func(t *testing.T) {
		engine := newStateOnlyEngine(t)
		id := "account-" + uuid.NewString()
		err := engine.Entity(ctx, NewAccountEventSourcedBehavior(id))
		require.ErrorIs(t, err, ErrEventsStoreRequired)
		exists, lookupErr := engine.ActorSystem().ActorExists(ctx, id)
		require.NoError(t, lookupErr)
		assert.False(t, exists, "no actor spawned")
	})

	t.Run("saga", func(t *testing.T) {
		engine := newStateOnlyEngine(t)
		id := "saga-" + uuid.NewString()
		err := engine.Saga(ctx, &testSagaBehavior{sagaID: id}, time.Second)
		require.ErrorIs(t, err, ErrEventsStoreRequired)
		exists, lookupErr := engine.ActorSystem().ActorExists(ctx, id)
		require.NoError(t, lookupErr)
		assert.False(t, exists, "no actor spawned")
	})

	t.Run("durable-state entity still spawns", func(t *testing.T) {
		engine := newStateOnlyEngine(t)
		require.NoError(t, engine.DurableStateEntity(ctx, NewAccountDurableStateBehavior("durable-"+uuid.NewString())))
	})
}
