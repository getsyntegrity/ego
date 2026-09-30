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

package eventsource

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
)

// retryPoll bounds every wait on another goroutine. The retry under test never
// sleeps on real time: these options only limit how long a test waits for a
// broken retry before it fails instead of hanging.
var retryPoll = []specs.PollOption{specs.WithTimeout(time.Second), specs.WithInterval(time.Millisecond)}

// fixedBackoff returns a backoff on clk whose jitter is always factor.
func fixedBackoff(clk clock, factor float64) backoff {
	return backoff{clock: clk, jitter: func() float64 { return factor }}
}

// goRetry runs retryWithBackoff on its own goroutine and returns the channel
// that receives its result. The case waits for the goroutine, so a caller must
// pass a context it cancels before the case ends.
func goRetry(ctx *specs.Context, cctx context.Context, b backoff, maxRetries int, op func() error) <-chan error {
	done := make(chan error, 1)
	ctx.Go(func(*specs.Context) { done <- retryWithBackoff(cctx, b, maxRetries, op) })
	return done
}

// awaitTimer waits until the retry has exactly one timer pending.
func awaitTimer(ctx *specs.Context, clk *manualClock) {
	ctx.Eventually(func() any { return clk.Pending() }, specs.Equal(1), retryPoll...)
}

// awaitResult waits for the retry started by goRetry to return and gives its error.
func awaitResult(ctx *specs.Context, done <-chan error) error {
	ctx.Eventually(func() any { return len(done) }, specs.Equal(1), retryPoll...)
	return <-done
}

// jitterCase is one row of the jitter table: the first wait for a factor.
type jitterCase struct {
	name   string
	factor float64
	want   time.Duration
}

func TestRetryWithBackoff(t *testing.T) {
	specs.Describe(t, "retryWithBackoff retries an operation with exponential backoff until it succeeds, runs out of attempts or the context ends", func(s *specs.Spec) {
		s.It("succeeds on first attempt", func(ctx *specs.Context) {
			clk := newAutoClock()
			var calls int32
			err := retryWithBackoff(context.Background(), fixedBackoff(clk, 1), defaultMaxRetries, func() error {
				atomic.AddInt32(&calls, 1)
				return nil
			})
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(atomic.LoadInt32(&calls)).ToEqual(int32(1))
			ctx.Expect(clk.timers()).To(specs.BeEmpty())
		})
		s.It("succeeds on second attempt", func(ctx *specs.Context) {
			clk := newAutoClock()
			var calls int32
			err := retryWithBackoff(context.Background(), fixedBackoff(clk, 1), defaultMaxRetries, func() error {
				n := atomic.AddInt32(&calls, 1)
				if n < 2 {
					return errors.New("transient")
				}
				return nil
			})
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(atomic.LoadInt32(&calls)).ToEqual(int32(2))
			ctx.Expect(clk.timers()).To(specs.Equal([]time.Duration{100 * time.Millisecond}))
		})
		s.It("succeeds on last attempt", func(ctx *specs.Context) {
			clk := newAutoClock()
			maxRetries := 3
			var calls int32
			err := retryWithBackoff(context.Background(), fixedBackoff(clk, 1), maxRetries, func() error {
				n := atomic.AddInt32(&calls, 1)
				if int(n) <= maxRetries {
					return errors.New("transient")
				}
				return nil
			})
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(atomic.LoadInt32(&calls)).ToEqual(int32(maxRetries + 1))
			ctx.Expect(clk.timers()).To(specs.Equal([]time.Duration{
				100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond,
			}))
		})
		s.It("returns last error after all attempts exhausted", func(ctx *specs.Context) {
			clk := newAutoClock()
			sentinel := errors.New("persistent failure")
			var calls int32
			err := retryWithBackoff(context.Background(), fixedBackoff(clk, 1), defaultMaxRetries, func() error {
				atomic.AddInt32(&calls, 1)
				return sentinel
			})
			ctx.Expect(err).To(specs.MatchError(sentinel))
			ctx.Expect(atomic.LoadInt32(&calls)).ToEqual(int32(defaultMaxRetries + 1))
			// four attempts wait three times: there is no wait after the last one
			ctx.Expect(clk.timers()).To(specs.HaveLen(defaultMaxRetries))
		})
		s.It("zero max retries executes exactly once", func(ctx *specs.Context) {
			clk := newAutoClock()
			var calls int32
			sentinel := errors.New("fail")
			err := retryWithBackoff(context.Background(), fixedBackoff(clk, 1), 0, func() error {
				atomic.AddInt32(&calls, 1)
				return sentinel
			})
			ctx.Expect(err).To(specs.MatchError(sentinel))
			ctx.Expect(atomic.LoadInt32(&calls)).ToEqual(int32(1))
			ctx.Expect(clk.timers()).To(specs.BeEmpty())
		})
		s.It("context cancelled before retry", func(ctx *specs.Context) {
			// The clock never advances, so the only way out of the wait is the context.
			clk := newManualClock()
			cctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls int32
			err := retryWithBackoff(cctx, fixedBackoff(clk, 1), defaultMaxRetries, func() error {
				n := atomic.AddInt32(&calls, 1)
				if n == 1 {
					cancel()
				}
				return errors.New("transient")
			})
			ctx.Expect(err).To(specs.MatchError(context.Canceled))
			ctx.Expect(atomic.LoadInt32(&calls)).ToEqual(int32(1))
			// the wait was armed once and stopped when the context ended
			ctx.Expect(clk.timers()).To(specs.Equal([]time.Duration{100 * time.Millisecond}))
			ctx.Expect(clk.Pending()).To(specs.Equal(0))
		})
		s.It("context deadline exceeded before retry", func(ctx *specs.Context) {
			// A deadline in the past is already expired: no real time has to pass.
			cctx, cancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
			defer cancel()

			clk := newManualClock()
			var calls int32
			err := retryWithBackoff(cctx, fixedBackoff(clk, 1), 10, func() error {
				atomic.AddInt32(&calls, 1)
				return errors.New("transient")
			})
			ctx.Expect(err).To(specs.MatchError(context.DeadlineExceeded))
			ctx.Expect(atomic.LoadInt32(&calls)).ToEqual(int32(1))
			ctx.Expect(clk.Pending()).To(specs.Equal(0))
		})
		s.It("backoff delays increase between attempts", func(ctx *specs.Context) {
			clk := newAutoClock()
			var calls int
			err := retryWithBackoff(context.Background(), fixedBackoff(clk, 1), 2, func() error {
				calls++
				return errors.New("fail")
			})
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(calls).To(specs.Equal(3))
			// Attempt 0 waits base, attempt 1 waits twice the base.
			ctx.Expect(clk.timers()).To(specs.Equal([]time.Duration{100 * time.Millisecond, 200 * time.Millisecond}))
		})
		s.It("waits the exponential delay times the jitter between attempts", func(ctx *specs.Context) {
			clk := newManualClock()
			cctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			var calls atomic.Int32
			done := goRetry(ctx, cctx, fixedBackoff(clk, 1.25), 1, func() error {
				calls.Add(1)
				return errors.New("fail")
			})

			awaitTimer(ctx, clk)
			ctx.Expect(clk.timers()).To(specs.Equal([]time.Duration{125 * time.Millisecond}))

			// One millisecond short of the delay: the retry keeps waiting.
			clk.Advance(124 * time.Millisecond)
			ctx.Expect(clk.Pending()).To(specs.Equal(1))
			ctx.Expect(calls.Load()).To(specs.Equal(int32(1)))

			clk.Advance(time.Millisecond)
			ctx.Expect(awaitResult(ctx, done)).To(specs.Not(specs.BeNil()))
			ctx.Expect(calls.Load()).To(specs.Equal(int32(2)))
		})
		s.It("context cancelled during a wait", func(ctx *specs.Context) {
			clk := newManualClock()
			cctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			var calls atomic.Int32
			done := goRetry(ctx, cctx, fixedBackoff(clk, 1), defaultMaxRetries, func() error {
				calls.Add(1)
				return errors.New("fail")
			})

			// Advance nothing: the retry is blocked in its first wait when the context ends.
			awaitTimer(ctx, clk)
			cancel()

			ctx.Expect(awaitResult(ctx, done)).To(specs.MatchError(context.Canceled))
			ctx.Expect(calls.Load()).To(specs.Equal(int32(1)))
			ctx.Expect(clk.Pending()).To(specs.Equal(0))
		})
		s.It("caps the delay at two seconds", func(ctx *specs.Context) {
			clk := newAutoClock()
			err := retryWithBackoff(context.Background(), fixedBackoff(clk, 1), 7, func() error {
				return errors.New("fail")
			})
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(clk.timers()).To(specs.Equal([]time.Duration{
				100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond,
				1600 * time.Millisecond, 2 * time.Second, 2 * time.Second,
			}))
		})

		specs.Table(s, []jitterCase{
			{name: "the lowest jitter halves the delay", factor: 0.5, want: 50 * time.Millisecond},
			{name: "a jitter of one keeps the delay", factor: 1, want: 100 * time.Millisecond},
			{name: "the highest jitter stays under one and a half times the delay", factor: math.Nextafter(1.5, 0), want: 149999999 * time.Nanosecond},
		}, func(c jitterCase) string { return c.name }, func(ctx *specs.Context, c jitterCase) {
			clk := newAutoClock()
			err := retryWithBackoff(context.Background(), fixedBackoff(clk, c.factor), 1, func() error {
				return errors.New("fail")
			})
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(clk.timers()).To(specs.Equal([]time.Duration{c.want}))
			ctx.Expect(clk.timers()[0]).To(specs.BeLessThan(150 * time.Millisecond))
		})

		s.It("the default jitter stays within [0.5, 1.5)", func(ctx *specs.Context) {
			jitter := defaultBackoff().jitter
			draws := make([]float64, 1000)
			for i := range draws {
				draws[i] = jitter()
			}
			ctx.Expect(draws).To(specs.EveryElement(specs.BeBetween(0.5, math.Nextafter(1.5, 0))))
		})
		s.It("the default clock arms a timer that can be stopped", func(ctx *specs.Context) {
			t := defaultBackoff().clock.NewTimer(time.Hour)
			ctx.Expect(t.Stop()).To(specs.BeTrue())
		})
	})
}
