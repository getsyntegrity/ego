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

package tenancy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablogore/ego/v4/tenancy"
)

// fixedResolver is a stand-in for a "real" multi-tenant TenantResolver: it
// resolves to whatever TenantContext it was built with. It exists only to
// prove WithSingleTenant's output is indistinguishable in kind from any
// other TenantResolver's (spec.md: "Unified Single/Multi-Tenant Resolver
// Machinery").
type fixedResolver struct {
	tc tenancy.TenantContext
}

func (r fixedResolver) Resolve(context.Context) (tenancy.TenantContext, error) {
	return r.tc, nil
}

func mustTenantID(t *testing.T, s string) tenancy.TenantID {
	t.Helper()
	id, err := tenancy.NewTenantID(s)
	require.NoError(t, err)
	return id
}

func TestWithSingleTenant_RejectsInvalidTenantID(t *testing.T) {
	_, err := tenancy.WithSingleTenant(tenancy.TenantID(""))
	require.Error(t, err)
	assert.True(t, errors.Is(err, tenancy.ErrInvalid))
}

func TestWithSingleTenant_ProducesTenantScopedContext(t *testing.T) {
	id := mustTenantID(t, "acme-corp")

	resolver, err := tenancy.WithSingleTenant(id)
	require.NoError(t, err)

	tc, err := resolver.Resolve(context.Background())
	require.NoError(t, err)

	assert.Equal(t, tenancy.ScopeTenant, tc.Scope())
	gotID, ok := tc.Tenant()
	require.True(t, ok)
	assert.Equal(t, id, gotID)
}

func TestWithSingleTenant_IgnoresIncomingContext(t *testing.T) {
	id := mustTenantID(t, "acme-corp")
	resolver, err := tenancy.WithSingleTenant(id)
	require.NoError(t, err)

	type otherKey struct{}
	ctxA := context.Background()
	ctxB := context.WithValue(context.Background(), otherKey{}, "unrelated")

	tcA, err := resolver.Resolve(ctxA)
	require.NoError(t, err)
	tcB, err := resolver.Resolve(ctxB)
	require.NoError(t, err)

	assert.Equal(t, tcA, tcB, "a single-tenant resolver must resolve the same identity regardless of the incoming context")
}

// TestWithSingleTenant_IndistinguishableFromAnyResolver is the acceptance
// scenario named in proposal.md/spec.md: a TenantContext produced by
// WithSingleTenant must have the same shape and guarantees as one produced
// by any other TenantResolver implementation — not merely "look similar",
// but compare equal and expose identical Scope()/Tenant() behavior.
func TestWithSingleTenant_IndistinguishableFromAnyResolver(t *testing.T) {
	id := mustTenantID(t, "globex-corp")

	singleTenant, err := tenancy.WithSingleTenant(id)
	require.NoError(t, err)

	wantTC, err := tenancy.NewTenantContext(id)
	require.NoError(t, err)
	custom := fixedResolver{tc: wantTC}

	var resolvers = []tenancy.TenantResolver{singleTenant, custom}

	var results []tenancy.TenantContext
	for _, r := range resolvers {
		tc, err := r.Resolve(context.Background())
		require.NoError(t, err)
		results = append(results, tc)
	}

	assert.Equal(t, results[0], results[1], "WithSingleTenant's TenantContext must be indistinguishable in kind from any other resolver's")
	assert.Equal(t, results[0].Scope(), results[1].Scope())

	gotID0, ok0 := results[0].Tenant()
	gotID1, ok1 := results[1].Tenant()
	require.True(t, ok0)
	require.True(t, ok1)
	assert.Equal(t, gotID0, gotID1)
}

// advertisingResolver implements FixedTenantResolver and reports whatever
// it was built with. With has=false it is the multi-tenant resolver the
// FixedTenantResolver contract allows: it can be asked for a fixed tenant
// but has none.
type advertisingResolver struct {
	fixedResolver
	id  tenancy.TenantID
	has bool
}

func (r advertisingResolver) FixedTenant() (tenancy.TenantID, bool) { return r.id, r.has }

var _ tenancy.FixedTenantResolver = advertisingResolver{}

// resolveCountingResolver implements FixedTenantResolver and counts Resolve
// calls, which asking for a fixed tenant must never make.
type resolveCountingResolver struct{ calls int }

func (r *resolveCountingResolver) Resolve(context.Context) (tenancy.TenantContext, error) {
	r.calls++
	return tenancy.TenantContext{}, nil
}

func (r *resolveCountingResolver) FixedTenant() (tenancy.TenantID, bool) { return "", false }

func TestCapFixedTenant_IsAnUntypedConstant(t *testing.T) {
	// Untyped: it converts to adapter.Capability without tenancy importing
	// port/adapter (ego-arch-004 design §D3). Assigning it to a plain
	// string and to a named string type both compile only if it is untyped.
	type capability string
	var asString string = tenancy.CapFixedTenant
	var asNamed capability = tenancy.CapFixedTenant
	assert.Equal(t, "tenancy.fixed-tenant", asString)
	assert.Equal(t, capability("tenancy.fixed-tenant"), asNamed)
}

func TestAsFixedTenantResolver(t *testing.T) {
	single, err := tenancy.WithSingleTenant(mustTenantID(t, "acme"))
	require.NoError(t, err)

	tests := []struct {
		name     string
		resolver tenancy.TenantResolver
		want     bool
	}{
		{"nil resolver", nil, false},
		{"plain resolver", fixedResolver{}, false},
		{"single-tenant resolver", single, true},
		{"resolver with a fixed tenant", advertisingResolver{id: "acme", has: true}, true},
		// The capability is the interface ("can be asked"), not the
		// answer: a multi-tenant resolver that implements it and reports
		// no fixed tenant still has it.
		{"multi-tenant resolver implementing the interface", advertisingResolver{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixed, ok := tenancy.AsFixedTenantResolver(tt.resolver)
			assert.Equal(t, tt.want, ok)
			if !tt.want {
				assert.Nil(t, fixed)
				return
			}
			require.NotNil(t, fixed)
			var asResolver tenancy.TenantResolver = fixed
			assert.Equal(t, tt.resolver, asResolver, "the accessor returns the resolver itself")
		})
	}
}

func TestFixedTenantOf(t *testing.T) {
	single, err := tenancy.WithSingleTenant(mustTenantID(t, "acme"))
	require.NoError(t, err)

	tests := []struct {
		name     string
		resolver tenancy.TenantResolver
		wantID   tenancy.TenantID
		wantOK   bool
	}{
		{"nil resolver", nil, "", false},
		{"plain resolver", fixedResolver{}, "", false},
		{"single-tenant resolver", single, "acme", true},
		{"resolver with a fixed tenant", advertisingResolver{id: "globex", has: true}, "globex", true},
		// ego-arch-004 spec 3 scenario "a multi-tenant resolver that
		// implements the interface": it reports no fixed tenant.
		{"multi-tenant resolver implementing the interface", advertisingResolver{}, "", false},
		// An ID reported next to false is not a fixed tenant.
		{"resolver reporting an ID with false", advertisingResolver{id: "stale", has: false}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := tenancy.FixedTenantOf(tt.resolver)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantID, id)
		})
	}
}

// Asking for a fixed tenant is not an execution-time resolution: neither
// accessor calls Resolve (TENANT-003 T4).
func TestFixedTenantAccessors_NeverResolve(t *testing.T) {
	r := &resolveCountingResolver{}
	_, _ = tenancy.FixedTenantOf(r)
	_, _ = tenancy.AsFixedTenantResolver(r)
	assert.Zero(t, r.calls)
}
