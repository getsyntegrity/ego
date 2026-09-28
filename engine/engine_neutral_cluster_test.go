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

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"

	testpb "github.com/getsyntegrity/ego/v4/test/data/testpb"
	"github.com/getsyntegrity/ego/v4/testkit"
)

// TestEngineMultiNodeNeutralBehaviors spawns behaviors through the public
// Spawn* methods on a two-node cluster, where GoAkt serializes every spawn's
// dependencies and may place the actor on the peer (ego-arch-002-s3 design,
// §8). One cluster is shared by all subtests, and only node 1 spawns.
//
// No subtest recovers a panic: GoAkt's Inject panics on a non-pointer type
// while it holds the actor-system lock, and a recovered panic would leave the
// cluster cleanup blocked on that lock. Run the test with a short timeout
// (-timeout 90s) so either the panic or the timeout ends the binary.
func TestEngineMultiNodeNeutralBehaviors(t *testing.T) {
	ctx := context.Background()

	nodeOpts := func() []Option {
		stateStore := testkit.NewDurableStore()
		require.NoError(t, stateStore.Connect(ctx))
		t.Cleanup(func() { _ = stateStore.Disconnect(ctx) })
		return []Option{
			WithEntityKinds(new(AccountEventSourcedBehavior)),
			WithStateStore(stateStore),
		}
	}
	cluster := newTestCluster(t, nodeOpts(), nodeOpts())
	engine1 := cluster.engines[0]
	node2 := cluster.systems[1]

	// requireNotSpawned asserts that no node hosts an actor named id.
	requireNotSpawned := func(t *testing.T, id string) {
		t.Helper()
		for i, sys := range cluster.systems {
			exists, err := sys.ActorExists(ctx, id)
			require.NoError(t, err)
			assert.False(t, exists, "node %d must not host an actor for a rejected behavior", i+1)
		}
	}

	t.Run("serializable behavior placed remotely", func(t *testing.T) {
		// With RoundRobin placement over two members, eight spawns from node
		// 1 place some entities on node 2, which decodes the behavior with the
		// kind it registered through WithEntityKinds.
		placedOnNode2 := 0
		for range 8 {
			id := uuid.NewString()
			require.NoError(t, engine1.SpawnEventSourced(ctx, NewAccountEventSourcedBehavior(id)))

			state, _, err := engine1.SendCommand(ctx, id, &testpb.CreateAccount{AccountBalance: 100}, time.Minute)
			require.NoError(t, err)
			account, ok := state.(*testpb.Account)
			require.True(t, ok)
			assert.EqualValues(t, 100, account.GetAccountBalance())

			pid, err := node2.ActorOf(ctx, id)
			require.NoError(t, err)
			if pid.IsLocal() {
				placedOnNode2++
			}
		}
		assert.Positive(t, placedOnNode2, "at least one entity must be hosted by node 2")
	})

	t.Run("domain-only behavior is rejected in cluster mode", func(t *testing.T) {
		cases := []struct {
			family string
			kind   string
			spawn  func(id string) error
		}{
			{"event-sourced", fmt.Sprintf("%T", &domainOnlyEventSourced{}), func(id string) error {
				return engine1.SpawnEventSourced(ctx, &domainOnlyEventSourced{id: id})
			}},
			{"durable state", fmt.Sprintf("%T", &domainOnlyDurableState{}), func(id string) error {
				return engine1.SpawnDurableState(ctx, &domainOnlyDurableState{id: id})
			}},
			{"saga", fmt.Sprintf("%T", &domainOnlySaga{}), func(id string) error {
				return engine1.SpawnSaga(ctx, &domainOnlySaga{id: id}, time.Minute)
			}},
		}
		for _, tc := range cases {
			t.Run(tc.family, func(t *testing.T) {
				id := uuid.NewString()
				err := tc.spawn(id)
				require.ErrorIs(t, err, ErrBehaviorNotSerializable)
				var placement *BehaviorPlacementError
				require.ErrorAs(t, err, &placement)
				assert.Equal(t, tc.kind, placement.Kind)
				assert.Equal(t, id, placement.EntityID)
				requireNotSpawned(t, id)
			})
		}
	})

	t.Run("value-type behavior in cluster mode", func(t *testing.T) {
		// The old contract with value receivers, spawned through the old API.
		// Before the spawn-site bridge (#123, S3-2) Entity handed the value
		// to GoAkt's Inject, which panicked in its type registry.
		id := uuid.NewString()
		err := engine1.Entity(ctx, valueTypeEventSourcedBehavior{id: id})
		require.ErrorIs(t, err, ErrBehaviorNotPointer)
		var placement *BehaviorPlacementError
		require.ErrorAs(t, err, &placement)
		assert.Equal(t, fmt.Sprintf("%T", valueTypeEventSourcedBehavior{}), placement.Kind)
		assert.Equal(t, id, placement.EntityID)
		requireNotSpawned(t, id)
	})

	t.Run("old and new registration interoperate", func(t *testing.T) {
		// A second cluster, because the nodes register their kinds
		// differently: node 1 with the old WithEntityKinds, node 2 with the
		// new WithBehaviorKinds (#123, S3-4). Each direction spawns a
		// different behavior type, so the receiving node can only decode it
		// with the kind its own option registered, never with the lazy
		// Inject the calling node does at spawn time. A spawn that lands on
		// the peer proves the registry key and bytes are the same for both
		// options.
		mixedOpts := func(kinds Option) []Option {
			stateStore := testkit.NewDurableStore()
			require.NoError(t, stateStore.Connect(ctx))
			t.Cleanup(func() { _ = stateStore.Disconnect(ctx) })
			return []Option{kinds, WithStateStore(stateStore)}
		}
		mixed := newTestCluster(t,
			mixedOpts(WithEntityKinds(new(AccountEventSourcedBehavior), new(AccountDurableStateBehavior))),
			mixedOpts(WithBehaviorKinds(new(AccountEventSourcedBehavior), new(AccountDurableStateBehavior))),
		)

		// hostedByPeer spawns eight entities from one node and counts those
		// the peer hosts; every spawn must answer a command.
		hostedByPeer := func(t *testing.T, from *Engine, peer goakt.ActorSystem, spawn func(id string) error) int {
			t.Helper()
			hosted := 0
			for range 8 {
				id := uuid.NewString()
				require.NoError(t, spawn(id))

				state, _, err := from.SendCommand(ctx, id, &testpb.CreateAccount{AccountBalance: 100}, time.Minute)
				require.NoError(t, err)
				account, ok := state.(*testpb.Account)
				require.True(t, ok)
				assert.EqualValues(t, 100, account.GetAccountBalance())

				pid, err := peer.ActorOf(ctx, id)
				require.NoError(t, err)
				if pid.IsLocal() {
					hosted++
				}
			}
			return hosted
		}

		t.Run("WithEntityKinds node spawns onto WithBehaviorKinds node", func(t *testing.T) {
			engine := mixed.engines[0]
			hosted := hostedByPeer(t, engine, mixed.systems[1], func(id string) error {
				return engine.SpawnEventSourced(ctx, NewAccountEventSourcedBehavior(id))
			})
			assert.Positive(t, hosted, "at least one entity must be hosted by the WithBehaviorKinds node")
		})

		t.Run("WithBehaviorKinds node spawns onto WithEntityKinds node", func(t *testing.T) {
			engine := mixed.engines[1]
			hosted := hostedByPeer(t, engine, mixed.systems[0], func(id string) error {
				return engine.SpawnDurableState(ctx, NewAccountDurableStateBehavior(id))
			})
			assert.Positive(t, hosted, "at least one entity must be hosted by the WithEntityKinds node")
		})
	})
}
