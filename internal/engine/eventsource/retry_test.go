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
	"sync/atomic"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
)

func TestRetryWithBackoff(t *testing.T) {
	specs.Describe(t, "retryWithBackoff retries an operation with exponential backoff until it succeeds, runs out of attempts or the context ends", func(s *specs.Spec) {
		s.It("succeeds on first attempt", func(ctx *specs.Context) {
			var calls int32
			err := retryWithBackoff(context.Background(), defaultMaxRetries, func() error {
				atomic.AddInt32(&calls, 1)
				return nil
			})
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(atomic.LoadInt32(&calls)).ToEqual(int32(1))
		})
		s.It("succeeds on second attempt", func(ctx *specs.Context) {
			var calls int32
			err := retryWithBackoff(context.Background(), defaultMaxRetries, func() error {
				n := atomic.AddInt32(&calls, 1)
				if n < 2 {
					return errors.New("transient")
				}
				return nil
			})
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(atomic.LoadInt32(&calls)).ToEqual(int32(2))
		})
		s.It("succeeds on last attempt", func(ctx *specs.Context) {
			maxRetries := 3
			var calls int32
			err := retryWithBackoff(context.Background(), maxRetries, func() error {
				n := atomic.AddInt32(&calls, 1)
				if int(n) <= maxRetries {
					return errors.New("transient")
				}
				return nil
			})
			ctx.Expect(err).To(specs.BeNil())
			ctx.Expect(atomic.LoadInt32(&calls)).ToEqual(int32(maxRetries + 1))
		})
		s.It("returns last error after all attempts exhausted", func(ctx *specs.Context) {
			sentinel := errors.New("persistent failure")
			var calls int32
			err := retryWithBackoff(context.Background(), defaultMaxRetries, func() error {
				atomic.AddInt32(&calls, 1)
				return sentinel
			})
			ctx.Expect(err).To(specs.MatchError(sentinel))
			ctx.Expect(atomic.LoadInt32(&calls)).ToEqual(int32(defaultMaxRetries + 1))
		})
		s.It("zero max retries executes exactly once", func(ctx *specs.Context) {
			var calls int32
			sentinel := errors.New("fail")
			err := retryWithBackoff(context.Background(), 0, func() error {
				atomic.AddInt32(&calls, 1)
				return sentinel
			})
			ctx.Expect(err).To(specs.MatchError(sentinel))
			ctx.Expect(atomic.LoadInt32(&calls)).ToEqual(int32(1))
		})
		s.It("context cancelled before retry", func(ctx *specs.Context) {
			cctx, cancel := context.WithCancel(context.Background())
			var calls int32
			err := retryWithBackoff(cctx, defaultMaxRetries, func() error {
				n := atomic.AddInt32(&calls, 1)
				if n == 1 {
					cancel()
				}
				return errors.New("transient")
			})
			ctx.Expect(err).To(specs.MatchError(context.Canceled))
			ctx.Expect(atomic.LoadInt32(&calls)).ToEqual(int32(1))
		})
		s.It("context deadline exceeded before retry", func(ctx *specs.Context) {
			cctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()

			var calls int32
			err := retryWithBackoff(cctx, 10, func() error {
				atomic.AddInt32(&calls, 1)
				return errors.New("transient")
			})
			ctx.Expect(err).To(specs.MatchError(context.DeadlineExceeded))
			// A partial run is between one attempt and all ten; an empty
			// offender list means the count was in range.
			var outOfRange []int32
			if got := atomic.LoadInt32(&calls); got < 1 || got >= 10 {
				outOfRange = append(outOfRange, got)
			}
			ctx.Expect(outOfRange).To(specs.BeNil())
		})
		s.It("backoff delays increase between attempts", func(ctx *specs.Context) {
			timestamps := make([]time.Time, 0, 4)
			err := retryWithBackoff(context.Background(), 2, func() error {
				timestamps = append(timestamps, time.Now())
				return errors.New("fail")
			})
			ctx.Expect(err).To(specs.Not(specs.BeNil()))
			ctx.Expect(len(timestamps)).ToEqual(3)

			delay1 := timestamps[1].Sub(timestamps[0])
			delay2 := timestamps[2].Sub(timestamps[1])

			// Attempt 0: base=100ms, jitter ∈ [0.5,1.5) → [50ms, 150ms)
			// Attempt 1: base=200ms, jitter ∈ [0.5,1.5) → [100ms, 300ms)
			// Use generous bounds to accommodate scheduler jitter. An empty
			// offender list means the delay was in range.
			var firstOutOfRange, secondOutOfRange []time.Duration
			if delay1 < 40*time.Millisecond || delay1 > 200*time.Millisecond {
				firstOutOfRange = append(firstOutOfRange, delay1)
			}
			if delay2 < 80*time.Millisecond || delay2 > 400*time.Millisecond {
				secondOutOfRange = append(secondOutOfRange, delay2)
			}
			ctx.Expect(firstOutOfRange).To(specs.BeNil())
			ctx.Expect(secondOutOfRange).To(specs.BeNil())
		})
	})
}
