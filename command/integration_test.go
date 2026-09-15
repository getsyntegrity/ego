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
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/pablogore/ego/v4/command"
)

// TestIntegrationMetadataEnvelopeResultCarrier exercises the full chain a
// real caller drives: a root Envelope is built, derived into a child
// operation (a saga step), the child's Metadata crosses a boundary as a
// Carrier and is reconstructed, and a Result is built from the
// reconstructed Metadata — proving the four types compose as one
// contract, not four contracts that merely happen to compile together.
func TestIntegrationMetadataEnvelopeResultCarrier(t *testing.T) {
	rootOp := mustOperationID(t, "op-root")
	tc := mustTenantContext(t, "tenant-1")
	principal, err := command.NewPrincipal("user-1", command.WithPrincipalKind("service-account"))
	require.NoError(t, err)
	deadline := time.Now().UTC().Add(time.Hour)

	rootMD, err := command.NewMetadata(rootOp,
		command.WithTenant(tc),
		command.WithPrincipal(principal),
		command.WithDeadline(deadline),
		command.WithCustom("region", "us-east-1"),
	)
	require.NoError(t, err)

	rootPayload := timestamppb.New(time.Unix(1, 0))
	rootEnvelope, err := command.NewEnvelope(rootPayload, rootMD)
	require.NoError(t, err)

	// A saga step derives a child operation from the root envelope.
	// Custom metadata is not inherited (D7), so it is re-supplied here.
	childOp := mustOperationID(t, "op-child")
	childPayload := timestamppb.New(time.Unix(2, 0))
	childEnvelope, err := rootEnvelope.Derive(childPayload, childOp, command.WithCustom("region", "us-east-1"))
	require.NoError(t, err)

	// The child's metadata crosses a boundary Envelope itself cannot
	// (e.g. goakt.SendSync) as a Carrier, and is reconstructed on the
	// far side.
	carrier := command.MarshalMetadata(childEnvelope.Metadata())
	reconstructed, err := command.UnmarshalMetadata(carrier)
	require.NoError(t, err)

	require.Equal(t, childEnvelope.Metadata().OperationID(), reconstructed.OperationID())
	require.Equal(t, rootMD.OperationID(), command.OperationID(reconstructed.CorrelationID()))

	wantCausation, ok := childEnvelope.Metadata().CausationID()
	require.True(t, ok)
	gotCausation, ok := reconstructed.CausationID()
	require.True(t, ok)
	require.Equal(t, wantCausation, gotCausation)
	require.Equal(t, rootOp, command.OperationID(gotCausation))

	reconstructedTenant, ok := reconstructed.Tenant()
	require.True(t, ok)
	wantTenantID, _ := tc.Tenant()
	gotTenantID, _ := reconstructedTenant.Tenant()
	require.Equal(t, wantTenantID, gotTenantID)

	reconstructedPrincipal, ok := reconstructed.Principal()
	require.True(t, ok)
	require.Equal(t, principal, reconstructedPrincipal)

	reconstructedCustom, ok := reconstructed.CustomValue("region")
	require.True(t, ok)
	require.Equal(t, "us-east-1", reconstructedCustom)

	// The far side of the boundary builds its Result against the
	// reconstructed Metadata, not the original in-process value —
	// proving Result composes with a Carrier-recovered Metadata exactly
	// as it does with one that never left the process.
	state := timestamppb.New(time.Unix(3, 0))
	result, err := command.NewSuccess(reconstructed, state, 1)
	require.NoError(t, err)
	require.Equal(t, command.OutcomeSuccess, result.Outcome())
	require.Equal(t, childOp, result.Metadata().OperationID())

	gotPayload, ok := command.PayloadAs[*timestamppb.Timestamp](childEnvelope)
	require.True(t, ok)
	require.Equal(t, int64(2), gotPayload.GetSeconds())

	gotState, ok := command.StateAs[*timestamppb.Timestamp](result)
	require.True(t, ok)
	require.Equal(t, int64(3), gotState.GetSeconds())
}

// TestIntegrationRejectedResultCarriesReconstructedMetadata proves a
// non-success outcome also composes across the Carrier boundary: the
// caller on the far side can build a Rejected Result from reconstructed
// Metadata and classify it via errors.Is exactly as it would in-process.
func TestIntegrationRejectedResultCarriesReconstructedMetadata(t *testing.T) {
	op := mustOperationID(t, "op-1")
	md, err := command.NewMetadata(op)
	require.NoError(t, err)

	envelope, err := command.NewEnvelope(timestamppb.New(time.Unix(1, 0)), md)
	require.NoError(t, err)

	carrier := command.MarshalMetadata(envelope.Metadata())
	reconstructed, err := command.UnmarshalMetadata(carrier)
	require.NoError(t, err)

	f, err := command.NewFailure("domain rejected")
	require.NoError(t, err)
	result, err := command.NewRejected(reconstructed, f)
	require.NoError(t, err)

	require.ErrorIs(t, result.Err(), command.ErrRejected)
	require.Equal(t, op, result.Metadata().OperationID())
}
