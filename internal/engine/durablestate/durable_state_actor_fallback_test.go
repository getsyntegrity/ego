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

package durablestate

import (
	"context"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/ego/internal/engine/enginetest"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
)

// TestActorFallsBackToHandleCommandWithoutMetadata mirrors
// TestEventSourcedActorFallsBackToHandleCommandWithoutMetadata for
// Actor.dispatchToBehavior.
func TestActorFallsBackToHandleCommandWithoutMetadata(t *testing.T) {
	specs.Describe(t, "dispatchToBehavior falls back to HandleCommand when the command carries no Metadata", func(s *specs.Spec) {
		s.It("calls HandleCommand once and never HandleEnvelope", func(ctx *specs.Context) {
			entity := &Actor{
				behavior: enginetest.NewEnvelopeCapturingDurableStateBehavior("no-metadata"),
			}

			newState, newVersion, err := entity.dispatchToBehavior(context.Background(), &testpb.CreateAccount{AccountBalance: 9}, 0, new(testpb.Account))
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(newVersion).ToEqual(uint64(1))
			ctx.Expect(newState == nil).To(specs.BeFalse())

			behavior := entity.behavior.(*enginetest.EnvelopeCapturingDurableStateBehavior)
			handleCommandHit, handleEnvelopeHit, _ := behavior.Snapshot()
			ctx.Expect(handleCommandHit).ToEqual(1)
			ctx.Expect(handleEnvelopeHit).ToEqual(0)
		})
	})
}
