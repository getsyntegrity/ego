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

package engine

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/urd/port/publishing"
)

// Compiles only if the types are identical (i.e. aliases).
var (
	_ *publishing.EventPublisher = (*EventPublisher)(nil)
	_ *publishing.StatePublisher = (*StatePublisher)(nil)
)

// TestPublisherContractsAliasPortPublishing pins the compatibility promise of
// ADR ego-arch-001 slice S1a: the publisher contracts moved to port/publishing
// and package engine keeps aliases, so both import paths name the same types and
// the same sentinel error.
func TestPublisherContractsAliasPortPublishing(t *testing.T) {
	specs.Describe(t, "the publisher contracts in package engine alias the publishing port types and sentinel", func(s *specs.Spec) {
		s.It("names the same types and the same sentinel error under both import paths", func(ctx *specs.Context) {
			var (
				eventPub publishing.EventPublisher = (*recordingEventPublisher)(nil)
				statePub publishing.StatePublisher = (*recordingStatePublisher)(nil)
			)

			var (
				_ EventPublisher = eventPub //nolint:staticcheck // compile-time alias assertion: the explicit type is the point
				_ StatePublisher = statePub //nolint:staticcheck // compile-time alias assertion: the explicit type is the point
			)

			ctx.Expect(publishing.ErrPublisherNotStarted == ErrPublisherNotStarted).To(specs.BeTrue()) //nolint:errorlint // identity is the point
			ctx.Expect(publishing.ErrPublisherNotStarted).To(specs.MatchError(ErrPublisherNotStarted))
		})
	})
}
