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

	actor "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/discovery"
	"github.com/tochemey/goakt/v4/remote"
	"github.com/travisjeffery/go-dynaport"

	"github.com/pablogore/ego/v4"
	"github.com/pablogore/ego/v4/compose"
	"github.com/pablogore/ego/v4/eventstream"
	behaviorport "github.com/pablogore/ego/v4/port/behavior"
	"github.com/pablogore/ego/v4/port/publishing"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
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
func (n *clusterNode) system() actor.ActorSystem { return n.app.Engine().ActorSystem() }

// hosts reports whether this node hosts the actor named id: ActorOf
// resolves it cluster-wide, and IsLocal tells whether this node runs it.
func (n *clusterNode) hosts(t *testing.T, ctx context.Context, id string) bool {
	t.Helper()
	pid, err := n.system().ActorOf(ctx, id)
	if err != nil {
		t.Fatalf("%s: ActorOf(%q): %v", n.name, id, err)
	}
	return pid.IsLocal()
}

// newClusterNodes builds two Apps with egoakt.New, each in cluster mode
// through WithCluster with the behavior kinds it may host (kindsA for node
// A, kindsB for node B), with its own testkit stores, one publisher of each
// kind and a counting event stream. It does not start them.
//
// Both nodes find each other through staticDiscovery on dynamic ports and
// use GoAkt's default RoundRobin placement, which spawnOnPeer relies on.
func newClusterNodes(t *testing.T, kindsA, kindsB []ego.BehaviorKind) (a, b *clusterNode) {
	t.Helper()
	const host = "127.0.0.1"
	// Three ports per node: gossip, peers and remoting.
	ports := dynaport.Get(6)
	gossip := []string{
		net.JoinHostPort(host, strconv.Itoa(ports[0])),
		net.JoinHostPort(host, strconv.Itoa(ports[3])),
	}

	newNode := func(name string, kinds []ego.BehaviorKind, gossipPort, peersPort, remotingPort int) *clusterNode {
		events, states, _ := connected(t)
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
		n.app = mustNew(t, compose.Spec{
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
func startCluster(t *testing.T, ctx context.Context, nodes ...*clusterNode) {
	t.Helper()
	errs := make(chan error, len(nodes))
	for _, n := range nodes {
		go func() {
			if err := n.app.Start(ctx); err != nil {
				errs <- fmt.Errorf("%s: Start: %w", n.name, err)
				return
			}
			errs <- nil
		}()
	}
	for range nodes {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}

	deadline := time.Now().Add(30 * time.Second)
	for {
		formed := true
		for _, n := range nodes {
			peers, err := n.system().Peers(ctx, time.Second)
			if err != nil || len(peers) != len(nodes)-1 {
				formed = false
			}
		}
		if formed {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the two nodes never formed a cluster")
		}
		time.Sleep(100 * time.Millisecond)
	}
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
func spawnOnPeer(t *testing.T, ctx context.Context, from, to *clusterNode, id string, spawn func(id string) error) {
	t.Helper()
	aligned := false
	for i := range 2 {
		alignerID := fmt.Sprintf("%s-aligner-%d", id, i)
		if err := spawn(alignerID); err != nil {
			t.Fatalf("%s: spawn aligner %q: %v", from.name, alignerID, err)
		}
		if from.hosts(t, ctx, alignerID) {
			aligned = true
			break
		}
	}
	if !aligned {
		t.Fatalf("%s: two consecutive RoundRobin spawns both left %s; the round-robin alternation this test relies on no longer holds", from.name, from.name)
	}

	if err := spawn(id); err != nil {
		t.Fatalf("%s: spawn %q: %v", from.name, id, err)
	}
	hostedByTarget := to.hosts(t, ctx, id)
	hostedByCaller := from.hosts(t, ctx, id)
	if !hostedByTarget || hostedByCaller {
		t.Fatalf("%q: local on %s = %v, local on %s = %v; want the spawn placed remotely on %s, not kept on the calling node %s",
			id, to.name, hostedByTarget, from.name, hostedByCaller, to.name, from.name)
	}
}

// TestApp_TwoNodeClusterPlacesAndStopsCleanly runs a real two-node GoAkt
// cluster built entirely through egoakt.New, WithCluster and App.Start
// (#146). Each node registers, through WithCluster, only the behavior type
// its peer places on it: node A a wallet, node B a ledger. So when node A
// places a ledger on node B, B can rebuild it only from its own
// registration, and the reverse holds for a wallet placed from B on A; the
// calling node's lazy registration at spawn time never reaches the peer.
// Each placed entity answers a command with the new state, and both Apps
// then stop cleanly through App.Stop.
func TestApp_TwoNodeClusterPlacesAndStopsCleanly(t *testing.T) {
	ctx := context.Background()
	nodeA, nodeB := newClusterNodes(t,
		[]ego.BehaviorKind{new(wallet)},
		[]ego.BehaviorKind{new(ledger)},
	)
	startCluster(t, ctx, nodeA, nodeB)

	t.Run("A places a ledger on B", func(t *testing.T) {
		engine := nodeA.app.Engine()
		const id = "ledger-1"
		spawnOnPeer(t, ctx, nodeA, nodeB, id, func(id string) error {
			return engine.SpawnEventSourced(ctx, &ledger{account{id: id}})
		})

		state, revision, err := engine.SendCommand(ctx, id, &testpb.CreateAccount{AccountBalance: 100}, waitTimeout)
		if err != nil {
			t.Fatalf("SendCommand from node-A: %v", err)
		}
		if got := state.(*testpb.Account); revision != 1 || got.GetAccountId() != id || got.GetAccountBalance() != 100 {
			t.Fatalf("SendCommand = (%v, %d), want %s with balance 100 at revision 1", state, revision, id)
		}
		// The event was persisted and published by node B's engine, the
		// one hosting the entity.
		select {
		case evt := <-nodeB.evPub.events:
			if evt.GetPersistenceId() != id {
				t.Fatalf("node-B published an event for %q, want %q", evt.GetPersistenceId(), id)
			}
		case <-time.After(waitTimeout):
			t.Fatal("node-B's events publisher received nothing")
		}
	})

	t.Run("B places a wallet on A", func(t *testing.T) {
		engine := nodeB.app.Engine()
		const id = "wallet-1"
		spawnOnPeer(t, ctx, nodeB, nodeA, id, func(id string) error {
			return engine.SpawnDurableState(ctx, &wallet{id: id})
		})

		state, revision, err := engine.SendCommand(ctx, id, &testpb.CreateAccount{AccountBalance: 250}, waitTimeout)
		if err != nil {
			t.Fatalf("SendCommand from node-B: %v", err)
		}
		if got := state.(*testpb.Account); revision != 1 || got.GetAccountId() != id || got.GetAccountBalance() != 250 {
			t.Fatalf("SendCommand = (%v, %d), want %s with balance 250 at revision 1", state, revision, id)
		}
		select {
		case st := <-nodeA.stPub.states:
			if st.GetPersistenceId() != id {
				t.Fatalf("node-A published a state for %q, want %q", st.GetPersistenceId(), id)
			}
		case <-time.After(waitTimeout):
			t.Fatal("node-A's state publisher received nothing")
		}
	})

	t.Run("both nodes stop cleanly", func(t *testing.T) {
		for _, n := range []*clusterNode{nodeA, nodeB} {
			sys := n.system()
			if err := n.app.Stop(ctx); err != nil {
				t.Fatalf("%s: Stop: %v", n.name, err)
			}
			if sys.Running() {
				t.Errorf("%s: the actor system is still running after Stop", n.name)
			}
			if n.app.Engine().Started() {
				t.Errorf("%s: the engine is still started after Stop", n.name)
			}
		}
		for _, n := range []*clusterNode{nodeA, nodeB} {
			// The engine's Stop closes the stream; the actor-system step
			// closes it again, which is a documented no-op (stopActorSystem).
			if n.stream == nil {
				t.Errorf("%s: the App never allocated its event stream", n.name)
			} else if n.stream.closed.Load() == 0 {
				t.Errorf("%s: the event stream was never closed", n.name)
			}
			if ev, st := n.evPub.closed.Load(), n.stPub.closed.Load(); ev != 1 || st != 1 {
				t.Errorf("%s: publisher closes = (events %d, state %d), want each closed exactly once", n.name, ev, st)
			}
			if err := n.app.Stop(ctx); err != nil {
				t.Errorf("%s: a second Stop = %v, want a no-op", n.name, err)
			}
		}
	})
}
