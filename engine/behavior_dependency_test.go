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
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"
	goakt "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/extension"
	"github.com/tochemey/goakt/v4/remote"
	"github.com/travisjeffery/go-dynaport"

	"github.com/getsyntegrity/ego/command"
	"github.com/getsyntegrity/ego/internal/extensions"
	testpb "github.com/getsyntegrity/ego/internal/testpb"
	behaviorport "github.com/getsyntegrity/ego/port/behavior"
	"github.com/getsyntegrity/ego/testkit"
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
	specs.Describe(t, "spawnDependency picks the dependency handed to GoAkt for a behavior value", func(s *specs.Spec) {
		for _, mode := range []struct {
			name      string
			inCluster bool
		}{
			{"outside cluster mode", false},
			{"in cluster mode", true},
		} {
			s.It("serializable pointer passes through as the same pointer "+mode.name, func(ctx *specs.Context) {
				sys := &placementProbeSystem{inCluster: mode.inCluster}
				b := NewAccountEventSourcedBehavior("acct-1")

				dep, err := spawnDependency(sys, b)
				ctx.Expect(err).To(specs.BeNil())
				// Identity, not equality: the wire bytes and the GoAkt type name
				// stay exactly those of the caller's value.
				got, ok := dep.(*AccountEventSourcedBehavior)
				ctx.Expect(ok).To(specs.BeTrue())
				ctx.Expect(got == b).To(specs.BeTrue())
				ctx.Expect(len(sys.injected)).ToEqual(1)
				injected, ok := sys.injected[0].(*AccountEventSourcedBehavior)
				ctx.Expect(ok).To(specs.BeTrue())
				ctx.Expect(injected == b).To(specs.BeTrue())
			})
		}

		s.It("value type outside cluster mode is carried locally and never injected", func(ctx *specs.Context) {
			sys := &placementProbeSystem{}
			b := valueTypeEventSourcedBehavior{id: "acct-2"}

			dep, err := spawnDependency(sys, b)
			ctx.Expect(err).To(specs.BeNil())
			local, ok := dep.(*extensions.LocalBehavior)
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(local.ID()).ToEqual("acct-2")
			ctx.Expect(local.Behavior()).ToEqual(b)
			ctx.Expect(len(sys.injected)).ToEqual(0)
		})

		for _, tc := range []struct {
			name     string
			behavior interface{ ID() string }
		}{
			{"event-sourced", &domainOnlyEventSourced{id: "es"}},
			{"durable state", &domainOnlyDurableState{id: "ds"}},
			{"saga", &domainOnlySaga{id: "saga"}},
		} {
			s.It("domain-only "+tc.name+" behavior outside cluster mode is carried locally", func(ctx *specs.Context) {
				sys := &placementProbeSystem{}
				dep, err := spawnDependency(sys, tc.behavior)
				ctx.Expect(err).To(specs.BeNil())
				local, ok := dep.(*extensions.LocalBehavior)
				ctx.Expect(ok).To(specs.BeTrue())
				ctx.Expect(local.Behavior() == tc.behavior).To(specs.BeTrue())
				ctx.Expect(local.ID()).ToEqual(tc.behavior.ID())
				ctx.Expect(len(sys.injected)).ToEqual(0)
			})
		}

		for _, tc := range []struct {
			name     string
			behavior interface{ ID() string }
			cause    error
			kind     string
		}{
			{"value type", valueTypeEventSourcedBehavior{id: "v-1"}, ErrBehaviorNotPointer, "engine.valueTypeEventSourcedBehavior"},
			{"domain-only event-sourced", &domainOnlyEventSourced{id: "v-1"}, ErrBehaviorNotSerializable, "*engine.domainOnlyEventSourced"},
			{"domain-only durable state", &domainOnlyDurableState{id: "v-1"}, ErrBehaviorNotSerializable, "*engine.domainOnlyDurableState"},
			{"domain-only saga", &domainOnlySaga{id: "v-1"}, ErrBehaviorNotSerializable, "*engine.domainOnlySaga"},
		} {
			s.It("cluster mode rejects a "+tc.name+" with a typed error before anything is injected", func(ctx *specs.Context) {
				sys := &placementProbeSystem{inCluster: true}
				dep, err := spawnDependency(sys, tc.behavior)
				ctx.Expect(dep == nil).To(specs.BeTrue())
				ctx.Expect(err).To(specs.MatchError(tc.cause))

				var placement *BehaviorPlacementError
				ctx.Expect(err).To(specs.MatchErrorAs(&placement))
				ctx.Expect(placement.Kind).ToEqual(tc.kind)
				ctx.Expect(placement.EntityID).ToEqual("v-1")
				ctx.Expect(len(sys.injected)).ToEqual(0)
			})
		}

		for _, inCluster := range []bool{false, true} {
			mode := "outside cluster mode"
			if inCluster {
				mode = "in cluster mode"
			}
			for _, tc := range []struct {
				name     string
				behavior interface{ ID() string }
				kind     string
			}{
				{"nil", nil, "<nil>"},
				{"typed-nil serializable event-sourced", (*AccountEventSourcedBehavior)(nil), "*enginetest.AccountEventSourcedBehavior"},
				{"typed-nil domain-only event-sourced", (*domainOnlyEventSourced)(nil), "*engine.domainOnlyEventSourced"},
				{"typed-nil serializable durable state", (*AccountDurableStateBehavior)(nil), "*enginetest.AccountDurableStateBehavior"},
				{"typed-nil domain-only durable state", (*domainOnlyDurableState)(nil), "*engine.domainOnlyDurableState"},
				{"typed-nil serializable saga", (*testSagaBehavior)(nil), "*engine.testSagaBehavior"},
				{"typed-nil domain-only saga", (*domainOnlySaga)(nil), "*engine.domainOnlySaga"},
			} {
				s.It(tc.name+" behavior is rejected "+mode, func(ctx *specs.Context) {
					sys := &placementProbeSystem{inCluster: inCluster}
					dep, err := spawnDependency(sys, tc.behavior)
					ctx.Expect(dep == nil).To(specs.BeTrue())
					ctx.Expect(err).To(specs.MatchError(ErrBehaviorNotPointer))
					var placement *BehaviorPlacementError
					ctx.Expect(err).To(specs.MatchErrorAs(&placement))
					ctx.Expect(placement.Kind).ToEqual(tc.kind)
					// a nil behavior has no readable ID
					ctx.Expect(placement.EntityID).ToEqual("")
					ctx.Expect(len(sys.injected)).ToEqual(0)
				})
			}
		}
	})
}

// nilBehaviorSpawn is one spawn of a nil or typed-nil behavior.
type nilBehaviorSpawn struct {
	name  string
	spawn func(ctx context.Context, engine *Engine) error
}

// nilBehaviorSpawns returns one spawn per family for a nil behavior and for
// typed-nil pointers, through the old public API and the unexported spawn
// functions. Each must be rejected before it reaches GoAkt.
func nilBehaviorSpawns() []nilBehaviorSpawn {
	return []nilBehaviorSpawn{
		{"Entity nil", func(ctx context.Context, engine *Engine) error { return engine.Entity(ctx, nil) }},
		{"Entity typed-nil", func(ctx context.Context, engine *Engine) error {
			return engine.Entity(ctx, (*AccountEventSourcedBehavior)(nil))
		}},
		{"spawnEventSourced typed-nil domain-only", func(ctx context.Context, engine *Engine) error {
			return engine.spawnEventSourced(ctx, (*domainOnlyEventSourced)(nil))
		}},
		{"DurableStateEntity nil", func(ctx context.Context, engine *Engine) error { return engine.DurableStateEntity(ctx, nil) }},
		{"DurableStateEntity typed-nil", func(ctx context.Context, engine *Engine) error {
			return engine.DurableStateEntity(ctx, (*AccountDurableStateBehavior)(nil))
		}},
		{"spawnDurableState typed-nil domain-only", func(ctx context.Context, engine *Engine) error {
			return engine.spawnDurableState(ctx, (*domainOnlyDurableState)(nil))
		}},
		{"Saga nil", func(ctx context.Context, engine *Engine) error { return engine.Saga(ctx, nil, 0) }},
		{"Saga typed-nil", func(ctx context.Context, engine *Engine) error {
			return engine.Saga(ctx, (*testSagaBehavior)(nil), 0)
		}},
		{"spawnSaga typed-nil domain-only", func(ctx context.Context, engine *Engine) error {
			return engine.spawnSaga(ctx, (*domainOnlySaga)(nil), 0)
		}},
	}
}

// nilBehaviorRejectionSpecs registers one case per nilBehaviorSpawns entry on s.
// Each case checks that the spawn returns a *BehaviorPlacementError wrapping
// ErrBehaviorNotPointer, does not panic, and spawns nothing. engine is read when
// a case runs, so the caller may build the engine in a BeforeEach hook.
func nilBehaviorRejectionSpecs(s *specs.Spec, engine func() *Engine) {
	for _, tc := range nilBehaviorSpawns() {
		s.It(tc.name, func(sc *specs.Context) {
			ctx := context.Background()
			e := engine()
			sys := e.ActorSystem()
			before := sys.NumActors()
			var err error
			sc.Expect(panicValue(func() { err = tc.spawn(ctx, e) })).To(specs.BeNil())
			sc.Expect(err).To(specs.MatchError(ErrBehaviorNotPointer))
			var placement *BehaviorPlacementError
			sc.Expect(err).To(specs.MatchErrorAs(&placement))
			sc.Expect(placement.EntityID).To(specs.BeEmpty())
			// nothing may be spawned for a nil behavior
			sc.Expect(sys.NumActors()).To(specs.Equal(before))
		})
	}
}

// TestEngineRejectsNilBehaviorsSingleNode covers nil and typed-nil
// behaviors outside cluster mode, where a non-serializable behavior would
// otherwise be carried by a LocalBehavior and its ID read at spawn.
func TestEngineRejectsNilBehaviorsSingleNode(t *testing.T) {
	specs.Describe(t, "Engine Rejects Nil Behaviors Single Node", func(s *specs.Spec) {
		ctx := context.Background()
		store := testkit.NewEventsStore()
		t.Cleanup(func() { _ = store.Disconnect(ctx) })
		stateStore := testkit.NewDurableStore()
		t.Cleanup(func() { _ = stateStore.Disconnect(ctx) })

		engine := newTestEngine(t, "NilBehaviors", store, WithLogger(DiscardLogger), WithStateStore(stateStore))
		// The engine is shared by the cases below, so the stores connect and the engine starts once.
		var startOnce sync.Once
		s.BeforeEach(func(sc *specs.Context) {
			startOnce.Do(func() {
				sc.Expect(store.Connect(ctx)).To(specs.BeNil())
				sc.Expect(stateStore.Connect(ctx)).To(specs.BeNil())
				sc.Expect(engine.Start(ctx)).To(specs.BeNil())
			})
		})

		nilBehaviorRejectionSpecs(s, func() *Engine { return engine })
	})
}

func TestBehaviorPlacementError(t *testing.T) {
	specs.Describe(t, "BehaviorPlacementError reports the kind, the entity id and the cause it wraps", func(s *specs.Spec) {
		s.It("names the kind, the id and the cause of a spawn error and unwraps to the cause", func(ctx *specs.Context) {
			spawnErr := &BehaviorPlacementError{Kind: "*main.Account", EntityID: "acct-1", Err: ErrBehaviorNotSerializable}
			ctx.Expect(spawnErr.Error()).To(specs.Contain("*main.Account"))
			ctx.Expect(spawnErr.Error()).To(specs.Contain(`"acct-1"`))
			ctx.Expect(spawnErr.Error()).To(specs.Contain(ErrBehaviorNotSerializable.Error()))
			ctx.Expect(errors.Unwrap(spawnErr) == ErrBehaviorNotSerializable).To(specs.BeTrue()) //nolint:errorlint // identity is the point

			registrationErr := &BehaviorPlacementError{Kind: "main.Account", Err: ErrBehaviorNotPointer}
			ctx.Expect(registrationErr.Error()).To(specs.Contain("main.Account"))
			ctx.Expect(registrationErr.Error()).To(specs.Not(specs.Contain(`""`)))
			ctx.Expect(registrationErr).To(specs.MatchError(ErrBehaviorNotPointer))
			ctx.Expect(registrationErr).To(specs.Not(specs.MatchError(ErrBehaviorNotSerializable)))
		})
	})
}

func TestBehaviorFrom(t *testing.T) {
	specs.Describe(t, "extensions.BehaviorFrom finds a behavior of the requested family", func(s *specs.Spec) {
		s.It("returns the pointer behavior, the wrapped domain-only one, and no match for another family", func(ctx *specs.Context) {
			pointer := NewAccountEventSourcedBehavior("acct-1")
			got, ok := extensions.BehaviorFrom[behaviorport.EventSourced](pointer)
			ctx.Expect(ok).To(specs.BeTrue())
			gotPointer, ok := got.(*AccountEventSourcedBehavior)
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(gotPointer == pointer).To(specs.BeTrue())

			domainOnly := &domainOnlyDurableState{id: "ds-1"}
			gotDS, ok := extensions.BehaviorFrom[behaviorport.DurableState](extensions.NewLocalBehavior(domainOnly))
			ctx.Expect(ok).To(specs.BeTrue())
			gotDomainOnly, ok := gotDS.(*domainOnlyDurableState)
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(gotDomainOnly == domainOnly).To(specs.BeTrue())

			// a wrapped behavior of another family must not match
			_, ok = extensions.BehaviorFrom[behaviorport.EventSourced](extensions.NewLocalBehavior(domainOnly))
			ctx.Expect(ok).To(specs.BeFalse())
			_, ok = extensions.BehaviorFrom[behaviorport.Saga](extensions.NewEntityConfig(1))
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}

// TestEngineSpawnsDomainOnlyBehaviorsSingleNode spawns behaviors that
// implement only the port/behavior contracts through the unexported spawn
// functions (the public Spawn* entry points arrive in S3-3). Each is carried
// by a LocalBehavior and answers on a single node.
func TestEngineSpawnsDomainOnlyBehaviorsSingleNode(t *testing.T) {
	specs.Describe(t, "Engine Spawns Domain Only Behaviors Single Node", func(s *specs.Spec) {
		ctx := context.Background()

		s.It("event-sourced, envelope-capable", func(sc *specs.Context) {
			t := sc.T
			store := testkit.NewEventsStore()
			sc.Expect(store.Connect(ctx)).To(specs.BeNil())
			t.Cleanup(func() { _ = store.Disconnect(ctx) })
			engine := newTestEngine(t, "DomainOnlyES", store, WithLogger(DiscardLogger))
			sc.Expect(engine.Start(ctx)).To(specs.BeNil())

			b := &domainOnlyEventSourced{id: uuid.NewString()}
			sc.Expect(engine.spawnEventSourced(ctx, b)).To(specs.BeNil())

			state, revision, err := engine.SendCommand(ctx, b.ID(), &testpb.CreateAccount{AccountBalance: 7}, time.Minute)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(revision).To(specs.Equal(uint64(1)))
			sc.Expect(state.(*testpb.Account).GetAccountBalance()).To(specs.Equal(float64(7)))
			sc.Expect(b.envelopeHits()).To(specs.Equal(1))
		})

		s.It("durable state", func(sc *specs.Context) {
			t := sc.T
			stateStore := testkit.NewDurableStore()
			sc.Expect(stateStore.Connect(ctx)).To(specs.BeNil())
			t.Cleanup(func() { _ = stateStore.Disconnect(ctx) })
			engine := newTestEngine(t, "DomainOnlyDS", nil, WithLogger(DiscardLogger), WithStateStore(stateStore))
			sc.Expect(engine.Start(ctx)).To(specs.BeNil())

			b := &domainOnlyDurableState{id: uuid.NewString()}
			sc.Expect(engine.spawnDurableState(ctx, b)).To(specs.BeNil())

			state, revision, err := engine.SendCommand(ctx, b.ID(), &testpb.CreateAccount{AccountBalance: 9}, time.Minute)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(revision).To(specs.Equal(uint64(1)))
			sc.Expect(state.(*testpb.Account).GetAccountBalance()).To(specs.Equal(float64(9)))
		})

		s.It("saga", func(sc *specs.Context) {
			t := sc.T
			store := testkit.NewEventsStore()
			sc.Expect(store.Connect(ctx)).To(specs.BeNil())
			t.Cleanup(func() { _ = store.Disconnect(ctx) })
			engine := newTestEngine(t, "DomainOnlySaga", store, WithLogger(DiscardLogger))
			sc.Expect(engine.Start(ctx)).To(specs.BeNil())

			b := &domainOnlySaga{id: "saga-" + uuid.NewString()}
			sc.Expect(engine.spawnSaga(ctx, b, 0)).To(specs.BeNil())

			sc.Eventually(func() any {
				info, err := engine.SagaStatus(ctx, b.ID(), time.Second)
				if err != nil || info == nil {
					return ""
				}
				return info.ID
			}, specs.Equal(b.ID()), specs.WithTimeout(10*time.Second), specs.WithInterval(50*time.Millisecond))
		})
	})
}

// TestClusterEngineRejectsUnplaceableBehaviors runs a single-node
// cluster, where GoAkt serializes every spawn's dependencies, and checks
// that the engine rejects behaviors it cannot hand to GoAkt with a typed
// error before any spawn, instead of panicking.
func TestClusterEngineRejectsUnplaceableBehaviors(t *testing.T) {
	specs.Describe(t, "Engine Rejects Unplaceable Behaviors In Cluster Mode", func(s *specs.Spec) {
		ctx := context.Background()
		store := testkit.NewEventsStore()
		t.Cleanup(func() { _ = store.Disconnect(ctx) })
		stateStore := testkit.NewDurableStore()
		t.Cleanup(func() { _ = stateStore.Disconnect(ctx) })

		// The single-node cluster is shared by the cases below, so it is built
		// once, before the first case runs.
		var (
			startOnce sync.Once
			engine    *Engine
		)
		s.BeforeEach(func(sc *specs.Context) {
			startOnce.Do(func() {
				sc.Expect(store.Connect(ctx)).To(specs.BeNil())
				sc.Expect(stateStore.Connect(ctx)).To(specs.BeNil())

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
				sc.Expect(err).To(specs.BeNil())
				sc.Expect(sys.Start(ctx)).To(specs.BeNil())
				t.Cleanup(func() { _ = sys.Stop(ctx) })
				sc.Eventually(func() any { return sys.InCluster() }, specs.BeTrue(),
					specs.WithTimeout(10*time.Second), specs.WithInterval(50*time.Millisecond))

				engine, err = NewEngine(sys, cfg)
				sc.Expect(err).To(specs.BeNil())
				sc.Expect(engine.Start(ctx)).To(specs.BeNil())
				t.Cleanup(func() { _ = engine.Stop(ctx) })
			})
		})

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
			s.It(tc.name, func(sc *specs.Context) {
				id := uuid.NewString()
				err := tc.spawn(id)
				sc.Expect(err).To(specs.MatchError(tc.cause))
				var placement *BehaviorPlacementError
				sc.Expect(err).To(specs.MatchErrorAs(&placement))
				sc.Expect(placement.EntityID).To(specs.Equal(id))

				exists, err := engine.EntityExists(ctx, id)
				sc.Expect(err).To(specs.BeNil())
				sc.Expect(exists).To(specs.BeFalse())
			})
		}

		s.Describe("nil and typed-nil behaviors", func(s *specs.Spec) {
			nilBehaviorRejectionSpecs(s, func() *Engine { return engine })
		})

		// A serializable pointer behavior still spawns and answers in cluster mode.
		s.It("a serializable pointer behavior still spawns and answers", func(sc *specs.Context) {
			id := uuid.NewString()
			sc.Expect(engine.Entity(ctx, NewAccountEventSourcedBehavior(id))).To(specs.BeNil())
			state, _, err := engine.SendCommand(ctx, id, &testpb.CreateAccount{AccountBalance: 3}, time.Minute)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(state.(*testpb.Account).GetAccountBalance()).To(specs.Equal(float64(3)))
		})
	})
}
