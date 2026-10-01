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
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"
	goakt "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/remote"
	"github.com/travisjeffery/go-dynaport"

	"github.com/getsyntegrity/ego/tenancy"
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
	specs.Describe(t, "spawn tenant binding across a two-node cluster", func(s *specs.Spec) {
		s.It("keeps same-tenant respawns idempotent and rejects other tenants, local or remote", func(ctx *specs.Context) {
			bg := context.Background()
			host := "127.0.0.1"

			ports := dynaport.Get(6)
			gossipAddrs := []string{
				net.JoinHostPort(host, strconv.Itoa(ports[0])),
				net.JoinHostPort(host, strconv.Itoa(ports[3])),
			}

			newNode := func(gossipPort, peersPort, remotingPort int) (goakt.ActorSystem, *Config) {
				store := newConnectedEventsStoreG3(ctx)

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
				ctx.Expect(err).To(specs.BeNil())
				return sys, cfg
			}

			sys1, cfg1 := newNode(ports[0], ports[1], ports[2])
			sys2, cfg2 := newNode(ports[3], ports[4], ports[5])

			errs := make(chan error, 2)
			go func() { errs <- sys1.Start(bg) }()
			go func() { errs <- sys2.Start(bg) }()
			ctx.Expect(<-errs).To(specs.BeNil())
			ctx.Expect(<-errs).To(specs.BeNil())
			ctx.Cleanup(func() {
				_ = sys1.Stop(bg)
				_ = sys2.Stop(bg)
			})

			// The two nodes must form a cluster.
			ctx.Eventually(func() any {
				peers1, err1 := sys1.Peers(bg, time.Second)
				peers2, err2 := sys2.Peers(bg, time.Second)
				return err1 == nil && err2 == nil && len(peers1) == 1 && len(peers2) == 1
			}, specs.BeTrue(), specs.WithTimeout(30*time.Second), specs.WithInterval(500*time.Millisecond))

			engine1, err := NewEngine(sys1, cfg1)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(engine1.Start(bg)).To(specs.BeNil())
			engine2, err := NewEngine(sys2, cfg2)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(engine2.Start(bg)).To(specs.BeNil())
			ctx.Cleanup(func() {
				_ = engine1.Stop(bg)
				_ = engine2.Stop(bg)
			})

			acme := WithTenant(tenancy.TenantID("acme"))
			globex := WithTenant(tenancy.TenantID("globex"))

			remoteSeen := 0
			for range 8 {
				entityID := uuid.NewString()
				// a valid tenant-aware spawn must succeed wherever it is placed
				ctx.Expect(engine1.Entity(bg, NewAccountEventSourcedBehavior(entityID), acme)).To(specs.BeNil())

				pid, err := sys1.ActorOf(bg, entityID)
				ctx.Expect(err).To(specs.BeNil())
				if pid.IsRemote() {
					remoteSeen++
				}

				for _, node := range []struct {
					name   string
					engine *Engine
				}{{"node1", engine1}, {"node2", engine2}} {
					// a same-tenant re-spawn must be an idempotent success
					ctx.Expect(node.engine.Entity(bg, NewAccountEventSourcedBehavior(entityID), acme)).To(specs.BeNil())

					// a different-tenant re-spawn must be rejected
					err := node.engine.Entity(bg, NewAccountEventSourcedBehavior(entityID), globex)
					ctx.Expect(err).To(specs.MatchError(ErrSpawnTenantMismatch))
					ctx.Expect(err).To(specs.MatchError(tenancy.ErrDenied))
				}
			}
			// RoundRobin over two members must place at least one entity on node2
			ctx.Expect(remoteSeen).To(specs.BeGreaterThan(0))
		})
	})
}
