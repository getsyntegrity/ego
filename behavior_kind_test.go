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
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/extension"
	"github.com/tochemey/goakt/v4/remote"
	"github.com/travisjeffery/go-dynaport"

	"github.com/pablogore/ego/v4/testkit"
)

// Compile-time proof that BehaviorKind and EntityKind (extension.Dependency)
// have the same method set: a value of either type is assignable to the
// other, with no conversion (ego-arch-002-s3 design, §5.5).
var (
	_ BehaviorKind = EntityKind(nil)
	_ EntityKind   = BehaviorKind(nil)
)

// TestBehaviorKindAssignability checks that single values move between
// EntityKind, extension.Dependency and BehaviorKind in both directions and
// keep their dynamic value.
//
// Slices do not convert: Go never converts []EntityKind to []BehaviorKind,
// so the following does not compile and a caller migrating a slice converts
// per element (or keeps WithEntityKinds, which accepts it unchanged):
//
//	kinds := []EntityKind{new(AccountEventSourcedBehavior)}
//	WithBehaviorKinds(kinds...) // cannot use kinds (variable of type []EntityKind) as []BehaviorKind value
func TestBehaviorKindAssignability(t *testing.T) {
	account := new(AccountEventSourcedBehavior)

	var entityKind EntityKind = account
	var behaviorKind BehaviorKind = entityKind
	var dependency extension.Dependency = behaviorKind
	var back EntityKind = behaviorKind

	assert.Same(t, account, behaviorKind)
	assert.Same(t, account, dependency)
	assert.Same(t, account, back)

	entityKinds := []EntityKind{account, new(AccountDurableStateBehavior)}
	behaviorKinds := make([]BehaviorKind, 0, len(entityKinds))
	for _, kind := range entityKinds {
		behaviorKinds = append(behaviorKinds, kind)
	}
	cfg := NewConfig(testkit.NewEventsStore(), WithBehaviorKinds(behaviorKinds...))
	assert.Equal(t, behaviorKinds, cfg.behaviorKinds)
}

// TestWithEntityKindsAndWithBehaviorKindsShareRegistration checks that both
// options append, in call order, to the one registration list NewEngine
// injects, and that an engine built with both options starts.
func TestWithEntityKindsAndWithBehaviorKindsShareRegistration(t *testing.T) {
	ctx := context.Background()
	eventSourced := new(AccountEventSourcedBehavior)
	durableState := new(AccountDurableStateBehavior)
	legacy := []EntityKind{new(FailingHandleEventBehavior)}

	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	opts := []Option{
		WithLogger(DiscardLogger),
		WithEntityKinds(eventSourced),
		WithBehaviorKinds(durableState),
		WithEntityKinds(legacy...),
	}
	cfg := NewConfig(store, opts...)
	assert.Equal(t, []BehaviorKind{eventSourced, durableState, legacy[0]}, cfg.behaviorKinds)

	engine := newTestEngine(t, "SharedKinds", store, opts...)
	require.NoError(t, engine.Start(ctx))
}

// unregistrableKinds are kinds GoAkt's type registry cannot name, each with
// the Kind string NewEngine must report.
func unregistrableKinds() []struct {
	name string
	kind BehaviorKind
	want string
} {
	return []struct {
		name string
		kind BehaviorKind
		want string
	}{
		{"value type", valueTypeEventSourcedBehavior{id: "v-1"}, fmt.Sprintf("%T", valueTypeEventSourcedBehavior{})},
		{"nil", nil, "<nil>"},
		{"typed-nil pointer", (*AccountEventSourcedBehavior)(nil), fmt.Sprintf("%T", (*AccountEventSourcedBehavior)(nil))},
	}
}

// requireKindRejected builds an engine on sys with cfg and asserts that
// NewEngine returns a *BehaviorPlacementError wrapping ErrBehaviorNotPointer
// without panicking, and leaves the actor system usable: GoAkt's Inject
// panics while holding the actor-system lock, so a successful Stop proves
// Inject was never reached with the bad kind.
func requireKindRejected(t *testing.T, sys goakt.ActorSystem, cfg *Config, want string) {
	t.Helper()
	var (
		engine *Engine
		err    error
	)
	require.NotPanics(t, func() { engine, err = NewEngine(sys, cfg) })
	require.Nil(t, engine)
	require.ErrorIs(t, err, ErrBehaviorNotPointer)
	var placement *BehaviorPlacementError
	require.ErrorAs(t, err, &placement)
	assert.Equal(t, want, placement.Kind)
	assert.Empty(t, placement.EntityID)
	require.NoError(t, sys.Stop(context.Background()))
}

// TestNewEngineRejectsUnregistrableKindsSingleNode covers the kind check on
// a single node, where kind registration still goes through GoAkt's type
// registry, for both registration options.
func TestNewEngineRejectsUnregistrableKindsSingleNode(t *testing.T) {
	options := map[string]func(BehaviorKind) Option{
		"WithBehaviorKinds": func(k BehaviorKind) Option { return WithBehaviorKinds(k) },
		"WithEntityKinds":   func(k BehaviorKind) Option { return WithEntityKinds(k) },
	}
	for optName, option := range options {
		for _, tc := range unregistrableKinds() {
			t.Run(optName+"/"+tc.name, func(t *testing.T) {
				ctx := context.Background()
				store := testkit.NewEventsStore()
				require.NoError(t, store.Connect(ctx))
				t.Cleanup(func() { _ = store.Disconnect(ctx) })

				// A valid kind first: the check covers the whole list before
				// anything is injected.
				cfg := NewConfig(store, WithLogger(DiscardLogger),
					WithBehaviorKinds(new(AccountEventSourcedBehavior)), option(tc.kind))
				sys, err := goakt.NewActorSystem("KindCheck", cfg.GoaktOptions()...)
				require.NoError(t, err)
				require.NoError(t, sys.Start(ctx))

				requireKindRejected(t, sys, cfg, tc.want)
			})
		}
	}
}

// TestNewEngineRejectsValueTypeKindInClusterMode covers the kind check on a
// one-member cluster.
func TestNewEngineRejectsValueTypeKindInClusterMode(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	ports := dynaport.Get(3)
	gossipPort, clusterPort, remotingPort := ports[0], ports[1], ports[2]
	host := "127.0.0.1"
	provider := &mockClusterProvider{
		id:    "kind-check",
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

	cfg := NewConfig(store, WithLogger(DiscardLogger),
		WithBehaviorKinds(valueTypeEventSourcedBehavior{id: "v-1"}))
	sys, err := goakt.NewActorSystem("KindCheckCluster", append(cfg.GoaktOptions(),
		goakt.WithCluster(clusterCfg),
		goakt.WithRemote(remote.NewConfig(host, remotingPort)),
	)...)
	require.NoError(t, err)
	require.NoError(t, sys.Start(ctx))
	require.Eventually(t, sys.InCluster, 10*time.Second, 50*time.Millisecond)

	requireKindRejected(t, sys, cfg, fmt.Sprintf("%T", valueTypeEventSourcedBehavior{}))
}
