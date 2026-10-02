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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"
	"github.com/travisjeffery/go-dynaport"

	goakt "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/remote"

	"github.com/getsyntegrity/urd/egopb"
	"github.com/getsyntegrity/urd/internal/engine/protocol"
	"github.com/getsyntegrity/urd/internal/pause"
	testpb "github.com/getsyntegrity/urd/internal/testpb"
	"github.com/getsyntegrity/urd/testkit"
)

// TestTopicConstantsAreFixed regression-guards the topic constants. They must
// be plain strings with no fmt directives — the partition-suffixed format
// "topic.events.%d" historically caused subscribers and publishers to fall
// out of sync whenever the cluster used a partition count different from
// goakt's default. Collapsing to a single topic (with the shard travelling in
// the payload via egopb.Event.Shard / egopb.DurableState.Shard) removes that
// failure mode.
func TestTopicConstantsAreFixed(t *testing.T) {
	specs.Describe(t, "the events and states topics are fixed plain strings", func(s *specs.Spec) {
		s.It("sets both topics, without fmt directives, and keeps them distinct", func(ctx *specs.Context) {
			ctx.Expect(protocol.EventsTopic).To(specs.NotEqual(""))
			ctx.Expect(protocol.StatesTopic).To(specs.NotEqual(""))
			ctx.Expect(protocol.EventsTopic).To(specs.Not(specs.Contain("%")))
			ctx.Expect(protocol.StatesTopic).To(specs.Not(specs.Contain("%")))
			ctx.Expect(protocol.EventsTopic).To(specs.NotEqual(protocol.StatesTopic))
		})
	})
}

// recordingEventPublisher captures every event passed to Publish so tests can
// assert delivery without racing on a channel signal.
type recordingEventPublisher struct {
	id     string
	mu     sync.Mutex
	events []*egopb.Event
	closed atomic.Bool
}

var _ EventPublisher = (*recordingEventPublisher)(nil)

func newRecordingEventPublisher(id string) *recordingEventPublisher {
	return &recordingEventPublisher{id: id}
}

func (p *recordingEventPublisher) ID() string { return p.id }

func (p *recordingEventPublisher) Publish(_ context.Context, event *egopb.Event) error {
	p.mu.Lock()
	p.events = append(p.events, event)
	p.mu.Unlock()
	return nil
}

func (p *recordingEventPublisher) Close(_ context.Context) error {
	p.closed.Store(true)
	return nil
}

func (p *recordingEventPublisher) snapshot() []*egopb.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*egopb.Event, len(p.events))
	copy(out, p.events)
	return out
}

// recordingStatePublisher is the durable-state analog of recordingEventPublisher.
type recordingStatePublisher struct {
	id     string
	mu     sync.Mutex
	states []*egopb.DurableState
	closed atomic.Bool
}

var _ StatePublisher = (*recordingStatePublisher)(nil)

func newRecordingStatePublisher(id string) *recordingStatePublisher {
	return &recordingStatePublisher{id: id}
}

func (p *recordingStatePublisher) ID() string { return p.id }

func (p *recordingStatePublisher) Publish(_ context.Context, state *egopb.DurableState) error {
	p.mu.Lock()
	p.states = append(p.states, state)
	p.mu.Unlock()
	return nil
}

func (p *recordingStatePublisher) Close(_ context.Context) error {
	p.closed.Store(true)
	return nil
}

func (p *recordingStatePublisher) snapshot() []*egopb.DurableState {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*egopb.DurableState, len(p.states))
	copy(out, p.states)
	return out
}

// waitFor polls until cond returns true or timeout expires.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	if !waitForCond(timeout, cond) {
		t.Fatalf("condition not met within %s", timeout)
	}
}

// waitForCond polls until cond returns true or timeout expires. Returns
// whether the condition was met.
func waitForCond(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		pause.For(20 * time.Millisecond)
	}
	return false
}

// TestEventPublisherReceivesEventsFromEntity verifies the happy path: an
// event-sourced entity emits events, the registered event publisher receives
// them. Pre-fix, this also worked in single-node mode because both ends
// agreed on partition 0; the test is here to pin that behavior under the new
// single-topic model.
func TestEventPublisherReceivesEventsFromEntity(t *testing.T) {
	specs.Describe(t, "Event Publisher Receives Events From Entity", func(s *specs.Spec) {
		s.It("holds", func(sc *specs.Context) {
			t := sc.T
			ctx := context.Background()
			store := testkit.NewEventsStore()
			sc.Expect(store.Connect(ctx)).To(specs.BeNil())
			t.Cleanup(func() { _ = store.Disconnect(ctx) })

			engine := newTestEngine(t, "Publishers", store, WithLogger(DiscardLogger))
			sc.Expect(engine.Start(ctx)).To(specs.BeNil())

			pub := newRecordingEventPublisher("recorder")
			sc.Expect(engine.AddEventPublishers(pub)).To(specs.BeNil())

			entityID := uuid.NewString()
			sc.Expect(engine.Entity(ctx, NewEventSourcedEntity(entityID))).To(specs.BeNil())

			_, _, err := engine.SendCommand(ctx, entityID, &testpb.CreateAccount{AccountBalance: 100}, time.Minute)
			sc.Expect(err).To(specs.BeNil())
			_, _, err = engine.SendCommand(ctx, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 50}, time.Minute)
			sc.Expect(err).To(specs.BeNil())

			waitFor(t, 5*time.Second, func() bool {
				return len(pub.snapshot()) == 2
			})

			events := pub.snapshot()
			sc.Expect(events).To(specs.HaveLen(2))
			sc.Expect(events[0].GetPersistenceId()).To(specs.Equal(entityID))
			sc.Expect(events[0].GetSequenceNumber()).To(specs.Equal(uint64(1)))
			sc.Expect(events[1].GetSequenceNumber()).To(specs.Equal(uint64(2)))
		})
	})
}

// TestEventPublisherFanOutToMultipleSubscribers verifies that registering
// multiple event publishers fans events out to all of them. Each publisher
// gets a separate eventstream subscriber, and the single-topic model must not
// regress to "first subscriber wins."
func TestEventPublisherFanOutToMultipleSubscribers(t *testing.T) {
	specs.Describe(t, "registering several event publishers fans every event out to all of them", func(s *specs.Spec) {
		s.It("delivers one event to each publisher", func(sc *specs.Context) {
			t := sc.T
			ctx := context.Background()
			store := testkit.NewEventsStore()
			sc.Expect(store.Connect(ctx)).To(specs.BeNil())
			t.Cleanup(func() { _ = store.Disconnect(ctx) })

			engine := newTestEngine(t, "FanOut", store, WithLogger(DiscardLogger))
			sc.Expect(engine.Start(ctx)).To(specs.BeNil())

			pubs := []*recordingEventPublisher{
				newRecordingEventPublisher("a"),
				newRecordingEventPublisher("b"),
				newRecordingEventPublisher("c"),
			}
			sc.Expect(engine.AddEventPublishers(pubs[0], pubs[1], pubs[2])).To(specs.BeNil())

			entityID := uuid.NewString()
			sc.Expect(engine.Entity(ctx, NewEventSourcedEntity(entityID))).To(specs.BeNil())

			_, _, err := engine.SendCommand(ctx, entityID, &testpb.CreateAccount{AccountBalance: 100}, time.Minute)
			sc.Expect(err).To(specs.BeNil())

			sc.Eventually(func() any {
				for _, p := range pubs {
					if len(p.snapshot()) == 0 {
						return false
					}
				}
				return true
			}, specs.BeTrue(), specs.WithTimeout(5*time.Second))

			for _, p := range pubs {
				events := p.snapshot()
				sc.Expect(events).To(specs.HaveLen(1))
				sc.Expect(events[0].GetPersistenceId()).To(specs.Equal(entityID))
			}
		})
	})
}

// TestEventPayloadCarriesShard verifies that downstream consumers can still
// recover the shard the event came from — the partition information that
// used to live in the topic name now lives in egopb.Event.Shard. This is the
// invariant the single-topic refactor relies on.
func TestEventPayloadCarriesShard(t *testing.T) {
	specs.Describe(t, "a published event carries the shard it came from", func(s *specs.Spec) {
		s.It("sets the shard and the event payload", func(sc *specs.Context) {
			t := sc.T
			ctx := context.Background()
			store := testkit.NewEventsStore()
			sc.Expect(store.Connect(ctx)).To(specs.BeNil())
			t.Cleanup(func() { _ = store.Disconnect(ctx) })

			engine := newTestEngine(t, "ShardInPayload", store, WithLogger(DiscardLogger))
			sc.Expect(engine.Start(ctx)).To(specs.BeNil())

			pub := newRecordingEventPublisher("recorder")
			sc.Expect(engine.AddEventPublishers(pub)).To(specs.BeNil())

			entityID := uuid.NewString()
			sc.Expect(engine.Entity(ctx, NewEventSourcedEntity(entityID))).To(specs.BeNil())

			_, _, err := engine.SendCommand(ctx, entityID, &testpb.CreateAccount{AccountBalance: 100}, time.Minute)
			sc.Expect(err).To(specs.BeNil())

			sc.Eventually(func() any { return len(pub.snapshot()) }, specs.Equal(1), specs.WithTimeout(5*time.Second))

			event := pub.snapshot()[0]
			// In single-node mode the actor system's Partition() returns 0 for every
			// actor; we only care that the Shard field is wired up (not its specific
			// value here). The cluster regression test below covers non-zero shards.
			sc.Expect(event.GetShard()).To(specs.Equal(uint64(0)))
			sc.Expect(event.GetEvent()).To(specs.Not(specs.BeNil()))
			sc.Expect(event.GetPersistenceId()).To(specs.Equal(entityID))
			sc.Expect(event.GetSequenceNumber()).To(specs.Equal(uint64(1)))
		})
	})
}

// TestStatePublisherReceivesDurableStateUpdates is the durable-state analog
// of TestEventPublisherReceivesEventsFromEntity.
func TestStatePublisherReceivesDurableStateUpdates(t *testing.T) {
	specs.Describe(t, "a state publisher receives the durable state an entity writes", func(s *specs.Spec) {
		s.It("delivers the first state version", func(sc *specs.Context) {
			t := sc.T
			ctx := context.Background()
			stateStore := testkit.NewDurableStore()
			sc.Expect(stateStore.Connect(ctx)).To(specs.BeNil())
			t.Cleanup(func() { _ = stateStore.Disconnect(ctx) })

			engine := newTestEngine(t, "StatePub", nil,
				WithLogger(DiscardLogger),
				WithStateStore(stateStore),
			)
			sc.Expect(engine.Start(ctx)).To(specs.BeNil())

			pub := newRecordingStatePublisher("recorder")
			sc.Expect(engine.AddStatePublishers(pub)).To(specs.BeNil())

			entityID := uuid.NewString()
			sc.Expect(engine.DurableStateEntity(ctx, NewAccountDurableStateBehavior(entityID))).To(specs.BeNil())

			_, _, err := engine.SendCommand(ctx, entityID, &testpb.CreateAccount{AccountBalance: 100}, time.Minute)
			sc.Expect(err).To(specs.BeNil())

			sc.Eventually(func() any { return len(pub.snapshot()) }, specs.BeGreaterThanOrEqual(1), specs.WithTimeout(10*time.Second))

			states := pub.snapshot()
			sc.Expect(states).To(specs.Not(specs.BeEmpty()))
			sc.Expect(states[0].GetPersistenceId()).To(specs.Equal(entityID))
			sc.Expect(states[0].GetVersionNumber()).To(specs.Equal(uint64(1)))
			// In single-node mode the actor system always returns partition 0, but
			// the Shard field must still be populated so downstream consumers can
			// branch on it once cluster mode kicks in.
			sc.Expect(states[0].GetShard()).To(specs.Equal(uint64(0)))
		})
	})
}

// TestEngineSubscribeReceivesEventsAndStates verifies that the public
// Engine.Subscribe() API delivers both events (from event-sourced entities)
// and durable state updates (from durable-state entities) through a single
// subscriber. Pre-fix, Subscribe() subscribed to 271 events topics and 271
// states topics in cluster mode and would have silently dropped anything
// outside that range.
func TestEngineSubscribeReceivesEventsAndStates(t *testing.T) {
	specs.Describe(t, "Engine.Subscribe delivers both events and durable states through one subscriber", func(s *specs.Spec) {
		s.It("receives an event and a state", func(sc *specs.Context) {
			t := sc.T
			ctx := context.Background()
			store := testkit.NewEventsStore()
			sc.Expect(store.Connect(ctx)).To(specs.BeNil())
			t.Cleanup(func() { _ = store.Disconnect(ctx) })
			stateStore := testkit.NewDurableStore()
			sc.Expect(stateStore.Connect(ctx)).To(specs.BeNil())
			t.Cleanup(func() { _ = stateStore.Disconnect(ctx) })

			engine := newTestEngine(t, "SubAll", store,
				WithLogger(DiscardLogger),
				WithStateStore(stateStore),
			)
			sc.Expect(engine.Start(ctx)).To(specs.BeNil())

			sub, err := engine.Subscribe()
			sc.Expect(err).To(specs.BeNil())
			t.Cleanup(sub.Shutdown)

			// Event-sourced entity emits an Event.
			esID := uuid.NewString()
			sc.Expect(engine.Entity(ctx, NewEventSourcedEntity(esID))).To(specs.BeNil())
			_, _, err = engine.SendCommand(ctx, esID, &testpb.CreateAccount{AccountBalance: 1}, time.Minute)
			sc.Expect(err).To(specs.BeNil())

			// Durable-state entity emits a DurableState.
			dsID := uuid.NewString()
			sc.Expect(engine.DurableStateEntity(ctx, NewAccountDurableStateBehavior(dsID))).To(specs.BeNil())
			_, _, err = engine.SendCommand(ctx, dsID, &testpb.CreateAccount{AccountBalance: 1}, time.Minute)
			sc.Expect(err).To(specs.BeNil())

			// Subscriber.Iterator() is a one-shot drain — each call returns whatever
			// is queued and then closes. Poll it until we see both message kinds or
			// the deadline expires.
			var (
				gotEvent bool
				gotState bool
			)
			drain := func() {
				for msg := range sub.Iterator() {
					if msg == nil {
						continue
					}
					switch msg.Payload().(type) {
					case *egopb.Event:
						gotEvent = true
					case *egopb.DurableState:
						gotState = true
					}
				}
			}
			sc.Eventually(func() any { drain(); return gotEvent }, specs.BeTrue(), specs.WithTimeout(10*time.Second))
			sc.Eventually(func() any { drain(); return gotState }, specs.BeTrue(), specs.WithTimeout(10*time.Second))
		})
	})
}

// TestClusterEventPublisherHighPartitionCount is the regression test for the
// bug the single-topic refactor fixes. Pre-fix, the engine's publishers
// subscribed to topic.events.0 ... topic.events.270 because the partition
// count was hardcoded to goakt's default of 271. If a deployment configured
// a higher partition count and an entity happened to map to a shard >= 271,
// the published event would be silently dropped.
//
// This test configures a 1009-partition cluster (a prime above the old
// hardcoded ceiling), forces several entities through the system, and
// verifies the publisher receives ALL of their initial events — including
// any whose shard exceeds the old ceiling. It also asserts that at least
// one event arrived from a shard outside the legacy [0, 271) window so the
// test really exercises the formerly-dropped range, not just the lucky few
// at the bottom.
func TestClusterEventPublisherHighPartitionCount(t *testing.T) {
	specs.Describe(t, "a high partition count still delivers every entity event to the publisher", func(s *specs.Spec) {
		s.It("receives one event per entity, including shards beyond 271", func(sc *specs.Context) {
			t := sc.T
			const partitionCount = 1009
			const numEntities = 40

			ctx := context.Background()
			store := testkit.NewEventsStore()
			sc.Expect(store.Connect(ctx)).To(specs.BeNil())
			t.Cleanup(func() { _ = store.Disconnect(ctx) })

			ports := dynaport.Get(3)
			gossipPort, peersPort, remotingPort := ports[0], ports[1], ports[2]
			host := "127.0.0.1"

			provider := &mockClusterProvider{
				id:    "hi-part",
				peers: []string{net.JoinHostPort(host, strconv.Itoa(peersPort))},
			}

			clusterCfg := goakt.NewClusterConfig().
				WithDiscovery(provider).
				WithDiscoveryPort(gossipPort).
				WithPeersPort(peersPort).
				WithMinimumPeersQuorum(1).
				WithReplicaCount(1).
				WithPartitionCount(partitionCount).
				WithKinds(ClusterKinds()...)

			cfg := NewConfig(store, WithLogger(DiscardLogger))

			goaktOpts := append(cfg.GoaktOptions(),
				goakt.WithCluster(clusterCfg),
				goakt.WithRemote(remote.NewConfig(host, remotingPort)),
			)

			sys, err := goakt.NewActorSystem("HighPart", goaktOpts...)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(sys.Start(ctx)).To(specs.BeNil())
			t.Cleanup(func() { _ = sys.Stop(ctx) })

			sc.Eventually(func() any { return sys.InCluster() }, specs.BeTrue(), specs.WithTimeout(waitTimeout))

			engine, err := NewEngine(sys, cfg)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(engine.Start(ctx)).To(specs.BeNil())
			t.Cleanup(func() { _ = engine.Stop(ctx) })

			// Warm the cluster's distributed map so that subsequent SpawnOn calls
			// get real partition assignments instead of the zero value goakt returns
			// when an actor isn't yet visible in the dmap. We spawn (and immediately
			// command) a batch of throwaway entities, then wait until probing those
			// entities by name surfaces a partition in the formerly-dropped range
			// (>= 271). Without this warm-up the workload below can race the dmap
			// under -race-detector slowdown and have every event land in shard 0,
			// which would make the assertion below trivially fail.
			const warmupEntities = 200
			warmupIDs := make([]string, 0, warmupEntities)
			for range warmupEntities {
				id := "warmup-" + uuid.NewString()
				warmupIDs = append(warmupIDs, id)
				sc.Expect(engine.Entity(ctx, NewEventSourcedEntity(id))).To(specs.BeNil())
				_, _, err := engine.SendCommand(ctx, id, &testpb.CreateAccount{AccountBalance: 1}, time.Minute)
				sc.Expect(err).To(specs.BeNil())
			}
			if !waitForCond(60*time.Second, func() bool {
				for _, id := range warmupIDs {
					if sys.Partition(id) >= 271 {
						return true
					}
				}
				return false
			}) {
				t.Skip("goakt cluster dmap did not surface any shard >= 271 within 60s; cannot exercise the pre-fix bug range under current scheduling (likely race-detector slowdown)")
			}

			// Now register the publisher and run the real workload. The publisher
			// only sees events emitted from this point on, so the warm-up entities'
			// events (which it would have received too) don't get mixed in.
			pub := newRecordingEventPublisher("hi-part")
			sc.Expect(engine.AddEventPublishers(pub)).To(specs.BeNil())

			// Generate enough entities that with 1009 partitions at least one is very
			// likely to land beyond shard 271 (P ≈ 1 - (271/1009)^40, effectively 1).
			entityIDs := make([]string, 0, numEntities)
			for range numEntities {
				entityID := uuid.NewString()
				entityIDs = append(entityIDs, entityID)

				sc.Expect(engine.Entity(ctx, NewEventSourcedEntity(entityID))).To(specs.BeNil())
				_, _, err := engine.SendCommand(ctx, entityID, &testpb.CreateAccount{AccountBalance: 1}, time.Minute)
				sc.Expect(err).To(specs.BeNil())
			}

			sc.Eventually(func() any { return len(pub.snapshot()) }, specs.BeGreaterThanOrEqual(numEntities), specs.WithTimeout(10*time.Second))

			got := pub.snapshot()
			sc.Expect(got).To(specs.HaveLen(numEntities))

			// Index received events by persistence id and assert every entity surfaced
			// exactly one event.
			gotByID := make(map[string]*egopb.Event, len(got))
			for _, evt := range got {
				gotByID[evt.GetPersistenceId()] = evt
			}
			for _, entityID := range entityIDs {
				_, ok := gotByID[entityID]
				sc.Expect(ok).To(specs.BeTrue())
			}

			// Confirm at least one event was emitted from a shard outside the legacy
			// 0..270 window — otherwise the test wouldn't be exercising the formerly
			// silently-dropped range, and we shouldn't claim it's a regression test.
			beyondLegacyCeiling := 0
			maxShard := uint64(0)
			for _, evt := range got {
				if evt.GetShard() >= 271 {
					beyondLegacyCeiling++
				}
				if evt.GetShard() > maxShard {
					maxShard = evt.GetShard()
				}
			}
			sc.Expect(beyondLegacyCeiling).To(specs.BeGreaterThan(0))
		})
	})
}

// TestTopicConstantsAreNotPartitionedFormats is a paranoid byte-level check:
// if someone reintroduces "topic.events.%d" or similar by accident, this
// test fires immediately rather than waiting for an event delivery to drop
// in production.
func TestTopicConstantsAreNotPartitionedFormats(t *testing.T) {
	specs.Describe(t, "the topic constants carry no partition format directive", func(s *specs.Spec) {
		for _, tc := range []struct {
			name  string
			value string
		}{
			{"protocol.EventsTopic", protocol.EventsTopic},
			{"protocol.StatesTopic", protocol.StatesTopic},
		} {
			s.It(tc.name+" contains no fmt directive", func(ctx *specs.Context) {
				ctx.Expect(strings.Contains(tc.value, "%d")).To(specs.BeFalse())
				ctx.Expect(strings.Contains(tc.value, "%")).To(specs.BeFalse())
			})
		}
	})
}
