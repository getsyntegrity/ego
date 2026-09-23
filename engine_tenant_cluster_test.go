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
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/remote"
	"github.com/travisjeffery/go-dynaport"

	"github.com/pablogore/ego/v4/tenancy"
	"github.com/pablogore/ego/v4/testkit"
)

// TestEngineRemoteSpawnTenantBinding runs the spawn-binding contract
// against a real two-node cluster. With RoundRobin placement over two
// members, a run of spawns from node1 places some entities on node2, so
// node1 receives REMOTE PIDs: the only way node1 can learn such an actor's
// binding is by asking the node that owns it. For every entity, whether it
// landed locally or remotely, a same-tenant re-spawn must be an idempotent
// success and a different-tenant re-spawn must fail with
// ErrSpawnTenantMismatch — from either node.
func TestEngineRemoteSpawnTenantBinding(t *testing.T) {
	ctx := context.Background()
	host := "127.0.0.1"

	ports := dynaport.Get(6)
	gossipAddrs := []string{
		net.JoinHostPort(host, strconv.Itoa(ports[0])),
		net.JoinHostPort(host, strconv.Itoa(ports[3])),
	}

	newNode := func(gossipPort, peersPort, remotingPort int) (goakt.ActorSystem, *Config) {
		store := testkit.NewEventsStore()
		require.NoError(t, store.Connect(ctx))
		t.Cleanup(func() { _ = store.Disconnect(ctx) })

		cfg := NewConfig(store,
			WithLogger(DiscardLogger),
			WithEntityKinds(new(AccountEventSourcedBehavior)),
			WithTenantResolver(perCallerTenantResolver{}),
		)

		provider := &mockClusterProvider{id: "test", peers: gossipAddrs}
		clusterCfg := goakt.NewClusterConfig().
			WithDiscovery(provider).
			WithDiscoveryPort(gossipPort).
			WithPeersPort(peersPort).
			WithMinimumPeersQuorum(1).
			WithReplicaCount(1).
			WithPartitionCount(7).
			WithKinds(ClusterKinds()...)

		goaktOpts := append(cfg.GoaktOptions(),
			goakt.WithCluster(clusterCfg),
			goakt.WithRemote(remote.NewConfig(host, remotingPort)),
		)

		sys, err := goakt.NewActorSystem("Sample", goaktOpts...)
		require.NoError(t, err)
		return sys, cfg
	}

	sys1, cfg1 := newNode(ports[0], ports[1], ports[2])
	sys2, cfg2 := newNode(ports[3], ports[4], ports[5])

	errs := make(chan error, 2)
	go func() { errs <- sys1.Start(ctx) }()
	go func() { errs <- sys2.Start(ctx) }()
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
	t.Cleanup(func() {
		_ = sys1.Stop(context.Background())
		_ = sys2.Stop(context.Background())
	})

	require.Eventually(t, func() bool {
		peers1, err1 := sys1.Peers(ctx, time.Second)
		peers2, err2 := sys2.Peers(ctx, time.Second)
		return err1 == nil && err2 == nil && len(peers1) == 1 && len(peers2) == 1
	}, 30*time.Second, 500*time.Millisecond, "the two nodes never formed a cluster")

	engine1, err := NewEngine(sys1, cfg1)
	require.NoError(t, err)
	require.NoError(t, engine1.Start(ctx))
	engine2, err := NewEngine(sys2, cfg2)
	require.NoError(t, err)
	require.NoError(t, engine2.Start(ctx))
	t.Cleanup(func() {
		_ = engine1.Stop(context.Background())
		_ = engine2.Stop(context.Background())
	})

	acme := WithTenant(tenancy.TenantID("acme"))
	globex := WithTenant(tenancy.TenantID("globex"))

	remoteSeen := 0
	for range 8 {
		entityID := uuid.NewString()
		require.NoError(t, engine1.Entity(ctx, NewAccountEventSourcedBehavior(entityID), acme),
			"a valid tenant-aware spawn must succeed wherever it is placed")

		pid, err := sys1.ActorOf(ctx, entityID)
		require.NoError(t, err)
		if pid.IsRemote() {
			remoteSeen++
		}

		for name, engine := range map[string]*Engine{"node1": engine1, "node2": engine2} {
			require.NoError(t, engine.Entity(ctx, NewAccountEventSourcedBehavior(entityID), acme),
				"%s: a same-tenant re-spawn must be an idempotent success (remote=%v)", name, pid.IsRemote())

			err := engine.Entity(ctx, NewAccountEventSourcedBehavior(entityID), globex)
			require.Error(t, err, "%s: a different-tenant re-spawn must be rejected (remote=%v)", name, pid.IsRemote())
			assert.ErrorIs(t, err, ErrSpawnTenantMismatch, "%s (remote=%v)", name, pid.IsRemote())
			assert.ErrorIs(t, err, tenancy.ErrDenied, "%s (remote=%v)", name, pid.IsRemote())
		}
	}
	require.Positive(t, remoteSeen, "RoundRobin over two members must place at least one entity on node2")
}
