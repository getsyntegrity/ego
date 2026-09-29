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
	"strings"
	"sync"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/port/publishing"
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

func byCheck(t testing.TB, results []Result) map[string]Result {
	t.Helper()
	out := map[string]Result{}
	for _, r := range results {
		t.Logf("%-28s %-14s %s", r.Check, r.Outcome, r.Detail)
		out[r.Check] = r
	}
	return out
}

func requireOutcome(t testing.TB, got map[string]Result, check string, want Outcome, detail string) {
	t.Helper()
	r, ok := got[check]
	if !ok {
		t.Fatalf("no result for %s", check)
	}
	if r.Outcome != want {
		t.Fatalf("%s: outcome %s, want %s (detail %q)", check, r.Outcome, want, r.Detail)
	}
	if detail != "" && !strings.Contains(r.Detail, detail) {
		t.Fatalf("%s: detail %q does not mention %q", check, r.Detail, detail)
	}
}

func requireOnlyFailure(t testing.TB, got map[string]Result, check, detail string) {
	t.Helper()
	requireOutcome(t, got, check, Failed, detail)
	for name, r := range got {
		if name != check && r.Outcome == Failed {
			t.Fatalf("%s also failed (%q); only %s should", name, r.Detail, check)
		}
	}
}

func TestRunEvents_CorrectPublisherPasses(t *testing.T) {
	specs.Describe(t, "RunEvents and the capture accept a correct events publisher", func(s *specs.Spec) {
		s.It("passes PT-1, PT-2 and PT-3", func(ctx *specs.Context) {
			RunEvents(ctx.T, eventsTarget(func() *fakeEvents { return &fakeEvents{} }))
			got := byCheck(ctx.T, captureEvents(ctx.T, eventsTarget(func() *fakeEvents { return &fakeEvents{} })))
			for _, check := range []string{"PT-1", "PT-2", "PT-3"} {
				requireOutcome(ctx.T, got, check, Passed, "")
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
				requireOutcome(ctx.T, got, check, Passed, "")
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
			requireOnlyFailure(ctx.T, got, "PT-1", "ErrPublisherNotStarted")

			gotState := byCheck(ctx.T, captureState(ctx.T, stateTarget(func() *fakeState {
				return &fakeState{fakeEvents: fakeEvents{keepPublishing: true}}
			})))
			requireOnlyFailure(ctx.T, gotState, "PT-1", "ErrPublisherNotStarted")
		})
	})
}

func TestCapture_UnstableIDFailsPT2(t *testing.T) {
	specs.Describe(t, "The capture reports a publisher with an unstable ID as failing PT-2 only", func(s *specs.Spec) {
		s.It("fails only PT-2", func(ctx *specs.Context) {
			got := byCheck(ctx.T, captureEvents(ctx.T, eventsTarget(func() *fakeEvents { return &fakeEvents{changingID: true} })))
			requireOnlyFailure(ctx.T, got, "PT-2", "stable")
		})
	})
}

func TestCapture_LostEventFailsPT3(t *testing.T) {
	specs.Describe(t, "The capture reports a publisher that loses events as failing PT-3 only", func(s *specs.Spec) {
		s.It("fails only PT-3", func(ctx *specs.Context) {
			got := byCheck(ctx.T, captureEvents(ctx.T, eventsTarget(func() *fakeEvents { return &fakeEvents{drop: true} })))
			requireOnlyFailure(ctx.T, got, "PT-3", "never arrived")
		})
	})
}

func TestCapture_NoObserverIsNotExercised(t *testing.T) {
	specs.Describe(t, "The capture reports PT-3 as not exercised when the target has no observer", func(s *specs.Spec) {
		s.It("marks PT-3 not exercised", func(ctx *specs.Context) {
			target := eventsTarget(func() *fakeEvents { return &fakeEvents{} })
			target.Received = nil
			got := byCheck(ctx.T, captureEvents(ctx.T, target))
			requireOutcome(ctx.T, got, "PT-3", NotExercised, "no hook")
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

		s.It("skips every check when New reports an unreachable backend", func(ctx *specs.Context) {
			skip := EventsTarget{Received: observed, New: func(*testing.T) (publishing.EventPublisher, error) {
				return nil, fmt.Errorf("no broker: %w", unreachable{})
			}}
			var offenders []string
			for _, r := range byCheck(ctx.T, captureEvents(ctx.T, skip)) {
				if r.Outcome != Skipped {
					offenders = append(offenders, fmt.Sprintf("%s: outcome %s, want skipped", r.Check, r.Outcome))
				}
			}
			ctx.Expect(offenders).To(specs.BeNil())
		})

		s.It("fails every check when New fails for another reason", func(ctx *specs.Context) {
			fail := EventsTarget{Received: observed, New: func(*testing.T) (publishing.EventPublisher, error) { return nil, errors.New("bad config") }}
			var offenders []string
			for _, r := range byCheck(ctx.T, captureEvents(ctx.T, fail)) {
				if r.Outcome != Failed {
					offenders = append(offenders, fmt.Sprintf("%s: outcome %s, want failed", r.Check, r.Outcome))
				}
			}
			ctx.Expect(offenders).To(specs.BeNil())
		})

		s.It("fails every check, naming nil, when New returns a typed nil publisher", func(ctx *specs.Context) {
			var typedNil *fakeEvents
			nilValue := EventsTarget{Received: observed, New: func(*testing.T) (publishing.EventPublisher, error) { return typedNil, nil }}
			var offenders []string
			for _, r := range byCheck(ctx.T, captureEvents(ctx.T, nilValue)) {
				if r.Outcome != Failed || !strings.Contains(r.Detail, "nil") {
					offenders = append(offenders, fmt.Sprintf("%s: outcome %s (%q), want failed naming nil", r.Check, r.Outcome, r.Detail))
				}
			}
			ctx.Expect(offenders).To(specs.BeNil())
		})
	})
}
