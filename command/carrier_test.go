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
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/pablogore/ego/v4/command"
)

func TestMarshalMetadataUsesCanonicalKeys(t *testing.T) {
	op := mustOperationID(t, "op-1")
	md, err := command.NewMetadata(op)
	require.NoError(t, err)

	carrier := command.MarshalMetadata(md)
	require.Equal(t, string(op), carrier["ego.cmd.operation_id"])
	require.Equal(t, string(command.CorrelationID(op)), carrier["ego.cmd.correlation_id"])
	require.NotEmpty(t, carrier["ego.cmd.timestamp"])
	require.NotContains(t, carrier, "ego.cmd.causation_id")
	require.NotContains(t, carrier, "ego.cmd.deadline")
	require.NotContains(t, carrier, "ego.cmd.principal_id")
	require.NotContains(t, carrier, "ego.cmd.principal_kind")
}

func TestCarrierRoundTripPreservesIdentity(t *testing.T) {
	root := mustOperationID(t, "op-root")
	rootMD, err := command.NewMetadata(root)
	require.NoError(t, err)

	child := mustOperationID(t, "op-child")
	childMD, err := rootMD.Derive(child)
	require.NoError(t, err)

	carrier := command.MarshalMetadata(childMD)
	got, err := command.UnmarshalMetadata(carrier)
	require.NoError(t, err)

	require.Equal(t, childMD.OperationID(), got.OperationID())
	require.Equal(t, childMD.CorrelationID(), got.CorrelationID())

	wantCausation, ok := childMD.CausationID()
	require.True(t, ok)
	gotCausation, ok := got.CausationID()
	require.True(t, ok)
	require.Equal(t, wantCausation, gotCausation)
}

func TestCarrierRoundTripOptionalFieldsAbsent(t *testing.T) {
	op := mustOperationID(t, "op-1")
	md, err := command.NewMetadata(op)
	require.NoError(t, err)

	carrier := command.MarshalMetadata(md)
	got, err := command.UnmarshalMetadata(carrier)
	require.NoError(t, err)

	_, ok := got.CausationID()
	require.False(t, ok)
	_, ok = got.Tenant()
	require.False(t, ok)
	_, ok = got.Principal()
	require.False(t, ok)
	_, ok = got.Deadline()
	require.False(t, ok)
	require.Empty(t, got.Custom())
}

func TestCarrierRoundTripOptionalFieldsPresent(t *testing.T) {
	op := mustOperationID(t, "op-1")
	tc := mustTenantContext(t, "tenant-1")
	principal, err := command.NewPrincipal("user-1", command.WithPrincipalKind("service-account"))
	require.NoError(t, err)
	deadline := time.Now().UTC().Add(time.Hour).Truncate(time.Nanosecond)

	md, err := command.NewMetadata(op,
		command.WithTenant(tc),
		command.WithPrincipal(principal),
		command.WithDeadline(deadline),
		command.WithCustom("region", "us-east-1"),
	)
	require.NoError(t, err)

	carrier := command.MarshalMetadata(md)
	got, err := command.UnmarshalMetadata(carrier)
	require.NoError(t, err)

	gotTenant, ok := got.Tenant()
	require.True(t, ok)
	gotTenantID, _ := gotTenant.Tenant()
	wantTenantID, _ := tc.Tenant()
	require.Equal(t, wantTenantID, gotTenantID)

	gotPrincipal, ok := got.Principal()
	require.True(t, ok)
	require.Equal(t, principal, gotPrincipal)

	gotDeadline, ok := got.Deadline()
	require.True(t, ok)
	require.True(t, deadline.Equal(gotDeadline))

	gotCustom, ok := got.CustomValue("region")
	require.True(t, ok)
	require.Equal(t, "us-east-1", gotCustom)
}

func TestUnmarshalMetadataRejectsMissingOperationID(t *testing.T) {
	carrier := command.Carrier{
		"ego.cmd.correlation_id": "flow-1",
		"ego.cmd.timestamp":      time.Now().UTC().Format(time.RFC3339Nano),
	}

	_, err := command.UnmarshalMetadata(carrier)
	require.ErrorIs(t, err, command.ErrInvalidMetadata)
}

func TestUnmarshalMetadataRejectsReservedBareKey(t *testing.T) {
	op := mustOperationID(t, "op-1")
	md, err := command.NewMetadata(op)
	require.NoError(t, err)

	carrier := command.MarshalMetadata(md)
	carrier["operation_id"] = "shadow-attempt"

	_, err = command.UnmarshalMetadata(carrier)
	require.ErrorIs(t, err, command.ErrReservedKey)
}

func TestUnmarshalMetadataRejectsInvalidCustomValue(t *testing.T) {
	op := mustOperationID(t, "op-1")
	md, err := command.NewMetadata(op)
	require.NoError(t, err)

	carrier := command.MarshalMetadata(md)
	carrier["region"] = "us-east-1\x00"

	_, err = command.UnmarshalMetadata(carrier)
	require.ErrorIs(t, err, command.ErrInvalidMetadata)
}

func TestUnmarshalMetadataIgnoresUnknownEgoCmdKey(t *testing.T) {
	op := mustOperationID(t, "op-1")
	md, err := command.NewMetadata(op)
	require.NoError(t, err)

	carrier := command.MarshalMetadata(md)
	carrier["ego.cmd.future_field"] = "from-a-newer-writer"

	got, err := command.UnmarshalMetadata(carrier)
	require.NoError(t, err)
	require.Equal(t, md.OperationID(), got.OperationID())
	require.Empty(t, got.Custom())
}

// TestUnmarshalMetadataRejectsUnrecognizedEgoNamespace proves a carrier
// key under D6's reserved "ego." prefix but outside the two namespaces
// design.md's Carrier type comment grants forward compatibility to
// (ego.cmd.* ∪ ego.tenant.* — D9) is rejected with ErrReservedKey, not
// silently dropped. A hypothetical stray WRITE-005 ego.idem.* value
// stands in for that case here; reconstructing from a Carrier must not
// be more permissive than constructing directly via WithCustom, which
// already rejects any "ego."-prefixed custom key.
func TestUnmarshalMetadataRejectsUnrecognizedEgoNamespace(t *testing.T) {
	op := mustOperationID(t, "op-1")
	md, err := command.NewMetadata(op)
	require.NoError(t, err)

	carrier := command.MarshalMetadata(md)
	carrier["ego.idem.key"] = "future-namespace-value"

	_, err = command.UnmarshalMetadata(carrier)
	require.ErrorIs(t, err, command.ErrReservedKey)
}

func TestCarrierRoundTripExpectedRevisionPresent(t *testing.T) {
	op := mustOperationID(t, "op-1")
	md, err := command.NewMetadata(op, command.WithExpectedRevision(7))
	require.NoError(t, err)

	carrier := command.MarshalMetadata(md)
	require.Equal(t, "7", carrier["ego.cmd.expected_revision"])

	got, err := command.UnmarshalMetadata(carrier)
	require.NoError(t, err)

	revision, ok := got.ExpectedRevision()
	require.True(t, ok)
	require.Equal(t, uint64(7), revision)
}

func TestCarrierRoundTripExpectedRevisionAbsentStaysAbsent(t *testing.T) {
	op := mustOperationID(t, "op-1")
	md, err := command.NewMetadata(op)
	require.NoError(t, err)

	carrier := command.MarshalMetadata(md)
	require.NotContains(t, carrier, "ego.cmd.expected_revision")

	got, err := command.UnmarshalMetadata(carrier)
	require.NoError(t, err)

	_, ok := got.ExpectedRevision()
	require.False(t, ok)
}

func TestUnmarshalMetadataRejectsMalformedExpectedRevision(t *testing.T) {
	op := mustOperationID(t, "op-1")
	md, err := command.NewMetadata(op)
	require.NoError(t, err)

	carrier := command.MarshalMetadata(md)
	carrier["ego.cmd.expected_revision"] = "not-a-number"

	_, err = command.UnmarshalMetadata(carrier)
	require.ErrorIs(t, err, command.ErrInvalidMetadata)
}

func TestCarrierDelegatesTenantSerializationToTenancyPackage(t *testing.T) {
	op := mustOperationID(t, "op-1")
	tc := mustTenantContext(t, "tenant-1")
	md, err := command.NewMetadata(op, command.WithTenant(tc))
	require.NoError(t, err)

	carrier := command.MarshalMetadata(md)
	require.Equal(t, "tenant", carrier["ego.tenant.scope"])
	require.Equal(t, "tenant-1", carrier["ego.tenant.id"])
}
