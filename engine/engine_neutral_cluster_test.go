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
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"
	goakt "github.com/tochemey/goakt/v4/actor"

	testpb "github.com/getsyntegrity/urd/internal/testpb"
)

// clusterNodeOptsG4 returns the options of one cluster node: the given
// behavior-kind option plus a durable-state store that is disconnected when the
// case ends.
func clusterNodeOptsG4(ctx *specs.Context, kinds Option) []Option {
	return []Option{kinds, WithStateStore(connectedDurableStore(ctx))}
}

// expectNotSpawnedG4 checks that no node of the cluster hosts an actor named id.
func expectNotSpawnedG4(ctx *specs.Context, cluster *testCluster, id string) {
	for _, sys := range cluster.systems {
		exists, err := sys.ActorExists(context.Background(), id)
		ctx.Expect(err).To(specs.BeNil())
		// no node may host an actor for a rejected behavior
		ctx.Expect(exists).To(specs.BeFalse())
	}
}

// hostedByPeerG4 spawns eight entities from one node, makes each answer a
// command, and counts those the peer node hosts. With RoundRobin placement over
// two members some entities land on the peer, which decodes the behavior with
// the kind it registered.
func hostedByPeerG4(ctx *specs.Context, from *Engine, peer goakt.ActorSystem, spawn func(id string) error) int {
	bg := context.Background()
	hosted := 0
	for range 8 {
		id := uuid.NewString()
		ctx.Expect(spawn(id)).To(specs.BeNil())

		state, _, err := from.SendCommand(bg, id, &testpb.CreateAccount{AccountBalance: 100}, time.Minute)
		ctx.Expect(err).To(specs.BeNil())
		account, ok := state.(*testpb.Account)
		ctx.Expect(ok).To(specs.BeTrue())
		ctx.Expect(account.GetAccountBalance()).ToEqual(float64(100))

		pid, err := peer.ActorOf(bg, id)
		ctx.Expect(err).To(specs.BeNil())
		if pid.IsLocal() {
			hosted++
		}
	}
	return hosted
}

// domainOnlySpawnCaseG4 is one family spawned through its public Spawn method
// with a behavior that implements only the port contracts.
type domainOnlySpawnCaseG4 struct {
	family string
	kind   string
	spawn  func(engine *Engine, id string) error
}

// TestClusterEngineNeutralBehaviors spawns behaviors through the public
// Spawn* methods on a two-node cluster, where GoAkt serializes every spawn's
// dependencies and may place the actor on the peer (ego-arch-002-s3 design,
// §8). Each case starts its own cluster, and only node 1 spawns.
//
// No case recovers a panic: GoAkt's Inject panics on a non-pointer type
// while it holds the actor-system lock, and a recovered panic would leave the
// cluster cleanup blocked on that lock. Run the test with a short timeout
// (-timeout 90s) so either the panic or the timeout ends the binary.
func TestClusterEngineNeutralBehaviors(t *testing.T) {
	specs.Describe(t, "behaviors spawned through the Spawn methods on a two-node cluster", func(s *specs.Spec) {
		// newCluster starts the cluster of the case: both nodes register
		// AccountEventSourcedBehavior through WithEntityKinds.
		newCluster := func(ctx *specs.Context) *testCluster {
			kinds := WithEntityKinds(new(AccountEventSourcedBehavior))
			return newTestCluster(ctx.T, clusterNodeOptsG4(ctx, kinds), clusterNodeOptsG4(ctx, kinds))
		}

		s.It("serializable behavior placed remotely", func(ctx *specs.Context) {
			cluster := newCluster(ctx)

			// With RoundRobin placement over two members, eight spawns from node
			// 1 place some entities on node 2, which decodes the behavior with
			// the kind it registered through WithEntityKinds.
			placedOnNode2 := hostedByPeerG4(ctx, cluster.engines[0], cluster.systems[1], func(id string) error {
				return cluster.engines[0].SpawnEventSourced(context.Background(), NewAccountEventSourcedBehavior(id))
			})
			// at least one entity must be hosted by node 2
			ctx.Expect(placedOnNode2).To(specs.BeGreaterThan(0))
		})

		specs.Table(s, []domainOnlySpawnCaseG4{
			{"event-sourced", fmt.Sprintf("%T", &domainOnlyEventSourced{}), func(e *Engine, id string) error {
				return e.SpawnEventSourced(context.Background(), &domainOnlyEventSourced{id: id})
			}},
			{"durable state", fmt.Sprintf("%T", &domainOnlyDurableState{}), func(e *Engine, id string) error {
				return e.SpawnDurableState(context.Background(), &domainOnlyDurableState{id: id})
			}},
			{"saga", fmt.Sprintf("%T", &domainOnlySaga{}), func(e *Engine, id string) error {
				return e.SpawnSaga(context.Background(), &domainOnlySaga{id: id}, time.Minute)
			}},
		}, func(c domainOnlySpawnCaseG4) string {
			return "domain-only behavior is rejected in cluster mode: " + c.family
		}, func(ctx *specs.Context, c domainOnlySpawnCaseG4) {
			cluster := newCluster(ctx)
			id := uuid.NewString()

			err := c.spawn(cluster.engines[0], id)

			ctx.Expect(err).To(specs.MatchError(ErrBehaviorNotSerializable))
			var placement *BehaviorPlacementError
			ctx.Expect(err).To(specs.MatchErrorAs(&placement))
			ctx.Expect(placement.Kind).To(specs.Equal(c.kind))
			ctx.Expect(placement.EntityID).To(specs.Equal(id))
			expectNotSpawnedG4(ctx, cluster, id)
		})

		s.It("value-type behavior in cluster mode", func(ctx *specs.Context) {
			cluster := newCluster(ctx)

			// The old contract with value receivers, spawned through the old API.
			// Before the spawn-site bridge (#123, S3-2) Entity handed the value
			// to GoAkt's Inject, which panicked in its type registry.
			id := uuid.NewString()
			err := cluster.engines[0].Entity(context.Background(), valueTypeEventSourcedBehavior{id: id})

			ctx.Expect(err).To(specs.MatchError(ErrBehaviorNotPointer))
			var placement *BehaviorPlacementError
			ctx.Expect(err).To(specs.MatchErrorAs(&placement))
			ctx.Expect(placement.Kind).To(specs.Equal(fmt.Sprintf("%T", valueTypeEventSourcedBehavior{})))
			ctx.Expect(placement.EntityID).To(specs.Equal(id))
			expectNotSpawnedG4(ctx, cluster, id)
		})

		// The nodes register their kinds differently: node 1 with the old
		// WithEntityKinds, node 2 with the new WithBehaviorKinds (#123, S3-4).
		// Each direction spawns a different behavior type, so the receiving
		// node can only decode it with the kind its own option registered,
		// never with the lazy Inject the calling node does at spawn time. A
		// spawn that lands on the peer proves the registry key and bytes are
		// the same for both options.
		newMixedCluster := func(ctx *specs.Context) *testCluster {
			return newTestCluster(ctx.T,
				clusterNodeOptsG4(ctx, WithEntityKinds(new(AccountEventSourcedBehavior), new(AccountDurableStateBehavior))),
				clusterNodeOptsG4(ctx, WithBehaviorKinds(new(AccountEventSourcedBehavior), new(AccountDurableStateBehavior))),
			)
		}

		s.It("old and new registration interoperate: WithEntityKinds node spawns onto WithBehaviorKinds node", func(ctx *specs.Context) {
			mixed := newMixedCluster(ctx)
			engine := mixed.engines[0]

			hosted := hostedByPeerG4(ctx, engine, mixed.systems[1], func(id string) error {
				return engine.SpawnEventSourced(context.Background(), NewAccountEventSourcedBehavior(id))
			})

			// at least one entity must be hosted by the WithBehaviorKinds node
			ctx.Expect(hosted).To(specs.BeGreaterThan(0))
		})

		s.It("old and new registration interoperate: WithBehaviorKinds node spawns onto WithEntityKinds node", func(ctx *specs.Context) {
			mixed := newMixedCluster(ctx)
			engine := mixed.engines[1]

			hosted := hostedByPeerG4(ctx, engine, mixed.systems[0], func(id string) error {
				return engine.SpawnDurableState(context.Background(), NewAccountDurableStateBehavior(id))
			})

			// at least one entity must be hosted by the WithEntityKinds node
			ctx.Expect(hosted).To(specs.BeGreaterThan(0))
		})
	})
}
