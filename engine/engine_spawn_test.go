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
	"context"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"

	testpb "github.com/getsyntegrity/urd/internal/testpb"
	"github.com/getsyntegrity/urd/testkit"
)

// TestEngineSpawnMethodsDomainOnlySingleNode spawns behaviors that implement
// only the port/behavior contracts through the public SpawnEventSourced,
// SpawnDurableState and SpawnSaga methods on a single node (#123 criterion
// 2). Each answers a command round trip, and an envelope-capable behavior
// still receives HandleEnvelope.
func TestEngineSpawnMethodsDomainOnlySingleNode(t *testing.T) {
	specs.Describe(t, "the Spawn methods spawn behaviors that implement only the port contracts", func(s *specs.Spec) {
		bg := context.Background()

		s.It("SpawnEventSourced, envelope-capable", func(ctx *specs.Context) {
			engine := newTestEngine(ctx.T, "SpawnEventSourced", connectedEventsStore(ctx), WithLogger(DiscardLogger))
			ctx.Expect(engine.Start(bg)).To(specs.BeNil())

			b := &domainOnlyEventSourced{id: uuid.NewString()}
			ctx.Expect(engine.SpawnEventSourced(bg, b)).To(specs.BeNil())

			state, revision, err := engine.SendCommand(bg, b.ID(), &testpb.CreateAccount{AccountBalance: 7}, time.Minute)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(revision).ToEqual(uint64(1))
			ctx.Expect(state.(*testpb.Account).GetAccountBalance()).ToEqual(float64(7))
			// an envelope-capable behavior still receives HandleEnvelope
			ctx.Expect(b.envelopeHits()).ToEqual(1)
		})

		s.It("SpawnDurableState", func(ctx *specs.Context) {
			engine := newTestEngine(ctx.T, "SpawnDurableState", nil, WithLogger(DiscardLogger),
				WithStateStore(connectedDurableStore(ctx)))
			ctx.Expect(engine.Start(bg)).To(specs.BeNil())

			b := &domainOnlyDurableState{id: uuid.NewString()}
			ctx.Expect(engine.SpawnDurableState(bg, b)).To(specs.BeNil())

			state, revision, err := engine.SendCommand(bg, b.ID(), &testpb.CreateAccount{AccountBalance: 9}, time.Minute)
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(revision).ToEqual(uint64(1))
			ctx.Expect(state.(*testpb.Account).GetAccountBalance()).ToEqual(float64(9))
		})

		s.It("SpawnSaga", func(ctx *specs.Context) {
			engine := newTestEngine(ctx.T, "SpawnSaga", connectedEventsStore(ctx), WithLogger(DiscardLogger))
			ctx.Expect(engine.Start(bg)).To(specs.BeNil())

			b := &domainOnlySaga{id: "saga-" + uuid.NewString()}
			ctx.Expect(engine.SpawnSaga(bg, b, 0)).To(specs.BeNil())

			// the spawned saga answers a status query under its own ID
			ctx.Eventually(func() any {
				info, err := engine.SagaStatus(bg, b.ID(), time.Second)
				if err != nil || info == nil {
					return ""
				}
				return info.ID
			}, specs.Equal(b.ID()), specs.WithTimeout(waitTimeout), specs.WithInterval(50*time.Millisecond))
		})

		s.It("not started", func(ctx *specs.Context) {
			engine := newTestEngine(ctx.T, "SpawnNotStarted", testkit.NewEventsStore(), WithLogger(DiscardLogger))
			ctx.Expect(engine.SpawnEventSourced(bg, &domainOnlyEventSourced{id: "a"})).To(specs.MatchError(ErrEngineNotStarted))
			ctx.Expect(engine.SpawnDurableState(bg, &domainOnlyDurableState{id: "b"})).To(specs.MatchError(ErrEngineNotStarted))
			ctx.Expect(engine.SpawnSaga(bg, &domainOnlySaga{id: "c"}, 0)).To(specs.MatchError(ErrEngineNotStarted))
		})
	})
}
