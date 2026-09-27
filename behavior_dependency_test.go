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
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/extension"
	"github.com/tochemey/goakt/v4/remote"
	"github.com/travisjeffery/go-dynaport"

	"github.com/pablogore/ego/v4/command"
	"github.com/pablogore/ego/v4/internal/extensions"
	behaviorport "github.com/pablogore/ego/v4/port/behavior"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
	"github.com/pablogore/ego/v4/testkit"
)

// domainOnlyEventSourced implements behaviorport.EventSourcedEnvelope and
// nothing else: no MarshalBinary/UnmarshalBinary. It delegates the domain
// logic to AccountEventSourcedBehavior and counts HandleEnvelope calls.
type domainOnlyEventSourced struct {
	id string

	mu             sync.Mutex
	handleEnvelope int
}

var _ behaviorport.EventSourcedEnvelope = (*domainOnlyEventSourced)(nil)

func (d *domainOnlyEventSourced) ID() string { return d.id }

func (d *domainOnlyEventSourced) InitialState() State {
	return NewAccountEventSourcedBehavior(d.id).InitialState()
}

func (d *domainOnlyEventSourced) HandleCommand(ctx context.Context, cmd Command, prior State) ([]Event, error) {
	return NewAccountEventSourcedBehavior(d.id).HandleCommand(ctx, cmd, prior)
}

func (d *domainOnlyEventSourced) HandleEnvelope(ctx context.Context, env command.Envelope, prior State) ([]Event, error) {
	d.mu.Lock()
	d.handleEnvelope++
	d.mu.Unlock()
	return NewAccountEventSourcedBehavior(d.id).HandleCommand(ctx, env.Payload(), prior)
}

func (d *domainOnlyEventSourced) HandleEvent(ctx context.Context, evt Event, prior State) (State, error) {
	return NewAccountEventSourcedBehavior(d.id).HandleEvent(ctx, evt, prior)
}

func (d *domainOnlyEventSourced) envelopeHits() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.handleEnvelope
}

// domainOnlyDurableState implements behaviorport.DurableState only.
type domainOnlyDurableState struct{ id string }

var _ behaviorport.DurableState = (*domainOnlyDurableState)(nil)

func (d *domainOnlyDurableState) ID() string { return d.id }

func (d *domainOnlyDurableState) InitialState() State {
	return NewAccountDurableStateBehavior(d.id).InitialState()
}

func (d *domainOnlyDurableState) HandleCommand(ctx context.Context, cmd Command, priorVersion uint64, prior State) (State, uint64, error) {
	return NewAccountDurableStateBehavior(d.id).HandleCommand(ctx, cmd, priorVersion, prior)
}

// domainOnlySaga implements behaviorport.Saga only.
type domainOnlySaga struct{ id string }

var _ behaviorport.Saga = (*domainOnlySaga)(nil)

func (d *domainOnlySaga) ID() string { return d.id }

func (d *domainOnlySaga) InitialState() State { return (&testSagaBehavior{}).InitialState() }

func (d *domainOnlySaga) HandleEvent(context.Context, Event, State) (*SagaAction, error) {
	return &SagaAction{}, nil
}

func (d *domainOnlySaga) HandleResult(context.Context, string, State, State) (*SagaAction, error) {
	return &SagaAction{Complete: true}, nil
}

func (d *domainOnlySaga) HandleError(context.Context, string, error, State) (*SagaAction, error) {
	return &SagaAction{Compensate: true}, nil
}

func (d *domainOnlySaga) ApplyEvent(_ context.Context, _ Event, state State) (State, error) {
	return state, nil
}

func (d *domainOnlySaga) Compensate(context.Context, State) ([]SagaCommand, error) {
	return nil, nil
}

// placementProbeSystem is a goakt.ActorSystem whose cluster mode is set by
// the test and which records every Inject call. Every other method panics
// (nil embedded interface), which proves spawnDependency uses none of them.
type placementProbeSystem struct {
	goakt.ActorSystem
	inCluster bool
	injected  []extension.Dependency
}

func (p *placementProbeSystem) InCluster() bool { return p.inCluster }

func (p *placementProbeSystem) Inject(deps ...extension.Dependency) error {
	p.injected = append(p.injected, deps...)
	return nil
}

// TestSpawnDependency covers every row of the ego-arch-002-s3 §5.3 table:
// which spawn dependency the engine hands to GoAkt for a behavior value, in
// and out of cluster mode.
func TestSpawnDependency(t *testing.T) {
	t.Run("serializable pointer passes through as the same pointer", func(t *testing.T) {
		for _, inCluster := range []bool{false, true} {
			sys := &placementProbeSystem{inCluster: inCluster}
			b := NewAccountEventSourcedBehavior("acct-1")

			dep, err := spawnDependency(sys, b)
			require.NoError(t, err)
			// Identity, not equality: the wire bytes and the GoAkt type name
			// stay exactly those of the caller's value.
			require.Same(t, b, dep.(*AccountEventSourcedBehavior), "inCluster=%v", inCluster)
			require.Len(t, sys.injected, 1)
			assert.Same(t, b, sys.injected[0].(*AccountEventSourcedBehavior))
		}
	})

	t.Run("value type outside cluster mode is carried locally and never injected", func(t *testing.T) {
		sys := &placementProbeSystem{}
		b := valueTypeEventSourcedBehavior{id: "acct-2"}

		dep, err := spawnDependency(sys, b)
		require.NoError(t, err)
		local, ok := dep.(*extensions.LocalBehavior)
		require.True(t, ok, "got %T", dep)
		assert.Equal(t, "acct-2", local.ID())
		assert.Equal(t, b, local.Behavior())
		assert.Empty(t, sys.injected)
	})

	t.Run("domain-only behavior outside cluster mode is carried locally", func(t *testing.T) {
		for _, b := range []interface{ ID() string }{
			&domainOnlyEventSourced{id: "es"},
			&domainOnlyDurableState{id: "ds"},
			&domainOnlySaga{id: "saga"},
		} {
			sys := &placementProbeSystem{}
			dep, err := spawnDependency(sys, b)
			require.NoError(t, err)
			local, ok := dep.(*extensions.LocalBehavior)
			require.True(t, ok, "got %T", dep)
			assert.Same(t, b, local.Behavior())
			assert.Equal(t, b.ID(), local.ID())
			assert.Empty(t, sys.injected)
		}
	})

	t.Run("cluster mode rejects with a typed error before anything is injected", func(t *testing.T) {
		cases := []struct {
			name     string
			behavior interface{ ID() string }
			cause    error
			kind     string
		}{
			{"value type", valueTypeEventSourcedBehavior{id: "v-1"}, ErrBehaviorNotPointer, "ego.valueTypeEventSourcedBehavior"},
			{"domain-only event-sourced", &domainOnlyEventSourced{id: "v-1"}, ErrBehaviorNotSerializable, "*ego.domainOnlyEventSourced"},
			{"domain-only durable state", &domainOnlyDurableState{id: "v-1"}, ErrBehaviorNotSerializable, "*ego.domainOnlyDurableState"},
			{"domain-only saga", &domainOnlySaga{id: "v-1"}, ErrBehaviorNotSerializable, "*ego.domainOnlySaga"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				sys := &placementProbeSystem{inCluster: true}
				dep, err := spawnDependency(sys, tc.behavior)
				require.Nil(t, dep)
				require.ErrorIs(t, err, tc.cause)

				var placement *BehaviorPlacementError
				require.ErrorAs(t, err, &placement)
				assert.Equal(t, tc.kind, placement.Kind)
				assert.Equal(t, "v-1", placement.EntityID)
				assert.Empty(t, sys.injected)
			})
		}
	})

	t.Run("nil and typed-nil behaviors are rejected in every mode", func(t *testing.T) {
		cases := []struct {
			name     string
			behavior interface{ ID() string }
			kind     string
		}{
			{"nil", nil, "<nil>"},
			{"typed-nil serializable event-sourced", (*AccountEventSourcedBehavior)(nil), "*ego.AccountEventSourcedBehavior"},
			{"typed-nil domain-only event-sourced", (*domainOnlyEventSourced)(nil), "*ego.domainOnlyEventSourced"},
			{"typed-nil serializable durable state", (*AccountDurableStateBehavior)(nil), "*ego.AccountDurableStateBehavior"},
			{"typed-nil domain-only durable state", (*domainOnlyDurableState)(nil), "*ego.domainOnlyDurableState"},
			{"typed-nil serializable saga", (*testSagaBehavior)(nil), "*ego.testSagaBehavior"},
			{"typed-nil domain-only saga", (*domainOnlySaga)(nil), "*ego.domainOnlySaga"},
		}
		for _, inCluster := range []bool{false, true} {
			for _, tc := range cases {
				sys := &placementProbeSystem{inCluster: inCluster}
				dep, err := spawnDependency(sys, tc.behavior)
				require.Nil(t, dep, "%s inCluster=%v", tc.name, inCluster)
				require.ErrorIs(t, err, ErrBehaviorNotPointer, "%s inCluster=%v", tc.name, inCluster)
				var placement *BehaviorPlacementError
				require.ErrorAs(t, err, &placement)
				assert.Equal(t, tc.kind, placement.Kind)
				assert.Empty(t, placement.EntityID, "a nil behavior has no readable ID")
				assert.Empty(t, sys.injected)
			}
		}
	})
}

// nilBehaviorSpawns returns one spawn per family for a nil behavior and for
// typed-nil pointers, through the old public API and the unexported spawn
// functions. Each must be rejected before it reaches GoAkt.
func nilBehaviorSpawns(ctx context.Context, engine *Engine) []struct {
	name  string
	spawn func() error
} {
	return []struct {
		name  string
		spawn func() error
	}{
		{"Entity nil", func() error { return engine.Entity(ctx, nil) }},
		{"Entity typed-nil", func() error { return engine.Entity(ctx, (*AccountEventSourcedBehavior)(nil)) }},
		{"spawnEventSourced typed-nil domain-only", func() error {
			return engine.spawnEventSourced(ctx, (*domainOnlyEventSourced)(nil))
		}},
		{"DurableStateEntity nil", func() error { return engine.DurableStateEntity(ctx, nil) }},
		{"DurableStateEntity typed-nil", func() error {
			return engine.DurableStateEntity(ctx, (*AccountDurableStateBehavior)(nil))
		}},
		{"spawnDurableState typed-nil domain-only", func() error {
			return engine.spawnDurableState(ctx, (*domainOnlyDurableState)(nil))
		}},
		{"Saga nil", func() error { return engine.Saga(ctx, nil, 0) }},
		{"Saga typed-nil", func() error { return engine.Saga(ctx, (*testSagaBehavior)(nil), 0) }},
		{"spawnSaga typed-nil domain-only", func() error { return engine.spawnSaga(ctx, (*domainOnlySaga)(nil), 0) }},
	}
}

// requireNilBehaviorsRejected runs every nilBehaviorSpawns case against
// engine: each returns a *BehaviorPlacementError wrapping
// ErrBehaviorNotPointer, does not panic, and spawns nothing.
func requireNilBehaviorsRejected(t *testing.T, engine *Engine) {
	t.Helper()
	ctx := context.Background()
	sys := engine.ActorSystem()
	for _, tc := range nilBehaviorSpawns(ctx, engine) {
		t.Run(tc.name, func(t *testing.T) {
			before := sys.NumActors()
			var err error
			require.NotPanics(t, func() { err = tc.spawn() })
			require.ErrorIs(t, err, ErrBehaviorNotPointer)
			var placement *BehaviorPlacementError
			require.ErrorAs(t, err, &placement)
			assert.Empty(t, placement.EntityID)
			assert.Equal(t, before, sys.NumActors(), "nothing may be spawned for a nil behavior")
		})
	}
}

// TestEngineRejectsNilBehaviorsSingleNode covers nil and typed-nil
// behaviors outside cluster mode, where a non-serializable behavior would
// otherwise be carried by a LocalBehavior and its ID read at spawn.
func TestEngineRejectsNilBehaviorsSingleNode(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })
	stateStore := testkit.NewDurableStore()
	require.NoError(t, stateStore.Connect(ctx))
	t.Cleanup(func() { _ = stateStore.Disconnect(ctx) })

	engine := newTestEngine(t, "NilBehaviors", store, WithLogger(DiscardLogger), WithStateStore(stateStore))
	require.NoError(t, engine.Start(ctx))

	requireNilBehaviorsRejected(t, engine)
}

func TestBehaviorPlacementError(t *testing.T) {
	spawnErr := &BehaviorPlacementError{Kind: "*main.Account", EntityID: "acct-1", Err: ErrBehaviorNotSerializable}
	assert.Contains(t, spawnErr.Error(), "*main.Account")
	assert.Contains(t, spawnErr.Error(), `"acct-1"`)
	assert.Contains(t, spawnErr.Error(), ErrBehaviorNotSerializable.Error())
	assert.Same(t, ErrBehaviorNotSerializable, errors.Unwrap(spawnErr))

	registrationErr := &BehaviorPlacementError{Kind: "main.Account", Err: ErrBehaviorNotPointer}
	assert.Contains(t, registrationErr.Error(), "main.Account")
	assert.NotContains(t, registrationErr.Error(), `""`)
	assert.ErrorIs(t, registrationErr, ErrBehaviorNotPointer)
	assert.NotErrorIs(t, registrationErr, ErrBehaviorNotSerializable)
}

func TestBehaviorFrom(t *testing.T) {
	pointer := NewAccountEventSourcedBehavior("acct-1")
	got, ok := behaviorFrom[behaviorport.EventSourced](pointer)
	require.True(t, ok)
	assert.Same(t, pointer, got.(*AccountEventSourcedBehavior))

	domainOnly := &domainOnlyDurableState{id: "ds-1"}
	gotDS, ok := behaviorFrom[behaviorport.DurableState](extensions.NewLocalBehavior(domainOnly))
	require.True(t, ok)
	assert.Same(t, domainOnly, gotDS.(*domainOnlyDurableState))

	_, ok = behaviorFrom[behaviorport.EventSourced](extensions.NewLocalBehavior(domainOnly))
	assert.False(t, ok, "a wrapped behavior of another family must not match")
	_, ok = behaviorFrom[behaviorport.Saga](extensions.NewEntityConfig(1))
	assert.False(t, ok)
}

// TestEngineSpawnsDomainOnlyBehaviorsSingleNode spawns behaviors that
// implement only the port/behavior contracts through the unexported spawn
// functions (the public Spawn* entry points arrive in S3-3). Each is carried
// by a LocalBehavior and answers on a single node.
func TestEngineSpawnsDomainOnlyBehaviorsSingleNode(t *testing.T) {
	ctx := context.Background()

	t.Run("event-sourced, envelope-capable", func(t *testing.T) {
		store := testkit.NewEventsStore()
		require.NoError(t, store.Connect(ctx))
		t.Cleanup(func() { _ = store.Disconnect(ctx) })
		engine := newTestEngine(t, "DomainOnlyES", store, WithLogger(DiscardLogger))
		require.NoError(t, engine.Start(ctx))

		b := &domainOnlyEventSourced{id: uuid.NewString()}
		require.NoError(t, engine.spawnEventSourced(ctx, b))

		state, revision, err := engine.SendCommand(ctx, b.ID(), &testpb.CreateAccount{AccountBalance: 7}, time.Minute)
		require.NoError(t, err)
		assert.EqualValues(t, 1, revision)
		assert.EqualValues(t, 7, state.(*testpb.Account).GetAccountBalance())
		assert.Equal(t, 1, b.envelopeHits(), "an envelope-capable behavior still receives HandleEnvelope")
	})

	t.Run("durable state", func(t *testing.T) {
		stateStore := testkit.NewDurableStore()
		require.NoError(t, stateStore.Connect(ctx))
		t.Cleanup(func() { _ = stateStore.Disconnect(ctx) })
		engine := newTestEngine(t, "DomainOnlyDS", nil, WithLogger(DiscardLogger), WithStateStore(stateStore))
		require.NoError(t, engine.Start(ctx))

		b := &domainOnlyDurableState{id: uuid.NewString()}
		require.NoError(t, engine.spawnDurableState(ctx, b))

		state, revision, err := engine.SendCommand(ctx, b.ID(), &testpb.CreateAccount{AccountBalance: 9}, time.Minute)
		require.NoError(t, err)
		assert.EqualValues(t, 1, revision)
		assert.EqualValues(t, 9, state.(*testpb.Account).GetAccountBalance())
	})

	t.Run("saga", func(t *testing.T) {
		store := testkit.NewEventsStore()
		require.NoError(t, store.Connect(ctx))
		t.Cleanup(func() { _ = store.Disconnect(ctx) })
		engine := newTestEngine(t, "DomainOnlySaga", store, WithLogger(DiscardLogger))
		require.NoError(t, engine.Start(ctx))

		b := &domainOnlySaga{id: "saga-" + uuid.NewString()}
		require.NoError(t, engine.spawnSaga(ctx, b, 0))

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			info, err := engine.SagaStatus(ctx, b.ID(), time.Second)
			require.NoError(c, err)
			require.NotNil(c, info)
			assert.Equal(c, b.ID(), info.ID)
		}, 10*time.Second, 50*time.Millisecond)
	})
}

// TestEngineRejectsUnplaceableBehaviorsInClusterMode runs a single-node
// cluster, where GoAkt serializes every spawn's dependencies, and checks
// that the engine rejects behaviors it cannot hand to GoAkt with a typed
// error before any spawn, instead of panicking.
func TestEngineRejectsUnplaceableBehaviorsInClusterMode(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })
	stateStore := testkit.NewDurableStore()
	require.NoError(t, stateStore.Connect(ctx))
	t.Cleanup(func() { _ = stateStore.Disconnect(ctx) })

	ports := dynaport.Get(3)
	gossipPort, clusterPort, remotingPort := ports[0], ports[1], ports[2]
	host := "127.0.0.1"
	provider := &mockClusterProvider{
		id:    "placement",
		peers: []string{net.JoinHostPort(host, strconv.Itoa(clusterPort))},
	}
	clusterCfg := goakt.NewClusterConfig().
		WithDiscovery(provider).
		WithDiscoveryPort(gossipPort).
		WithPeersPort(clusterPort).
		WithMinimumPeersQuorum(1).
		WithReplicaCount(1).
		WithPartitionCount(4).
		WithKinds(ClusterKinds()...)

	cfg := NewConfig(store, WithLogger(DiscardLogger), WithStateStore(stateStore))
	sys, err := goakt.NewActorSystem("Placement", append(cfg.GoaktOptions(),
		goakt.WithCluster(clusterCfg),
		goakt.WithRemote(remote.NewConfig(host, remotingPort)),
	)...)
	require.NoError(t, err)
	require.NoError(t, sys.Start(ctx))
	t.Cleanup(func() { _ = sys.Stop(ctx) })
	require.Eventually(t, sys.InCluster, 10*time.Second, 50*time.Millisecond)

	engine, err := NewEngine(sys, cfg)
	require.NoError(t, err)
	require.NoError(t, engine.Start(ctx))
	t.Cleanup(func() { _ = engine.Stop(ctx) })

	cases := []struct {
		name  string
		spawn func(id string) error
		cause error
	}{
		{"value type through the old Entity API", func(id string) error {
			return engine.Entity(ctx, valueTypeEventSourcedBehavior{id: id})
		}, ErrBehaviorNotPointer},
		{"domain-only event-sourced", func(id string) error {
			return engine.spawnEventSourced(ctx, &domainOnlyEventSourced{id: id})
		}, ErrBehaviorNotSerializable},
		{"domain-only durable state", func(id string) error {
			return engine.spawnDurableState(ctx, &domainOnlyDurableState{id: id})
		}, ErrBehaviorNotSerializable},
		{"domain-only saga", func(id string) error {
			return engine.spawnSaga(ctx, &domainOnlySaga{id: id}, 0)
		}, ErrBehaviorNotSerializable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.NewString()
			err := tc.spawn(id)
			require.ErrorIs(t, err, tc.cause)
			var placement *BehaviorPlacementError
			require.ErrorAs(t, err, &placement)
			assert.Equal(t, id, placement.EntityID)

			exists, err := engine.EntityExists(ctx, id)
			require.NoError(t, err)
			assert.False(t, exists, "nothing may be spawned for a rejected behavior")
		})
	}

	t.Run("nil and typed-nil behaviors", func(t *testing.T) {
		requireNilBehaviorsRejected(t, engine)
	})

	// A serializable pointer behavior still spawns and answers in cluster mode.
	id := uuid.NewString()
	require.NoError(t, engine.Entity(ctx, NewAccountEventSourcedBehavior(id)))
	state, _, err := engine.SendCommand(ctx, id, &testpb.CreateAccount{AccountBalance: 3}, time.Minute)
	require.NoError(t, err)
	assert.EqualValues(t, 3, state.(*testpb.Account).GetAccountBalance())
}
