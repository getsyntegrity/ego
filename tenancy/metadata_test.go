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

func TestMarshalMetadata_TenantScope_UsesEgoTenantKeys(t *testing.T) {
	id := mustTenantID(t, "acme-corp")
	tc, err := tenancy.NewTenantContext(id)
	require.NoError(t, err)

	md := tenancy.MarshalMetadata(tc)

	assert.Equal(t, "tenant", md["ego.tenant.scope"])
	assert.Equal(t, "acme-corp", md["ego.tenant.id"])
	_, hasActor := md["ego.tenant.admin_actor"]
	assert.False(t, hasActor, "a tenant-scoped context must not carry administrative keys")
}

func TestMarshalMetadata_AdministrativeScope_UsesEgoTenantKeys(t *testing.T) {
	admin, err := tenancy.NewAdministrative("ops-oncall", "crypto-shred deleted tenant data")
	require.NoError(t, err)
	admin = admin.WithCorrelationID("corr-123")
	tc, err := tenancy.NewAdministrativeContext(admin)
	require.NoError(t, err)

	md := tenancy.MarshalMetadata(tc)

	assert.Equal(t, "administrative", md["ego.tenant.scope"])
	assert.Equal(t, "ops-oncall", md["ego.tenant.admin_actor"])
	assert.Equal(t, "crypto-shred deleted tenant data", md["ego.tenant.admin_reason"])
	assert.Equal(t, "corr-123", md["ego.tenant.admin_correlation_id"])
	_, hasID := md["ego.tenant.id"]
	assert.False(t, hasID, "an administrative context must not carry a tenant id key")
}

func TestMarshalMetadata_AdministrativeScope_OmitsCorrelationIDWhenAbsent(t *testing.T) {
	admin, err := tenancy.NewAdministrative("ops-oncall", "manual replay")
	require.NoError(t, err)
	tc, err := tenancy.NewAdministrativeContext(admin)
	require.NoError(t, err)

	md := tenancy.MarshalMetadata(tc)

	_, ok := md["ego.tenant.admin_correlation_id"]
	assert.False(t, ok)
}

func TestMetadata_RoundTrip_TenantScope(t *testing.T) {
	id := mustTenantID(t, "globex-corp")
	want, err := tenancy.NewTenantContext(id)
	require.NoError(t, err)

	md := tenancy.MarshalMetadata(want)
	got, err := tenancy.UnmarshalMetadata(md)
	require.NoError(t, err)

	assert.Equal(t, want, got)
	gotID, ok := got.Tenant()
	require.True(t, ok)
	assert.Equal(t, id, gotID)
}

func TestMetadata_RoundTrip_AdministrativeScope(t *testing.T) {
	admin, err := tenancy.NewAdministrative("ops-oncall", "crypto-shred deleted tenant data")
	require.NoError(t, err)
	admin = admin.WithCorrelationID("corr-456")
	want, err := tenancy.NewAdministrativeContext(admin)
	require.NoError(t, err)

	md := tenancy.MarshalMetadata(want)
	got, err := tenancy.UnmarshalMetadata(md)
	require.NoError(t, err)

	assert.Equal(t, want, got)
	gotAdmin, ok := got.Administrative()
	require.True(t, ok)
	assert.Equal(t, "ops-oncall", gotAdmin.Actor())
	assert.Equal(t, "crypto-shred deleted tenant data", gotAdmin.Reason())
	corrID, ok := gotAdmin.CorrelationID()
	require.True(t, ok)
	assert.Equal(t, "corr-456", corrID)
}

func TestUnmarshalMetadata_RejectsMissingScope(t *testing.T) {
	_, err := tenancy.UnmarshalMetadata(tenancy.Metadata{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, tenancy.ErrInvalid))
}

func TestUnmarshalMetadata_RejectsUnrecognizedScope(t *testing.T) {
	_, err := tenancy.UnmarshalMetadata(tenancy.Metadata{"ego.tenant.scope": "bogus"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, tenancy.ErrInvalid))
}

func TestUnmarshalMetadata_RejectsTenantScopeWithInvalidID(t *testing.T) {
	_, err := tenancy.UnmarshalMetadata(tenancy.Metadata{"ego.tenant.scope": "tenant", "ego.tenant.id": ""})
	require.Error(t, err)
	assert.True(t, errors.Is(err, tenancy.ErrInvalid))
}

func TestUnmarshalMetadata_RejectsAdministrativeScopeMissingAttribution(t *testing.T) {
	_, err := tenancy.UnmarshalMetadata(tenancy.Metadata{"ego.tenant.scope": "administrative"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, tenancy.ErrInvalid))
}
