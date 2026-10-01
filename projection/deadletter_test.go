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

package projection

import (
	"context"
	"errors"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
	"google.golang.org/protobuf/types/known/anypb"
)

func TestDiscardHandler_InterfaceCompliance(t *testing.T) {
	specs.Describe(t, "DiscardHandler satisfies the Handler interface", func(s *specs.Spec) {
		s.It("is assignable to Handler", func(ctx *specs.Context) {
			var _ Handler = (*DiscardHandler)(nil)
		})
	})
}

func TestNewDiscardHandler(t *testing.T) {
	specs.Describe(t, "NewDiscardHandler builds a discard handler", func(s *specs.Spec) {
		s.It("returns a non-nil handler", func(ctx *specs.Context) {
			handler := NewDiscardHandler()
			ctx.Expect(handler).To(specs.Not(specs.BeNil()))
		})
	})
}

func TestDiscardHandler_Handle(t *testing.T) {
	specs.Describe(t, "DiscardHandler discards every event", func(s *specs.Spec) {
		s.It("handles an event without error", func(ctx *specs.Context) {
			handler := NewDiscardHandler()
			err := handler.Handle(context.Background(), "persistence-id-1", &anypb.Any{}, 1)
			ctx.Expect(err).To(specs.BeNil())
		})
	})
}

func TestDiscardDeadLetterHandler_InterfaceCompliance(t *testing.T) {
	specs.Describe(t, "DiscardDeadLetterHandler satisfies the DeadLetterHandler interface", func(s *specs.Spec) {
		s.It("is assignable to DeadLetterHandler", func(ctx *specs.Context) {
			var _ DeadLetterHandler = (*DiscardDeadLetterHandler)(nil)
		})
	})
}

func TestNewDiscardDeadLetterHandler(t *testing.T) {
	specs.Describe(t, "NewDiscardDeadLetterHandler builds a discard dead-letter handler", func(s *specs.Spec) {
		s.It("returns a non-nil handler", func(ctx *specs.Context) {
			handler := NewDiscardDeadLetterHandler()
			ctx.Expect(handler).To(specs.Not(specs.BeNil()))
		})
	})
}

func TestDiscardDeadLetterHandler_Handle(t *testing.T) {
	specs.Describe(t, "DiscardDeadLetterHandler discards every dead letter", func(s *specs.Spec) {
		s.It("handles a dead letter without error", func(ctx *specs.Context) {
			handler := NewDiscardDeadLetterHandler()
			err := handler.Handle(
				context.Background(),
				"test-projection",
				"persistence-id-1",
				&anypb.Any{},
				1,
				errors.New("some failure"),
			)
			ctx.Expect(err).To(specs.BeNil())
		})
	})
}
