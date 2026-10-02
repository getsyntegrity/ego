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

package goakt

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	actor "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/discovery"
	"github.com/tochemey/goakt/v4/remote"
	"github.com/travisjeffery/go-dynaport"

	"github.com/getsyntegrity/urd/compose"
	"github.com/getsyntegrity/urd/engine"
	"github.com/getsyntegrity/urd/eventstream"
	testpb "github.com/getsyntegrity/urd/internal/testpb"
	behaviorport "github.com/getsyntegrity/urd/port/behavior"
	"github.com/getsyntegrity/urd/port/publishing"
)

// ledger is a serializable event-sourced behavior: account's command and
// event handling plus MarshalBinary and UnmarshalBinary, so GoAkt can carry
// it to the node it places it on. It is a type of its own, distinct from
// wallet, so that a node can only decode a ledger through a registration
// that names it.
type ledger struct{ account }

var _ behaviorport.EventSourced = (*ledger)(nil)

func (l *ledger) MarshalBinary() ([]byte, error) { return json.Marshal(l.id) }
func (l *ledger) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, &l.id)
}

// staticDiscovery is a GoAkt discovery provider that returns a fixed list
// of gossip addresses, so an in-process cluster needs no external service.
type staticDiscovery struct{ peers []string }

var _ discovery.Provider = (*staticDiscovery)(nil)

func (d *staticDiscovery) ID() string                       { return "static" }
func (d *staticDiscovery) Initialize() error                { return nil }
func (d *staticDiscovery) Register() error                  { return nil }
func (d *staticDiscovery) Deregister() error                { return nil }
func (d *staticDiscovery) DiscoverPeers() ([]string, error) { return d.peers, nil }
func (d *staticDiscovery) Close() error                     { return nil }

// clusterNode is one App of the two-node cluster plus the fakes the test
// inspects on it.
type clusterNode struct {
	name   string
	app    *App
	evPub  *eventPublisher
	stPub  *statePublisher
	stream *countingStream
}

// system is the node's running actor system.

// system is the node's running actor system.
func (n *clusterNode) system() actor.ActorSystem { return n.app.Engine().ActorSystem() }

// hosts reports whether this node hosts the actor named id: ActorOf
// resolves it cluster-wide, and IsLocal tells whether this node runs it. An
// ActorOf failure fails the spec.
func (n *clusterNode) hosts(ctx *specs.Context, id string) bool {
	pid, err := n.system().ActorOf(context.Background(), id)
	ctx.Expect(err).To(specs.BeNil())
	return pid.IsLocal()
}

// newClusterNodes builds two Apps with urdakt.New, each in cluster mode
// through WithCluster with the behavior kinds it may host (kindsA for node
// A, kindsB for node B), with its own testkit stores, one publisher of each
// kind and a counting event stream. It does not start them.
//
// Both nodes find each other through staticDiscovery on dynamic ports and
// use GoAkt's default RoundRobin placement, which spawnOnPeer relies on.
func newClusterNodes(ctx *specs.Context, kindsA, kindsB []engine.BehaviorKind) (a, b *clusterNode) {
	const host = "127.0.0.1"
	// Three ports per node: gossip, peers and remoting.
	ports := dynaport.Get(6)
	gossip := []string{
		net.JoinHostPort(host, strconv.Itoa(ports[0])),
		net.JoinHostPort(host, strconv.Itoa(ports[3])),
	}

	newNode := func(name string, kinds []engine.BehaviorKind, gossipPort, peersPort, remotingPort int) *clusterNode {
		events, states, _ := connected(ctx)
		n := &clusterNode{
			name:  name,
			evPub: newEventPublisher(name + "-events"),
			stPub: newStatePublisher(name + "-states"),
		}
		clusterCfg := actor.NewClusterConfig().
			WithDiscovery(&staticDiscovery{peers: gossip}).
			WithDiscoveryPort(gossipPort).
			WithPeersPort(peersPort).
			WithMinimumPeersQuorum(1).
			WithReplicaCount(1).
			WithPartitionCount(7)
		n.app = mustNew(ctx, compose.Spec{
			// Both nodes belong to the same actor system.
			Name:            "compose-cluster",
			Families:        compose.EventSourced | compose.DurableState,
			EventsStore:     events,
			StateStore:      states,
			EventPublishers: []publishing.EventPublisher{n.evPub},
			StatePublishers: []publishing.StatePublisher{n.stPub},
			ShutdownTimeout: 20 * time.Second,
		},
			WithCluster(clusterCfg, kinds...),
			WithActorSystemOptions(actor.WithRemote(remote.NewConfig(host, remotingPort))),
		)
		n.app.hooks.newEventStream = func() eventstream.Stream {
			n.stream = &countingStream{Stream: eventstream.New()}
			return n.stream
		}
		return n
	}
	return newNode("node-A", kindsA, ports[0], ports[1], ports[2]),
		newNode("node-B", kindsB, ports[3], ports[4], ports[5])
}

// startCluster starts both Apps concurrently, so their actor systems
// bootstrap the cluster together, and waits until each sees the other.
func startCluster(ctx *specs.Context, nodes ...*clusterNode) {
	bg := context.Background()
	errs := make(chan error, len(nodes))
	for _, n := range nodes {
		go func() {
			if err := n.app.Start(bg); err != nil {
				errs <- fmt.Errorf("%s: Start: %w", n.name, err)
				return
			}
			errs <- nil
		}()
	}
	for range nodes {
		ctx.Expect(<-errs).To(specs.BeNil())
	}

	// The cluster has formed once every node sees all the others; the poll
	// reports the nodes that do not yet.
	ctx.Eventually(func() any {
		var lonely []string
		for _, n := range nodes {
			peers, err := n.system().Peers(bg, time.Second)
			if err != nil || len(peers) != len(nodes)-1 {
				lonely = append(lonely, n.name)
			}
		}
		return lonely
	}, specs.BeEmpty(), specs.WithTimeout(30*time.Second), specs.WithInterval(100*time.Millisecond))
}

// receive returns the next value on ch, polling instead of blocking, and
// fails the spec when nothing arrives within waitTimeout.
func receive[T any](ctx *specs.Context, ch <-chan T) T {
	var got T
	var ok bool
	ctx.Eventually(func() any {
		select {
		case got = <-ch:
			ok = true
		default:
		}
		return ok
	}, specs.BeTrue(), specs.WithTimeout(waitTimeout), specs.WithInterval(10*time.Millisecond))
	return got
}

// spawnOnPeer spawns, from node from, the entity id so that it is placed on
// node to, and asserts that it was: to hosts it and from does not. spawn
// spawns one entity by id through from's engine.
//
// The placement is deterministic, not retried until lucky. With GoAkt's
// RoundRobin placement, SpawnOn picks members[(n-1) % len(members)], where
// n is a cluster-wide counter every SpawnOn increments by one (goakt
// v4.5.4, actor/spawn.go actorsRoundRobinPlacementPeer) and members is the
// cluster membership sorted by birth date (olric's GetMembers), the same
// order on every call. With two members, consecutive spawns therefore
// alternate between the two nodes. The counter's value when the test
// begins is unknown, so spawnOnPeer first aligns it: it spawns aligner
// entities until one lands on from itself, which takes at most two spawns.
// The next spawn, the subject, then goes to the other member, to. This
// holds only under two conditions, and the test meets both: nothing else
// calls SpawnOn concurrently, so no other spawn advances the counter
// between the aligner and the subject; and the membership is stable while
// the test runs (both nodes joined before the first spawn and none leaves
// until Stop), so members keeps the same two entries in the same order. If
// the subject stays on from —
// placement Local, a single-member cluster, a changed strategy — the test
// fails instead of retrying.
func spawnOnPeer(ctx *specs.Context, from, to *clusterNode, id string, spawn func(id string) error) {
	aligned := false
	for i := range 2 {
		alignerID := fmt.Sprintf("%s-aligner-%d", id, i)
		ctx.Expect(spawn(alignerID)).To(specs.BeNil())
		if from.hosts(ctx, alignerID) {
			aligned = true
			break
		}
	}
	// Two consecutive RoundRobin spawns that both left from mean the
	// alternation this test relies on no longer holds.
	ctx.Expect(aligned).To(specs.BeTrue())

	ctx.Expect(spawn(id)).To(specs.BeNil())
	// Placed remotely on to, not kept on the calling node from.
	ctx.Expect(to.hosts(ctx, id)).To(specs.BeTrue())
	ctx.Expect(from.hosts(ctx, id)).To(specs.BeFalse())
}

// TestCluster_AppTwoNodePlacesAndStopsCleanly runs a real two-node GoAkt
// cluster built entirely through urdakt.New, WithCluster and App.Start
// (#146). Each node registers, through WithCluster, only the behavior type
// its peer places on it: node A a wallet, node B a ledger. So when node A
// places a ledger on node B, B can rebuild it only from its own
// registration, and the reverse holds for a wallet placed from B on A; the
// calling node's lazy registration at spawn time never reaches the peer.
// Each placed entity answers a command with the new state, and both Apps
// then stop cleanly through App.Stop.
//
// The cluster is built once for the group and the cases run in order, as
// the t.Run subtests they replace did.
func TestCluster_AppTwoNodePlacesAndStopsCleanly(t *testing.T) {
	specs.Describe(t, "a two-node GoAkt cluster built through urdakt.New", func(s *specs.Spec) {
		bg := context.Background()
		var nodeA, nodeB *clusterNode

		s.BeforeAll(func(ctx *specs.Context) {
			nodeA, nodeB = newClusterNodes(ctx,
				[]engine.BehaviorKind{new(wallet)},
				[]engine.BehaviorKind{new(ledger)},
			)
			startCluster(ctx, nodeA, nodeB)
		})
		// Stop is idempotent, so this only matters when a case fails before
		// the "stop cleanly" case; its error is ignored on purpose.
		s.AfterAll(func(*specs.Context) {
			for _, n := range []*clusterNode{nodeA, nodeB} {
				if n != nil {
					_ = n.app.Stop(bg)
				}
			}
		})

		s.It("A places a ledger on B", func(ctx *specs.Context) {
			engine := nodeA.app.Engine()
			const id = "ledger-1"
			spawnOnPeer(ctx, nodeA, nodeB, id, func(id string) error {
				return engine.SpawnEventSourced(bg, &ledger{account{id: id}})
			})

			state, revision, err := engine.SendCommand(bg, id, &testpb.CreateAccount{AccountBalance: 100}, waitTimeout)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(revision).To(specs.Equal(uint64(1)))
			ctx.Expect(state.(*testpb.Account).GetAccountId()).To(specs.Equal(id))
			ctx.Expect(state.(*testpb.Account).GetAccountBalance()).To(specs.Equal(float64(100)))
			// The event was persisted and published by node B's engine, the
			// one hosting the entity.
			ctx.Expect(receive(ctx, nodeB.evPub.events).GetPersistenceId()).To(specs.Equal(id))
		})

		s.It("B places a wallet on A", func(ctx *specs.Context) {
			engine := nodeB.app.Engine()
			const id = "wallet-1"
			spawnOnPeer(ctx, nodeB, nodeA, id, func(id string) error {
				return engine.SpawnDurableState(bg, &wallet{id: id})
			})

			state, revision, err := engine.SendCommand(bg, id, &testpb.CreateAccount{AccountBalance: 250}, waitTimeout)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(revision).To(specs.Equal(uint64(1)))
			ctx.Expect(state.(*testpb.Account).GetAccountId()).To(specs.Equal(id))
			ctx.Expect(state.(*testpb.Account).GetAccountBalance()).To(specs.Equal(float64(250)))
			ctx.Expect(receive(ctx, nodeA.stPub.states).GetPersistenceId()).To(specs.Equal(id))
		})

		s.It("both nodes stop cleanly", func(ctx *specs.Context) {
			for _, n := range []*clusterNode{nodeA, nodeB} {
				sys := n.system()
				ctx.Expect(n.app.Stop(bg)).To(specs.BeNil())
				ctx.Expect(sys.Running()).To(specs.BeFalse())
				ctx.Expect(n.app.Engine().Started()).To(specs.BeFalse())
			}
			for _, n := range []*clusterNode{nodeA, nodeB} {
				// The engine's Stop closes the stream; the actor-system step
				// closes it again, which is a documented no-op (stopActorSystem).
				ctx.Expect(n.stream).To(specs.Not(specs.BeNil()))
				ctx.Expect(n.stream.closed.Load()).To(specs.BeGreaterThan(int32(0)))
				// Each publisher is closed exactly once.
				ctx.Expect(n.evPub.closed.Load()).To(specs.Equal(int32(1)))
				ctx.Expect(n.stPub.closed.Load()).To(specs.Equal(int32(1)))
				// A second Stop is a no-op.
				ctx.Expect(n.app.Stop(bg)).To(specs.BeNil())
			}
		})
	})
}
