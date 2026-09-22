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

package persistence_test

import (
	"errors"
	"testing"

	"github.com/pablogore/ego/v4/persistence"
	"github.com/pablogore/ego/v4/tenancy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScopeZeroValueIsInvalid(t *testing.T) {
	var zero persistence.Scope

	assert.False(t, zero.Valid())
}

func TestScopeUnscopedIsValid(t *testing.T) {
	s := persistence.Unscoped()

	assert.True(t, s.Valid())
	assert.True(t, s.IsUnscoped())
	assert.Equal(t, tenancy.TenantID(""), s.TenantID())
}

func TestScopeNewTenantScopeIsValid(t *testing.T) {
	id, err := tenancy.NewTenantID("acme")
	require.NoError(t, err)

	s, err := persistence.NewTenantScope(id)
	require.NoError(t, err)

	assert.True(t, s.Valid())
	assert.False(t, s.IsUnscoped())
	assert.Equal(t, id, s.TenantID())
}

func TestNewTenantScopeRejectsEmptyTenantID(t *testing.T) {
	var empty tenancy.TenantID

	_, err := persistence.NewTenantScope(empty)

	require.Error(t, err)
	assert.True(t, errors.Is(err, persistence.ErrInvalidScope))
}

func TestScopeUnscopedNotEqualToTenantScope(t *testing.T) {
	id, err := tenancy.NewTenantID("acme")
	require.NoError(t, err)

	tenantScope, err := persistence.NewTenantScope(id)
	require.NoError(t, err)

	unscoped := persistence.Unscoped()

	assert.False(t, unscoped.Equal(tenantScope))
	assert.False(t, tenantScope.Equal(unscoped))
}

func TestScopeTwoTenantScopesWithDifferentIDsAreNotEqual(t *testing.T) {
	acme, err := tenancy.NewTenantID("acme")
	require.NoError(t, err)
	other, err := tenancy.NewTenantID("other")
	require.NoError(t, err)

	acmeScope, err := persistence.NewTenantScope(acme)
	require.NoError(t, err)
	otherScope, err := persistence.NewTenantScope(other)
	require.NoError(t, err)

	assert.False(t, acmeScope.Equal(otherScope))
}

func TestScopeTwoTenantScopesWithSameIDAreEqual(t *testing.T) {
	acme, err := tenancy.NewTenantID("acme")
	require.NoError(t, err)

	first, err := persistence.NewTenantScope(acme)
	require.NoError(t, err)
	second, err := persistence.NewTenantScope(acme)
	require.NoError(t, err)

	assert.True(t, first.Equal(second))
}

func TestScopeIsUnscoped(t *testing.T) {
	id, err := tenancy.NewTenantID("acme")
	require.NoError(t, err)
	tenantScope, err := persistence.NewTenantScope(id)
	require.NoError(t, err)

	assert.True(t, persistence.Unscoped().IsUnscoped())
	assert.False(t, tenantScope.IsUnscoped())
}

func TestScopeTenantIDRoundTrips(t *testing.T) {
	id, err := tenancy.NewTenantID("acme")
	require.NoError(t, err)

	tenantScope, err := persistence.NewTenantScope(id)
	require.NoError(t, err)

	assert.Equal(t, id, tenantScope.TenantID())
	assert.Equal(t, tenancy.TenantID(""), persistence.Unscoped().TenantID())
}

func TestScopeStringDistinguishesKinds(t *testing.T) {
	id, err := tenancy.NewTenantID("acme")
	require.NoError(t, err)
	tenantScope, err := persistence.NewTenantScope(id)
	require.NoError(t, err)

	assert.Equal(t, "unscoped", persistence.Unscoped().String())
	assert.Equal(t, "tenant:acme", tenantScope.String())
}

// A tenant whose id is literally the string "unscoped" MUST NOT equal
// Unscoped(): String() is a diagnostic rendering only, never a key, and a
// tenant id that happens to collide with another scope's rendering must
// never be able to forge that scope.
func TestScopeTenantNamedUnscopedDoesNotEqualUnscoped(t *testing.T) {
	id, err := tenancy.NewTenantID("unscoped")
	require.NoError(t, err)

	tenantScope, err := persistence.NewTenantScope(id)
	require.NoError(t, err)

	unscoped := persistence.Unscoped()

	assert.NotEqual(t, unscoped.String(), "")
	assert.False(t, unscoped.Equal(tenantScope))
	assert.False(t, tenantScope.Equal(unscoped))
	assert.NotEqual(t, unscoped, tenantScope)
}
