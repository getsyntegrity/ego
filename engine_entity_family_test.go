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
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablogore/ego/v4/eventstream"
	"github.com/pablogore/ego/v4/testkit"
)

// familyTestEngine builds a started engine with an events store and a state
// store, so a rejected spawn can only come from the declared-family guard,
// never from a missing store.
func familyTestEngine(t *testing.T, name string, opts ...Option) *Engine {
	t.Helper()
	ctx := context.Background()
	eventsStore := testkit.NewEventsStore()
	require.NoError(t, eventsStore.Connect(ctx))
	stateStore := testkit.NewDurableStore()
	require.NoError(t, stateStore.Connect(ctx))
	t.Cleanup(func() {
		_ = eventsStore.Disconnect(ctx)
		_ = stateStore.Disconnect(ctx)
	})
	opts = append([]Option{WithLogger(DiscardLogger), WithStateStore(stateStore)}, opts...)
	engine := newTestEngine(t, name, eventsStore, opts...)
	require.NoError(t, engine.Start(ctx))
	return engine
}

// spawnCase is one spawn entry point for one family: the deprecated public
// method or its runtime-neutral successor.
type spawnCase struct {
	name   string
	family EntityFamily
	spawn  func(ctx context.Context, engine *Engine, id string) error
}

// everySpawnEntryPoint lists both entry points of every family, so the
// guard is proven to live in the one shared unexported spawn function.
func everySpawnEntryPoint() []spawnCase {
	return []spawnCase{
		{"Entity (deprecated)", EventSourcedFamily, func(ctx context.Context, e *Engine, id string) error {
			return e.Entity(ctx, NewAccountEventSourcedBehavior(id))
		}},
		{"SpawnEventSourced", EventSourcedFamily, func(ctx context.Context, e *Engine, id string) error {
			return e.SpawnEventSourced(ctx, &domainOnlyEventSourced{id: id})
		}},
		{"DurableStateEntity (deprecated)", DurableStateFamily, func(ctx context.Context, e *Engine, id string) error {
			return e.DurableStateEntity(ctx, NewAccountDurableStateBehavior(id))
		}},
		{"SpawnDurableState", DurableStateFamily, func(ctx context.Context, e *Engine, id string) error {
			return e.SpawnDurableState(ctx, &domainOnlyDurableState{id: id})
		}},
		{"Saga (deprecated)", SagaFamily, func(ctx context.Context, e *Engine, id string) error {
			return e.Saga(ctx, &testSagaBehavior{sagaID: id}, time.Second)
		}},
		{"SpawnSaga", SagaFamily, func(ctx context.Context, e *Engine, id string) error {
			return e.SpawnSaga(ctx, &domainOnlySaga{id: id}, time.Second)
		}},
	}
}

// TestWithEntityFamilies_UndeclaredFamilyIsRejected declares one family at a
// time and spawns through every entry point: the declared family spawns, the
// other two return ErrEntityFamilyNotDeclared before anything is spawned
// (ego-arch-003 design §D3, IMPL-4).
func TestWithEntityFamilies_UndeclaredFamilyIsRejected(t *testing.T) {
	ctx := context.Background()
	for _, declared := range []EntityFamily{EventSourcedFamily, DurableStateFamily, SagaFamily} {
		t.Run("declared "+declared.String(), func(t *testing.T) {
			engine := familyTestEngine(t, "family-"+uuid.NewString()[:8], WithEntityFamilies(declared))
			for _, c := range everySpawnEntryPoint() {
				t.Run(c.name, func(t *testing.T) {
					id := "family-" + uuid.NewString()
					err := c.spawn(ctx, engine, id)
					if c.family == declared {
						require.NoError(t, err)
						return
					}
					require.ErrorIs(t, err, ErrEntityFamilyNotDeclared)
					assert.Contains(t, err.Error(), c.family.String(), "the error names the undeclared family")
					exists, existsErr := engine.EntityExists(ctx, id)
					require.NoError(t, existsErr)
					assert.False(t, exists, "a rejected spawn must not spawn anything")
				})
			}
		})
	}
}

// TestWithEntityFamilies_Combined declares several families in one or more
// calls; the declared set is their union.
func TestWithEntityFamilies_Combined(t *testing.T) {
	ctx := context.Background()
	engine := familyTestEngine(t, "family-union",
		WithEntityFamilies(EventSourcedFamily), WithEntityFamilies(SagaFamily))
	for _, c := range everySpawnEntryPoint() {
		t.Run(c.name, func(t *testing.T) {
			err := c.spawn(ctx, engine, "union-"+uuid.NewString())
			if c.family == DurableStateFamily {
				require.ErrorIs(t, err, ErrEntityFamilyNotDeclared)
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestWithEntityFamilies_NotDeclaredAllowsEveryFamily keeps the manual path
// unchanged: without WithEntityFamilies, or with no family passed to it,
// every family spawns as before.
func TestWithEntityFamilies_NotDeclaredAllowsEveryFamily(t *testing.T) {
	ctx := context.Background()
	for name, opts := range map[string][]Option{
		"option absent":     nil,
		"no family passed":  {WithEntityFamilies()},
		"zero family value": {WithEntityFamilies(0)},
	} {
		t.Run(name, func(t *testing.T) {
			engine := familyTestEngine(t, "family-any-"+uuid.NewString()[:8], opts...)
			for _, c := range everySpawnEntryPoint() {
				require.NoError(t, c.spawn(ctx, engine, "any-"+uuid.NewString()), c.name)
			}
		})
	}
}

// TestWithEntityFamilies_UnknownBitsAreIgnored masks a declaration to the
// three known families: an unknown bit alone declares nothing (every family
// spawns, as without the option), and an unknown bit next to a known one
// declares only the known one.
func TestWithEntityFamilies_UnknownBitsAreIgnored(t *testing.T) {
	ctx := context.Background()

	t.Run("unknown bit only", func(t *testing.T) {
		engine := familyTestEngine(t, "family-unknown-only", WithEntityFamilies(EntityFamily(8)))
		for _, c := range everySpawnEntryPoint() {
			require.NoError(t, c.spawn(ctx, engine, "unknown-"+uuid.NewString()), c.name)
		}
	})

	t.Run("unknown bit next to a known one", func(t *testing.T) {
		engine := familyTestEngine(t, "family-unknown-mixed", WithEntityFamilies(EventSourcedFamily|EntityFamily(8)))
		for _, c := range everySpawnEntryPoint() {
			err := c.spawn(ctx, engine, "mixed-"+uuid.NewString())
			if c.family == EventSourcedFamily {
				require.NoError(t, err, c.name)
				continue
			}
			require.ErrorIs(t, err, ErrEntityFamilyNotDeclared, c.name)
		}
	})
}

func TestEntityFamily_String(t *testing.T) {
	assert.Equal(t, "EventSourced", EventSourcedFamily.String())
	assert.Equal(t, "DurableState", DurableStateFamily.String())
	assert.Equal(t, "Saga", SagaFamily.String())
	assert.Equal(t, "EventSourced|Saga", (EventSourcedFamily | SagaFamily).String())
	assert.Equal(t, "EntityFamily(0)", EntityFamily(0).String())
	assert.Equal(t, "EntityFamily(8)", EntityFamily(8).String())
}

// closeCountingStream records Close calls on an otherwise real stream.
type closeCountingStream struct {
	eventstream.Stream
	closed atomic.Int32
}

func (s *closeCountingStream) Close() {
	s.closed.Add(1)
	s.Stream.Close()
}

// TestWithEventStream_UsesTheGivenStream proves the stream handed in is the
// one the actor system and the engine share: the engine's publishers read
// from it and Engine.Stop closes it (ego-arch-003 design §D5).
func TestWithEventStream_UsesTheGivenStream(t *testing.T) {
	ctx := context.Background()
	stream := &closeCountingStream{Stream: eventstream.New()}

	cfg := NewConfig(testkit.NewEventsStore(), WithLogger(DiscardLogger), WithEventStream(stream))
	require.Same(t, stream, cfg.eventStream)

	engine := newTestEngine(t, "with-event-stream", testkit.NewEventsStore(), WithLogger(DiscardLogger), WithEventStream(stream))
	require.NoError(t, engine.Start(ctx))
	subscriber, err := engine.Subscribe()
	require.NoError(t, err)
	assert.Equal(t, 1, stream.SubscribersCount(eventsTopic), "Subscribe must register on the given stream")
	subscriber.Shutdown()

	require.NoError(t, engine.Stop(ctx))
	assert.EqualValues(t, 1, stream.closed.Load(), "Engine.Stop must close the given stream")
}

// TestWithEventStream_NilKeepsTheDefault ignores a nil or typed-nil stream,
// so NewConfig's own stream stays in place.
func TestWithEventStream_NilKeepsTheDefault(t *testing.T) {
	for name, stream := range map[string]eventstream.Stream{
		"nil":       nil,
		"typed nil": (*eventstream.EventsStream)(nil),
	} {
		t.Run(name, func(t *testing.T) {
			cfg := NewConfig(nil, WithEventStream(stream))
			require.NotNil(t, cfg.eventStream)
			def, isDefault := cfg.eventStream.(*eventstream.EventsStream)
			assert.True(t, isDefault)
			assert.NotNil(t, def, "the default stream must be kept, not replaced by a typed nil")
		})
	}
}
