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
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/internal/extensions"
	"github.com/pablogore/ego/v4/persistence"
	"github.com/pablogore/ego/v4/tenancy"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
	"github.com/pablogore/ego/v4/testkit"
)

// This file pins the spawn half of the "shared entity id across tenants"
// contract (openspec/changes/ego-tenant-003/specs/persistence-tenant-
// isolation/spec.md): re-spawning a live id under the SAME declared tenant
// is idempotent, while re-spawning it under a DIFFERENT tenant is rejected
// with ErrSpawnTenantMismatch instead of silently returning the other
// tenant's actor. GoAkt's local Spawn returns an already-running actor's PID
// with a nil error, so the engine checks the returned actor's own spawn
// binding (its EntityTenantScope dependency) after Spawn returns.

func newRespawnTestEngine(t *testing.T) *Engine {
	t.Helper()
	ctx := context.Background()
	eventsStore := testkit.NewEventsStore()
	require.NoError(t, eventsStore.Connect(ctx))
	stateStore := testkit.NewDurableStore()
	require.NoError(t, stateStore.Connect(ctx))
	t.Cleanup(func() {
		_ = eventsStore.Disconnect(ctx)
		_ = stateStore.Disconnect(ctx)
	})

	engine := newTestEngine(t, "Respawn", eventsStore,
		WithTenantResolver(perCallerTenantResolver{}),
		WithStateStore(stateStore))
	require.NoError(t, engine.Start(ctx))
	return engine
}

func requireSpawnTenantMismatch(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSpawnTenantMismatch)
	assert.ErrorIs(t, err, tenancy.ErrDenied)
	var tenancyErr *tenancy.Error
	assert.True(t, errors.As(err, &tenancyErr), "the tenancy mismatch must be recoverable via errors.As")
}

func TestEngineRespawnUnderAnotherTenantIsRejected(t *testing.T) {
	ctx := context.Background()
	acme := WithTenant(tenancy.TenantID("acme"))
	globex := WithTenant(tenancy.TenantID("globex"))
	globexCtx := context.WithValue(ctx, perCallerTenantKey{}, "globex")

	t.Run("EventSourced entity", func(t *testing.T) {
		engine := newRespawnTestEngine(t)
		id := uuid.NewString()
		owner := newTenancyProbeEventSourcedBehavior(id)
		require.NoError(t, engine.Entity(ctx, owner, acme), "a new actor with a valid tenant spawns")
		require.NoError(t, engine.Entity(ctx, newTenancyProbeEventSourcedBehavior(id), acme), "a same-tenant respawn is idempotent")

		requireSpawnTenantMismatch(t, engine.Entity(ctx, newTenancyProbeEventSourcedBehavior(id), globex))

		_, _, err := engine.SendCommand(globexCtx, id, &testpb.CreateAccount{AccountBalance: 1}, time.Minute)
		require.Error(t, err)
		assert.Zero(t, owner.invocationCount(), "a foreign command must never reach HandleCommand")
	})

	t.Run("DurableStateEntity", func(t *testing.T) {
		engine := newRespawnTestEngine(t)
		id := uuid.NewString()
		owner := newTenancyProbeDurableStateBehavior(id)
		require.NoError(t, engine.DurableStateEntity(ctx, owner, acme))
		require.NoError(t, engine.DurableStateEntity(ctx, newTenancyProbeDurableStateBehavior(id), acme))

		requireSpawnTenantMismatch(t, engine.DurableStateEntity(ctx, newTenancyProbeDurableStateBehavior(id), globex))

		_, _, err := engine.SendCommand(globexCtx, id, &testpb.CreateAccount{AccountBalance: 1}, time.Minute)
		require.Error(t, err)
		assert.Zero(t, owner.invocationCount(), "a foreign command must never reach HandleCommand")
	})

	t.Run("Saga", func(t *testing.T) {
		engine := newRespawnTestEngine(t)
		id := "saga-" + uuid.NewString()
		saga := func() *callbackSagaBehavior {
			return &callbackSagaBehavior{id: id, handleEvent: func(context.Context, Event, State) (*SagaAction, error) {
				return &SagaAction{}, nil
			}}
		}
		require.NoError(t, engine.Saga(ctx, saga(), 0, acme))
		require.NoError(t, engine.Saga(ctx, saga(), 0, acme))

		requireSpawnTenantMismatch(t, engine.Saga(ctx, saga(), 0, globex))
	})
}

// TestEngineConcurrentCrossTenantSpawnHasExactlyOneWinner races two spawns
// of the same entity id under different tenants. Exactly one must succeed,
// the other must be rejected, and no command from the losing tenant may
// reach HandleCommand.
func TestEngineConcurrentCrossTenantSpawnHasExactlyOneWinner(t *testing.T) {
	ctx := context.Background()
	engine := newRespawnTestEngine(t)
	tenants := []string{"acme", "globex"}

	for range 20 {
		id := uuid.NewString()
		probes := []*tenancyProbeEventSourcedBehavior{newTenancyProbeEventSourcedBehavior(id), newTenancyProbeEventSourcedBehavior(id)}
		errs := make([]error, len(tenants))

		var ready, done sync.WaitGroup
		start := make(chan struct{})
		for i, tenant := range tenants {
			ready.Add(1)
			done.Add(1)
			go func() {
				defer done.Done()
				ready.Done()
				<-start
				errs[i] = engine.Entity(ctx, probes[i], WithTenant(tenancy.TenantID(tenant)))
			}()
		}
		ready.Wait()
		close(start)
		done.Wait()

		winners := 0
		loser := -1
		for i, err := range errs {
			if err == nil {
				winners++
				continue
			}
			requireSpawnTenantMismatch(t, err)
			loser = i
		}
		require.Equal(t, 1, winners, "exactly one tenant must win the spawn race: %v", errs)

		loserCtx := context.WithValue(ctx, perCallerTenantKey{}, tenants[loser])
		_, _, err := engine.SendCommand(loserCtx, id, &testpb.CreateAccount{AccountBalance: 1}, time.Minute)
		require.Error(t, err, "the losing tenant's command must be rejected")
		assert.Zero(t, probes[0].invocationCount()+probes[1].invocationCount(), "a foreign command must never reach HandleCommand")
	}
}

func TestEngineRespawnInLegacyModeIsUnchanged(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "RespawnLegacy", store)
	require.NoError(t, engine.Start(ctx))

	id := uuid.NewString()
	require.NoError(t, engine.Entity(ctx, NewAccountEventSourcedBehavior(id)))
	require.NoError(t, engine.Entity(ctx, NewAccountEventSourcedBehavior(id)), "legacy respawn of a live id stays a no-op success")
	// WithTenant is ignored in legacy mode, exactly as before.
	require.NoError(t, engine.Entity(ctx, NewAccountEventSourcedBehavior(id), WithTenant(tenancy.TenantID("acme"))))
}

// TestClassifyTenantBinding pins how the engine maps the owning actor's
// TenantBindingReply to the spawn outcome: a match is success, a different
// binding is ErrSpawnTenantMismatch, and an actor holding no binding is
// ErrSpawnTenantUnverified, which asserts no conflict.
func TestClassifyTenantBinding(t *testing.T) {
	requested := extensions.NewEntityTenantScope("acme")

	require.NoError(t, classifyTenantBinding("order-1", requested, &egopb.TenantBindingReply{TenantAware: true, Matches: true}))
	requireSpawnTenantMismatch(t, classifyTenantBinding("order-1", requested, &egopb.TenantBindingReply{TenantAware: true}))

	err := classifyTenantBinding("order-1", requested, &egopb.TenantBindingReply{})
	require.ErrorIs(t, err, ErrSpawnTenantUnverified)
	assert.NotErrorIs(t, err, ErrSpawnTenantMismatch)
}

// TestAnswerTenantBinding pins the actors' shared query handler: it answers
// from the bound scope only, never discloses the bound tenant, and reports
// no binding in legacy mode or for an administrative-looking query.
func TestAnswerTenantBinding(t *testing.T) {
	acme, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)

	match := answerTenantBinding(true, acme, &egopb.TenantBindingQuery{TenantId: "acme"})
	assert.True(t, match.GetTenantAware())
	assert.True(t, match.GetMatches())

	other := answerTenantBinding(true, acme, &egopb.TenantBindingQuery{TenantId: "globex"})
	assert.True(t, other.GetTenantAware())
	assert.False(t, other.GetMatches())

	invalid := answerTenantBinding(true, acme, &egopb.TenantBindingQuery{TenantId: ""})
	assert.False(t, invalid.GetMatches(), "an invalid queried tenant never matches")

	legacy := answerTenantBinding(false, persistence.Unscoped(), &egopb.TenantBindingQuery{TenantId: "acme"})
	assert.False(t, legacy.GetTenantAware())
	assert.False(t, legacy.GetMatches())
}

// TestDispatchRejectsTenantBindingQuery pins that the control message can
// never be sent as a command, so a caller cannot probe which tenant owns an
// entity id.
func TestDispatchRejectsTenantBindingQuery(t *testing.T) {
	ctx := context.Background()
	engine := newRespawnTestEngine(t)
	id := uuid.NewString()
	require.NoError(t, engine.Entity(ctx, newTenancyProbeEventSourcedBehavior(id), WithTenant(tenancy.TenantID("acme"))))

	globexCtx := context.WithValue(ctx, perCallerTenantKey{}, "globex")
	_, _, err := engine.SendCommand(globexCtx, id, &egopb.TenantBindingQuery{TenantId: "acme"}, time.Minute)
	require.ErrorIs(t, err, ErrNotACommand)
}
