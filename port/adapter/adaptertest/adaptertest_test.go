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

// The self-checks in this file prove that adaptertest fails when it should:
// each deliberately broken fake below breaks one rule, and the test asserts
// that exactly the check for that rule fails. A conformance suite that
// passes a broken adapter is worse than none.
package adaptertest_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/pablogore/ego/v4/persistence"
	"github.com/pablogore/ego/v4/port/adapter"
	"github.com/pablogore/ego/v4/port/adapter/adaptertest"
)

const (
	publisherPort adapter.Port       = "publishing.EventPublisher"
	storePort     adapter.Port       = persistence.PortEventsStore
	flush         adapter.Capability = "test.flush"
)

// ---------------------------------------------------------------------------
// Fakes. owned is a correct owned adapter; every broken variant is built
// from it by flipping one field.
// ---------------------------------------------------------------------------

type owned struct {
	desc     *adapter.Descriptor // nil: undeclared
	unstable bool                // Describe returns a different Name each call
	calls    int

	startErr   error
	closed     int
	closeErr   error         // returned by every Close after the first
	closeBlock chan struct{} // Close ignores its context and waits on this
}

func (o *owned) Close(ctx context.Context) error {
	if o.closeBlock != nil {
		<-o.closeBlock
		return nil
	}
	o.closed++
	if o.closed > 1 && o.closeErr != nil {
		return o.closeErr
	}
	return nil
}

func (o *owned) describe() adapter.Descriptor {
	o.calls++
	d := *o.desc
	if o.unstable {
		d.Name = fmt.Sprintf("%s-%d", d.Name, o.calls)
	}
	return d
}

// ownedDescribed adds Describe.
type ownedDescribed struct{ *owned }

func (o ownedDescribed) Describe() adapter.Descriptor { return o.describe() }

// starter adds Describe, Start and Ping.
type starter struct{ *owned }

func (s starter) Describe() adapter.Descriptor   { return s.describe() }
func (s starter) Start(context.Context) error    { return s.startErr }
func (s starter) Ping(ctx context.Context) error { return ctx.Err() }

// flusher is an owned, constructor-acquiring adapter with a port-specific
// capability the suite can only check through Target.Capabilities.
type flusher struct {
	ownedDescribed
}

func (flusher) Flush() {}

// store is a correct borrowed adapter.
type store struct {
	pingErr error
}

func (s *store) Connect(context.Context) error    { return nil }
func (s *store) Disconnect(context.Context) error { return nil }
func (s *store) Ping(context.Context) error       { return s.pingErr }
func (s *store) Describe() adapter.Descriptor {
	return adapter.Descriptor{Ports: []adapter.Port{storePort}, Name: "fake-store"}
}

func desc(name string, caps ...adapter.Capability) *adapter.Descriptor {
	return &adapter.Descriptor{Ports: []adapter.Port{publisherPort}, Name: name, Capabilities: caps}
}

// newOf returns a factory that hands out v itself on every call. The fakes
// that use it keep no state a second check could trip over; a fake whose
// release is stateful gets a fresh value per call instead.
func newOf(v any) func(*testing.T) (any, error) {
	return func(*testing.T) (any, error) { return v, nil }
}

func ownedTarget(v any) adaptertest.Target {
	return adaptertest.Target{Port: publisherPort, Ownership: adaptertest.Owned, New: newOf(v)}
}

// outcomes indexes results by check name.
func outcomes(t *testing.T, results []adaptertest.Result) map[string]adaptertest.Result {
	t.Helper()
	out := map[string]adaptertest.Result{}
	for _, r := range results {
		t.Logf("%-38s %-14s %s", r.Check, r.Outcome, r.Detail)
		out[r.Check] = r
	}
	return out
}

func requireOutcome(t *testing.T, got map[string]adaptertest.Result, check string, want adaptertest.Outcome, detail string) {
	t.Helper()
	r, ok := got[check]
	if !ok {
		t.Fatalf("no result for %s", check)
	}
	if r.Outcome != want {
		t.Errorf("%s: outcome %s, want %s (detail %q)", check, r.Outcome, want, r.Detail)
	}
	if detail != "" && !strings.Contains(r.Detail, detail) {
		t.Errorf("%s: detail %q does not mention %q", check, r.Detail, detail)
	}
}

// requireOnlyFailure asserts that check failed and every other exercised
// check passed, so a broken fake is caught by the rule it breaks and by no
// other.
func requireOnlyFailure(t *testing.T, got map[string]adaptertest.Result, check, detail string) {
	t.Helper()
	requireOutcome(t, got, check, adaptertest.Failed, detail)
	for name, r := range got {
		if name != check && r.Outcome == adaptertest.Failed {
			t.Errorf("%s also failed (%q); only %s should", name, r.Detail, check)
		}
	}
}

// ---------------------------------------------------------------------------
// Correct adapters pass, and Run reports what it could not exercise.
// ---------------------------------------------------------------------------

func TestRun_CorrectStarterPassesEveryCheck(t *testing.T) {
	failing := errors.New("dial refused")
	results := adaptertest.Run(t, adaptertest.Target{
		Port:      publisherPort,
		Ownership: adaptertest.Owned,
		New: func(*testing.T) (any, error) {
			return starter{&owned{desc: desc("fake", adapter.CapStart, adapter.CapReady)}}, nil
		},
		FailStart: func(*testing.T) (any, error) {
			return starter{&owned{desc: desc("fake", adapter.CapStart, adapter.CapReady), startErr: failing}}, nil
		},
		Stall: func(*testing.T) {},
	})
	got := outcomes(t, results)
	for _, check := range []string{"AT-1", "AT-2", "AT-3/release twice", "AT-3/release without acquire", "AT-3/release after failed acquire", "AT-4", "AT-5"} {
		requireOutcome(t, got, check, adaptertest.Passed, "")
	}
	if len(got) != 7 {
		t.Errorf("got %d results, want 7", len(got))
	}
}

func TestRun_CorrectBorrowedStorePasses(t *testing.T) {
	got := outcomes(t, adaptertest.Run(t, adaptertest.Target{
		Port:      storePort,
		Ownership: adaptertest.Borrowed,
		New:       func(*testing.T) (any, error) { return &store{}, nil },
	}))
	requireOutcome(t, got, "AT-1", adaptertest.Passed, "")
	requireOutcome(t, got, "AT-3/release twice", adaptertest.Passed, "")
	requireOutcome(t, got, "AT-3/release without acquire", adaptertest.Passed, "")
	requireOutcome(t, got, "AT-5", adaptertest.Passed, "")
	requireOutcome(t, got, "AT-2", adaptertest.NotExercised, "no hook")
	requireOutcome(t, got, "AT-3/release after failed acquire", adaptertest.NotExercised, "no hook")
	requireOutcome(t, got, "AT-4", adaptertest.NotExercised, "no hook")
}

// Requirement "a skip means unreachable, nothing else": an adapter that
// acquires in its constructor has AT-2 and the failed-acquire case of AT-3
// reported as not exercised, never as passed, even when FailStart is set.
func TestCapture_ConstructorAcquireIsNotExercised(t *testing.T) {
	target := ownedTarget(ownedDescribed{&owned{desc: desc("ctor")}})
	target.FailStart = func(*testing.T) (any, error) { return nil, errors.New("dial refused") }
	got := outcomes(t, adaptertest.Capture(t, target))
	requireOutcome(t, got, "AT-2", adaptertest.NotExercised, "acquire happens in the constructor")
	requireOutcome(t, got, "AT-3/release after failed acquire", adaptertest.NotExercised, "acquire happens in the constructor")
	requireOutcome(t, got, "AT-4", adaptertest.NotExercised, "no hook")
	requireOutcome(t, got, "AT-5", adaptertest.NotExercised, "Pinger")
	requireOutcome(t, got, "AT-1", adaptertest.Passed, "")
	requireOutcome(t, got, "AT-3/release twice", adaptertest.Passed, "")
}

func TestCapture_UndeclaredAdapterIsNotExercisedByAT1(t *testing.T) {
	got := outcomes(t, adaptertest.Capture(t, ownedTarget(&owned{})))
	requireOutcome(t, got, "AT-1", adaptertest.NotExercised, "undeclared")
}

// ---------------------------------------------------------------------------
// Only ErrUnreachable skips.
// ---------------------------------------------------------------------------

func TestCapture_OnlyErrUnreachableSkips(t *testing.T) {
	t.Run("wrapped ErrUnreachable skips every check", func(t *testing.T) {
		target := ownedTarget(nil)
		target.New = func(*testing.T) (any, error) {
			return nil, fmt.Errorf("no broker at localhost:9092: %w", adaptertest.ErrUnreachable)
		}
		results := adaptertest.Capture(t, target)
		if len(results) == 0 {
			t.Fatal("no results")
		}
		for _, r := range outcomes(t, results) {
			if r.Outcome != adaptertest.Skipped {
				t.Errorf("%s: outcome %s, want skipped", r.Check, r.Outcome)
			}
		}
	})
	t.Run("any other factory error fails", func(t *testing.T) {
		target := ownedTarget(nil)
		target.New = func(*testing.T) (any, error) { return nil, errors.New("bad config") }
		for _, r := range outcomes(t, adaptertest.Capture(t, target)) {
			if r.Outcome != adaptertest.Failed {
				t.Errorf("%s: outcome %s, want failed", r.Check, r.Outcome)
			}
		}
	})
	t.Run("a nil value fails", func(t *testing.T) {
		var typedNil *owned
		for _, r := range outcomes(t, adaptertest.Capture(t, ownedTarget(typedNil))) {
			if r.Outcome != adaptertest.Failed || !strings.Contains(r.Detail, "nil") {
				t.Errorf("%s: outcome %s (%q), want failed naming nil", r.Check, r.Outcome, r.Detail)
			}
		}
	})
	t.Run("a FailStart error other than ErrUnreachable fails AT-2", func(t *testing.T) {
		target := ownedTarget(starter{&owned{desc: desc("fake", adapter.CapStart, adapter.CapReady)}})
		target.FailStart = func(*testing.T) (any, error) { return nil, errors.New("bad config") }
		got := outcomes(t, adaptertest.Capture(t, target))
		requireOutcome(t, got, "AT-2", adaptertest.Failed, "bad config")
	})
}

// ---------------------------------------------------------------------------
// AT-1: a lying or unstable descriptor fails.
// ---------------------------------------------------------------------------

// Spec scenario "a lying descriptor fails".
func TestCapture_LyingDescriptorFailsAT1NamingCapStart(t *testing.T) {
	got := outcomes(t, adaptertest.Capture(t, ownedTarget(ownedDescribed{&owned{desc: desc("liar", adapter.CapStart)}})))
	requireOnlyFailure(t, got, "AT-1", string(adapter.CapStart))
}

func TestCapture_UndeclaredCapabilityFailsAT1(t *testing.T) {
	got := outcomes(t, adaptertest.Capture(t, ownedTarget(starter{&owned{desc: desc("shy", adapter.CapReady)}})))
	requireOutcome(t, got, "AT-1", adaptertest.Failed, string(adapter.CapStart))
}

func TestCapture_DeclaredReadyWithoutPingFailsAT1(t *testing.T) {
	got := outcomes(t, adaptertest.Capture(t, ownedTarget(ownedDescribed{&owned{desc: desc("liar", adapter.CapReady)}})))
	requireOnlyFailure(t, got, "AT-1", string(adapter.CapReady))
}

func TestCapture_WrongPortFailsAT1(t *testing.T) {
	target := ownedTarget(ownedDescribed{&owned{desc: desc("fake")}})
	target.Port = "publishing.StatePublisher"
	got := outcomes(t, adaptertest.Capture(t, target))
	requireOnlyFailure(t, got, "AT-1", "publishing.StatePublisher")
}

func TestCapture_UnstableDescriptorFailsAT1(t *testing.T) {
	got := outcomes(t, adaptertest.Capture(t, ownedTarget(ownedDescribed{&owned{desc: desc("fake"), unstable: true}})))
	requireOnlyFailure(t, got, "AT-1", "stable")
}

func TestCapture_EmptyNameFailsAT1(t *testing.T) {
	got := outcomes(t, adaptertest.Capture(t, ownedTarget(ownedDescribed{&owned{desc: desc("")}})))
	requireOnlyFailure(t, got, "AT-1", "Name")
}

// Spec scenario "a capability without a check fails".
func TestCapture_CapabilityWithoutCheckFailsAT1(t *testing.T) {
	got := outcomes(t, adaptertest.Capture(t, ownedTarget(flusher{ownedDescribed{&owned{desc: desc("fake", flush)}}})))
	requireOnlyFailure(t, got, "AT-1", "no check supplied")
}

// Target.Capabilities is checked in both directions: declared implies
// implemented, and implemented implies declared.
func TestCapture_TargetCapabilitiesAreCheckedBothWays(t *testing.T) {
	implementsFlush := func(v any) bool {
		_, ok := v.(interface{ Flush() })
		return ok
	}
	cases := []struct {
		name   string
		value  any
		passes bool
	}{
		{"declared and implemented", flusher{ownedDescribed{&owned{desc: desc("fake", flush)}}}, true},
		{"declared, not implemented", ownedDescribed{&owned{desc: desc("fake", flush)}}, false},
		{"implemented, not declared", flusher{ownedDescribed{&owned{desc: desc("fake")}}}, false},
		{"neither", ownedDescribed{&owned{desc: desc("fake")}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := ownedTarget(tc.value)
			target.Capabilities = map[adapter.Capability]func(any) bool{flush: implementsFlush}
			got := outcomes(t, adaptertest.Capture(t, target))
			if tc.passes {
				requireOutcome(t, got, "AT-1", adaptertest.Passed, "")
			} else {
				requireOnlyFailure(t, got, "AT-1", string(flush))
			}
		})
	}
}

func TestCapture_TargetCapabilitiesMayNotListSuiteCapabilities(t *testing.T) {
	target := ownedTarget(ownedDescribed{&owned{desc: desc("fake")}})
	target.Capabilities = map[adapter.Capability]func(any) bool{adapter.CapStart: func(any) bool { return false }}
	got := outcomes(t, adaptertest.Capture(t, target))
	requireOnlyFailure(t, got, "AT-1", string(adapter.CapStart))
}

// CapReady is implied by the store ports, so a store that implements Ping
// without declaring CapReady passes AT-1.
func TestCapture_ImpliedCapReadyIsNotRequiredForStores(t *testing.T) {
	got := outcomes(t, adaptertest.Capture(t, adaptertest.Target{
		Port: storePort, Ownership: adaptertest.Borrowed, New: newOf(&store{}),
	}))
	requireOutcome(t, got, "AT-1", adaptertest.Passed, "")
}

// ---------------------------------------------------------------------------
// AT-2, AT-3, AT-4, AT-5: broken lifecycles fail the matching check.
// ---------------------------------------------------------------------------

func TestCapture_NonIdempotentCloseFailsAT3(t *testing.T) {
	target := ownedTarget(nil)
	target.New = func(*testing.T) (any, error) {
		return ownedDescribed{&owned{desc: desc("fake"), closeErr: errors.New("use of closed network connection")}}, nil
	}
	got := outcomes(t, adaptertest.Capture(t, target))
	requireOnlyFailure(t, got, "AT-3/release twice", "use of closed network connection")
}

func TestCapture_CloseIgnoringTheDeadlineFailsAT4(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	stalled := &atomic.Bool{}
	target := ownedTarget(nil)
	target.Stall = func(*testing.T) { stalled.Store(true) }
	target.New = func(*testing.T) (any, error) {
		return lazyBlock{ownedDescribed{&owned{desc: desc("fake")}}, stalled, block}, nil
	}
	got := outcomes(t, adaptertest.Capture(t, target))
	requireOnlyFailure(t, got, "AT-4", "deadline")
}

// lazyBlock's Close ignores its context once the backend is stalled.
type lazyBlock struct {
	ownedDescribed
	stalled *atomic.Bool
	block   chan struct{}
}

func (l lazyBlock) Close(ctx context.Context) error {
	if l.stalled.Load() {
		<-l.block
		return nil
	}
	return l.ownedDescribed.Close(ctx)
}

func TestCapture_FailStartWhoseAcquireSucceedsFailsAT2(t *testing.T) {
	target := ownedTarget(starter{&owned{desc: desc("fake", adapter.CapStart, adapter.CapReady)}})
	target.FailStart = newOf(starter{&owned{desc: desc("fake", adapter.CapStart, adapter.CapReady)}})
	got := outcomes(t, adaptertest.Capture(t, target))
	requireOutcome(t, got, "AT-2", adaptertest.Failed, "succeeded")
}

func TestCapture_CloseAfterFailedStartFailsAT3(t *testing.T) {
	failing := errors.New("dial refused")
	target := ownedTarget(starter{&owned{desc: desc("fake", adapter.CapStart, adapter.CapReady)}})
	target.FailStart = func(*testing.T) (any, error) {
		return starter{&owned{desc: desc("fake", adapter.CapStart, adapter.CapReady), startErr: failing}}, nil
	}
	got := outcomes(t, adaptertest.Capture(t, target))
	requireOutcome(t, got, "AT-3/release after failed acquire", adaptertest.Passed, "")

	target.FailStart = func(*testing.T) (any, error) {
		return failedCloser{starter{&owned{desc: desc("fake", adapter.CapStart, adapter.CapReady), startErr: failing}}}, nil
	}
	got = outcomes(t, adaptertest.Capture(t, target))
	requireOnlyFailure(t, got, "AT-3/release after failed acquire", "never started")
}

// failedCloser's Close fails when Start never succeeded.
type failedCloser struct{ starter }

func (failedCloser) Close(context.Context) error { return errors.New("close: never started") }

func TestCapture_FailingPingFailsAT5(t *testing.T) {
	got := outcomes(t, adaptertest.Capture(t, adaptertest.Target{
		Port: storePort, Ownership: adaptertest.Borrowed, New: newOf(&store{pingErr: errors.New("not ready")}),
	}))
	requireOnlyFailure(t, got, "AT-5", "not ready")
}

// ---------------------------------------------------------------------------
// Target validation.
// ---------------------------------------------------------------------------

func TestCapture_InvalidTargetFails(t *testing.T) {
	cases := map[string]adaptertest.Target{
		"no port":              {Ownership: adaptertest.Owned, New: newOf(&owned{})},
		"no ownership":         {Port: publisherPort, New: newOf(&owned{})},
		"no factory":           {Port: publisherPort, Ownership: adaptertest.Owned},
		"owned, no Close":      {Port: publisherPort, Ownership: adaptertest.Owned, New: newOf(struct{}{})},
		"borrowed, no Connect": {Port: storePort, Ownership: adaptertest.Borrowed, New: newOf(&owned{})},
	}
	for name, target := range cases {
		t.Run(name, func(t *testing.T) {
			results := adaptertest.Capture(t, target)
			if len(results) == 0 {
				t.Fatal("no results")
			}
			for _, r := range outcomes(t, results) {
				if r.Outcome != adaptertest.Failed {
					t.Errorf("%s: outcome %s, want failed", r.Check, r.Outcome)
				}
			}
		})
	}
}
