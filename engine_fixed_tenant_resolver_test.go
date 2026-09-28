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
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/pablogore/ego/v4/tenancy"
	"github.com/pablogore/ego/v4/testkit"
)

// multiTenantFixedResolver is the multi-tenant resolver the
// tenancy.FixedTenantResolver contract allows: it implements the interface
// but reports no fixed tenant. It counts FixedTenant and Resolve calls.
type multiTenantFixedResolver struct {
	stubTenantResolver
	asked    atomic.Int32
	resolved atomic.Int32
}

var _ tenancy.FixedTenantResolver = (*multiTenantFixedResolver)(nil)

func (r *multiTenantFixedResolver) Resolve(ctx context.Context) (tenancy.TenantContext, error) {
	r.resolved.Add(1)
	return r.stubTenantResolver.Resolve(ctx)
}

func (r *multiTenantFixedResolver) FixedTenant() (tenancy.TenantID, bool) {
	r.asked.Add(1)
	return "", false
}

// TestEngineSpawnWithMultiTenantFixedTenantResolverNeedsWithTenant is the
// ego-arch-004 spec 3 scenario "a multi-tenant resolver that implements the
// interface": the engine asks the resolver for its fixed tenant through
// tenancy.FixedTenantOf, gets none, and fails the spawn with
// ErrSpawnTenantUndetermined, exactly as before the call site moved behind
// the accessor. It never calls Resolve at spawn.
func TestEngineSpawnWithMultiTenantFixedTenantResolverNeedsWithTenant(t *testing.T) {
	ctx := context.Background()
	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	resolver := &multiTenantFixedResolver{stubTenantResolver: stubTenantResolver{id: "acme"}}
	engine := newTestEngine(t, "Sample", store, WithTenantResolver(resolver))
	require.NoError(t, engine.Start(ctx))
	t.Cleanup(func() { _ = engine.Stop(ctx) })

	entityID := uuid.NewString()
	err := engine.Entity(ctx, newTenancyProbeEventSourcedBehavior(entityID))
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrSpawnTenantUndetermined), "got %v, want ErrSpawnTenantUndetermined", err)

	exists, err := engine.EntityExists(ctx, entityID)
	require.NoError(t, err)
	require.False(t, exists, "no actor may be spawned when the tenant cannot be determined")
	require.Positive(t, resolver.asked.Load(), "the engine must ask the resolver for its fixed tenant")
	require.Zero(t, resolver.resolved.Load(), "the engine must never call Resolve at spawn")

	// With WithTenant the same resolver spawns: the fixed tenant is only
	// the fallback.
	require.NoError(t, engine.Entity(ctx, newTenancyProbeEventSourcedBehavior(uuid.NewString()), WithTenant("acme")))
}
