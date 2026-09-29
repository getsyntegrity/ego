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

package testkit

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
	"google.golang.org/protobuf/proto"

	testpb "github.com/getsyntegrity/ego/test/data/testpb"
)

// ---------------------------------------------------------------------------
// Test behaviors (defined locally to avoid import cycles with the engine package)
// ---------------------------------------------------------------------------

// accountEventSourcedBehavior implements EventSourcedBehavior for testing.
type accountEventSourcedBehavior struct {
	id string
}

var _ EventSourcedBehavior = (*accountEventSourcedBehavior)(nil)

func (b *accountEventSourcedBehavior) InitialState() proto.Message {
	return new(testpb.Account)
}

func (b *accountEventSourcedBehavior) HandleCommand(_ context.Context, command proto.Message, _ proto.Message) ([]proto.Message, error) {
	switch cmd := command.(type) {
	case *testpb.CreateAccount:
		return []proto.Message{
			&testpb.AccountCreated{
				AccountId:      b.id,
				AccountBalance: cmd.GetAccountBalance(),
			},
		}, nil

	case *testpb.CreditAccount:
		if cmd.GetAccountId() == b.id {
			return []proto.Message{
				&testpb.AccountCredited{
					AccountId:      cmd.GetAccountId(),
					AccountBalance: cmd.GetBalance(),
				},
			}, nil
		}
		return nil, errors.New("command sent to the wrong entity")

	case *testpb.TestNoEvent:
		return nil, nil

	case *testpb.TestPanic:
		// emits an event that HandleEvent does not recognize
		return []proto.Message{&testpb.TestSend{}}, nil

	default:
		return nil, errors.New("unhandled command")
	}
}

func (b *accountEventSourcedBehavior) HandleEvent(_ context.Context, event proto.Message, priorState proto.Message) (proto.Message, error) {
	switch evt := event.(type) {
	case *testpb.AccountCreated:
		return &testpb.Account{
			AccountId:      evt.GetAccountId(),
			AccountBalance: evt.GetAccountBalance(),
		}, nil

	case *testpb.AccountCredited:
		account := priorState.(*testpb.Account)
		bal := account.GetAccountBalance() + evt.GetAccountBalance()
		return &testpb.Account{
			AccountId:      evt.GetAccountId(),
			AccountBalance: bal,
		}, nil

	default:
		return nil, errors.New("unhandled event")
	}
}

// accountDurableStateBehavior implements DurableStateBehavior for testing.
type accountDurableStateBehavior struct {
	id string
}

var _ DurableStateBehavior = (*accountDurableStateBehavior)(nil)

func (b *accountDurableStateBehavior) InitialState() proto.Message {
	return new(testpb.Account)
}

func (b *accountDurableStateBehavior) HandleCommand(_ context.Context, command proto.Message, priorVersion uint64, priorState proto.Message) (proto.Message, uint64, error) {
	switch cmd := command.(type) {
	case *testpb.CreateAccount:
		return &testpb.Account{
			AccountId:      b.id,
			AccountBalance: cmd.GetAccountBalance(),
		}, priorVersion + 1, nil

	case *testpb.CreditAccount:
		if cmd.GetAccountId() == b.id {
			account := priorState.(*testpb.Account)
			bal := account.GetAccountBalance() + cmd.GetBalance()
			return &testpb.Account{
				AccountId:      cmd.GetAccountId(),
				AccountBalance: bal,
			}, priorVersion + 1, nil
		}
		return nil, 0, errors.New("command sent to the wrong entity")

	default:
		return nil, 0, errors.New("unhandled command")
	}
}

// recordingTB captures assertion failures instead of failing the enclosing test,
// so the scenario's own failure paths can themselves be asserted.
type recordingTB struct {
	testing.TB
	failed bool
}

func (r *recordingTB) Errorf(string, ...any) { r.failed = true }
func (r *recordingTB) Helper()               {}

func (r *recordingTB) FailNow() {
	r.failed = true
	runtime.Goexit()
}

// recordFailure runs the assertion against a recording testing.TB and reports
// whether it failed. The assertion runs on its own goroutine because a failed
// require aborts through runtime.Goexit. That goroutine touches only the
// recorder, never a spec context, and recordFailure joins it before returning,
// so it stays a raw goroutine.
func recordFailure(t testing.TB, assert func(t testing.TB)) bool {
	recorder := &recordingTB{TB: t}
	done := make(chan struct{})

	go func() {
		defer close(done)
		assert(recorder)
	}()

	<-done
	return recorder.failed
}

// ---------------------------------------------------------------------------
// EventSourcedScenario tests
// ---------------------------------------------------------------------------

func TestEventSourcedScenario_GivenStateWhenCommandThenEventsAndState(t *testing.T) {
	specs.Describe(t, "EventSourcedScenario checks events and state after a command on a given state", func(s *specs.Spec) {
		s.It("given a state, a command produces the expected events and resulting state", func(ctx *specs.Context) {
			behavior := &accountEventSourcedBehavior{id: "acc-1"}

			ForEventSourcedBehavior(behavior).
				Given(&testpb.Account{
					AccountId:      "acc-1",
					AccountBalance: 100.00,
				}).
				When(&testpb.CreditAccount{
					AccountId: "acc-1",
					Balance:   50.00,
				}).
				ThenEvents(ctx.T,
					&testpb.AccountCredited{
						AccountId:      "acc-1",
						AccountBalance: 50.00,
					},
				).
				ThenState(ctx.T, &testpb.Account{
					AccountId:      "acc-1",
					AccountBalance: 150.00,
				})
		})
	})
}

func TestEventSourcedScenario_WhenCommandFromInitialState(t *testing.T) {
	specs.Describe(t, "EventSourcedScenario runs a command from the initial state", func(s *specs.Spec) {
		s.It("a command from the initial state produces the expected events and state", func(ctx *specs.Context) {
			behavior := &accountEventSourcedBehavior{id: "acc-2"}

			ForEventSourcedBehavior(behavior).
				When(&testpb.CreateAccount{
					AccountBalance: 200.00,
				}).
				ThenEvents(ctx.T,
					&testpb.AccountCreated{
						AccountId:      "acc-2",
						AccountBalance: 200.00,
					},
				).
				ThenState(ctx.T, &testpb.Account{
					AccountId:      "acc-2",
					AccountBalance: 200.00,
				})
		})
	})
}

func TestEventSourcedScenario_WhenCommandReturnsError(t *testing.T) {
	specs.Describe(t, "EventSourcedScenario reports a command handler error", func(s *specs.Spec) {
		s.It("a command for the wrong entity fails with its error", func(ctx *specs.Context) {
			behavior := &accountEventSourcedBehavior{id: "acc-3"}

			ForEventSourcedBehavior(behavior).
				When(&testpb.CreditAccount{
					AccountId: "wrong-id",
					Balance:   10.00,
				}).
				ThenError(ctx.T, "command sent to the wrong entity")
		})
	})
}

func TestEventSourcedScenario_WhenCommandProducesNoEvents(t *testing.T) {
	specs.Describe(t, "EventSourcedScenario reports a command that emits nothing", func(s *specs.Spec) {
		s.It("a command that emits nothing has no events", func(ctx *specs.Context) {
			behavior := &accountEventSourcedBehavior{id: "acc-4"}

			ForEventSourcedBehavior(behavior).
				When(&testpb.TestNoEvent{}).
				ThenNoEvents(ctx.T)
		})
	})
}

func TestEventSourcedScenario_HandleEventFailsOnProducedEvent(t *testing.T) {
	specs.Describe(t, "EventSourcedScenario surfaces an event the behavior cannot apply", func(s *specs.Spec) {
		s.It("an unrecognized produced event fails while deriving the state", func(ctx *specs.Context) {
			behavior := &accountEventSourcedBehavior{id: "acc-5"}

			// TestPanic makes the command handler emit an event that HandleEvent
			// does not recognize, which surfaces while deriving the resulting state.
			ForEventSourcedBehavior(behavior).
				When(&testpb.TestPanic{}).
				ThenError(ctx.T, "unhandled event")
		})
	})
}

func TestEventSourcedScenario_GivenStateIsPassedToCommandHandler(t *testing.T) {
	specs.Describe(t, "EventSourcedScenario hands the given state to the command handler as-is", func(s *specs.Spec) {
		s.It("the command result builds on the given state without replaying events", func(ctx *specs.Context) {
			behavior := &accountEventSourcedBehavior{id: "acc-6"}

			// The prior state is handed to the command handler as-is: no event replay
			// happens during the Given phase, so HandleEvent is only exercised on the
			// events the command produced.
			ForEventSourcedBehavior(behavior).
				Given(&testpb.Account{
					AccountId:      "acc-6",
					AccountBalance: 125.00,
				}).
				When(&testpb.CreditAccount{
					AccountId: "acc-6",
					Balance:   75.00,
				}).
				ThenEvents(ctx.T,
					&testpb.AccountCredited{
						AccountId:      "acc-6",
						AccountBalance: 75.00,
					},
				).
				ThenState(ctx.T, &testpb.Account{
					AccountId:      "acc-6",
					AccountBalance: 200.00,
				})
		})
	})
}

func TestEventSourcedScenario_GivenEventsBuildTheState(t *testing.T) {
	specs.Describe(t, "EventSourcedScenario builds the arranged state from given events", func(s *specs.Spec) {
		s.It("given events are applied to build the state the command runs against", func(ctx *specs.Context) {
			behavior := &accountEventSourcedBehavior{id: "acc-8"}

			ForEventSourcedBehavior(behavior).
				GivenEvents(
					&testpb.AccountCreated{
						AccountId:      "acc-8",
						AccountBalance: 100.00,
					},
					&testpb.AccountCredited{
						AccountId:      "acc-8",
						AccountBalance: 25.00,
					},
				).
				When(&testpb.CreditAccount{
					AccountId: "acc-8",
					Balance:   75.00,
				}).
				ThenEvents(ctx.T,
					&testpb.AccountCredited{
						AccountId:      "acc-8",
						AccountBalance: 75.00,
					},
				).
				ThenState(ctx.T, &testpb.Account{
					AccountId:      "acc-8",
					AccountBalance: 200.00,
				})
		})
	})
}

func TestEventSourcedScenario_GivenEventsApplyOnTopOfGivenState(t *testing.T) {
	specs.Describe(t, "EventSourcedScenario applies given events on top of a given state", func(s *specs.Spec) {
		s.It("given events replay over the given state like a snapshot recovery", func(ctx *specs.Context) {
			behavior := &accountEventSourcedBehavior{id: "acc-9"}

			// mirrors an entity recovered from a snapshot and then replayed
			ForEventSourcedBehavior(behavior).
				Given(&testpb.Account{
					AccountId:      "acc-9",
					AccountBalance: 100.00,
				}).
				GivenEvents(
					&testpb.AccountCredited{
						AccountId:      "acc-9",
						AccountBalance: 25.00,
					},
				).
				When(&testpb.CreditAccount{
					AccountId: "acc-9",
					Balance:   75.00,
				}).
				ThenState(ctx.T, &testpb.Account{
					AccountId:      "acc-9",
					AccountBalance: 200.00,
				})
		})
	})
}

func TestEventSourcedScenario_GivenEventsFailureIsReportedAsArrangementFailure(t *testing.T) {
	specs.Describe(t, "EventSourcedScenario reports a failed arrangement instead of a command outcome", func(s *specs.Spec) {
		behavior := &accountEventSourcedBehavior{id: "acc-10"}

		// TestNoEvent is not recognized by HandleEvent, so the arrangement cannot be
		// built. Every assertion must report that instead of a command outcome, even
		// ThenError, which would otherwise let a broken setup pass as a failed command.
		result := ForEventSourcedBehavior(behavior).
			GivenEvents(&testpb.TestNoEvent{}).
			When(&testpb.CreateAccount{
				AccountBalance: 10.00,
			})

		s.It("records the arrangement error and no command error", func(ctx *specs.Context) {
			ctx.Expect(result.arrangeErr).To(specs.Not(specs.BeNil()))
			ctx.Expect(result.arrangeErr.Error()).To(specs.Contain("given events could not be applied"))
			ctx.Expect(result.err).To(specs.BeNil())
		})

		assertions := []struct {
			name   string
			assert func(t testing.TB)
		}{
			{"ThenEvents", func(t testing.TB) { result.ThenEvents(t, &testpb.AccountCreated{}) }},
			{"ThenState", func(t testing.TB) { result.ThenState(t, &testpb.Account{}) }},
			{"ThenNoEvents", func(t testing.TB) { result.ThenNoEvents(t) }},
			{"ThenError", func(t testing.TB) { result.ThenError(t, "unhandled event") }},
		}

		for _, assertion := range assertions {
			s.It(assertion.name+" fails when the arrangement failed", func(ctx *specs.Context) {
				ctx.Expect(recordFailure(ctx.T, assertion.assert)).To(specs.BeTrue())
			})
		}
	})
}

func TestEventSourcedScenario_UnhandledCommandReturnsError(t *testing.T) {
	specs.Describe(t, "EventSourcedScenario reports a command the behavior does not handle", func(s *specs.Spec) {
		s.It("an unhandled command fails with the behavior's error", func(ctx *specs.Context) {
			behavior := &accountEventSourcedBehavior{id: "acc-7"}

			// TestSend is not handled by our test behavior
			ForEventSourcedBehavior(behavior).
				When(&testpb.TestSend{}).
				ThenError(ctx.T, "unhandled command")
		})
	})
}

// ---------------------------------------------------------------------------
// DurableStateScenario tests
// ---------------------------------------------------------------------------

func TestDurableStateScenario_GivenStateWhenCommandThenStateAndVersion(t *testing.T) {
	specs.Describe(t, "DurableStateScenario checks state and version after a command on a given state", func(s *specs.Spec) {
		s.It("given a state and version, a command produces the expected state and next version", func(ctx *specs.Context) {
			behavior := &accountDurableStateBehavior{id: "ds-1"}

			ForDurableStateBehavior(behavior).
				Given(
					&testpb.Account{
						AccountId:      "ds-1",
						AccountBalance: 100.00,
					},
					1, // prior version
				).
				When(&testpb.CreditAccount{
					AccountId: "ds-1",
					Balance:   50.00,
				}).
				ThenState(ctx.T, &testpb.Account{
					AccountId:      "ds-1",
					AccountBalance: 150.00,
				}).
				ThenVersion(ctx.T, 2)
		})
	})
}

func TestDurableStateScenario_WhenCommandFromInitialState(t *testing.T) {
	specs.Describe(t, "DurableStateScenario runs a command from the initial state", func(s *specs.Spec) {
		s.It("a command from the initial state produces the expected state and version", func(ctx *specs.Context) {
			behavior := &accountDurableStateBehavior{id: "ds-2"}

			ForDurableStateBehavior(behavior).
				When(&testpb.CreateAccount{
					AccountBalance: 300.00,
				}).
				ThenState(ctx.T, &testpb.Account{
					AccountId:      "ds-2",
					AccountBalance: 300.00,
				}).
				ThenVersion(ctx.T, 1)
		})
	})
}

func TestDurableStateScenario_WhenCommandReturnsError(t *testing.T) {
	specs.Describe(t, "DurableStateScenario reports a command handler error", func(s *specs.Spec) {
		s.It("a command for the wrong entity fails with its error", func(ctx *specs.Context) {
			behavior := &accountDurableStateBehavior{id: "ds-3"}

			ForDurableStateBehavior(behavior).
				When(&testpb.CreditAccount{
					AccountId: "wrong-id",
					Balance:   10.00,
				}).
				ThenError(ctx.T, "command sent to the wrong entity")
		})
	})
}

func TestDurableStateScenario_UnhandledCommandReturnsError(t *testing.T) {
	specs.Describe(t, "DurableStateScenario reports a command the behavior does not handle", func(s *specs.Spec) {
		s.It("an unhandled command fails with the behavior's error", func(ctx *specs.Context) {
			behavior := &accountDurableStateBehavior{id: "ds-4"}

			// TestSend is not handled by our test behavior
			ForDurableStateBehavior(behavior).
				When(&testpb.TestSend{}).
				ThenError(ctx.T, "unhandled command")
		})
	})
}
