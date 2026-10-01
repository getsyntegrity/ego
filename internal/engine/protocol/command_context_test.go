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

package protocol

import (
	"context"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/ego/command"
)

func TestCarrierFromContext_NoneAttached(t *testing.T) {
	specs.Describe(t, "CarrierFromContext reports no carrier for a context that never had one attached", func(s *specs.Spec) {
		s.It("returns ok=false on a bare context", func(ctx *specs.Context) {
			_, ok := CarrierFromContext(context.Background())
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}

func TestAttachCarrier_RoundTrip(t *testing.T) {
	specs.Describe(t, "AttachCarrier stores a carrier that CarrierFromContext returns unchanged", func(s *specs.Spec) {
		s.It("returns the attached carrier", func(ctx *specs.Context) {
			op, err := command.NewOperationID("op-1")
			ctx.Expect(err).To(specs.BeNil())
			md, err := command.NewMetadata(op)
			ctx.Expect(err).To(specs.BeNil())

			attached := AttachCarrier(context.Background(), command.MarshalMetadata(md))

			c, ok := CarrierFromContext(attached)
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(c).ToEqual(command.MarshalMetadata(md))
		})
	})
}

func TestMetadataFromContext_NoneAttached(t *testing.T) {
	specs.Describe(t, "MetadataFromContext reports no metadata for a context that never had a carrier attached", func(s *specs.Spec) {
		s.It("returns ok=false on a bare context", func(ctx *specs.Context) {
			_, ok := MetadataFromContext(context.Background())
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}

func TestMetadataFromContext_RematerializesMetadata(t *testing.T) {
	specs.Describe(t, "MetadataFromContext rebuilds the metadata from the attached carrier", func(s *specs.Spec) {
		s.It("keeps the operation id, correlation id and timestamp", func(ctx *specs.Context) {
			op, err := command.NewOperationID("op-1")
			ctx.Expect(err).To(specs.BeNil())
			corr, err := command.NewOperationID("corr-1")
			ctx.Expect(err).To(specs.BeNil())
			md, err := command.NewMetadata(op, command.WithCorrelationID(command.CorrelationID(corr)))
			ctx.Expect(err).To(specs.BeNil())

			attached := AttachCarrier(context.Background(), command.MarshalMetadata(md))

			got, ok := MetadataFromContext(attached)
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(got.OperationID()).ToEqual(md.OperationID())
			ctx.Expect(got.CorrelationID()).ToEqual(md.CorrelationID())
			ctx.Expect(got.Timestamp()).ToEqual(md.Timestamp())
		})
	})
}

func TestMetadataFromContext_InvalidCarrierFailsClosed(t *testing.T) {
	specs.Describe(t, "MetadataFromContext treats an invalid carrier as no metadata", func(s *specs.Spec) {
		s.It("returns ok=false for a carrier missing the operation id", func(ctx *specs.Context) {
			// A Carrier missing the required operation_id key fails to unmarshal;
			// MetadataFromContext reports this as "no metadata available" rather
			// than propagating the error, so callers fall back to legacy dispatch.
			attached := AttachCarrier(context.Background(), command.Carrier{})
			_, ok := MetadataFromContext(attached)
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}
