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

package projectionrunner

import (
	"context"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	"go.uber.org/atomic"
)

// retryOutcome is what a retryOn run ended with.
type retryOutcome struct {
	err      error
	attempts int32
	delays   []time.Duration
}

// runRetry drives retryOn on a manual clock in a task. fn fails failures times,
// then succeeds; each of the waits armed timers is advanced by step.
func runRetry(ctx *specs.Context, maxTries int, delay time.Duration, failures int32, waits int, step time.Duration) retryOutcome {
	clk := newManualClock()
	attempts := atomic.NewInt32(0)
	done := make(chan error, 1)

	// A retry that never finishes would block the spec until the test binary
	// times out, since a task is never cancelled: ending the context on any exit
	// lets a stuck retry fail within the poll timeout instead.
	retryCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctx.Go(func(*specs.Context) {
		done <- retryOn(retryCtx, clk, maxTries, delay, func(context.Context) error {
			if attempts.Inc() <= failures {
				return errFailed
			}
			return nil
		})
	})

	for range waits {
		awaitTimer(ctx, clk)
		clk.Advance(step)
	}

	var err error
	ctx.Eventually(func() any {
		select {
		case err = <-done:
			return true
		default:
			return false
		}
	}, specs.BeTrue(), poll...)

	return retryOutcome{err: err, attempts: attempts.Load(), delays: clk.timers()}
}

type retryOnCase struct {
	name     string
	maxTries int
	delay    time.Duration
	failures int32
	waits    int
	step     time.Duration
	want     retryOutcome
}

func TestRetryOn(t *testing.T) {
	specs.Describe(t, "retryOn retries on the clock like the retrier it replaces", func(s *specs.Spec) {
		specs.Table(s, []retryOnCase{
			{
				name: "succeeds on the first attempt without waiting", maxTries: 3, delay: time.Second, failures: 0, waits: 0, step: time.Second,
				want: retryOutcome{attempts: 1},
			},
			{
				name: "waits the delay between attempts until one succeeds", maxTries: 5, delay: time.Second, failures: 2, waits: 2, step: time.Second,
				want: retryOutcome{attempts: 3, delays: []time.Duration{time.Second, time.Second}},
			},
			{
				name: "returns the last error after the last attempt without a final wait", maxTries: 3, delay: time.Second, failures: 9, waits: 2, step: time.Second,
				want: retryOutcome{err: errFailed, attempts: 3, delays: []time.Duration{time.Second, time.Second}},
			},
			{
				name: "defaults to five attempts when the maximum is not positive", maxTries: 0, delay: time.Second, failures: 9, waits: 4, step: time.Second,
				want: retryOutcome{err: errFailed, attempts: 5, delays: []time.Duration{time.Second, time.Second, time.Second, time.Second}},
			},
			{
				name: "defaults to 200ms between attempts when the delay is not positive", maxTries: 2, delay: 0, failures: 1, waits: 1, step: 200 * time.Millisecond,
				want: retryOutcome{attempts: 2, delays: []time.Duration{200 * time.Millisecond}},
			},
		}, func(c retryOnCase) string { return c.name }, func(ctx *specs.Context, c retryOnCase) {
			got := runRetry(ctx, c.maxTries, c.delay, c.failures, c.waits, c.step)
			ctx.Expect(got.attempts).To(specs.Equal(c.want.attempts))
			ctx.Expect(got.err).To(specs.Equal(c.want.err))
			ctx.Expect(got.delays).To(specs.Equal(c.want.delays))
		})

		s.It("returns the last error when the context ends during a wait", func(ctx *specs.Context) {
			clk := newManualClock()
			cancelable, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)

			ctx.Go(func(*specs.Context) {
				done <- retryOn(cancelable, clk, 5, time.Second, func(context.Context) error { return errFailed })
			})

			awaitTimer(ctx, clk)
			cancel()

			ctx.Expect(awaitFailure(ctx, done)).To(specs.MatchError(errFailed))
			// the wait that was cut short leaves no timer behind
			ctx.Expect(clk.Pending()).To(specs.Equal(0))
		})
	})
}
