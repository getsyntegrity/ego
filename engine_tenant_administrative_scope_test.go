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
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/persistence"
	"github.com/pablogore/ego/v4/tenancy"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
	"github.com/pablogore/ego/v4/testkit"
)

// administrativeScopeKey marks a ctx that administrativeScopeResolver
// resolves to an administrative (non-tenant) tenancy.TenantContext.
type administrativeScopeKey struct{}

// administrativeScopeResolver resolves an administrative TenantContext when
// ctx carries administrativeScopeKey and otherwise behaves like
// perCallerTenantResolver. It exposes no fixed tenant.
type administrativeScopeResolver struct{}

var _ tenancy.TenantResolver = administrativeScopeResolver{}

func (administrativeScopeResolver) Resolve(ctx context.Context) (tenancy.TenantContext, error) {
	if ctx.Value(administrativeScopeKey{}) != nil {
		admin, err := tenancy.NewAdministrative("ops-team", "audit")
		if err != nil {
			return tenancy.TenantContext{}, err
		}
		return tenancy.NewAdministrativeContext(admin)
	}
	return perCallerTenantResolver{}.Resolve(ctx)
}

// TestAdministrativeScopeIsNeverAnAggregateTenantScope pins that an
// administrative TenantContext can never stand in for an aggregate's tenant
// scope (administrative bypass is TENANT-008's scope, not TENANT-003's): it
// cannot declare a spawn, cannot command a tenant-bound entity, and cannot
// scope an erasure.
func TestAdministrativeScopeIsNeverAnAggregateTenantScope(t *testing.T) {
	ctx := context.Background()
	adminCtx := context.WithValue(ctx, administrativeScopeKey{}, true)

	t.Run("an administrative-only resolver cannot bind a spawn", func(t *testing.T) {
		store := testkit.NewEventsStore()
		require.NoError(t, store.Connect(ctx))
		t.Cleanup(func() { _ = store.Disconnect(ctx) })

		engine := newTestEngine(t, "Sample", store, WithTenantResolver(administrativeScopeResolver{}))
		require.NoError(t, engine.Start(ctx))
		t.Cleanup(func() { _ = engine.Stop(ctx) })

		err := engine.Entity(adminCtx, newTenancyProbeEventSourcedBehavior(uuid.NewString()))
		require.ErrorIs(t, err, ErrSpawnTenantUndetermined)
	})

	t.Run("an administrative command is rejected by a tenant-bound entity", func(t *testing.T) {
		store := testkit.NewEventsStore()
		require.NoError(t, store.Connect(ctx))
		t.Cleanup(func() { _ = store.Disconnect(ctx) })

		engine := newTestEngine(t, "Sample", store, WithTenantResolver(administrativeScopeResolver{}))
		require.NoError(t, engine.Start(ctx))
		t.Cleanup(func() { _ = engine.Stop(ctx) })

		entityID := uuid.NewString()
		probe := newTenancyProbeEventSourcedBehavior(entityID)
		require.NoError(t, engine.Entity(ctx, probe, WithTenant(tenancy.TenantID("acme"))))

		_, _, err := engine.SendCommand(adminCtx, entityID, &testpb.CreateAccount{AccountBalance: 500}, time.Minute)
		require.Error(t, err)
		assert.Zero(t, probe.invocationCount(), "HandleCommand must never run under an administrative scope")

		acme, err := persistence.NewTenantScope("acme")
		require.NoError(t, err)
		latest, err := store.GetLatestEvent(ctx, acme, entityID)
		require.NoError(t, err)
		assert.Nil(t, latest, "nothing may be persisted under the entity's tenant")
	})

	t.Run("an administrative erasure is denied and erases nothing", func(t *testing.T) {
		store := testkit.NewEventsStore()
		require.NoError(t, store.Connect(ctx))
		t.Cleanup(func() { _ = store.Disconnect(ctx) })

		persistenceID := uuid.NewString()
		eventAny, err := anypb.New(&testpb.AccountCreated{AccountId: persistenceID, AccountBalance: 100})
		require.NoError(t, err)
		acme, err := persistence.NewTenantScope("acme")
		require.NoError(t, err)
		for _, scope := range []persistence.Scope{persistence.Unscoped(), acme} {
			require.NoError(t, store.WriteEvents(ctx, scope, []*egopb.Event{{
				PersistenceId:  persistenceID,
				SequenceNumber: 1,
				Event:          eventAny,
				Timestamp:      time.Now().UnixNano(),
			}}, persistence.Unconditional()))
		}

		engine := newTestEngine(t, "Sample", store, WithTenantResolver(administrativeScopeResolver{}))
		require.NoError(t, engine.Start(ctx))
		t.Cleanup(func() { _ = engine.Stop(ctx) })

		err = engine.EraseEntity(adminCtx, persistenceID, true)
		require.ErrorIs(t, err, tenancy.ErrDenied)

		for _, scope := range []persistence.Scope{persistence.Unscoped(), acme} {
			latest, err := store.GetLatestEvent(ctx, scope, persistenceID)
			require.NoError(t, err)
			assert.NotNil(t, latest, "an administrative erasure must not touch %s", scope)
		}
	})
}
