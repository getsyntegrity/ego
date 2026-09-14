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
