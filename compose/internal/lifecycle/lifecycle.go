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

// Package lifecycle is the runtime-free start/stop sequencer shared by Ego's
// composition roots (openspec/changes/ego-arch-003/design.md §D1, §D6, §D7).
//
// A Sequence runs named steps in order. When a step fails, it undoes the
// steps that already started, in reverse order, then releases resources no
// step owns yet, and reports what failed as a *compose.StartError. Stop
// undoes every started step in reverse order and attempts every one even
// when some fail. The package knows nothing about any runtime: a step is
// just a pair of functions.
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/getsyntegrity/ego/v4/compose"
)

// DefaultShutdownTimeout bounds rollback and Stop when Config.ShutdownTimeout
// is zero. It is the value the design suggests (§D6); the default itself is
// still an open decision (§9), so it lives in this one constant.
const DefaultShutdownTimeout = 30 * time.Second

// ErrNotStartable is returned by Start when the sequence is not in StateNew:
// a Sequence is single-use, so it cannot be started twice, restarted after
// Stop, or retried after a failed Start.
var ErrNotStartable = errors.New("lifecycle: sequence cannot be started")

// Step is one ordered unit of work.
type Step struct {
	// Name identifies the step in errors; it becomes StartError.Step.
	Name string
	// Start runs the step with the caller's context. It is required. When
	// it fails, it must release whatever it acquired itself: the sequence
	// never calls Stop on the step that failed.
	Start func(ctx context.Context) error
	// Stop undoes Start, during rollback of a later failure or during
	// Sequence.Stop, under the cleanup context. It is optional; nil means
	// there is nothing to undo.
	Stop func(ctx context.Context) error
}

// Config describes a Sequence.
type Config struct {
	// Steps run in slice order on Start and are undone in reverse order.
	Steps []Step
	// Release frees resources handed to the sequence that no started step
	// owns yet (design §D5: publishers not yet attached). It runs after the
	// undos of a failed Start, and on Stop of a sequence that never
	// started. It is optional.
	Release func(ctx context.Context) error
	// ShutdownTimeout bounds the whole cleanup of one rollback or one Stop
	// — every undo step plus Release, together — not each step on its
	// own: all of them share one context with this deadline. Zero means
	// DefaultShutdownTimeout; a negative value is rejected by New.
	ShutdownTimeout time.Duration
}

// State is where a Sequence is in its single-use life.
type State int

const (
	// StateNew is a sequence that has not been started.
	StateNew State = iota
	// StateStarting is a sequence inside Start.
	StateStarting
	// StateRunning is a sequence whose steps all started.
	StateRunning
	// StateStopping is a sequence inside Stop.
	StateStopping
	// StateStopped is a sequence Stop has finished with.
	StateStopped
	// StateFailed is a sequence whose Start failed and was rolled back.
	StateFailed
)

// String returns the state's name.
func (st State) String() string {
	switch st {
	case StateNew:
		return "new"
	case StateStarting:
		return "starting"
	case StateRunning:
		return "running"
	case StateStopping:
		return "stopping"
	case StateStopped:
		return "stopped"
	case StateFailed:
		return "failed"
	default:
		return fmt.Sprintf("State(%d)", int(st))
	}
}

// Sequence runs a Config's steps. It is single-use: New → Starting →
// Running → Stopping → Stopped, or New → Starting → Failed when Start
// fails. Start and Stop are serialized by a mutex, so concurrent callers
// cannot interleave them; State can be read at any time, including from
// inside a step.
//
// Two consequences of that mutex. A Stop that arrives while Start is still
// running a step waits for Start to return, and that wait is not bounded by
// the shutdown timeout, which only starts once Stop holds the mutex; a
// signal-driven Stop is therefore unblocked by canceling the context passed
// to Start, so the running step returns. And a step that calls Start or
// Stop on its own sequence deadlocks.
type Sequence struct {
	mu      sync.Mutex // serializes Start and Stop
	state   atomic.Int32
	steps   []Step
	release func(ctx context.Context) error
	timeout time.Duration
	started int // number of steps whose Start succeeded; guarded by mu
}

// New returns a Sequence in StateNew. It builds nothing and starts
// nothing. It fails when a step has no name or no Start function.
func New(cfg Config) (*Sequence, error) {
	for i, step := range cfg.Steps {
		if step.Name == "" {
			return nil, fmt.Errorf("lifecycle: step %d has no name", i)
		}
		if step.Start == nil {
			return nil, fmt.Errorf("lifecycle: step %q has no Start function", step.Name)
		}
	}
	timeout := cfg.ShutdownTimeout
	if timeout < 0 {
		return nil, fmt.Errorf("lifecycle: ShutdownTimeout must not be negative, got %s (zero means the default)", timeout)
	}
	if timeout == 0 {
		timeout = DefaultShutdownTimeout
	}
	return &Sequence{
		steps:   slices.Clone(cfg.Steps),
		release: cfg.Release,
		timeout: timeout,
	}, nil
}

// State returns the sequence's current state.
func (s *Sequence) State() State {
	return State(s.state.Load())
}

func (s *Sequence) setState(st State) {
	s.state.Store(int32(st))
}

// Start runs every step in order with ctx. It returns ErrNotStartable,
// without running anything, unless the sequence is in StateNew.
//
// Before each step, Start checks ctx.Err(). When ctx is already done, the
// step about to run is not called and counts as failed with ctx.Err()
// (context.Canceled or context.DeadlineExceeded), so the StartError names
// that step and the steps already started roll back as below.
//
// When step k fails, Start does not run the steps after it. It calls Stop
// on steps k-1 down to 1, then Release, attempting every one even when some
// fail, under the cleanup context (see Stop). It then returns a
// *compose.StartError whose Step is the failed step's name, whose Err is
// that step's error and whose Rollback joins every cleanup error (nil when
// cleanup was clean); the sequence ends in StateFailed.
//
// When a step panics, Start rolls back the same way — stopping the steps
// that already started, then Release — and marks the sequence StateFailed
// before the panic continues unchanged, so the resources are released and a
// later Stop is a no-op. Cleanup errors in that case are dropped: there is
// no StartError to carry them.
func (s *Sequence) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if st := s.State(); st != StateNew {
		return fmt.Errorf("%w: state is %s", ErrNotStartable, st)
	}
	s.setState(StateStarting)

	// settled is set on every normal return; if it is still false when
	// this runs, a step panicked. The deferred call does not recover, so
	// the panic propagates once cleanup is done.
	settled := false
	defer func() {
		if !settled {
			_ = s.cleanup(ctx, true)
			s.setState(StateFailed)
		}
	}()

	for _, step := range s.steps {
		// A done context fails the step about to run without calling it,
		// so a caller that gave up does not start one more component that
		// rollback must immediately undo.
		err := ctx.Err()
		if err == nil {
			err = step.Start(ctx)
		}
		if err != nil {
			settled = true // cleanup below runs once; the deferred one must not repeat it
			rollback := s.cleanup(ctx, true)
			s.setState(StateFailed)
			return &compose.StartError{Step: step.Name, Err: err, Rollback: rollback}
		}
		s.started++
	}
	settled = true
	s.setState(StateRunning)
	return nil
}

// Stop calls Stop on every started step in reverse order, attempting every
// one even when some fail, and returns their errors joined. On a sequence
// that was never started it only calls Release. Stop on a sequence that is
// already stopped, or whose Start failed (rollback already released
// everything), is a no-op that returns nil. Otherwise the sequence ends in
// StateStopped, whether or not a step failed to stop.
//
// Stop and the rollback of a failed Start run under one cleanup context:
// context.WithoutCancel(ctx) bounded by the shutdown timeout. The caller's
// values are kept, its cancellation is not, because Stop is usually called
// once the caller's context is already done (design §D6).
func (s *Sequence) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var release bool
	switch s.State() {
	case StateStopped, StateFailed:
		return nil
	case StateNew:
		// Nothing started; only resources handed over are released.
		release = true
	}
	s.setState(StateStopping)
	err := s.cleanup(ctx, release)
	s.setState(StateStopped)
	return err
}

// cleanup stops the started steps in reverse order and, when release is
// true, then calls Release, all under one cleanup context. It attempts
// every call and joins their errors, each naming where it came from.
func (s *Sequence) cleanup(ctx context.Context, release bool) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.timeout)
	defer cancel()

	var errs []error
	for i := s.started - 1; i >= 0; i-- {
		step := s.steps[i]
		if step.Stop == nil {
			continue
		}
		if err := step.Stop(cleanupCtx); err != nil {
			errs = append(errs, fmt.Errorf("lifecycle: stop step %q: %w", step.Name, err))
		}
	}
	s.started = 0
	if release && s.release != nil {
		if err := s.release(cleanupCtx); err != nil {
			errs = append(errs, fmt.Errorf("lifecycle: release: %w", err))
		}
	}
	return errors.Join(errs...)
}
