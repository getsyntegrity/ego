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

package compat_test

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/ego/engine"
	"github.com/getsyntegrity/ego/port/publishing"
	"github.com/getsyntegrity/ego/publisher/kafka"
	"github.com/getsyntegrity/ego/publisher/nats"
	"github.com/getsyntegrity/ego/publisher/pulsar"
	"github.com/getsyntegrity/ego/publisher/websocket"
)

// The publishers still satisfy the compatibility aliases in package engine (ADR
// ego-arch-001, S1 criterion 3), which existing consumers may still name them
// through. These are the eight compile-time assertions each publisher's
// compat_test.go used to hold, two per publisher.
var (
	_ engine.EventPublisher = (*kafka.EventsPublisher)(nil)
	_ engine.StatePublisher = (*kafka.DurableStatePublisher)(nil)

	_ engine.EventPublisher = (*nats.EventsPublisher)(nil)
	_ engine.StatePublisher = (*nats.DurableStatePublisher)(nil)

	_ engine.EventPublisher = (*pulsar.EventsPublisher)(nil)
	_ engine.StatePublisher = (*pulsar.DurableStatePublisher)(nil)

	_ engine.EventPublisher = (*websocket.EventsPublisher)(nil)
	_ engine.StatePublisher = (*websocket.DurableStatePublisher)(nil)
)

// TestEgoSentinelIsThePublishingSentinel is the module-crossing half of the
// historical runtime check "a stopped publisher's error matches
// engine.ErrPublisherNotStarted" (ADR ego-arch-006, §6 S1). The other half runs
// inside each publisher module: TestPublishBeforeStartMatchesPublishingSentinel
// in publisher_contract_test.go checks that Publish before Start returns an
// error matching publishing.ErrPublisherNotStarted, for events and state.
// Because the two sentinels are the same error value, every error that
// matches one matches the other, so together the two checks prove the
// original assertion for every publisher.
func TestEgoSentinelIsThePublishingSentinel(t *testing.T) {
	specs.Describe(t, "engine.ErrPublisherNotStarted is the publishing sentinel", func(s *specs.Spec) {
		s.It("is the same error value", func(ctx *specs.Context) {
			// Identity, not errors.Is, is the claim: the alias is the very same value.
			same := engine.ErrPublisherNotStarted == publishing.ErrPublisherNotStarted //nolint:errorlint // identity is what this case proves
			ctx.Expect(same).To(specs.BeTrue())
		})
		s.It("the engine alias matches the publishing sentinel", func(ctx *specs.Context) {
			ctx.Expect(engine.ErrPublisherNotStarted).To(specs.MatchError(publishing.ErrPublisherNotStarted))
		})
		s.It("the publishing sentinel matches the engine alias", func(ctx *specs.Context) {
			ctx.Expect(publishing.ErrPublisherNotStarted).To(specs.MatchError(engine.ErrPublisherNotStarted))
		})
	})
}
