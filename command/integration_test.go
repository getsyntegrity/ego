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

	"github.com/getsyntegrity/go-specs/specs"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/getsyntegrity/ego/command"
)

// TestIntegrationMetadataEnvelopeResultCarrier exercises the full chain a
// real caller drives: a root Envelope is built, derived into a child
// operation (a saga step), the child's Metadata crosses a boundary as a
// Carrier and is reconstructed, and a Result is built from the
// reconstructed Metadata — proving the four types compose as one
// contract, not four contracts that merely happen to compile together.
func TestIntegrationMetadataEnvelopeResultCarrier(t *testing.T) {
	specs.Describe(t, "metadata, envelope, carrier and result compose as one contract", func(s *specs.Spec) {
		s.It("derives a child, crosses a carrier boundary and builds a result from the reconstructed metadata", func(ctx *specs.Context) {
			rootOp := mustOperationID(ctx.T, "op-root")
			tc := mustTenantContext(ctx.T, "tenant-1")
			principal, err := command.NewPrincipal("user-1", command.WithPrincipalKind("service-account"))
			ctx.Expect(err).To(specs.BeNil())
			deadline := time.Now().UTC().Add(time.Hour)

			rootMD, err := command.NewMetadata(rootOp,
				command.WithTenant(tc),
				command.WithPrincipal(principal),
				command.WithDeadline(deadline),
				command.WithCustom("region", "us-east-1"),
			)
			ctx.Expect(err).To(specs.BeNil())

			rootPayload := timestamppb.New(time.Unix(1, 0))
			rootEnvelope, err := command.NewEnvelope(rootPayload, rootMD)
			ctx.Expect(err).To(specs.BeNil())

			// A saga step derives a child operation from the root envelope.
			// Custom metadata is not inherited (D7), so it is re-supplied here.
			childOp := mustOperationID(ctx.T, "op-child")
			childPayload := timestamppb.New(time.Unix(2, 0))
			childEnvelope, err := rootEnvelope.Derive(childPayload, childOp, command.WithCustom("region", "us-east-1"))
			ctx.Expect(err).To(specs.BeNil())

			// The child's metadata crosses a boundary Envelope itself cannot
			// (e.g. goakt.SendSync) as a Carrier, and is reconstructed on the
			// far side.
			carrier := command.MarshalMetadata(childEnvelope.Metadata())
			reconstructed, err := command.UnmarshalMetadata(carrier)
			ctx.Expect(err).To(specs.BeNil())

			ctx.Expect(reconstructed.OperationID()).ToEqual(childEnvelope.Metadata().OperationID())
			ctx.Expect(command.OperationID(reconstructed.CorrelationID())).ToEqual(rootMD.OperationID())

			wantCausation, ok := childEnvelope.Metadata().CausationID()
			ctx.Expect(ok).To(specs.BeTrue())
			gotCausation, ok := reconstructed.CausationID()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(gotCausation).ToEqual(wantCausation)
			ctx.Expect(command.OperationID(gotCausation)).ToEqual(rootOp)

			reconstructedTenant, ok := reconstructed.Tenant()
			ctx.Expect(ok).To(specs.BeTrue())
			wantTenantID, _ := tc.Tenant()
			gotTenantID, _ := reconstructedTenant.Tenant()
			ctx.Expect(gotTenantID).ToEqual(wantTenantID)

			reconstructedPrincipal, ok := reconstructed.Principal()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(reconstructedPrincipal).ToEqual(principal)

			reconstructedCustom, ok := reconstructed.CustomValue("region")
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(reconstructedCustom).ToEqual("us-east-1")

			// The far side of the boundary builds its Result against the
			// reconstructed Metadata, not the original in-process value —
			// proving Result composes with a Carrier-recovered Metadata exactly
			// as it does with one that never left the process.
			state := timestamppb.New(time.Unix(3, 0))
			result, err := command.NewSuccess(reconstructed, state, 1)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(result.Outcome()).ToEqual(command.OutcomeSuccess)
			ctx.Expect(result.Metadata().OperationID()).ToEqual(childOp)

			gotPayload, ok := command.PayloadAs[*timestamppb.Timestamp](childEnvelope)
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(gotPayload.GetSeconds()).ToEqual(int64(2))

			gotState, ok := command.StateAs[*timestamppb.Timestamp](result)
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(gotState.GetSeconds()).ToEqual(int64(3))
		})
	})
}

// TestIntegrationRejectedResultCarriesReconstructedMetadata proves a
// non-success outcome also composes across the Carrier boundary: the
// caller on the far side can build a Rejected Result from reconstructed
// Metadata and classify it via errors.Is exactly as it would in-process.
func TestIntegrationRejectedResultCarriesReconstructedMetadata(t *testing.T) {
	specs.Describe(t, "a rejected result composes with metadata reconstructed from a carrier", func(s *specs.Spec) {
		s.It("classifies as ErrRejected and keeps the operation id", func(ctx *specs.Context) {
			op := mustOperationID(ctx.T, "op-1")
			md, err := command.NewMetadata(op)
			ctx.Expect(err).To(specs.BeNil())

			envelope, err := command.NewEnvelope(timestamppb.New(time.Unix(1, 0)), md)
			ctx.Expect(err).To(specs.BeNil())

			carrier := command.MarshalMetadata(envelope.Metadata())
			reconstructed, err := command.UnmarshalMetadata(carrier)
			ctx.Expect(err).To(specs.BeNil())

			f, err := command.NewFailure("domain rejected")
			ctx.Expect(err).To(specs.BeNil())
			result, err := command.NewRejected(reconstructed, f)
			ctx.Expect(err).To(specs.BeNil())

			ctx.Expect(result.Err()).To(specs.MatchError(command.ErrRejected))
			ctx.Expect(result.Metadata().OperationID()).ToEqual(op)
		})
	})
}
