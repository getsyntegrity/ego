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
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/mock"
	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/ego/compose"
)

// recorder builds steps that append "start X" or "stop X" to one shared,
// ordered log and fail when told to, so a test can assert the exact call
// order the sequence produced.
//
// It stays a hand-written fake because it has real behavior that is an input
// of the tests: failStart, failStop and failRel decide which step fails. The
// log itself is a mock.Spy, which is safe for concurrent use.
type recorder struct {
	log       *mock.Spy
	failStart map[string]error
	failStop  map[string]error
	failRel   error
}

func newRecorder() *recorder {
	return &recorder{log: mock.NewSpy(), failStart: map[string]error{}, failStop: map[string]error{}}
}

func (r *recorder) record(entry string) {
	r.log.Call(entry)
}

func (r *recorder) calls() []string {
	recorded := r.log.Calls()
	entries := make([]string, len(recorded))
	for i, call := range recorded {
		entries[i] = call.Args[0].(string)
	}
	return entries
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

func mustNew(ctx *specs.Context, cfg Config) *Sequence {
	seq, err := New(cfg)
	ctx.Expect(err).To(specs.BeNil())
	return seq
}

func mustStart(ctx *specs.Context, seq *Sequence) {
	ctx.Expect(seq.Start(context.Background())).To(specs.BeNil())
}

func mustStop(ctx *specs.Context, seq *Sequence) {
	ctx.Expect(seq.Stop(context.Background())).To(specs.BeNil())
}

// errorMessage is the text of an error, for Project.
func errorMessage(err error) string { return err.Error() }

var names = []string{"probe", "runtime", "engine", "publishers", "projections"}

func TestNew_RejectsIncompleteSteps(t *testing.T) {
	type incompleteStep struct {
		desc string
		step Step
	}
	specs.Describe(t, "New rejects a step without a name or a Start and accepts one without Stop", func(s *specs.Spec) {
		noop := func(context.Context) error { return nil }
		specs.Table(s, []incompleteStep{
			{"missing name", Step{Start: noop}},
			{"missing start", Step{Name: "runtime"}},
		}, func(c incompleteStep) string { return c.desc }, func(ctx *specs.Context, c incompleteStep) {
			_, err := New(Config{Steps: []Step{c.step}})
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
		})
		s.It("accepts a step without Stop, which is optional", func(ctx *specs.Context) {
			_, err := New(Config{Steps: []Step{{Name: "runtime", Start: noop}}})
			ctx.Expect(err).To(specs.BeNil())
		})
	})
}

func TestNew_RejectsNegativeShutdownTimeout(t *testing.T) {
	specs.Describe(t, "New rejects a negative shutdown timeout, where only zero means the default", func(s *specs.Spec) {
		s.It("rejects a negative timeout naming ShutdownTimeout and accepts zero", func(ctx *specs.Context) {
			noop := func(context.Context) error { return nil }
			steps := []Step{{Name: "runtime", Start: noop}}

			_, err := New(Config{Steps: steps, ShutdownTimeout: -time.Second})
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(err).To(specs.Project("message", errorMessage, specs.Contain("ShutdownTimeout")))

			_, err = New(Config{Steps: steps})
			ctx.Expect(err).To(specs.BeNil())
		})
	})
}

func TestStart_PanickingStepRollsBackReleasesAndFails(t *testing.T) {
	specs.Describe(t, "Start rolls back and releases before a step's panic propagates, leaving the sequence failed", func(s *specs.Spec) {
		s.It("re-raises the panic unchanged after rollback and Release, and Stop then does nothing", func(ctx *specs.Context) {
			rec := newRecorder()
			cfg := rec.config(names[:3]...)
			cfg.Steps[1].Start = func(context.Context) error {
				rec.record("start runtime")
				panic("runtime exploded")
			}
			seq := mustNew(ctx, cfg)

			recovered := func() (r any) {
				defer func() { r = recover() }()
				_ = seq.Start(context.Background())
				return nil
			}()

			ctx.Expect(recovered).ToEqual("runtime exploded")
			want := []string{"start probe", "start runtime", "stop probe", "release"}
			ctx.Expect(rec.calls()).ToEqual(want)
			ctx.Expect(seq.State()).ToEqual(StateFailed)
			mustStop(ctx, seq)
			ctx.Expect(rec.callsFrom(len(want))).To(specs.BeEmpty())
		})
	})
}

func TestStart_RunsStepsInOrder(t *testing.T) {
	specs.Describe(t, "Start runs every step in declaration order and leaves the sequence running", func(s *specs.Spec) {
		s.It("starts probe, runtime, engine, publishers, projections and reports StateRunning", func(ctx *specs.Context) {
			rec := newRecorder()
			seq := mustNew(ctx, rec.config(names...))

			mustStart(ctx, seq)
			want := []string{"start probe", "start runtime", "start engine", "start publishers", "start projections"}
			ctx.Expect(rec.calls()).ToEqual(want)
			ctx.Expect(seq.State()).ToEqual(StateRunning)
		})
	})
}

func TestStart_FailureAtEachStepRollsBackInReverseThenReleases(t *testing.T) {
	specs.Describe(t, "Start rolls back the started steps in reverse and then releases when a step fails", func(s *specs.Spec) {
		specs.Table(s, names, func(failing string) string { return failing }, func(ctx *specs.Context, failing string) {
			k := slices.Index(names, failing)
			rec := newRecorder()
			cause := fmt.Errorf("%s broke", failing)
			rec.failStart[failing] = cause
			seq := mustNew(ctx, rec.config(names...))

			err := seq.Start(context.Background())

			// Later steps never start and the failing step is not stopped.
			var want []string
			for _, name := range names[:k+1] {
				want = append(want, "start "+name)
			}
			for i := k - 1; i >= 0; i-- {
				want = append(want, "stop "+names[i])
			}
			want = append(want, "release")
			ctx.Expect(rec.calls()).ToEqual(want)

			var se *compose.StartError
			ctx.Expect(err).To(specs.MatchErrorAs(&se))
			ctx.Expect(se.Step).ToEqual(failing)
			ctx.Expect(se.Err).To(specs.MatchError(cause))
			ctx.Expect(err).To(specs.MatchError(cause))
			ctx.Expect(se.Rollback).To(specs.BeNil())
			ctx.Expect(seq.State()).ToEqual(StateFailed)
		})
	})
}

func TestStart_RollbackAttemptsEveryUndoAndReportsEveryError(t *testing.T) {
	specs.Describe(t, "Start rollback attempts every undo and reports every error it hit", func(s *specs.Spec) {
		s.It("runs all undos and Release after a failed step and joins their errors into StartError.Rollback", func(ctx *specs.Context) {
			rec := newRecorder()
			cause := errors.New("publisher attach failed")
			stopEngine := errors.New("engine did not stop")
			stopProbe := errors.New("probe undo failed")
			releaseErr := errors.New("publisher close failed")
			rec.failStart["publishers"] = cause
			rec.failStop["engine"] = stopEngine
			rec.failStop["probe"] = stopProbe
			rec.failRel = releaseErr
			seq := mustNew(ctx, rec.config(names...))

			err := seq.Start(context.Background())

			want := []string{
				"start probe", "start runtime", "start engine", "start publishers",
				"stop engine", "stop runtime", "stop probe", "release",
			}
			ctx.Expect(rec.calls()).ToEqual(want)
			var se *compose.StartError
			ctx.Expect(err).To(specs.MatchErrorAs(&se))
			ctx.Expect(se.Step).ToEqual("publishers")
			ctx.Expect(se.Err).To(specs.MatchError(cause))
			// Every cleanup error is reachable both in Rollback and through the
			// StartError itself; the matcher's failure names the missing one.
			everyCleanupError := specs.All(specs.MatchError(stopEngine), specs.MatchError(stopProbe), specs.MatchError(releaseErr))
			ctx.Expect(se.Rollback).To(everyCleanupError)
			ctx.Expect(err).To(everyCleanupError)
			// StartError.Err holds only the step error, not the rollback errors.
			ctx.Expect(se.Err).To(specs.Not(specs.MatchError(stopEngine)))
			ctx.Expect(se.Rollback).To(specs.Project("message", errorMessage,
				specs.All(specs.Contain("engine"), specs.Contain("probe"))))
		})
	})
}

func TestStart_StepsWithoutStopAreSkippedOnRollback(t *testing.T) {
	specs.Describe(t, "Start rollback skips steps that declare no Stop", func(s *specs.Spec) {
		s.It("stops only the earlier step that has a Stop, then releases", func(ctx *specs.Context) {
			rec := newRecorder()
			cfg := rec.config(names[:3]...)
			cfg.Steps[1].Stop = nil
			rec.failStart["engine"] = errors.New("boom")
			seq := mustNew(ctx, cfg)

			ctx.Expect(seq.Start(context.Background())).To(specs.Not(specs.BeNil()))
			want := []string{"start probe", "start runtime", "start engine", "stop probe", "release"}
			ctx.Expect(rec.calls()).ToEqual(want)
		})
	})
}

func TestStop_UndoesEveryStepInReverseOrder(t *testing.T) {
	specs.Describe(t, "Stop undoes every started step in reverse order", func(s *specs.Spec) {
		s.It("stops projections down to probe, leaves Release to resources no step owns and reports StateStopped", func(ctx *specs.Context) {
			rec := newRecorder()
			seq := mustNew(ctx, rec.config(names...))
			mustStart(ctx, seq)

			mustStop(ctx, seq)
			want := []string{"stop projections", "stop publishers", "stop engine", "stop runtime", "stop probe"}
			ctx.Expect(rec.callsFrom(len(names))).ToEqual(want)
			ctx.Expect(seq.State()).ToEqual(StateStopped)
		})
	})
}

func TestStop_FailureAtEachStepStillRunsTheRest(t *testing.T) {
	specs.Describe(t, "Stop attempts every step even when one fails and reports the failure", func(s *specs.Spec) {
		specs.Table(s, names, func(failing string) string { return failing }, func(ctx *specs.Context, failing string) {
			rec := newRecorder()
			cause := fmt.Errorf("%s did not stop", failing)
			rec.failStop[failing] = cause
			seq := mustNew(ctx, rec.config(names...))
			mustStart(ctx, seq)

			err := seq.Stop(context.Background())

			want := []string{"stop projections", "stop publishers", "stop engine", "stop runtime", "stop probe"}
			ctx.Expect(rec.callsFrom(len(names))).ToEqual(want)
			ctx.Expect(err).To(specs.MatchError(cause))
			ctx.Expect(err).To(specs.Project("message", errorMessage, specs.Contain(failing)))
			ctx.Expect(seq.State()).ToEqual(StateStopped)
		})
	})
}

func TestStop_JoinsEveryError(t *testing.T) {
	specs.Describe(t, "Stop joins the error of every step that failed to stop", func(s *specs.Spec) {
		s.It("wraps every step's stop error", func(ctx *specs.Context) {
			rec := newRecorder()
			var causes []specs.Matcher
			for _, name := range names {
				cause := fmt.Errorf("%s did not stop", name)
				rec.failStop[name] = cause
				causes = append(causes, specs.MatchError(cause))
			}
			seq := mustNew(ctx, rec.config(names...))
			mustStart(ctx, seq)

			err := seq.Stop(context.Background())

			// The matcher's failure names the cause that is missing.
			ctx.Expect(err).To(specs.All(causes...))
		})
	})
}

func TestStop_IsIdempotent(t *testing.T) {
	specs.Describe(t, "Stop is idempotent: a second Stop succeeds and does nothing", func(s *specs.Spec) {
		s.It("returns nil and makes no calls the second time, even after the first Stop failed", func(ctx *specs.Context) {
			rec := newRecorder()
			rec.failStop["engine"] = errors.New("engine did not stop")
			seq := mustNew(ctx, rec.config(names...))
			mustStart(ctx, seq)
			ctx.Expect(seq.Stop(context.Background())).To(specs.Not(specs.BeNil()))
			before := len(rec.calls())

			mustStop(ctx, seq)
			ctx.Expect(rec.callsFrom(before)).To(specs.BeEmpty())
		})
	})
}

func TestStop_NeverStartedOnlyReleases(t *testing.T) {
	specs.Describe(t, "Stop on a never-started sequence only runs Release", func(s *specs.Spec) {
		s.It("releases once, reports the release error, ends stopped and does not release again", func(ctx *specs.Context) {
			rec := newRecorder()
			releaseErr := errors.New("publisher close failed")
			rec.failRel = releaseErr
			seq := mustNew(ctx, rec.config(names...))

			err := seq.Stop(context.Background())

			ctx.Expect(rec.calls()).ToEqual([]string{"release"})
			ctx.Expect(err).To(specs.MatchError(releaseErr))
			ctx.Expect(seq.State()).ToEqual(StateStopped)
			mustStop(ctx, seq)
			ctx.Expect(rec.calls()).ToEqual([]string{"release"})
		})
	})
}

func TestStop_AfterFailedStartIsNoOp(t *testing.T) {
	specs.Describe(t, "Stop after a failed Start does nothing because rollback already released everything", func(s *specs.Spec) {
		s.It("returns nil, makes no calls and keeps StateFailed terminal", func(ctx *specs.Context) {
			rec := newRecorder()
			rec.failStart["engine"] = errors.New("boom")
			seq := mustNew(ctx, rec.config(names...))
			ctx.Expect(seq.Start(context.Background())).To(specs.Not(specs.BeNil()))
			before := len(rec.calls())

			mustStop(ctx, seq)
			ctx.Expect(rec.callsFrom(before)).To(specs.BeEmpty())
			ctx.Expect(seq.State()).ToEqual(StateFailed)
		})
	})
}

func TestStart_IsSingleUse(t *testing.T) {
	type singleUseCase struct {
		desc    string
		prepare func(ctx *specs.Context, seq *Sequence, rec *recorder)
		state   State
	}
	cases := []singleUseCase{
		{"after a successful Start", func(ctx *specs.Context, seq *Sequence, _ *recorder) {
			mustStart(ctx, seq)
		}, StateRunning},
		{"after Stop", func(ctx *specs.Context, seq *Sequence, _ *recorder) {
			mustStart(ctx, seq)
			mustStop(ctx, seq)
		}, StateStopped},
		{"after Stop on a never-started sequence", func(ctx *specs.Context, seq *Sequence, _ *recorder) {
			mustStop(ctx, seq)
		}, StateStopped},
		{"after a failed Start", func(ctx *specs.Context, seq *Sequence, rec *recorder) {
			rec.failStart["runtime"] = errors.New("boom")
			ctx.Expect(seq.Start(context.Background())).To(specs.Not(specs.BeNil()))
			delete(rec.failStart, "runtime")
		}, StateFailed},
	}
	specs.Describe(t, "Start is single-use: a second Start is rejected with ErrNotStartable and changes nothing", func(s *specs.Spec) {
		specs.Table(s, cases, func(c singleUseCase) string { return c.desc }, func(ctx *specs.Context, c singleUseCase) {
			rec := newRecorder()
			seq := mustNew(ctx, rec.config(names...))
			c.prepare(ctx, seq, rec)
			before := len(rec.calls())

			err := seq.Start(context.Background())

			ctx.Expect(err).To(specs.MatchError(ErrNotStartable))
			ctx.Expect(rec.callsFrom(before)).To(specs.BeEmpty())
			ctx.Expect(seq.State()).ToEqual(c.state)
		})
	})
}

func TestState_TransitionsAreVisibleInsideSteps(t *testing.T) {
	specs.Describe(t, "State reports Starting and Stopping from inside the steps", func(s *specs.Spec) {
		s.It("moves from StateNew to Starting during Start and Stopping during Stop", func(ctx *specs.Context) {
			var seq *Sequence
			var during []State
			observe := func(context.Context) error {
				during = append(during, seq.State())
				return nil
			}
			seq = mustNew(ctx, Config{Steps: []Step{{Name: "only", Start: observe, Stop: observe}}})
			ctx.Expect(seq.State()).ToEqual(StateNew)
			mustStart(ctx, seq)
			mustStop(ctx, seq)
			ctx.Expect(during).ToEqual([]State{StateStarting, StateStopping})
		})
	})
}

type ctxKey struct{}

// cleanupView is what a cleanup function saw of its context when it ran. It
// is a snapshot because the context is cancelled once cleanup returns.
type cleanupView struct {
	Err         error
	Value       any
	Deadline    time.Time
	HasDeadline bool
}

// cleanupProbe stands in for a step's Stop or for Release. It is a
// mock.Controller method that must be called exactly once, and the captor
// keeps what the cleanup context looked like at that moment.
type cleanupProbe struct {
	method *mock.Method
	views  *mock.Captor[cleanupView]
}

func newCleanupProbe(ctx *specs.Context, name string) *cleanupProbe {
	ctrl := mock.NewController(ctx) // verifies the call count when the case ends
	views := mock.NewCaptor[cleanupView]()
	method := ctrl.Method(name)
	method.Expect(views.Matcher()).Times(1)
	return &cleanupProbe{method: method, views: views}
}

func (p *cleanupProbe) observe(cleanupCtx context.Context) error {
	deadline, hasDeadline := cleanupCtx.Deadline()
	p.method.Call(cleanupView{
		Err:         cleanupCtx.Err(),
		Value:       cleanupCtx.Value(ctxKey{}),
		Deadline:    deadline,
		HasDeadline: hasDeadline,
	})
	return nil
}

// check asserts that the cleanup context kept the caller's values, was not
// cancelled with the caller, and was bounded by the shutdown timeout.
func (p *cleanupProbe) check(ctx *specs.Context, bound time.Duration, before time.Time) {
	ctx.Expect(p.views.Values()).To(specs.HaveLen(1))
	// The deadline is set from time.Now() inside the sequence, after before
	// was read, so it lies in (before, before+bound+slack]. The slack covers
	// only the time between reading before and calling Start/Stop; nothing
	// here waits on it.
	timeLeft := func(v cleanupView) time.Duration { return v.Deadline.Sub(before) }
	deadline := []specs.Matcher{specs.BeGreaterThan(time.Duration(0)), specs.BeLessThanOrEqual(bound + time.Minute)}
	if bound < time.Minute {
		// A configured timeout shorter than the slack must not be ignored.
		deadline = append(deadline, specs.BeLessThan(time.Minute))
	}
	matchers := []specs.Matcher{
		// It must not inherit the caller's cancellation.
		specs.Project("Err", func(v cleanupView) error { return v.Err }, specs.BeNil()),
		// It must keep the caller's values.
		specs.Project("Value", func(v cleanupView) any { return v.Value }, specs.Equal("kept")),
		// The shutdown timeout must bound it.
		specs.Project("HasDeadline", func(v cleanupView) bool { return v.HasDeadline }, specs.BeTrue()),
		specs.Project("time left until the deadline", timeLeft, specs.All(deadline...)),
	}
	ctx.Expect(p.views.Last()).To(specs.All(matchers...))
}

func cancelledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "kept"))
	cancel()
	return ctx
}

func TestCleanup_RunsUnderWithoutCancelAndTimeout(t *testing.T) {
	const timeout = 7 * time.Second

	specs.Describe(t, "cleanup runs under a context detached from the caller's cancellation and bounded by the shutdown timeout", func(s *specs.Spec) {
		s.It("rollback, caller context cancelled", func(ctx *specs.Context) {
			undo := newCleanupProbe(ctx, "Stop")
			release := newCleanupProbe(ctx, "Release")
			cause := context.Canceled
			// The caller's context is cancelled while the first step runs, so
			// the second step fails on it and rollback runs with a context
			// whose parent is already done.
			callerCtx, cancel := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "kept"))
			ctx.Cleanup(cancel)
			seq := mustNew(ctx, Config{
				Steps: []Step{
					{Name: "runtime", Start: func(context.Context) error { cancel(); return nil }, Stop: undo.observe},
					{Name: "engine", Start: func(stepCtx context.Context) error { return stepCtx.Err() }},
				},
				Release:         release.observe,
				ShutdownTimeout: timeout,
			})
			before := time.Now()
			err := seq.Start(callerCtx)

			// Start fails as a StartError for the engine step carrying context.Canceled.
			var se *compose.StartError
			ctx.Expect(err).To(specs.MatchErrorAs(&se))
			ctx.Expect(se.Step).ToEqual("engine")
			ctx.Expect(se.Err).To(specs.MatchError(cause))
			undo.check(ctx, timeout, before)
			release.check(ctx, timeout, before)
		})

		s.It("stop, caller context cancelled", func(ctx *specs.Context) {
			undo := newCleanupProbe(ctx, "Stop")
			seq := mustNew(ctx, Config{
				Steps:           []Step{{Name: "runtime", Start: func(context.Context) error { return nil }, Stop: undo.observe}},
				ShutdownTimeout: timeout,
			})
			mustStart(ctx, seq)
			before := time.Now()
			ctx.Expect(seq.Stop(cancelledCtx())).To(specs.BeNil())
			undo.check(ctx, timeout, before)
		})

		s.It("stop never started, zero timeout uses the default", func(ctx *specs.Context) {
			release := newCleanupProbe(ctx, "Release")
			seq := mustNew(ctx, Config{
				Steps:   []Step{{Name: "runtime", Start: func(context.Context) error { return nil }}},
				Release: release.observe,
			})
			before := time.Now()
			ctx.Expect(seq.Stop(cancelledCtx())).To(specs.BeNil())
			release.check(ctx, DefaultShutdownTimeout, before)
		})

		s.It("cleanup context is released after cleanup", func(ctx *specs.Context) {
			var kept context.Context
			seq := mustNew(ctx, Config{
				Steps: []Step{{Name: "runtime", Start: func(context.Context) error { return nil }, Stop: func(stopCtx context.Context) error {
					kept = stopCtx
					return nil
				}}},
				ShutdownTimeout: timeout,
			})
			mustStart(ctx, seq)
			mustStop(ctx, seq)
			// A live cleanup context after Stop returned means its timer leaks.
			ctx.Expect(kept).To(specs.Not(specs.BeNil()))
			ctx.Expect(kept.Err()).To(specs.MatchError(context.Canceled))
		})
	})
}

func TestStartAndStop_AreSerialized(t *testing.T) {
	specs.Describe(t, "Stop waits for an in-flight Start instead of interleaving with it", func(s *specs.Spec) {
		s.It("records the whole Start before any Stop call", func(ctx *specs.Context) {
			rec := newRecorder()
			entered := make(chan struct{})
			proceed := make(chan struct{})
			cfg := rec.config("runtime", "engine")
			start := cfg.Steps[1].Start
			cfg.Steps[1].Start = func(stepCtx context.Context) error {
				close(entered)
				<-proceed
				return start(stepCtx)
			}
			seq := mustNew(ctx, cfg)

			// The goroutines only drive concurrency and never touch ctx; both
			// are joined through their result channels before the case ends.
			startDone := make(chan error, 1)
			go func() { startDone <- seq.Start(context.Background()) }()
			returnedEarly := false
			var earlyErr error
			select {
			case <-entered:
			case earlyErr = <-startDone:
				returnedEarly = true
			}
			// Start returning before the blocking step ran would be a failure.
			ctx.Expect(returnedEarly).To(specs.BeFalse())
			ctx.Expect(earlyErr).To(specs.BeNil())

			stopDone := make(chan error, 1)
			stopCalling := make(chan struct{})
			go func() {
				close(stopCalling)
				stopDone <- seq.Stop(context.Background())
			}()
			<-stopCalling
			close(proceed)

			startErr := <-startDone
			stopErr := <-stopDone
			ctx.Expect(startErr).To(specs.BeNil())
			ctx.Expect(stopErr).To(specs.BeNil())
			want := []string{"start runtime", "start engine", "stop engine", "stop runtime"}
			ctx.Expect(rec.calls()).ToEqual(want)
		})
	})
}

// TestStart_ChecksContextBeforeEachStep covers the maintainer decision of
// 2026-09-27: Start checks ctx.Err() before running each step. A context
// that is already done when a step is about to run fails that step without
// calling it, and the steps already started roll back as for any failure.
func TestStart_ChecksContextBeforeEachStep(t *testing.T) {
	specs.Describe(t, "Start fails a step whose context is already done without calling it", func(s *specs.Spec) {
		s.It("cancelled before the first step", func(ctx *specs.Context) {
			rec := newRecorder()
			seq := mustNew(ctx, rec.config("probe", "runtime"))

			err := seq.Start(cancelledCtx())

			var se *compose.StartError
			ctx.Expect(err).To(specs.MatchErrorAs(&se))
			ctx.Expect(se.Step).ToEqual("probe")
			ctx.Expect(se.Err).To(specs.MatchError(context.Canceled))
			ctx.Expect(se.Rollback).To(specs.BeNil())
			// No step may run on a done context; only Release does.
			ctx.Expect(rec.calls()).ToEqual([]string{"release"})
			ctx.Expect(seq.State()).ToEqual(StateFailed)
		})

		s.It("cancelled while a step runs", func(ctx *specs.Context) {
			rec := newRecorder()
			callerCtx, cancel := context.WithCancel(context.Background())
			ctx.Cleanup(cancel)
			cfg := rec.config("probe", "runtime", "engine")
			start := cfg.Steps[1].Start
			cfg.Steps[1].Start = func(stepCtx context.Context) error {
				cancel() // the step itself succeeds; the caller gives up meanwhile
				return start(stepCtx)
			}
			seq := mustNew(ctx, cfg)

			err := seq.Start(callerCtx)

			// The failing step is the engine, the one about to run.
			var se *compose.StartError
			ctx.Expect(err).To(specs.MatchErrorAs(&se))
			ctx.Expect(se.Step).ToEqual("engine")
			ctx.Expect(se.Err).To(specs.MatchError(context.Canceled))
			want := []string{"start probe", "start runtime", "stop runtime", "stop probe", "release"}
			ctx.Expect(rec.calls()).ToEqual(want)
		})

		s.It("deadline exceeded", func(ctx *specs.Context) {
			rec := newRecorder()
			callerCtx, cancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
			ctx.Cleanup(cancel)
			seq := mustNew(ctx, rec.config("probe"))

			err := seq.Start(callerCtx)

			var se *compose.StartError
			ctx.Expect(err).To(specs.MatchErrorAs(&se))
			ctx.Expect(se.Step).ToEqual("probe")
			ctx.Expect(se.Err).To(specs.MatchError(context.DeadlineExceeded))
		})
	})
}
