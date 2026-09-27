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

package adaptertest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/pablogore/ego/v4/port/adapter"
)

// ErrUnreachable is the only error that makes a check skip. A Target
// factory returns it, usually wrapped, when the adapter's backend is not
// reachable from this test run:
//
//	return nil, fmt.Errorf("no broker at %s: %w", addr, adaptertest.ErrUnreachable)
//
// Its value also has an Unreachable() bool method that reports true, so a
// suite that cannot import this package (port/publishing/publishingtest)
// recognizes the same error.
var ErrUnreachable error = unreachableError{}

type unreachableError struct{}

func (unreachableError) Error() string { return "adapter backend unreachable" }

// Unreachable marks the error for suites that cannot import adaptertest.
func (unreachableError) Unreachable() bool { return true }

// Ownership says how the suite acquires and releases an adapter.
type Ownership int

const (
	// Owned adapters (publishers) are acquired by Start, when
	// adapter.StarterOf finds one, and released by Close. Without Start
	// the constructor is the acquire.
	Owned Ownership = iota + 1
	// Borrowed adapters (stores) are acquired by Connect and released by
	// Disconnect.
	Borrowed
)

// String returns "owned", "borrowed", or a placeholder for an invalid value.
func (o Ownership) String() string {
	switch o {
	case Owned:
		return "owned"
	case Borrowed:
		return "borrowed"
	default:
		return fmt.Sprintf("Ownership(%d)", int(o))
	}
}

// Target is what the suite needs to know about the adapter under test.
type Target struct {
	// Port is the slot port under test; it must be in Descriptor.Ports.
	Port adapter.Port
	// Ownership selects acquire and release (see Owned and Borrowed).
	Ownership Ownership
	// New returns a fresh value for one check. Wrap ErrUnreachable to skip.
	New func(t *testing.T) (any, error)
	// FailStart optionally returns a value whose separate acquire fails
	// (AT-2 and the failed-acquire case of AT-3). It only applies to an
	// adapter whose acquire is separate from its constructor.
	FailStart func(t *testing.T) (any, error)
	// Capabilities maps each port-specific capability to its implements
	// check, built from the owning contract package's accessor, for
	// example {tenancy.CapFixedTenant: func(v any) bool { _, ok :=
	// tenancy.AsFixedTenantResolver(v.(tenancy.TenantResolver)); return ok }}.
	// It must not list adapter.CapStart or adapter.CapReady, which the
	// suite checks itself.
	Capabilities map[adapter.Capability]func(v any) bool
	// Stall optionally makes the backend stop answering (AT-4). AT-4 runs
	// last, so the backend may stay stalled afterwards.
	Stall func(t *testing.T)
}

// Outcome is the result of one check.
type Outcome int

const (
	// Passed means the check ran and found nothing wrong.
	Passed Outcome = iota + 1
	// Failed means the check ran and found a violation, or could not run
	// because of a broken Target.
	Failed
	// Skipped means a factory returned ErrUnreachable.
	Skipped
	// NotExercised means the suite could not run the check: a hook is
	// missing or the adapter cannot be driven that way. It is never a pass.
	NotExercised
)

// String returns the outcome in words, as the summary prints it.
func (o Outcome) String() string {
	switch o {
	case Passed:
		return "passed"
	case Failed:
		return "failed"
	case Skipped:
		return "skipped"
	case NotExercised:
		return "not exercised"
	default:
		return fmt.Sprintf("Outcome(%d)", int(o))
	}
}

// Result reports one check.
type Result struct {
	// Check is the check's name, for example "AT-3/release twice".
	Check string
	// Outcome is what happened.
	Outcome Outcome
	// Detail says why a check was not exercised or skipped and, from
	// Capture, the failure messages.
	Detail string
}

// Timeouts. opTimeout bounds every ordinary adapter call; an adapter that
// does not return by opTimeout+grace fails the check (its goroutine is left
// behind). AT-4 gives release stallDeadline and the same grace.
const (
	opTimeout     = 5 * time.Second
	stallDeadline = 200 * time.Millisecond
	grace         = time.Second
)

// readyPorts are the ports whose interface already has Ping, so CapReady
// is implied there and never declared (design §D3). They are string
// literals because this package imports only the standard library and
// port/adapter; a test pins them to the contract constants.
var readyPorts = map[adapter.Port]bool{
	"persistence.EventsStore":   true,
	"persistence.StateStore":    true,
	"persistence.SnapshotStore": true,
	"offsetstore.OffsetStore":   true,
}

func impliesReady(p adapter.Port) bool { return readyPorts[p] }

// closer, connector: the release and acquire methods the suite finds
// structurally. Start and Ping are found only through adapter.StarterOf and
// adapter.PingerOf.
type closer interface {
	Close(ctx context.Context) error
}

type connector interface {
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
}

// tb is the part of testing.TB the checks use. *testing.T implements it
// for Run; recorder implements it for Capture.
type tb interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
	Skipf(format string, args ...any)
	Logf(format string, args ...any)
}

// probe is what the suite learns from one value built by New before the
// checks run.
type probe struct {
	value    any
	desc     adapter.Descriptor
	declared bool
	starter  bool
	pinger   bool
}

type check struct {
	name string
	// notExercised returns why the check cannot run, or "".
	notExercised func(s *suite, p probe) string
	// run performs the check. t reports; ft is the *testing.T handed to
	// the Target factories.
	run func(s *suite, p probe, t tb, ft *testing.T)
}

type suite struct {
	target Target
}

// Run runs every check against target, each exercised check as its own
// subtest of t, and returns the results. Checks that are not exercised get
// no subtest; Run logs them and lists every outcome in a closing summary.
// When New reports ErrUnreachable before the first check, Run skips t.
func Run(t *testing.T, target Target) []Result {
	t.Helper()
	s := &suite{target: target}
	if msg := s.validate(); msg != "" {
		t.Fatalf("adaptertest: invalid Target: %s", msg)
	}
	p := s.probe(t, t)

	results := make([]Result, 0, len(checks))
	for i, c := range checks {
		if i == 1 {
			// The probe served AT-1; release it before the other checks
			// run, so a check that stalls the backend cannot block it.
			s.releaseQuietly(p.value)
		}
		if reason := c.notExercised(s, p); reason != "" {
			t.Logf("%s: not exercised: %s", c.name, reason)
			results = append(results, Result{Check: c.name, Outcome: NotExercised, Detail: reason})
			continue
		}
		var sub *testing.T
		t.Run(c.name, func(st *testing.T) {
			sub = st
			c.run(s, p, st, st)
		})
		results = append(results, Result{Check: c.name, Outcome: outcomeOf(sub)})
	}
	logSummary(t, target, results)
	return results
}

func outcomeOf(t *testing.T) Outcome {
	switch {
	case t == nil:
		return Failed
	case t.Failed():
		return Failed
	case t.Skipped():
		return Skipped
	default:
		return Passed
	}
}

func logSummary(t *testing.T, target Target, results []Result) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "adaptertest summary for %s adapter on port %q:", target.Ownership, target.Port)
	for _, r := range results {
		fmt.Fprintf(&b, "\n  %-34s %s", r.Check, r.Outcome)
		if r.Outcome == NotExercised {
			fmt.Fprintf(&b, ": %s", r.Detail)
		}
	}
	t.Log(b.String())
}

// Capture runs the same checks as Run without failing t, and returns their
// results with the failure messages in Detail. It exists so a suite's own
// tests can assert that a check fails against a deliberately broken
// adapter.
//
// The Target factories (New, FailStart) and Stall receive t itself, but
// Capture calls them on a goroutine of its own, not on t's test goroutine.
// They may use t.Helper, t.Log, t.Cleanup and t.TempDir, and may report
// with t.Error, but must not call t.Fatal, t.FailNow, t.Skip or
// t.SkipNow: the testing package allows those only on the test goroutine.
// A factory reports failure by returning an error instead.
func Capture(t *testing.T, target Target) []Result {
	s := &suite{target: target}
	if msg := s.validate(); msg != "" {
		return allResults(Failed, "invalid Target: "+msg)
	}
	var p probe
	outcome, detail := record(func(r tb) { p = s.probe(r, t) })
	if outcome != Passed {
		return allResults(outcome, detail)
	}

	results := make([]Result, 0, len(checks))
	for i, c := range checks {
		if i == 1 {
			s.releaseQuietly(p.value)
		}
		if reason := c.notExercised(s, p); reason != "" {
			results = append(results, Result{Check: c.name, Outcome: NotExercised, Detail: reason})
			continue
		}
		outcome, detail := record(func(r tb) { c.run(s, p, r, t) })
		results = append(results, Result{Check: c.name, Outcome: outcome, Detail: detail})
	}
	return results
}

func allResults(outcome Outcome, detail string) []Result {
	out := make([]Result, 0, len(checks))
	for _, c := range checks {
		out = append(out, Result{Check: c.name, Outcome: outcome, Detail: detail})
	}
	return out
}

// recorder implements tb by recording instead of reporting. Fatalf and
// Skipf end the calling goroutine with runtime.Goexit, as *testing.T
// does, so record runs fn in a goroutine of its own.
type recorder struct {
	failed   bool
	skipped  bool
	messages []string
}

func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.failed = true
	r.messages = append(r.messages, fmt.Sprintf(format, args...))
}

func (r *recorder) Fatalf(format string, args ...any) {
	r.Errorf(format, args...)
	runtime.Goexit()
}

func (r *recorder) Skipf(format string, args ...any) {
	r.skipped = true
	r.messages = append(r.messages, fmt.Sprintf(format, args...))
	runtime.Goexit()
}

func (r *recorder) Logf(string, ...any) {}

func record(fn func(r tb)) (Outcome, string) {
	r := &recorder{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(r)
	}()
	<-done
	detail := strings.Join(r.messages, "; ")
	switch {
	case r.failed:
		return Failed, detail
	case r.skipped:
		return Skipped, detail
	default:
		return Passed, detail
	}
}

func (s *suite) validate() string {
	switch {
	case s.target.Port == "":
		return "Port is empty"
	case s.target.Ownership != Owned && s.target.Ownership != Borrowed:
		return fmt.Sprintf("Ownership is %s, want Owned or Borrowed", s.target.Ownership)
	case s.target.New == nil:
		return "New is nil"
	}
	return ""
}

// newValue calls factory and applies the skip rule: only ErrUnreachable
// skips; any other error, or a nil or typed-nil value, fails.
func (s *suite) newValue(t tb, ft *testing.T, name string, factory func(*testing.T) (any, error)) any {
	t.Helper()
	v, err := factory(ft)
	if errors.Is(err, ErrUnreachable) {
		t.Skipf("%s: %v", name, err)
	}
	if err != nil {
		t.Fatalf("%s returned an error that does not match adaptertest.ErrUnreachable, so the check fails instead of skipping: %v", name, err)
	}
	if isNil(v) {
		t.Fatalf("%s returned a nil value (%T); a nil or typed-nil value is undeclared and cannot be checked", name, v)
	}
	switch s.target.Ownership {
	case Owned:
		if _, ok := v.(closer); !ok {
			t.Fatalf("%s returned %T, which has no Close(context.Context) error; an Owned adapter is released by Close", name, v)
		}
	case Borrowed:
		if _, ok := v.(connector); !ok {
			t.Fatalf("%s returned %T, which has no Connect and Disconnect; a Borrowed adapter is acquired by Connect and released by Disconnect", name, v)
		}
	}
	return v
}

func (s *suite) probe(t tb, ft *testing.T) probe {
	t.Helper()
	v := s.newValue(t, ft, "Target.New", s.target.New)
	p := probe{value: v}
	p.desc, p.declared = adapter.Describe(v)
	_, p.starter = adapter.StarterOf(v)
	_, p.pinger = adapter.PingerOf(v)
	return p
}

// separateAcquire reports whether the adapter's acquire is separate from
// its constructor.
func (s *suite) separateAcquire(p probe) bool {
	return s.target.Ownership == Borrowed || p.starter
}

func (s *suite) acquire(ctx context.Context, v any) error {
	if s.target.Ownership == Borrowed {
		return v.(connector).Connect(ctx)
	}
	if st, ok := adapter.StarterOf(v); ok {
		return st.Start(ctx)
	}
	return nil // the constructor acquired
}

func (s *suite) release(ctx context.Context, v any) error {
	if s.target.Ownership == Borrowed {
		return v.(connector).Disconnect(ctx)
	}
	return v.(closer).Close(ctx)
}

func (s *suite) releaseQuietly(v any) {
	_, _ = bounded(opTimeout, func(ctx context.Context) error { return s.release(ctx, v) })
}

// bounded calls fn with a context that expires after deadline and waits
// at most deadline+grace for it. returned is false when fn did not return
// in time; its goroutine is then left behind.
func bounded(deadline time.Duration, fn func(ctx context.Context) error) (returned bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- fn(ctx) }()
	timer := time.NewTimer(deadline + grace)
	defer timer.Stop()
	select {
	case err := <-done:
		return true, err
	case <-timer.C:
		return false, nil
	}
}

// mustAcquire acquires v and fails the check when that does not succeed.
func (s *suite) mustAcquire(t tb, v any) {
	t.Helper()
	ok, err := bounded(opTimeout, func(ctx context.Context) error { return s.acquire(ctx, v) })
	if !ok {
		t.Fatalf("acquire did not return within %v", opTimeout+grace)
	}
	if err != nil {
		t.Fatalf("acquire of a Target.New value failed: %v", err)
	}
}

// expectRelease releases v and reports an error when release fails or
// does not return.
func (s *suite) expectRelease(t tb, v any, what string) {
	t.Helper()
	ok, err := bounded(opTimeout, func(ctx context.Context) error { return s.release(ctx, v) })
	switch {
	case !ok:
		t.Errorf("%s did not return within %v (L2)", what, opTimeout+grace)
	case err != nil:
		t.Errorf("%s = %v, want nil (L2)", what, err)
	}
}

// failedAcquireValue builds a FailStart value and acquires it, which must
// fail.
func (s *suite) failedAcquireValue(t tb, ft *testing.T) any {
	t.Helper()
	v := s.newValue(t, ft, "Target.FailStart", s.target.FailStart)
	if s.target.Ownership == Owned {
		if _, ok := adapter.StarterOf(v); !ok {
			t.Fatalf("Target.FailStart returned %T, which does not implement adapter.Starter; the hook must return a value whose Start fails", v)
		}
	}
	ok, err := bounded(opTimeout, func(ctx context.Context) error { return s.acquire(ctx, v) })
	switch {
	case !ok:
		t.Fatalf("acquire of the Target.FailStart value did not return within %v", opTimeout+grace)
	case err == nil:
		s.releaseQuietly(v)
		t.Fatalf("acquire of the Target.FailStart value succeeded; the hook must return a value whose acquire fails")
	}
	return v
}

func failedAcquireNotExercised(s *suite, p probe) string {
	if !s.separateAcquire(p) {
		return "acquire happens in the constructor (the adapter has no Start), so no value is left after a failed acquire"
	}
	if s.target.FailStart == nil {
		return "no hook: Target.FailStart is not set"
	}
	return ""
}

func never(*suite, probe) string { return "" }

// checks lists every check in the order they run. AT-4 runs last because
// Stall may leave the backend stalled.
var checks = []check{
	{name: "AT-1", notExercised: func(_ *suite, p probe) string {
		if !p.declared {
			return "the value is undeclared (it does not implement adapter.Describer), so there is no descriptor to check"
		}
		return ""
	}, run: (*suite).checkDescriptor},
	{name: "AT-2", notExercised: failedAcquireNotExercised, run: func(s *suite, _ probe, t tb, ft *testing.T) {
		v := s.failedAcquireValue(t, ft)
		s.releaseQuietly(v)
	}},
	{name: "AT-3/release twice", notExercised: never, run: func(s *suite, _ probe, t tb, ft *testing.T) {
		v := s.newValue(t, ft, "Target.New", s.target.New)
		s.mustAcquire(t, v)
		s.expectRelease(t, v, "first release")
		s.expectRelease(t, v, "second release")
	}},
	{name: "AT-3/release without acquire", notExercised: never, run: func(s *suite, _ probe, t tb, ft *testing.T) {
		v := s.newValue(t, ft, "Target.New", s.target.New)
		s.expectRelease(t, v, "release without acquire")
	}},
	{name: "AT-3/release after failed acquire", notExercised: failedAcquireNotExercised, run: func(s *suite, _ probe, t tb, ft *testing.T) {
		v := s.failedAcquireValue(t, ft)
		s.expectRelease(t, v, "release after a failed acquire")
		s.expectRelease(t, v, "second release after a failed acquire")
	}},
	{name: "AT-5", notExercised: func(_ *suite, p probe) string {
		if !p.pinger {
			return "the adapter does not implement adapter.Pinger"
		}
		return ""
	}, run: func(s *suite, _ probe, t tb, ft *testing.T) {
		v := s.newValue(t, ft, "Target.New", s.target.New)
		s.mustAcquire(t, v)
		defer s.releaseQuietly(v)
		pinger, _ := adapter.PingerOf(v)
		ok, err := bounded(opTimeout, pinger.Ping)
		switch {
		case !ok:
			t.Errorf("Ping after acquire did not return within %v (L4)", opTimeout+grace)
		case err != nil:
			t.Errorf("Ping after acquire = %v, want nil for a reachable adapter (L4)", err)
		}
	}},
	{name: "AT-4", notExercised: func(s *suite, _ probe) string {
		if s.target.Stall == nil {
			return "no hook: Target.Stall is not set"
		}
		return ""
	}, run: func(s *suite, _ probe, t tb, ft *testing.T) {
		v := s.newValue(t, ft, "Target.New", s.target.New)
		s.mustAcquire(t, v)
		s.target.Stall(ft)
		start := time.Now()
		ok, err := bounded(stallDeadline, func(ctx context.Context) error { return s.release(ctx, v) })
		if !ok {
			t.Errorf("release ignored its deadline: it did not return within %v of a %v deadline while the backend was stalled (L3)", grace, stallDeadline)
			return
		}
		t.Logf("release under a stalled backend returned after %v: %v", time.Since(start).Round(time.Millisecond), err)
	}},
}

// checkDescriptor is AT-1.
func (s *suite) checkDescriptor(p probe, t tb, _ *testing.T) {
	t.Helper()
	d := p.desc
	if again, _ := adapter.Describe(p.value); !reflect.DeepEqual(d, again) {
		t.Errorf("descriptor is not stable across calls: first %+v, then %+v", d, again)
	}
	if d.Name == "" {
		t.Errorf("descriptor has an empty Name; Ports and Name together identify the adapter type")
	}
	if !d.Serves(s.target.Port) {
		t.Errorf("descriptor Ports %v do not include the target port %q", d.Ports, s.target.Port)
	}

	agree(t, d, adapter.CapStart, p.starter, "adapter.Starter")
	if !impliesReady(s.target.Port) {
		agree(t, d, adapter.CapReady, p.pinger, "adapter.Pinger")
	}

	for c, implements := range s.target.Capabilities {
		switch {
		case c == adapter.CapStart || c == adapter.CapReady:
			t.Errorf("Target.Capabilities lists %q; adaptertest checks it itself, remove it", c)
		case implements == nil:
			t.Errorf("Target.Capabilities has a nil check for %q", c)
		default:
			agree(t, d, c, implements(p.value), "the Target.Capabilities check")
		}
	}
	for _, c := range d.Capabilities {
		if c == adapter.CapStart || c == adapter.CapReady {
			continue
		}
		if _, ok := s.target.Capabilities[c]; !ok {
			t.Errorf("descriptor declares %q but no check supplied in Target.Capabilities", c)
		}
	}
}

// agree reports a capability whose declaration and implementation differ.
func agree(t tb, d adapter.Descriptor, c adapter.Capability, implemented bool, by string) {
	t.Helper()
	switch declared := d.Declares(c); {
	case declared && !implemented:
		t.Errorf("descriptor declares %q but the value does not implement it (checked through %s)", c, by)
	case implemented && !declared:
		t.Errorf("the value implements %q (checked through %s) but the descriptor does not declare it", c, by)
	}
}

// isNil reports whether v is nil or a nil value of a kind that can be nil,
// the same rule the port/adapter accessors apply.
func isNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.UnsafePointer:
		return rv.IsNil()
	default:
		return false
	}
}
