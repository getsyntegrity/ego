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
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mockpersistence "github.com/pablogore/ego/v4/mocks/persistence"
	"github.com/pablogore/ego/v4/persistence"
	"github.com/pablogore/ego/v4/tenancy"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
	"github.com/pablogore/ego/v4/testkit"
)

// This file covers TENANT-003 T4's corrected spawn-time tenant design: CI
// caught the earlier design's Resolve-Once, Propagate-After violation
// (TestSendCommandResolverSwapIdenticalSequence, engine_test.go, observed a
// resolver invoked twice for one spawn-plus-command sequence). The engine
// now never calls tenancy.TenantResolver.Resolve at spawn; instead the
// application declares an entity/durable-state entity/saga's tenant via
// ego.WithTenant, and the built-in tenancy.WithSingleTenant resolver
// exposes its fixed tenant via tenancy.FixedTenantResolver so single-tenant
// mode needs no such declaration (acceptance criterion 6). See
// engine.go's spawnTenantScope and spawn_config.go's WithTenant.

// TestEngineEntitySpawnRequiresExplicitTenantWhenResolverHasNoFixedTenant
// covers the fail-closed half of the corrected design: tenancy is active (a
// resolver is registered), that resolver exposes no fixed tenant (it is not
// a tenancy.FixedTenantResolver, or reports it has none), and the caller
// did not pass ego.WithTenant. The spawn must be refused outright with the
// typed ErrSpawnTenantUndetermined, and — because the engine never calls
// Resolve to find this out — no store method may be invoked at all.
func TestEngineEntitySpawnRequiresExplicitTenantWhenResolverHasNoFixedTenant(t *testing.T) {
	ctx := context.Background()

	t.Run("Entity refuses to spawn and touches no store", func(t *testing.T) {
		store := mockpersistence.NewEventsStore(t)

		engine := newTestEngine(t, "Sample", store, WithTenantResolver(&stubTenantResolver{id: "acme"}))
		require.NoError(t, engine.Start(ctx))

		entityID := uuid.NewString()
		probe := newTenancyProbeEventSourcedBehavior(entityID)

		err := engine.Entity(ctx, probe)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrSpawnTenantUndetermined),
			"the rejection must be the typed ErrSpawnTenantUndetermined, not an invented error")

		exists, existsErr := engine.EntityExists(ctx, entityID)
		require.NoError(t, existsErr)
		require.False(t, exists, "no actor may be spawned when the tenant cannot be determined")
		require.Zero(t, probe.invocationCount(), "HandleCommand must never run: the entity was never spawned")

		// No expectation was registered on this mock at all, so ANY call to
		// it (Ping, GetLatestEvent, WriteEvents, ...) would already fail the
		// test via testify's unexpected-call panic; these are an explicit,
		// named assertion of that guarantee rather than an accident of test
		// ordering. spawnTenantScope must reject before the actor is ever
		// created, let alone reaches a store.
		store.AssertExpectations(t)

		require.NoError(t, engine.Stop(ctx))
	})

	t.Run("DurableStateEntity refuses to spawn and touches no store", func(t *testing.T) {
		store := testkit.NewEventsStore()
		require.NoError(t, store.Connect(ctx))
		t.Cleanup(func() { _ = store.Disconnect(ctx) })

		durableStore := mockpersistence.NewStateStore(t)

		engine := newTestEngine(t, "Sample", store,
			WithTenantResolver(&stubTenantResolver{id: "acme"}),
			WithStateStore(durableStore))
		require.NoError(t, engine.Start(ctx))

		entityID := uuid.NewString()
		behavior := NewAccountDurableStateBehavior(entityID)

		err := engine.DurableStateEntity(ctx, behavior)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrSpawnTenantUndetermined))

		exists, existsErr := engine.EntityExists(ctx, entityID)
		require.NoError(t, existsErr)
		require.False(t, exists)

		durableStore.AssertExpectations(t)

		require.NoError(t, engine.Stop(ctx))
	})
}

// TestEngineEntitySpawnWithExplicitTenantResolvesOnce is the dedicated
// regression guard for the defect CI caught: for one spawn-plus-command
// sequence against an ordinary multi-tenant resolver, Resolve must be
// invoked exactly once — by SendCommand, at the command trust boundary —
// never a second time at spawn. See also
// TestSendCommandResolverSwapIdenticalSequence's "multi-tenant resolver"
// subtest (engine_test.go), which proves the same invariant end to end
// against SendCommand's observed TenantContext.
func TestEngineEntitySpawnWithExplicitTenantResolvesOnce(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	resolver := &countingTenantResolver{id: "acme"}
	engine := newTestEngine(t, "Sample", store, WithTenantResolver(resolver))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	probe := newTenancyProbeEventSourcedBehavior(entityID)
	require.NoError(t, engine.Entity(ctx, probe, WithTenant(tenancy.TenantID("acme"))))

	assert.Zero(t, resolver.callCount(), "spawn must never call Resolve when the tenant was declared via WithTenant")

	_, _, err := engine.SendCommand(ctx, entityID, &testpb.CreateAccount{AccountBalance: 500}, time.Minute)
	require.NoError(t, err)

	assert.EqualValues(t, 1, resolver.callCount(), "Resolve must be invoked exactly once, by SendCommand, never at spawn")

	require.NoError(t, engine.Stop(ctx))
}

// TestEngineWithSingleTenantSpawnNeedsNoWithTenant covers acceptance
// criterion 6 directly at the spawn boundary: a tenancy.WithSingleTenant
// resolver lets Entity spawn with zero application-invented tenant
// plumbing — no ego.WithTenant — because it exposes its fixed tenant via
// tenancy.FixedTenantResolver, and the spawned entity is still correctly
// bound to that tenant's scope (proven by a subsequent command observing
// it).
func TestEngineWithSingleTenantSpawnNeedsNoWithTenant(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	resolver, err := tenancy.WithSingleTenant(tenancy.TenantID("acme"))
	require.NoError(t, err)

	engine := newTestEngine(t, "Sample", store, WithTenantResolver(resolver))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	probe := newTenancyProbeEventSourcedBehavior(entityID)

	// No ego.WithTenant option at all.
	require.NoError(t, engine.Entity(ctx, probe))

	exists, err := engine.EntityExists(ctx, entityID)
	require.NoError(t, err)
	require.True(t, exists, "the entity must spawn using the resolver's fixed tenant, with no WithTenant declaration")

	_, _, err = engine.SendCommand(ctx, entityID, &testpb.CreateAccount{AccountBalance: 500}, time.Minute)
	require.NoError(t, err)

	scope, err := persistence.NewTenantScope(tenancy.TenantID("acme"))
	require.NoError(t, err)
	latest, err := store.GetLatestEvent(ctx, scope, entityID)
	require.NoError(t, err)
	require.NotNil(t, latest, "the entity must have been bound to and written under the resolver's fixed tenant scope")

	require.NoError(t, engine.Stop(ctx))
}

// TestEngineEntitySpawnWithoutResolverStaysUnscoped covers the legacy-mode
// invariant that must survive this correction unchanged: with no
// tenancy.TenantResolver registered at all, Entity spawn requires no
// ego.WithTenant, injects no tenant scope, and every store call still
// carries persistence.Unscoped(), exactly as before TENANT-003.
func TestEngineEntitySpawnWithoutResolverStaysUnscoped(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	engine := newTestEngine(t, "Sample", store, WithLogger(DiscardLogger))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	probe := newTenancyProbeEventSourcedBehavior(entityID)
	require.NoError(t, engine.Entity(ctx, probe))

	_, _, err := engine.SendCommand(ctx, entityID, &testpb.CreateAccount{AccountBalance: 500}, time.Minute)
	require.NoError(t, err)

	latest, err := store.GetLatestEvent(ctx, persistence.Unscoped(), entityID)
	require.NoError(t, err)
	require.NotNil(t, latest, "legacy mode must still write under persistence.Unscoped()")

	require.NoError(t, engine.Stop(ctx))
}

// TestEngineCommandRejectsTenantMismatchWithSpawnDeclaredTenant proves the
// spawn-declared tenant (ego.WithTenant) is not merely advisory: a command
// whose resolver-attached TenantContext disagrees with the tenant the actor
// was actually spawned under must be rejected by the actor's existing
// cross-check (tenancy.VerifyUnchanged), before HandleCommand ever runs.
func TestEngineCommandRejectsTenantMismatchWithSpawnDeclaredTenant(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	// perCallerTenantResolver (option_test.go) resolves whichever tenant id
	// the caller placed on ctx, simulating a resolver that derives identity
	// from request-scoped data rather than a fixed value.
	engine := newTestEngine(t, "Sample", store, WithTenantResolver(perCallerTenantResolver{}))
	require.NoError(t, engine.Start(ctx))

	entityID := uuid.NewString()
	probe := newTenancyProbeEventSourcedBehavior(entityID)
	require.NoError(t, engine.Entity(ctx, probe, WithTenant(tenancy.TenantID("acme"))))

	// SendCommand's ctx resolves to "globex", a different tenant than the
	// one this entity was spawned under.
	mismatchedCtx := context.WithValue(ctx, perCallerTenantKey{}, "globex")
	_, _, err := engine.SendCommand(mismatchedCtx, entityID, &testpb.CreateAccount{AccountBalance: 500}, time.Minute)
	require.Error(t, err, "a command resolved to a different tenant than the entity was spawned under must be rejected")
	assert.Zero(t, probe.invocationCount(), "HandleCommand must never run for a mismatched tenant")

	require.NoError(t, engine.Stop(ctx))
}
