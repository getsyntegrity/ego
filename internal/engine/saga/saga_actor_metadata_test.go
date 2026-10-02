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

package saga

import (
	"context"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/urd/command"
	"github.com/getsyntegrity/urd/internal/engine/enginetest"
	"github.com/getsyntegrity/urd/internal/engine/protocol"
)

// TestActorAttachCommandMetadata exercises sagaCommand's dual metadata
// behavior (#60, issue #60's "sagaCommand gana metadata" scope item):
// when a sagaCommand carries an explicit Metadata, attachCommandMetadata
// must use it verbatim; when left as the zero value, it must derive a
// fresh child from the saga's own rootMetadata (correlation inherited,
// causation set to the saga's root operation).
func TestActorAttachCommandMetadata(t *testing.T) {
	specs.Describe(t, "attachCommandMetadata uses explicit metadata verbatim and derives the rest from the saga's root", func(s *specs.Spec) {
		var (
			rootMetadata command.Metadata
			actor        *Actor
		)
		s.BeforeEach(func(ctx *specs.Context) {
			rootOp, err := command.NewOperationID("saga-root-1")
			ctx.Expect(err).To(specs.BeNil())
			rootMetadata, err = command.NewMetadata(rootOp)
			ctx.Expect(err).To(specs.BeNil())

			actor = &Actor{
				sagaID:       "saga-root-1",
				rootMetadata: rootMetadata,
				logger:       enginetest.DiscardLogger,
			}
		})

		s.It("zero-value metadata is auto-derived from the saga's root", func(ctx *specs.Context) {
			attached := actor.attachCommandMetadata(context.Background(), command.Metadata{})

			md, ok := protocol.MetadataFromContext(attached)
			ctx.Expect(ok).To(specs.BeTrue())
			// derived metadata must carry a fresh operation id, not the root's
			ctx.Expect(md.OperationID()).To(specs.NotEqual(rootMetadata.OperationID()))
			// correlation id must be inherited from the root (D7)
			ctx.Expect(md.CorrelationID()).ToEqual(rootMetadata.CorrelationID())
			causation, ok := md.CausationID()
			ctx.Expect(ok).To(specs.BeTrue())
			// causation must be the saga's root operation
			ctx.Expect(causation).ToEqual(command.CausationID(rootMetadata.OperationID()))
		})

		s.It("explicit metadata is used verbatim", func(ctx *specs.Context) {
			explicitOp, err := command.NewOperationID("explicit-op-1")
			ctx.Expect(err).To(specs.BeNil())
			explicit, err := command.NewMetadata(explicitOp, command.WithCorrelationID("explicit-correlation"))
			ctx.Expect(err).To(specs.BeNil())

			attached := actor.attachCommandMetadata(context.Background(), explicit)

			md, ok := protocol.MetadataFromContext(attached)
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(md.OperationID()).ToEqual(explicit.OperationID())
			ctx.Expect(md.CorrelationID()).ToEqual(explicit.CorrelationID())
		})
	})
}
