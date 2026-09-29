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
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/getsyntegrity/ego/command"
)

func TestNewEnvelope(t *testing.T) {
	specs.Describe(t, "NewEnvelope wraps a payload with its metadata", func(s *specs.Spec) {
		s.It("exposes the payload and the metadata it was built with", func(ctx *specs.Context) {
			op := mustOperationID(ctx.T, "op-1")
			md, err := command.NewMetadata(op)
			ctx.Expect(err).To(specs.BeNil())

			payload := timestamppb.New(time.Unix(100, 0))
			env, err := command.NewEnvelope(payload, md)
			ctx.Expect(err).To(specs.BeNil())

			ctx.Expect(proto.Equal(payload, env.Payload())).To(specs.BeTrue())
			ctx.Expect(env.Metadata()).ToEqual(md)
		})
	})
}

func TestNewEnvelopeRejectsNilPayload(t *testing.T) {
	specs.Describe(t, "NewEnvelope rejects a nil payload", func(s *specs.Spec) {
		s.It("fails with ErrInvalidEnvelope", func(ctx *specs.Context) {
			op := mustOperationID(ctx.T, "op-1")
			md, err := command.NewMetadata(op)
			ctx.Expect(err).To(specs.BeNil())

			_, err = command.NewEnvelope(nil, md)
			ctx.Expect(err).To(specs.MatchError(command.ErrInvalidEnvelope))
		})
	})
}

func TestPayloadAsTypedExtraction(t *testing.T) {
	specs.Describe(t, "PayloadAs extracts the payload as a concrete type", func(s *specs.Spec) {
		s.It("returns the payload for its own type and reports a different type as absent", func(ctx *specs.Context) {
			op := mustOperationID(ctx.T, "op-1")
			md, err := command.NewMetadata(op)
			ctx.Expect(err).To(specs.BeNil())

			payload := timestamppb.New(time.Unix(100, 0))
			env, err := command.NewEnvelope(payload, md)
			ctx.Expect(err).To(specs.BeNil())

			got, ok := command.PayloadAs[*timestamppb.Timestamp](env)
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(got.GetSeconds()).ToEqual(int64(100))

			_, ok = command.PayloadAs[*emptypb.Empty](env)
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}

func TestEnvelopeDeriveDelegatesToMetadataDerive(t *testing.T) {
	specs.Describe(t, "Envelope.Derive delegates to Metadata.Derive", func(s *specs.Spec) {
		s.It("carries the new payload, the inherited correlation and the parent as causation", func(ctx *specs.Context) {
			parentOp := mustOperationID(ctx.T, "op-1")
			parentMD, err := command.NewMetadata(parentOp)
			ctx.Expect(err).To(specs.BeNil())

			parentPayload := timestamppb.New(time.Unix(200, 0))
			parentEnv, err := command.NewEnvelope(parentPayload, parentMD)
			ctx.Expect(err).To(specs.BeNil())

			childOp := mustOperationID(ctx.T, "op-2")
			childPayload := timestamppb.New(time.Unix(300, 0))
			childEnv, err := parentEnv.Derive(childPayload, childOp)
			ctx.Expect(err).To(specs.BeNil())

			ctx.Expect(proto.Equal(childPayload, childEnv.Payload())).To(specs.BeTrue())
			ctx.Expect(childEnv.Metadata().CorrelationID()).ToEqual(parentMD.CorrelationID())
			ctx.Expect(childEnv.Metadata().OperationID()).ToEqual(childOp)

			causation, ok := childEnv.Metadata().CausationID()
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(causation).ToEqual(command.CausationID(parentOp))
		})
	})
}

func TestEnvelopeExpectedRevisionAbsentSurvivesCarrierRoundTrip(t *testing.T) {
	specs.Describe(t, "an absent expected revision survives an envelope carrier round trip", func(s *specs.Spec) {
		s.It("writes no key and rebuilds an envelope with no expected revision", func(ctx *specs.Context) {
			op := mustOperationID(ctx.T, "op-1")
			md, err := command.NewMetadata(op)
			ctx.Expect(err).To(specs.BeNil())

			payload := timestamppb.New(time.Unix(100, 0))
			env, err := command.NewEnvelope(payload, md)
			ctx.Expect(err).To(specs.BeNil())

			carrier := command.MarshalMetadata(env.Metadata())
			ctx.Expect(carrierHas(carrier, "ego.cmd.expected_revision")).To(specs.BeFalse())

			roundTripped, err := command.UnmarshalMetadata(carrier)
			ctx.Expect(err).To(specs.BeNil())

			rebuilt, err := command.NewEnvelope(payload, roundTripped)
			ctx.Expect(err).To(specs.BeNil())

			_, ok := rebuilt.Metadata().ExpectedRevision()
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}

func TestEnvelopeWithoutExpectedRevisionUnaffectedByNewField(t *testing.T) {
	specs.Describe(t, "an envelope without an expected revision marshals exactly as before the field existed", func(s *specs.Spec) {
		s.It("writes only the three required carrier keys", func(ctx *specs.Context) {
			op := mustOperationID(ctx.T, "op-1")
			ts := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

			md, err := command.NewMetadata(op, command.WithTimestamp(ts))
			ctx.Expect(err).To(specs.BeNil())

			payload := timestamppb.New(time.Unix(100, 0))
			env, err := command.NewEnvelope(payload, md)
			ctx.Expect(err).To(specs.BeNil())

			carrier := command.MarshalMetadata(env.Metadata())
			ctx.Expect(carrier).ToEqual(command.Carrier{
				"ego.cmd.operation_id":   string(op),
				"ego.cmd.correlation_id": string(command.CorrelationID(op)),
				"ego.cmd.timestamp":      ts.Format(time.RFC3339Nano),
			})
		})
	})
}

func TestEnvelopeDeriveRejectsNilPayload(t *testing.T) {
	specs.Describe(t, "Envelope.Derive rejects a nil payload", func(s *specs.Spec) {
		s.It("fails with ErrInvalidEnvelope", func(ctx *specs.Context) {
			op := mustOperationID(ctx.T, "op-1")
			md, err := command.NewMetadata(op)
			ctx.Expect(err).To(specs.BeNil())

			payload := timestamppb.New(time.Unix(200, 0))
			env, err := command.NewEnvelope(payload, md)
			ctx.Expect(err).To(specs.BeNil())

			childOp := mustOperationID(ctx.T, "op-2")
			_, err = env.Derive(nil, childOp)
			ctx.Expect(err).To(specs.MatchError(command.ErrInvalidEnvelope))
		})
	})
}
