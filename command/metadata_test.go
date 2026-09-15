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

package command_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/pablogore/ego/v4/command"
	"github.com/pablogore/ego/v4/tenancy"
)

func mustOperationID(t *testing.T, s string) command.OperationID {
	t.Helper()
	op, err := command.NewOperationID(s)
	require.NoError(t, err)
	return op
}

func mustTenantContext(t *testing.T, id string) tenancy.TenantContext {
	t.Helper()
	tenantID, err := tenancy.NewTenantID(id)
	require.NoError(t, err)
	tc, err := tenancy.NewTenantContext(tenantID)
	require.NoError(t, err)
	return tc
}

func TestNewMetadataRoot(t *testing.T) {
	op := mustOperationID(t, "op-1")

	md, err := command.NewMetadata(op)
	require.NoError(t, err)
	require.Equal(t, op, md.OperationID())
	require.Equal(t, command.CorrelationID(op), md.CorrelationID())

	_, ok := md.CausationID()
	require.False(t, ok)
}

func TestNewMetadataWithCorrelationID(t *testing.T) {
	op := mustOperationID(t, "op-1")
	corr := command.CorrelationID("flow-42")

	md, err := command.NewMetadata(op, command.WithCorrelationID(corr))
	require.NoError(t, err)
	require.Equal(t, corr, md.CorrelationID())
}

func TestNewMetadataWithTenant(t *testing.T) {
	op := mustOperationID(t, "op-1")
	tc := mustTenantContext(t, "acme")

	md, err := command.NewMetadata(op, command.WithTenant(tc))
	require.NoError(t, err)

	got, ok := md.Tenant()
	require.True(t, ok)
	require.Equal(t, tc, got)
}

func TestNewMetadataWithoutTenant(t *testing.T) {
	op := mustOperationID(t, "op-1")

	md, err := command.NewMetadata(op)
	require.NoError(t, err)

	_, ok := md.Tenant()
	require.False(t, ok)
}

func TestNewMetadataWithPrincipal(t *testing.T) {
	op := mustOperationID(t, "op-1")
	p, err := command.NewPrincipal("user-1", command.WithPrincipalKind("user"))
	require.NoError(t, err)

	md, err := command.NewMetadata(op, command.WithPrincipal(p))
	require.NoError(t, err)

	got, ok := md.Principal()
	require.True(t, ok)
	require.Equal(t, p, got)
}

func TestNewMetadataWithTimestampDefault(t *testing.T) {
	before := time.Now().UTC()
	op := mustOperationID(t, "op-1")

	md, err := command.NewMetadata(op)
	require.NoError(t, err)
	after := time.Now().UTC()

	require.False(t, md.Timestamp().Before(before))
	require.False(t, md.Timestamp().After(after))
}

func TestNewMetadataWithTimestampOverride(t *testing.T) {
	op := mustOperationID(t, "op-1")
	ts := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	md, err := command.NewMetadata(op, command.WithTimestamp(ts))
	require.NoError(t, err)
	require.True(t, ts.Equal(md.Timestamp()))
}

func TestNewMetadataWithDeadline(t *testing.T) {
	op := mustOperationID(t, "op-1")
	deadline := time.Now().Add(time.Hour)

	md, err := command.NewMetadata(op, command.WithDeadline(deadline))
	require.NoError(t, err)

	got, ok := md.Deadline()
	require.True(t, ok)
	require.True(t, deadline.Equal(got))
}

func TestNewMetadataWithoutDeadline(t *testing.T) {
	op := mustOperationID(t, "op-1")

	md, err := command.NewMetadata(op)
	require.NoError(t, err)

	_, ok := md.Deadline()
	require.False(t, ok)
}

func TestNewMetadataCustomDefensiveCopy(t *testing.T) {
	op := mustOperationID(t, "op-1")

	md, err := command.NewMetadata(op, command.WithCustom("region", "us-east"))
	require.NoError(t, err)

	custom := md.Custom()
	custom["region"] = "tampered"
	custom["extra"] = "tampered"

	v, ok := md.CustomValue("region")
	require.True(t, ok)
	require.Equal(t, "us-east", v)
	_, ok = md.CustomValue("extra")
	require.False(t, ok)
}

func TestNewMetadataCustomValue(t *testing.T) {
	op := mustOperationID(t, "op-1")

	md, err := command.NewMetadata(op, command.WithCustom("region", "us-east"))
	require.NoError(t, err)

	v, ok := md.CustomValue("region")
	require.True(t, ok)
	require.Equal(t, "us-east", v)

	_, ok = md.CustomValue("missing")
	require.False(t, ok)
}

func TestNewMetadataWithCustomRejectsReservedPrefix(t *testing.T) {
	op := mustOperationID(t, "op-1")

	_, err := command.NewMetadata(op, command.WithCustom("ego.tenant.id", "x"))
	require.ErrorIs(t, err, command.ErrReservedKey)
}

func TestNewMetadataWithCustomRejectsCanonicalKey(t *testing.T) {
	op := mustOperationID(t, "op-1")

	canonical := []string{
		"operation_id", "correlation_id", "causation_id",
		"timestamp", "deadline", "principal_id", "principal_kind", "tenant",
	}
	for _, key := range canonical {
		_, err := command.NewMetadata(op, command.WithCustom(key, "x"))
		require.ErrorIsf(t, err, command.ErrReservedKey, "key %q must be rejected", key)
	}
}

func TestNewMetadataWithCustomRejectsInvalidValue(t *testing.T) {
	op := mustOperationID(t, "op-1")

	_, err := command.NewMetadata(op, command.WithCustom("region", strings.Repeat("a", 1025)))
	require.Error(t, err)
}

func TestNewMetadataWithCustomAcceptsValidKeyValue(t *testing.T) {
	op := mustOperationID(t, "op-1")

	md, err := command.NewMetadata(op, command.WithCustom("region", "us-east"))
	require.NoError(t, err)

	v, ok := md.CustomValue("region")
	require.True(t, ok)
	require.Equal(t, "us-east", v)
}

func TestMetadataDeriveInheritsCorrelationAndChainsCausation(t *testing.T) {
	parentOp := mustOperationID(t, "op-1")
	parent, err := command.NewMetadata(parentOp)
	require.NoError(t, err)

	childOp := mustOperationID(t, "op-2")
	child, err := parent.Derive(childOp)
	require.NoError(t, err)

	require.Equal(t, parent.CorrelationID(), child.CorrelationID())
	require.Equal(t, childOp, child.OperationID())

	causation, ok := child.CausationID()
	require.True(t, ok)
	require.Equal(t, command.CausationID(parentOp), causation)
}

func TestMetadataDeriveRejectsSameOperationID(t *testing.T) {
	op := mustOperationID(t, "op-1")
	parent, err := command.NewMetadata(op)
	require.NoError(t, err)

	_, err = parent.Derive(op)
	require.ErrorIs(t, err, command.ErrSameOperationID)
}

func TestMetadataDeriveTenantMustNotChange(t *testing.T) {
	op := mustOperationID(t, "op-1")
	tc := mustTenantContext(t, "acme")
	parent, err := command.NewMetadata(op, command.WithTenant(tc))
	require.NoError(t, err)

	childOp := mustOperationID(t, "op-2")
	otherTenant := mustTenantContext(t, "other")

	_, err = parent.Derive(childOp, command.WithTenant(otherTenant))
	require.ErrorIs(t, err, tenancy.ErrDenied)
}

func TestMetadataDeriveTenantInheritedWhenUnspecified(t *testing.T) {
	op := mustOperationID(t, "op-1")
	tc := mustTenantContext(t, "acme")
	parent, err := command.NewMetadata(op, command.WithTenant(tc))
	require.NoError(t, err)

	childOp := mustOperationID(t, "op-2")
	child, err := parent.Derive(childOp)
	require.NoError(t, err)

	got, ok := child.Tenant()
	require.True(t, ok)
	require.Equal(t, tc, got)
}

func TestMetadataDeriveDeadlineMayOnlyShorten(t *testing.T) {
	op := mustOperationID(t, "op-1")
	deadline := time.Now().Add(time.Hour)
	parent, err := command.NewMetadata(op, command.WithDeadline(deadline))
	require.NoError(t, err)

	childOp := mustOperationID(t, "op-2")

	shorter := deadline.Add(-time.Minute)
	child, err := parent.Derive(childOp, command.WithDeadline(shorter))
	require.NoError(t, err)
	got, ok := child.Deadline()
	require.True(t, ok)
	require.True(t, shorter.Equal(got))

	longer := deadline.Add(time.Minute)
	_, err = parent.Derive(childOp, command.WithDeadline(longer))
	require.ErrorIs(t, err, command.ErrDeadlineExtension)
}

func TestMetadataDeriveCustomNotInherited(t *testing.T) {
	op := mustOperationID(t, "op-1")
	parent, err := command.NewMetadata(op, command.WithCustom("region", "us-east"))
	require.NoError(t, err)

	childOp := mustOperationID(t, "op-2")
	child, err := parent.Derive(childOp)
	require.NoError(t, err)

	_, ok := child.CustomValue("region")
	require.False(t, ok)
	require.Empty(t, child.Custom())
}

func TestMetadataDerivePrincipalInheritedUnlessOverridden(t *testing.T) {
	op := mustOperationID(t, "op-1")
	p, err := command.NewPrincipal("user-1")
	require.NoError(t, err)
	parent, err := command.NewMetadata(op, command.WithPrincipal(p))
	require.NoError(t, err)

	childOp := mustOperationID(t, "op-2")
	child, err := parent.Derive(childOp)
	require.NoError(t, err)

	got, ok := child.Principal()
	require.True(t, ok)
	require.Equal(t, p, got)

	other, err := command.NewPrincipal("user-2")
	require.NoError(t, err)
	child2, err := parent.Derive(childOp, command.WithPrincipal(other))
	require.NoError(t, err)

	got2, ok := child2.Principal()
	require.True(t, ok)
	require.Equal(t, other, got2)
}
