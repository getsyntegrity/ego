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

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/pablogore/ego/v4/tenancy"
	"github.com/pablogore/ego/v4/testkit"
)

// administrativeTenantResolver is a tenancy.TenantResolver that always
// resolves to an administrative (non-tenant) tenancy.TenantContext. Used to
// prove Engine.Entity's spawn-time tenant resolution (resolveSpawnTenantScope,
// TENANT-003 T4) refuses to spawn a tenant-bound actor under administrative
// scope, rather than silently falling back to persistence.Unscoped() — which
// would defeat the isolation TENANT-003 exists to enforce. Administrative
// access to entity actors is explicitly out of scope here (see TENANT-008).
type administrativeTenantResolver struct{}

var _ tenancy.TenantResolver = administrativeTenantResolver{}

func (administrativeTenantResolver) Resolve(context.Context) (tenancy.TenantContext, error) {
	admin, err := tenancy.NewAdministrative("ops-team", "bypass-attempt")
	if err != nil {
		return tenancy.TenantContext{}, err
	}
	return tenancy.NewAdministrativeContext(admin)
}

// TestEngineEntitySpawnRejectsAdministrativeScope covers TENANT-003 T4's
// engine.go:resolveSpawnTenantScope decision: a resolved administrative
// tenancy.TenantContext at spawn time must block Entity/DurableStateEntity/
// Saga outright with the typed ErrAdministrativeScopeEntitySpawn, and no
// actor may be created.
func TestEngineEntitySpawnRejectsAdministrativeScope(t *testing.T) {
	ctx := context.Background()

	t.Run("Entity refuses to spawn and creates no actor", func(t *testing.T) {
		store := testkit.NewEventsStore()
		require.NoError(t, store.Connect(ctx))
		t.Cleanup(func() { _ = store.Disconnect(ctx) })

		engine := newTestEngine(t, "Sample", store, WithTenantResolver(administrativeTenantResolver{}))
		require.NoError(t, engine.Start(ctx))

		entityID := uuid.NewString()
		probe := newTenancyProbeEventSourcedBehavior(entityID)

		err := engine.Entity(ctx, probe)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrAdministrativeScopeEntitySpawn),
			"the rejection must be the typed ErrAdministrativeScopeEntitySpawn, not an invented error")

		exists, err := engine.EntityExists(ctx, entityID)
		require.NoError(t, err)
		require.False(t, exists, "no actor may be spawned when the resolved scope is administrative")
		require.Zero(t, probe.invocationCount(), "HandleCommand must never run: the entity was never spawned")

		require.NoError(t, engine.Stop(ctx))
	})

	t.Run("DurableStateEntity refuses to spawn and creates no actor", func(t *testing.T) {
		store := testkit.NewEventsStore()
		require.NoError(t, store.Connect(ctx))
		t.Cleanup(func() { _ = store.Disconnect(ctx) })

		durableStore := testkit.NewDurableStore()
		require.NoError(t, durableStore.Connect(ctx))
		t.Cleanup(func() { _ = durableStore.Disconnect(ctx) })

		engine := newTestEngine(t, "Sample", store,
			WithTenantResolver(administrativeTenantResolver{}),
			WithStateStore(durableStore))
		require.NoError(t, engine.Start(ctx))

		entityID := uuid.NewString()
		behavior := NewAccountDurableStateBehavior(entityID)

		err := engine.DurableStateEntity(ctx, behavior)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrAdministrativeScopeEntitySpawn))

		exists, err := engine.EntityExists(ctx, entityID)
		require.NoError(t, err)
		require.False(t, exists)

		require.NoError(t, engine.Stop(ctx))
	})
}
