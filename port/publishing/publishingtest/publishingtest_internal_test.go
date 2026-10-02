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

// The self-checks in this file prove that publishingtest fails when it
// should: each broken fake breaks one rule, and the test asserts that
// exactly the check for that rule fails.
package publishingtest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/urd/egopb"
	"github.com/getsyntegrity/urd/port/publishing"
)

// fakeEvents is a correct in-memory events publisher; each field breaks
// one rule.
type fakeEvents struct {
	keepPublishing bool // Publish still succeeds after Close
	changingID     bool // ID differs on every call
	drop           bool // Publish succeeds but nothing is delivered

	mu        sync.Mutex
	closed    bool
	idCalls   int
	delivered []*egopb.Event
}

func (f *fakeEvents) ID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.idCalls++
	if f.changingID {
		return fmt.Sprintf("fake-%d", f.idCalls)
	}
	return "fake"
}

func (f *fakeEvents) Publish(_ context.Context, e *egopb.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed && !f.keepPublishing {
		return publishing.ErrPublisherNotStarted
	}
	if !f.drop {
		f.delivered = append(f.delivered, e)
	}
	return nil
}

func (f *fakeEvents) Close(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

// received is the observer for fakes: it reports whether want was
// delivered to any fake built by the factory.
func eventsTarget(build func() *fakeEvents) EventsTarget {
	var mu sync.Mutex
	var all []*fakeEvents
	return EventsTarget{
		New: func(*testing.T) (publishing.EventPublisher, error) {
			f := build()
			mu.Lock()
			all = append(all, f)
			mu.Unlock()
			return f, nil
		},
		Received: func(_ context.Context, _ *testing.T, want *egopb.Event) error {
			mu.Lock()
			defer mu.Unlock()
			for _, f := range all {
				f.mu.Lock()
				for _, got := range f.delivered {
					if got.GetPersistenceId() == want.GetPersistenceId() {
						f.mu.Unlock()
						return nil
					}
				}
				f.mu.Unlock()
			}
			return errors.New("event never arrived")
		},
	}
}

type fakeState struct {
	fakeEvents
	delivered []*egopb.DurableState
}

func (f *fakeState) Publish(_ context.Context, s *egopb.DurableState) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed && !f.keepPublishing {
		return publishing.ErrPublisherNotStarted
	}
	f.delivered = append(f.delivered, s)
	return nil
}

func stateTarget(build func() *fakeState) StateTarget {
	var mu sync.Mutex
	var all []*fakeState
	return StateTarget{
		New: func(*testing.T) (publishing.StatePublisher, error) {
			f := build()
			mu.Lock()
			all = append(all, f)
			mu.Unlock()
			return f, nil
		},
		Received: func(_ context.Context, _ *testing.T, want *egopb.DurableState) error {
			mu.Lock()
			defer mu.Unlock()
			for _, f := range all {
				for _, got := range f.delivered {
					if got.GetPersistenceId() == want.GetPersistenceId() {
						return nil
					}
				}
			}
			return errors.New("state never arrived")
		},
	}
}

// byCheck indexes the results by check name and logs each one; the log is
// only there to read a failing run.
func byCheck(t testing.TB, results []Result) map[string]Result {
	t.Helper()
	out := map[string]Result{}
	for _, r := range results {
		t.Logf("%-28s %-14s %s", r.Check, r.Outcome, r.Detail)
		out[r.Check] = r
	}
	return out
}

// hasOutcome matches a Result whose Outcome is want.
func hasOutcome(want Outcome) specs.Matcher {
	return specs.Project("Outcome", func(r Result) Outcome { return r.Outcome }, specs.Equal(want))
}

// hasDetail matches a Result whose Detail mentions sub.
func hasDetail(sub string) specs.Matcher {
	return specs.Project("Detail", func(r Result) string { return r.Detail }, specs.Contain(sub))
}

// resultFor matches a map of results that holds one for check with the given
// outcome, and with a detail that mentions detail when detail is not empty.
func resultFor(check string, want Outcome, detail string) specs.Matcher {
	result := hasOutcome(want)
	if detail != "" {
		result = specs.All(result, hasDetail(detail))
	}
	return specs.All(
		specs.HaveKey(check),
		specs.Project(check, func(m map[string]Result) Result { return m[check] }, result),
	)
}

// failedChecks lists the names of the checks that failed, in order.
func failedChecks(m map[string]Result) []string {
	failed := []string{}
	for name, r := range m {
		if r.Outcome == Failed {
			failed = append(failed, name)
		}
	}
	slices.Sort(failed)
	return failed
}

// onlyFailure matches a map of results in which check failed with a detail that
// mentions detail and no other check failed.
func onlyFailure(check, detail string) specs.Matcher {
	return specs.All(
		resultFor(check, Failed, detail),
		specs.Project("failed checks", failedChecks, specs.Equal([]string{check})),
	)
}

func TestRunEvents_CorrectPublisherPasses(t *testing.T) {
	specs.Describe(t, "RunEvents and the capture accept a correct events publisher", func(s *specs.Spec) {
		s.It("passes PT-1, PT-2 and PT-3", func(ctx *specs.Context) {
			RunEvents(ctx.T, eventsTarget(func() *fakeEvents { return &fakeEvents{} }))
			got := byCheck(ctx.T, captureEvents(ctx.T, eventsTarget(func() *fakeEvents { return &fakeEvents{} })))
			for _, check := range []string{"PT-1", "PT-2", "PT-3"} {
				ctx.Expect(got).To(resultFor(check, Passed, ""))
			}
		})
	})
}

func TestRunState_CorrectPublisherPasses(t *testing.T) {
	specs.Describe(t, "RunState and the capture accept a correct state publisher", func(s *specs.Spec) {
		s.It("passes PT-1, PT-2 and PT-3", func(ctx *specs.Context) {
			RunState(ctx.T, stateTarget(func() *fakeState { return &fakeState{} }))
			got := byCheck(ctx.T, captureState(ctx.T, stateTarget(func() *fakeState { return &fakeState{} })))
			for _, check := range []string{"PT-1", "PT-2", "PT-3"} {
				ctx.Expect(got).To(resultFor(check, Passed, ""))
			}
		})
	})
}

// Task 2 self-check: a publisher that keeps publishing after Close fails
// PT-1, and only PT-1.
func TestCapture_PublishingAfterCloseFailsPT1(t *testing.T) {
	specs.Describe(t, "The capture reports a publisher that publishes after Close as failing PT-1 only", func(s *specs.Spec) {
		s.It("fails only PT-1 for an events publisher and for a state publisher", func(ctx *specs.Context) {
			got := byCheck(ctx.T, captureEvents(ctx.T, eventsTarget(func() *fakeEvents { return &fakeEvents{keepPublishing: true} })))
			ctx.Expect(got).To(onlyFailure("PT-1", "ErrPublisherNotStarted"))

			gotState := byCheck(ctx.T, captureState(ctx.T, stateTarget(func() *fakeState {
				return &fakeState{fakeEvents: fakeEvents{keepPublishing: true}}
			})))
			ctx.Expect(gotState).To(onlyFailure("PT-1", "ErrPublisherNotStarted"))
		})
	})
}

func TestCapture_UnstableIDFailsPT2(t *testing.T) {
	specs.Describe(t, "The capture reports a publisher with an unstable ID as failing PT-2 only", func(s *specs.Spec) {
		s.It("fails only PT-2", func(ctx *specs.Context) {
			got := byCheck(ctx.T, captureEvents(ctx.T, eventsTarget(func() *fakeEvents { return &fakeEvents{changingID: true} })))
			ctx.Expect(got).To(onlyFailure("PT-2", "stable"))
		})
	})
}

func TestCapture_LostEventFailsPT3(t *testing.T) {
	specs.Describe(t, "The capture reports a publisher that loses events as failing PT-3 only", func(s *specs.Spec) {
		s.It("fails only PT-3", func(ctx *specs.Context) {
			got := byCheck(ctx.T, captureEvents(ctx.T, eventsTarget(func() *fakeEvents { return &fakeEvents{drop: true} })))
			ctx.Expect(got).To(onlyFailure("PT-3", "never arrived"))
		})
	})
}

func TestCapture_NoObserverIsNotExercised(t *testing.T) {
	specs.Describe(t, "The capture reports PT-3 as not exercised when the target has no observer", func(s *specs.Spec) {
		s.It("marks PT-3 not exercised", func(ctx *specs.Context) {
			target := eventsTarget(func() *fakeEvents { return &fakeEvents{} })
			target.Received = nil
			got := byCheck(ctx.T, captureEvents(ctx.T, target))
			ctx.Expect(got).To(resultFor("PT-3", NotExercised, "no hook"))
		})
	})
}

// unreachable mirrors adaptertest.ErrUnreachable, which this package
// recognizes by its Unreachable() bool method because it may not import
// adaptertest.
type unreachable struct{}

func (unreachable) Error() string     { return "adapter backend unreachable" }
func (unreachable) Unreachable() bool { return true }

func TestCapture_OnlyUnreachableSkips(t *testing.T) {
	specs.Describe(t, "The capture skips only for an unreachable backend", func(s *specs.Spec) {
		// Every target sets Received, so PT-3 is exercised too.
		observed := func(context.Context, *testing.T, *egopb.Event) error { return nil }
		var typedNil *fakeEvents

		type row struct {
			name   string
			target EventsTarget
			every  specs.Matcher
		}
		specs.Table(s, []row{
			{
				name: "skips every check when New reports an unreachable backend",
				target: EventsTarget{Received: observed, New: func(*testing.T) (publishing.EventPublisher, error) {
					return nil, fmt.Errorf("no broker: %w", unreachable{})
				}},
				every: hasOutcome(Skipped),
			},
			{
				name: "fails every check when New fails for another reason",
				target: EventsTarget{Received: observed, New: func(*testing.T) (publishing.EventPublisher, error) {
					return nil, errors.New("bad config")
				}},
				every: hasOutcome(Failed),
			},
			{
				name: "fails every check, naming nil, when New returns a typed nil publisher",
				target: EventsTarget{Received: observed, New: func(*testing.T) (publishing.EventPublisher, error) {
					return typedNil, nil
				}},
				every: specs.All(hasOutcome(Failed), hasDetail("nil")),
			},
		}, func(r row) string { return r.name }, func(ctx *specs.Context, r row) {
			results := captureEvents(ctx.T, r.target)
			byCheck(ctx.T, results)
			ctx.Expect(results).To(specs.HaveLen(3))
			ctx.Expect(results).To(specs.EveryElement(r.every))
		})
	})
}
