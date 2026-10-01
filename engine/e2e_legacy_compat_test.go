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
	"sync"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/google/uuid"

	"github.com/getsyntegrity/ego/command"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
)

// legacyCompatCaseG4 is one actor type the legacy-compatibility proof runs
// against. spawn starts an engine and one entity of that type. first sends the
// entity its opening command the legacy way and checks it succeeded with
// revision 1.
type legacyCompatCaseG4 struct {
	actor string
	spawn func(ctx *specs.Context, name string) (*Engine, string)
	first func(ctx *specs.Context, engine *Engine, entityID string)
}

// legacyCompatCasesG4 lists both actor types: EventSourcedActor over a real
// testkit.EventsStore and DurableStateActor over a real testkit.StateStore.
func legacyCompatCasesG4() []legacyCompatCaseG4 {
	bg := context.Background()
	return []legacyCompatCaseG4{
		{
			actor: "EventSourcedActor",
			spawn: func(ctx *specs.Context, name string) (*Engine, string) {
				engine := newTestEngine(ctx.T, name, connectedEventsStore(ctx), WithLogger(DiscardLogger))
				ctx.Expect(engine.Start(bg)).To(specs.BeNil())
				entityID := uuid.NewString()
				ctx.Expect(engine.Entity(bg, NewEventSourcedEntity(entityID))).To(specs.BeNil())
				return engine, entityID
			},
			first: func(ctx *specs.Context, engine *Engine, entityID string) {
				// The legacy entry point: SendCommand never carries ExpectedRevision,
				// so every write it issues must resolve to Unconditional() (D4/D8).
				state, revision, err := engine.SendCommand(bg, entityID, &testpb.CreateAccount{AccountBalance: 100}, time.Minute)
				ctx.Expect(err).To(specs.BeNil())
				acct, ok := state.(*testpb.Account)
				ctx.Expect(ok).To(specs.BeTrue())
				ctx.Expect(acct.GetAccountBalance()).ToEqual(float64(100))
				ctx.Expect(revision).ToEqual(uint64(1))
			},
		},
		{
			actor: "DurableStateActor",
			spawn: func(ctx *specs.Context, name string) (*Engine, string) {
				engine := newTestEngine(ctx.T, name, nil, WithLogger(DiscardLogger), WithStateStore(connectedDurableStore(ctx)))
				ctx.Expect(engine.Start(bg)).To(specs.BeNil())
				entityID := uuid.NewString()
				ctx.Expect(engine.DurableStateEntity(bg, NewAccountDurableStateBehavior(entityID))).To(specs.BeNil())
				return engine, entityID
			},
			first: func(ctx *specs.Context, engine *Engine, entityID string) {
				// dispatch with zero MetadataOptions declares no
				// ExpectedRevision, matching the legacy caller contract for
				// DurableStateActor as well.
				created := dispatch(ctx, engine, entityID, &testpb.CreateAccount{AccountBalance: 100})
				ctx.Expect(created.Outcome()).To(specs.Equal(command.OutcomeSuccess))
				ctx.Expect(created.Revision()).ToEqual(uint64(1))
			},
		},
	}
}

// This file is task 5.5's e2e proof for AC8 ("Legacy Caller Compatibility"):
// a legacy command carrying no ExpectedRevision metadata behaves
// unconditionally end-to-end through BOTH EventSourcedActor+real
// testkit.EventsStore AND DurableStateActor+real testkit.StateStore -- never
// producing concurrency_conflict, regardless of concurrent dispatch -- and
// ExpectedRevision present as 0 maps to ExpectGenesis() for both actor
// types. TestLegacyCompatEventSourcedAndDurableStateNeverConflict is one
// test artifact composing both actor types deliberately: no existing test
// demonstrates this cross-actor-type guarantee together (the closest
// existing coverage, TestEventSourcedLegacyCommandIsUnconditional and the
// DurableState genesis tests in durable_state_actor_expected_revision_test.go,
// each exercise only one actor type in isolation).
func TestLegacyCompatEventSourcedAndDurableStateNeverConflict(t *testing.T) {
	specs.Describe(t, "a command with no ExpectedRevision is unconditional and 0 maps to ExpectGenesis", func(s *specs.Spec) {
		cases := legacyCompatCasesG4()

		specs.Table(s, cases, func(c legacyCompatCaseG4) string {
			return c.actor + ": legacy commands never conflict under concurrent dispatch"
		}, func(ctx *specs.Context, c legacyCompatCaseG4) {
			engine, entityID := c.spawn(ctx, "e2e-legacy-"+c.actor)
			c.first(ctx, engine, entityID)

			// Multiple legacy commands dispatched concurrently against the same
			// entity: Unconditional() never declares an expectation that can go
			// stale, so none of these may ever surface concurrency_conflict,
			// regardless of the concurrent dispatch racing through the mailbox.
			// dispatch with zero MetadataOptions declares no
			// ExpectedRevision -- the same absence contract as the legacy path.
			const n = 10
			var wg sync.WaitGroup
			results := make([]command.Result, n)
			wg.Add(n)
			for i := range n {
				ctx.Go(func(task *specs.Context) {
					defer wg.Done()
					results[i] = dispatch(task, engine, entityID,
						&testpb.CreditAccount{AccountId: entityID, Balance: 1})
				})
			}
			wg.Wait()

			outcomes := make([]command.Outcome, 0, n)
			codes := make([]string, 0, n)
			for _, result := range results {
				outcomes = append(outcomes, result.Outcome())
				if failure, hasFailure := result.Failure(); hasFailure {
					if code, hasCode := failure.Code(); hasCode {
						codes = append(codes, code)
					}
				}
			}
			// every legacy command succeeds: none is rejected, none conflicts
			ctx.Expect(outcomes).To(specs.EveryElement(specs.Equal(command.OutcomeSuccess)))
			ctx.Expect(codes).To(specs.NoElement(specs.Equal(command.CodeConcurrencyConflict)))
		})

		specs.Table(s, cases, func(c legacyCompatCaseG4) string {
			return c.actor + ": ExpectedRevision=0 maps to ExpectGenesis()"
		}, func(ctx *specs.Context, c legacyCompatCaseG4) {
			engine, entityID := c.spawn(ctx, "e2e-legacy-genesis-"+c.actor)

			result := dispatch(ctx, engine, entityID, &testpb.CreateAccount{AccountBalance: 500}, command.WithExpectedRevision(0))
			ctx.Expect(result.Outcome()).To(specs.Equal(command.OutcomeSuccess))
			ctx.Expect(result.Revision()).ToEqual(uint64(1))

			// A second genesis declaration against the now-existing aggregate must
			// be rejected as a conflict, proving 0 was read as ExpectGenesis(),
			// never as Unconditional() nor silently ignored.
			conflict := dispatch(ctx, engine, entityID, &testpb.CreateAccount{AccountBalance: 999}, command.WithExpectedRevision(0))
			ctx.Expect(conflict.Outcome()).To(specs.Equal(command.OutcomeRejected))
			failure, ok := conflict.Failure()
			ctx.Expect(ok).To(specs.BeTrue())
			code, hasCode := failure.Code()
			ctx.Expect(hasCode).To(specs.BeTrue())
			ctx.Expect(code).To(specs.Equal(command.CodeConcurrencyConflict))
		})
	})
}
