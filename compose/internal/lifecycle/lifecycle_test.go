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

package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pablogore/ego/v4/compose"
)

// recorder is an ordered fake: every step it builds appends "start X" or
// "stop X" to one shared log, and fails when told to, so a test can assert
// the exact call order the sequence produced.
type recorder struct {
	mu        sync.Mutex
	log       []string
	failStart map[string]error
	failStop  map[string]error
	failRel   error
}

func newRecorder() *recorder {
	return &recorder{failStart: map[string]error{}, failStop: map[string]error{}}
}

func (r *recorder) record(entry string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.log = append(r.log, entry)
}

func (r *recorder) calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.log)
}

// callsFrom returns the calls recorded after the first n, or none when
// fewer than n were recorded.
func (r *recorder) callsFrom(n int) []string {
	calls := r.calls()
	if n >= len(calls) {
		return nil
	}
	return calls[n:]
}

func (r *recorder) step(name string) Step {
	return Step{
		Name: name,
		Start: func(context.Context) error {
			r.record("start " + name)
			return r.failStart[name]
		},
		Stop: func(context.Context) error {
			r.record("stop " + name)
			return r.failStop[name]
		},
	}
}

func (r *recorder) release(context.Context) error {
	r.record("release")
	return r.failRel
}

func (r *recorder) config(names ...string) Config {
	cfg := Config{Release: r.release}
	for _, name := range names {
		cfg.Steps = append(cfg.Steps, r.step(name))
	}
	return cfg
}

func mustNew(t *testing.T, cfg Config) *Sequence {
	t.Helper()
	seq, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return seq
}

var names = []string{"probe", "runtime", "engine", "publishers", "projections"}

func TestNew_RejectsIncompleteSteps(t *testing.T) {
	noop := func(context.Context) error { return nil }
	for _, tc := range []struct {
		desc string
		step Step
	}{
		{"missing name", Step{Start: noop}},
		{"missing start", Step{Name: "runtime"}},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			if _, err := New(Config{Steps: []Step{tc.step}}); err == nil {
				t.Fatal("New accepted an incomplete step")
			}
		})
	}
	if _, err := New(Config{Steps: []Step{{Name: "runtime", Start: noop}}}); err != nil {
		t.Fatalf("New rejected a step without Stop, which is optional: %v", err)
	}
}

func TestNew_RejectsNegativeShutdownTimeout(t *testing.T) {
	noop := func(context.Context) error { return nil }
	steps := []Step{{Name: "runtime", Start: noop}}

	_, err := New(Config{Steps: steps, ShutdownTimeout: -time.Second})
	if err == nil {
		t.Fatal("New accepted a negative shutdown timeout; only zero means the default")
	}
	if !strings.Contains(err.Error(), "ShutdownTimeout") {
		t.Errorf("error %q does not name ShutdownTimeout", err)
	}
	if _, err := New(Config{Steps: steps}); err != nil {
		t.Fatalf("New rejected a zero shutdown timeout, which means the default: %v", err)
	}
}

func TestStart_PanickingStepRollsBackReleasesAndFails(t *testing.T) {
	rec := newRecorder()
	cfg := rec.config(names[:3]...)
	cfg.Steps[1].Start = func(context.Context) error {
		rec.record("start runtime")
		panic("runtime exploded")
	}
	seq := mustNew(t, cfg)

	recovered := func() (r any) {
		defer func() { r = recover() }()
		_ = seq.Start(context.Background())
		return nil
	}()

	if recovered != "runtime exploded" {
		t.Fatalf("recovered %v, want the step's panic re-raised unchanged", recovered)
	}
	want := []string{"start probe", "start runtime", "stop probe", "release"}
	if got := rec.calls(); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v (rollback and Release before the panic propagates)", got, want)
	}
	if got := seq.State(); got != StateFailed {
		t.Fatalf("State = %v, want StateFailed", got)
	}
	if err := seq.Stop(context.Background()); err != nil {
		t.Fatalf("Stop after a panicking Start = %v, want nil", err)
	}
	if got := rec.callsFrom(len(want)); len(got) != 0 {
		t.Fatalf("Stop after a panicking Start made calls %v", got)
	}
}

func TestStart_RunsStepsInOrder(t *testing.T) {
	rec := newRecorder()
	seq := mustNew(t, rec.config(names...))

	if err := seq.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	want := []string{"start probe", "start runtime", "start engine", "start publishers", "start projections"}
	if got := rec.calls(); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	if got := seq.State(); got != StateRunning {
		t.Fatalf("State = %v, want StateRunning", got)
	}
}

func TestStart_FailureAtEachStepRollsBackInReverseThenReleases(t *testing.T) {
	for k, failing := range names {
		t.Run(failing, func(t *testing.T) {
			rec := newRecorder()
			cause := fmt.Errorf("%s broke", failing)
			rec.failStart[failing] = cause
			seq := mustNew(t, rec.config(names...))

			err := seq.Start(context.Background())

			var want []string
			for _, name := range names[:k+1] {
				want = append(want, "start "+name)
			}
			for i := k - 1; i >= 0; i-- {
				want = append(want, "stop "+names[i])
			}
			want = append(want, "release")
			if got := rec.calls(); !slices.Equal(got, want) {
				t.Fatalf("calls = %v, want %v (later steps never start, the failing step is not stopped)", got, want)
			}

			var se *compose.StartError
			if !errors.As(err, &se) {
				t.Fatalf("Start error %v is not a *compose.StartError", err)
			}
			if se.Step != failing {
				t.Errorf("StartError.Step = %q, want %q", se.Step, failing)
			}
			if !errors.Is(se.Err, cause) || !errors.Is(err, cause) {
				t.Errorf("StartError does not carry the step error: %v", err)
			}
			if se.Rollback != nil {
				t.Errorf("StartError.Rollback = %v, want nil for a clean rollback", se.Rollback)
			}
			if got := seq.State(); got != StateFailed {
				t.Errorf("State = %v, want StateFailed", got)
			}
		})
	}
}

func TestStart_RollbackAttemptsEveryUndoAndReportsEveryError(t *testing.T) {
	rec := newRecorder()
	cause := errors.New("publisher attach failed")
	stopEngine := errors.New("engine did not stop")
	stopProbe := errors.New("probe undo failed")
	releaseErr := errors.New("publisher close failed")
	rec.failStart["publishers"] = cause
	rec.failStop["engine"] = stopEngine
	rec.failStop["probe"] = stopProbe
	rec.failRel = releaseErr
	seq := mustNew(t, rec.config(names...))

	err := seq.Start(context.Background())

	want := []string{
		"start probe", "start runtime", "start engine", "start publishers",
		"stop engine", "stop runtime", "stop probe", "release",
	}
	if got := rec.calls(); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	var se *compose.StartError
	if !errors.As(err, &se) {
		t.Fatalf("Start error %v is not a *compose.StartError", err)
	}
	if se.Step != "publishers" || !errors.Is(se.Err, cause) {
		t.Errorf("StartError = {Step: %q, Err: %v}, want the publishers step and its cause", se.Step, se.Err)
	}
	for _, rollbackErr := range []error{stopEngine, stopProbe, releaseErr} {
		if !errors.Is(se.Rollback, rollbackErr) {
			t.Errorf("StartError.Rollback = %v, missing %v", se.Rollback, rollbackErr)
		}
		if !errors.Is(err, rollbackErr) {
			t.Errorf("errors.Is(StartError, %v) = false through Unwrap", rollbackErr)
		}
	}
	if errors.Is(se.Err, stopEngine) {
		t.Error("StartError.Err must hold only the step error, not rollback errors")
	}
	if msg := se.Rollback.Error(); !strings.Contains(msg, "engine") || !strings.Contains(msg, "probe") {
		t.Errorf("rollback error %q does not name the steps whose undo failed", msg)
	}
}

func TestStart_StepsWithoutStopAreSkippedOnRollback(t *testing.T) {
	rec := newRecorder()
	cfg := rec.config(names[:3]...)
	cfg.Steps[1].Stop = nil
	rec.failStart["engine"] = errors.New("boom")
	seq := mustNew(t, cfg)

	if err := seq.Start(context.Background()); err == nil {
		t.Fatal("Start succeeded, want the engine failure")
	}
	want := []string{"start probe", "start runtime", "start engine", "stop probe", "release"}
	if got := rec.calls(); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
}

func TestStop_UndoesEveryStepInReverseOrder(t *testing.T) {
	rec := newRecorder()
	seq := mustNew(t, rec.config(names...))
	if err := seq.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := seq.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	want := []string{"stop projections", "stop publishers", "stop engine", "stop runtime", "stop probe"}
	if got := rec.callsFrom(len(names)); !slices.Equal(got, want) {
		t.Fatalf("stop calls = %v, want %v (Release runs only for resources no step owns)", got, want)
	}
	if got := seq.State(); got != StateStopped {
		t.Fatalf("State = %v, want StateStopped", got)
	}
}

func TestStop_FailureAtEachStepStillRunsTheRest(t *testing.T) {
	for _, failing := range names {
		t.Run(failing, func(t *testing.T) {
			rec := newRecorder()
			cause := fmt.Errorf("%s did not stop", failing)
			rec.failStop[failing] = cause
			seq := mustNew(t, rec.config(names...))
			if err := seq.Start(context.Background()); err != nil {
				t.Fatalf("Start: %v", err)
			}

			err := seq.Stop(context.Background())

			want := []string{"stop projections", "stop publishers", "stop engine", "stop runtime", "stop probe"}
			if got := rec.callsFrom(len(names)); !slices.Equal(got, want) {
				t.Fatalf("stop calls = %v, want every step attempted: %v", got, want)
			}
			if !errors.Is(err, cause) {
				t.Fatalf("Stop error = %v, want it to wrap %v", err, cause)
			}
			if !strings.Contains(err.Error(), failing) {
				t.Errorf("Stop error %q does not name step %q", err, failing)
			}
			if got := seq.State(); got != StateStopped {
				t.Errorf("State = %v, want StateStopped even after a failed Stop", got)
			}
		})
	}
}

func TestStop_JoinsEveryError(t *testing.T) {
	rec := newRecorder()
	var causes []error
	for _, name := range names {
		cause := fmt.Errorf("%s did not stop", name)
		rec.failStop[name] = cause
		causes = append(causes, cause)
	}
	seq := mustNew(t, rec.config(names...))
	if err := seq.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	err := seq.Stop(context.Background())
	for _, cause := range causes {
		if !errors.Is(err, cause) {
			t.Errorf("Stop error %v is missing %v", err, cause)
		}
	}
}

func TestStop_IsIdempotent(t *testing.T) {
	rec := newRecorder()
	rec.failStop["engine"] = errors.New("engine did not stop")
	seq := mustNew(t, rec.config(names...))
	if err := seq.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := seq.Stop(context.Background()); err == nil {
		t.Fatal("first Stop succeeded, want the engine error")
	}
	before := len(rec.calls())

	if err := seq.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop = %v, want nil", err)
	}
	if got := rec.callsFrom(before); len(got) != 0 {
		t.Fatalf("second Stop made calls %v, want none", got)
	}
}

func TestStop_NeverStartedOnlyReleases(t *testing.T) {
	rec := newRecorder()
	releaseErr := errors.New("publisher close failed")
	rec.failRel = releaseErr
	seq := mustNew(t, rec.config(names...))

	err := seq.Stop(context.Background())

	if got, want := rec.calls(), []string{"release"}; !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	if !errors.Is(err, releaseErr) {
		t.Fatalf("Stop = %v, want the release error", err)
	}
	if got := seq.State(); got != StateStopped {
		t.Fatalf("State = %v, want StateStopped", got)
	}
	if err := seq.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop = %v, want nil", err)
	}
	if got := len(rec.calls()); got != 1 {
		t.Fatalf("second Stop released again: %v", rec.calls())
	}
}

func TestStop_AfterFailedStartIsNoOp(t *testing.T) {
	rec := newRecorder()
	rec.failStart["engine"] = errors.New("boom")
	seq := mustNew(t, rec.config(names...))
	if err := seq.Start(context.Background()); err == nil {
		t.Fatal("Start succeeded, want the engine failure")
	}
	before := len(rec.calls())

	if err := seq.Stop(context.Background()); err != nil {
		t.Fatalf("Stop after a failed Start = %v, want nil", err)
	}
	if got := rec.callsFrom(before); len(got) != 0 {
		t.Fatalf("Stop after a failed Start made calls %v; rollback already released everything", got)
	}
	if got := seq.State(); got != StateFailed {
		t.Fatalf("State = %v, want StateFailed to stay terminal", got)
	}
}

func TestStart_IsSingleUse(t *testing.T) {
	cases := []struct {
		desc    string
		prepare func(t *testing.T, seq *Sequence, rec *recorder)
		state   State
	}{
		{"after a successful Start", func(t *testing.T, seq *Sequence, _ *recorder) {
			if err := seq.Start(context.Background()); err != nil {
				t.Fatalf("Start: %v", err)
			}
		}, StateRunning},
		{"after Stop", func(t *testing.T, seq *Sequence, _ *recorder) {
			if err := seq.Start(context.Background()); err != nil {
				t.Fatalf("Start: %v", err)
			}
			if err := seq.Stop(context.Background()); err != nil {
				t.Fatalf("Stop: %v", err)
			}
		}, StateStopped},
		{"after Stop on a never-started sequence", func(t *testing.T, seq *Sequence, _ *recorder) {
			if err := seq.Stop(context.Background()); err != nil {
				t.Fatalf("Stop: %v", err)
			}
		}, StateStopped},
		{"after a failed Start", func(t *testing.T, seq *Sequence, rec *recorder) {
			rec.failStart["runtime"] = errors.New("boom")
			if err := seq.Start(context.Background()); err == nil {
				t.Fatal("Start succeeded, want the runtime failure")
			}
			delete(rec.failStart, "runtime")
		}, StateFailed},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			rec := newRecorder()
			seq := mustNew(t, rec.config(names...))
			tc.prepare(t, seq, rec)
			before := len(rec.calls())

			err := seq.Start(context.Background())

			if !errors.Is(err, ErrNotStartable) {
				t.Fatalf("Start = %v, want ErrNotStartable", err)
			}
			if got := rec.callsFrom(before); len(got) != 0 {
				t.Fatalf("rejected Start made calls %v", got)
			}
			if got := seq.State(); got != tc.state {
				t.Fatalf("State = %v, want %v unchanged", got, tc.state)
			}
		})
	}
}

func TestState_TransitionsAreVisibleInsideSteps(t *testing.T) {
	var seq *Sequence
	var during []State
	observe := func(context.Context) error {
		during = append(during, seq.State())
		return nil
	}
	seq = mustNew(t, Config{Steps: []Step{{Name: "only", Start: observe, Stop: observe}}})
	if got := seq.State(); got != StateNew {
		t.Fatalf("initial State = %v, want StateNew", got)
	}
	if err := seq.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := seq.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if want := []State{StateStarting, StateStopping}; !slices.Equal(during, want) {
		t.Fatalf("states seen inside steps = %v, want %v", during, want)
	}
}

type ctxKey struct{}

// cleanupProbe records the context a cleanup function received.
type cleanupProbe struct {
	err      error
	value    any
	deadline time.Time
	hasDL    bool
	calls    int
}

func (p *cleanupProbe) observe(ctx context.Context) error {
	p.calls++
	p.err = ctx.Err()
	p.value = ctx.Value(ctxKey{})
	p.deadline, p.hasDL = ctx.Deadline()
	return nil
}

func (p *cleanupProbe) check(t *testing.T, bound time.Duration, before time.Time) {
	t.Helper()
	if p.calls == 0 {
		t.Fatal("cleanup function was never called")
	}
	if p.err != nil {
		t.Errorf("cleanup context was cancelled (%v); it must not inherit the caller's cancellation", p.err)
	}
	if p.value != "kept" {
		t.Errorf("cleanup context value = %v, want the caller's value kept", p.value)
	}
	if !p.hasDL {
		t.Fatal("cleanup context has no deadline; the shutdown timeout must bound it")
	}
	// The deadline is set from time.Now() inside the sequence, after
	// before was read, so it lies in (before, before+bound+slack]. The
	// slack covers only the time between reading before and calling
	// Start/Stop; nothing here waits on it.
	if !p.deadline.After(before) || p.deadline.After(before.Add(bound+time.Minute)) {
		t.Errorf("cleanup deadline %v not within %v of %v", p.deadline, bound, before)
	}
	if bound < time.Minute && !p.deadline.Before(before.Add(time.Minute)) {
		t.Errorf("cleanup deadline %v ignores the configured timeout %v", p.deadline, bound)
	}
}

func cancelledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "kept"))
	cancel()
	return ctx
}

func TestCleanup_RunsUnderWithoutCancelAndTimeout(t *testing.T) {
	const timeout = 7 * time.Second

	t.Run("rollback, caller context cancelled", func(t *testing.T) {
		var undo, release cleanupProbe
		cause := context.Canceled
		seq := mustNew(t, Config{
			Steps: []Step{
				{Name: "runtime", Start: func(context.Context) error { return nil }, Stop: undo.observe},
				{Name: "engine", Start: func(ctx context.Context) error { return ctx.Err() }},
			},
			Release:         release.observe,
			ShutdownTimeout: timeout,
		})
		before := time.Now()
		err := seq.Start(cancelledCtx())

		var se *compose.StartError
		if !errors.As(err, &se) || se.Step != "engine" || !errors.Is(se.Err, cause) {
			t.Fatalf("Start = %v, want a StartError for the engine step carrying context.Canceled", err)
		}
		undo.check(t, timeout, before)
		release.check(t, timeout, before)
	})

	t.Run("stop, caller context cancelled", func(t *testing.T) {
		var undo cleanupProbe
		seq := mustNew(t, Config{
			Steps:           []Step{{Name: "runtime", Start: func(context.Context) error { return nil }, Stop: undo.observe}},
			ShutdownTimeout: timeout,
		})
		if err := seq.Start(context.Background()); err != nil {
			t.Fatalf("Start: %v", err)
		}
		before := time.Now()
		if err := seq.Stop(cancelledCtx()); err != nil {
			t.Fatalf("Stop: %v", err)
		}
		undo.check(t, timeout, before)
	})

	t.Run("stop never started, zero timeout uses the default", func(t *testing.T) {
		var release cleanupProbe
		seq := mustNew(t, Config{
			Steps:   []Step{{Name: "runtime", Start: func(context.Context) error { return nil }}},
			Release: release.observe,
		})
		before := time.Now()
		if err := seq.Stop(cancelledCtx()); err != nil {
			t.Fatalf("Stop: %v", err)
		}
		release.check(t, DefaultShutdownTimeout, before)
	})

	t.Run("cleanup context is released after cleanup", func(t *testing.T) {
		var kept context.Context
		seq := mustNew(t, Config{
			Steps: []Step{{Name: "runtime", Start: func(context.Context) error { return nil }, Stop: func(ctx context.Context) error {
				kept = ctx
				return nil
			}}},
			ShutdownTimeout: timeout,
		})
		if err := seq.Start(context.Background()); err != nil {
			t.Fatalf("Start: %v", err)
		}
		if err := seq.Stop(context.Background()); err != nil {
			t.Fatalf("Stop: %v", err)
		}
		if kept == nil || kept.Err() == nil {
			t.Fatal("cleanup context still live after Stop returned; its timer leaks")
		}
	})
}

func TestStartAndStop_AreSerialized(t *testing.T) {
	rec := newRecorder()
	entered := make(chan struct{})
	proceed := make(chan struct{})
	cfg := rec.config("runtime", "engine")
	start := cfg.Steps[1].Start
	cfg.Steps[1].Start = func(ctx context.Context) error {
		close(entered)
		<-proceed
		return start(ctx)
	}
	seq := mustNew(t, cfg)

	startDone := make(chan error, 1)
	go func() { startDone <- seq.Start(context.Background()) }()
	select {
	case <-entered:
	case err := <-startDone:
		t.Fatalf("Start returned (%v) without running the blocking step", err)
	}

	stopDone := make(chan error, 1)
	stopCalling := make(chan struct{})
	go func() {
		close(stopCalling)
		stopDone <- seq.Stop(context.Background())
	}()
	<-stopCalling
	close(proceed)

	if err := <-startDone; err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := <-stopDone; err != nil {
		t.Fatalf("Stop: %v", err)
	}
	want := []string{"start runtime", "start engine", "stop engine", "stop runtime"}
	if got := rec.calls(); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v: Stop must wait for Start instead of interleaving", got, want)
	}
}
