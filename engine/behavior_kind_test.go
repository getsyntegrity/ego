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
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	goakt "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/extension"
	"github.com/tochemey/goakt/v4/remote"
	"github.com/travisjeffery/go-dynaport"

	"github.com/getsyntegrity/ego/testkit"
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
	specs.Describe(t, "a behavior kind moves between EntityKind, extension.Dependency and BehaviorKind keeping its dynamic value", func(s *specs.Spec) {
		s.It("keeps the same pointer in both directions and in the registration list", func(ctx *specs.Context) {
			account := new(AccountEventSourcedBehavior)

			var entityKind EntityKind = account
			var behaviorKind BehaviorKind = entityKind
			var dependency extension.Dependency = behaviorKind
			var back EntityKind = behaviorKind

			ctx.Expect(behaviorKind == account).To(specs.BeTrue())
			ctx.Expect(dependency == account).To(specs.BeTrue())
			ctx.Expect(back == account).To(specs.BeTrue())

			entityKinds := []EntityKind{account, new(AccountDurableStateBehavior)}
			behaviorKinds := make([]BehaviorKind, 0, len(entityKinds))
			for _, kind := range entityKinds {
				behaviorKinds = append(behaviorKinds, kind)
			}
			cfg := NewConfig(testkit.NewEventsStore(), WithBehaviorKinds(behaviorKinds...))
			ctx.Expect(cfg.behaviorKinds).ToEqual(behaviorKinds)
		})
	})
}

// TestWithEntityKindsAndWithBehaviorKindsShareRegistration checks that both
// options append, in call order, to the one registration list NewEngine
// injects, and that an engine built with both options starts.
func TestWithEntityKindsAndWithBehaviorKindsShareRegistration(t *testing.T) {
	specs.Describe(t, "With Entity Kinds And With Behavior Kinds Share Registration", func(s *specs.Spec) {
		s.It("holds", func(sc *specs.Context) {
			t := sc.T
			ctx := context.Background()
			eventSourced := new(AccountEventSourcedBehavior)
			durableState := new(AccountDurableStateBehavior)
			legacy := []EntityKind{new(FailingHandleEventBehavior)}

			store := testkit.NewEventsStore()
			sc.Expect(store.Connect(ctx)).To(specs.BeNil())
			t.Cleanup(func() { _ = store.Disconnect(ctx) })

			opts := []Option{
				WithLogger(DiscardLogger),
				WithEntityKinds(eventSourced),
				WithBehaviorKinds(durableState),
				WithEntityKinds(legacy...),
			}
			cfg := NewConfig(store, opts...)
			sc.Expect(cfg.behaviorKinds).To(specs.Equal([]BehaviorKind{eventSourced, durableState, legacy[0]}))

			engine := newTestEngine(t, "SharedKinds", store, opts...)
			sc.Expect(engine.Start(ctx)).To(specs.BeNil())
		})
	})
}

// kindOptions are the two registration options, keyed by name.
func kindOptions() map[string]func(BehaviorKind) Option {
	return map[string]func(BehaviorKind) Option{
		"WithBehaviorKinds": func(k BehaviorKind) Option { return WithBehaviorKinds(k) },
		"WithEntityKinds":   func(k BehaviorKind) Option { return WithEntityKinds(k) },
	}
}

// TestErrBehaviorNotPointerMessage checks that the message states the three
// conditions it covers: a spawned behavior must be non-nil in any mode, and a
// pointer in cluster mode; a registered kind must be a pointer type, and a
// typed nil is allowed.
func TestErrBehaviorNotPointerMessage(t *testing.T) {
	specs.Describe(t, "ErrBehaviorNotPointer states the conditions on a spawned behavior and a registered kind", func(s *specs.Spec) {
		s.It("names the three conditions in its message", func(ctx *specs.Context) {
			msg := ErrBehaviorNotPointer.Error()
			ctx.Expect(msg).To(specs.Contain("a behavior must be non-nil to be spawned"))
			ctx.Expect(msg).To(specs.Contain("and a pointer to be spawned in cluster mode"))
			ctx.Expect(msg).To(specs.Contain("a behavior kind registered with WithBehaviorKinds or WithEntityKinds must be a pointer type (a typed nil is allowed)"))
		})
	})
}

// unregistrableKinds are kinds GoAkt's type registry cannot name, each with
// the Kind string NewEngine must report. A typed-nil pointer is not one of
// them: the registry names it through its pointer type, like new(T).
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
	}
}

// TestNewEngineAcceptsTypedNilPointerKind keeps v4 behavior: GoAkt registers
// a kind by its pointer type and decodes into a fresh value of that type, so
// a typed-nil pointer such as (*T)(nil) registers the same type as new(T) and
// must keep working through both options.
func TestNewEngineAcceptsTypedNilPointerKind(t *testing.T) {
	specs.Describe(t, "New Engine Accepts Typed Nil Pointer Kind", func(s *specs.Spec) {
		for optName, option := range kindOptions() {
			s.It(optName, func(sc *specs.Context) {
				t := sc.T
				ctx := context.Background()
				store := testkit.NewEventsStore()
				sc.Expect(store.Connect(ctx)).To(specs.BeNil())
				t.Cleanup(func() { _ = store.Disconnect(ctx) })

				cfg := NewConfig(store, WithLogger(DiscardLogger), option((*AccountEventSourcedBehavior)(nil)))
				sys, err := goakt.NewActorSystem("TypedNilKind", cfg.GoaktOptions()...)
				sc.Expect(err).To(specs.BeNil())
				sc.Expect(sys.Start(ctx)).To(specs.BeNil())

				var engine *Engine
				sc.Expect(panicValue(func() { engine, err = NewEngine(sys, cfg) })).To(specs.BeNil())
				sc.Expect(err).To(specs.BeNil())
				sc.Expect(engine.Start(ctx)).To(specs.BeNil())
				sc.Expect(engine.Stop(ctx)).To(specs.BeNil())
				sc.Expect(sys.Stop(ctx)).To(specs.BeNil())
			})
		}
	})
}

// requireKindRejected builds an engine on sys with cfg and asserts that
// NewEngine returns a *BehaviorPlacementError wrapping ErrBehaviorNotPointer
// without panicking, and leaves the actor system usable: GoAkt's Inject
// panics while holding the actor-system lock, so a successful Stop proves
// Inject was never reached with the bad kind.
func requireKindRejected(sc *specs.Context, sys goakt.ActorSystem, cfg *Config, want string) {
	var (
		engine *Engine
		err    error
	)
	sc.Expect(panicValue(func() { engine, err = NewEngine(sys, cfg) })).To(specs.BeNil())
	sc.Expect(engine).To(specs.BeNil())
	sc.Expect(err).To(specs.MatchError(ErrBehaviorNotPointer))
	var placement *BehaviorPlacementError
	sc.Expect(err).To(specs.MatchErrorAs(&placement))
	sc.Expect(placement.Kind).To(specs.Equal(want))
	sc.Expect(placement.EntityID).To(specs.BeEmpty())
	sc.Expect(sys.Stop(context.Background())).To(specs.BeNil())
}

// TestNewEngineRejectsUnregistrableKindsSingleNode covers the kind check on
// a single node, where kind registration still goes through GoAkt's type
// registry, for both registration options.
func TestNewEngineRejectsUnregistrableKindsSingleNode(t *testing.T) {
	specs.Describe(t, "New Engine Rejects Unregistrable Kinds Single Node", func(s *specs.Spec) {
		for optName, option := range kindOptions() {
			for _, tc := range unregistrableKinds() {
				s.It(optName+"/"+tc.name, func(sc *specs.Context) {
					t := sc.T
					ctx := context.Background()
					store := testkit.NewEventsStore()
					sc.Expect(store.Connect(ctx)).To(specs.BeNil())
					t.Cleanup(func() { _ = store.Disconnect(ctx) })

					// A valid kind first: the check covers the whole list before
					// anything is injected.
					cfg := NewConfig(store, WithLogger(DiscardLogger),
						WithBehaviorKinds(new(AccountEventSourcedBehavior)), option(tc.kind))
					sys, err := goakt.NewActorSystem("KindCheck", cfg.GoaktOptions()...)
					sc.Expect(err).To(specs.BeNil())
					sc.Expect(sys.Start(ctx)).To(specs.BeNil())

					requireKindRejected(sc, sys, cfg, tc.want)
				})
			}
		}
	})
}

// TestClusterNewEngineRejectsValueTypeKind covers the kind check on a
// one-member cluster.
func TestClusterNewEngineRejectsValueTypeKind(t *testing.T) {
	specs.Describe(t, "New Engine Rejects Value Type Kind In Cluster Mode", func(s *specs.Spec) {
		s.It("holds", func(sc *specs.Context) {
			t := sc.T
			ctx := context.Background()
			store := testkit.NewEventsStore()
			sc.Expect(store.Connect(ctx)).To(specs.BeNil())
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
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(sys.Start(ctx)).To(specs.BeNil())
			sc.Eventually(func() any { return sys.InCluster() }, specs.BeTrue(), specs.WithTimeout(10*time.Second), specs.WithInterval(50*time.Millisecond))

			requireKindRejected(sc, sys, cfg, fmt.Sprintf("%T", valueTypeEventSourcedBehavior{}))
		})
	})
}
