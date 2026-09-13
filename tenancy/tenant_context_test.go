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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pablogore/ego/v4/tenancy"
)

func TestNewTenantContext_RejectsEmptyTenantID(t *testing.T) {
	_, err := tenancy.NewTenantContext("")
	require.Error(t, err)
	assert.True(t, errors.Is(err, tenancy.ErrInvalid))
}

func TestNewTenantContext_RejectsZeroValueTenantID(t *testing.T) {
	var zero tenancy.TenantID
	_, err := tenancy.NewTenantContext(zero)
	require.Error(t, err)
	assert.True(t, errors.Is(err, tenancy.ErrInvalid))
}

func TestNewTenantContext_ProducesTenantScopedContext(t *testing.T) {
	id, err := tenancy.NewTenantID("acme-corp")
	require.NoError(t, err)

	tc, err := tenancy.NewTenantContext(id)
	require.NoError(t, err)

	assert.Equal(t, tenancy.ScopeTenant, tc.Scope())

	gotID, ok := tc.Tenant()
	assert.True(t, ok)
	assert.Equal(t, id, gotID)

	_, ok = tc.Administrative()
	assert.False(t, ok)
}

func TestNewTenantContext_DifferentTenantsProduceDifferentContexts(t *testing.T) {
	acme, err := tenancy.NewTenantID("acme-corp")
	require.NoError(t, err)
	globex, err := tenancy.NewTenantID("globex-corp")
	require.NoError(t, err)

	tcAcme, err := tenancy.NewTenantContext(acme)
	require.NoError(t, err)
	tcGlobex, err := tenancy.NewTenantContext(globex)
	require.NoError(t, err)

	gotAcme, _ := tcAcme.Tenant()
	gotGlobex, _ := tcGlobex.Tenant()
	assert.NotEqual(t, gotAcme, gotGlobex)
}

func TestNewAdministrative_RequiresActorAndReason(t *testing.T) {
	tests := []struct {
		name   string
		actor  string
		reason string
	}{
		{"empty actor", "", "incident response"},
		{"empty reason", "ops-oncall", ""},
		{"both empty", "", ""},
		{"whitespace only actor", "   ", "incident response"},
		{"whitespace only reason", "ops-oncall", "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tenancy.NewAdministrative(tt.actor, tt.reason)
			require.Error(t, err)
			assert.True(t, errors.Is(err, tenancy.ErrInvalid))
		})
	}
}

func TestNewAdministrative_AcceptsActorAndReason(t *testing.T) {
	admin, err := tenancy.NewAdministrative("ops-oncall", "crypto-shred deleted tenant data")
	require.NoError(t, err)
	assert.Equal(t, "ops-oncall", admin.Actor())
	assert.Equal(t, "crypto-shred deleted tenant data", admin.Reason())
}

func TestNewAdministrativeContext_IsTypeDistinctAndAttributed(t *testing.T) {
	admin, err := tenancy.NewAdministrative("ops-oncall", "crypto-shred deleted tenant data")
	require.NoError(t, err)

	tc, err := tenancy.NewAdministrativeContext(admin)
	require.NoError(t, err)

	assert.Equal(t, tenancy.ScopeAdministrative, tc.Scope())
	assert.NotEqual(t, tenancy.ScopeTenant, tc.Scope())

	gotAdmin, ok := tc.Administrative()
	require.True(t, ok)
	assert.Equal(t, "ops-oncall", gotAdmin.Actor())
	assert.Equal(t, "crypto-shred deleted tenant data", gotAdmin.Reason())

	_, ok = tc.Tenant()
	assert.False(t, ok, "administrative context must never report a tenant identity")
}

func TestNewAdministrativeContext_RejectsZeroValueAdministrative(t *testing.T) {
	var zero tenancy.Administrative
	_, err := tenancy.NewAdministrativeContext(zero)
	require.Error(t, err)
	assert.True(t, errors.Is(err, tenancy.ErrInvalid))
}

func TestAdministrative_WithCorrelationIDIsOptional(t *testing.T) {
	admin, err := tenancy.NewAdministrative("ops-oncall", "manual replay")
	require.NoError(t, err)

	_, ok := admin.CorrelationID()
	assert.False(t, ok, "correlation id is optional and unset by default")

	withCorrelation := admin.WithCorrelationID("corr-123")
	gotID, ok := withCorrelation.CorrelationID()
	require.True(t, ok)
	assert.Equal(t, "corr-123", gotID)

	// WithCorrelationID must not mutate the receiver (value semantics).
	_, ok = admin.CorrelationID()
	assert.False(t, ok)
}

func TestTenantContext_ZeroValueIsNeitherScope(t *testing.T) {
	var zero tenancy.TenantContext
	assert.NotEqual(t, tenancy.ScopeTenant, zero.Scope())
	assert.NotEqual(t, tenancy.ScopeAdministrative, zero.Scope())

	_, ok := zero.Tenant()
	assert.False(t, ok)
	_, ok = zero.Administrative()
	assert.False(t, ok)
}
