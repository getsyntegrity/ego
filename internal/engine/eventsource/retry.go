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
	"math"
	"math/rand/v2"
	"time"
)

const (
	defaultMaxRetries = 3
	retryBaseDelay    = 100 * time.Millisecond
	retryMaxDelay     = 2 * time.Second
)

// clock is the only source of time retryWithBackoff reads. It is package-private
// on purpose: it exists so the tests of this package can replace real time with
// a manual clock and move it explicitly instead of sleeping.
type clock interface {
	// NewTimer returns a timer that fires once, d from now.
	NewTimer(d time.Duration) timer
}

// timer is a one-shot timer created by a clock.
type timer interface {
	// C returns the channel the timer fires on.
	C() <-chan time.Time
	// Stop prevents the timer from firing. It reports whether the timer was
	// still pending.
	Stop() bool
}

// realClock is the production clock, backed by the time package.
type realClock struct{}

var _ clock = realClock{}

// NewTimer returns a timer backed by time.Timer.
func (realClock) NewTimer(d time.Duration) timer { return realTimer{time.NewTimer(d)} }

// realTimer adapts time.Timer, whose channel is a field, to the timer interface.
type realTimer struct{ t *time.Timer }

// C returns the channel the timer fires on.
func (r realTimer) C() <-chan time.Time { return r.t.C }

// Stop stops the timer and reports whether it was still pending.
func (r realTimer) Stop() bool { return r.t.Stop() }

// backoff holds the two sources retryWithBackoff draws on: the clock that waits
// and the jitter that scales each wait. Passing them as a value, instead of
// reading package variables, lets a test give each case its own and keeps the
// cases independent of one another.
type backoff struct {
	// clock arms the timer for each wait.
	clock clock
	// jitter returns the factor, in [0.5, 1.5), that scales each delay.
	jitter func() float64
}

// defaultBackoff returns the production backoff: real time and a jitter drawn
// uniformly from [0.5, 1.5).
func defaultBackoff() backoff {
	return backoff{
		clock: realClock{},
		jitter: func() float64 {
			return 0.5 + rand.Float64() //nolint:gosec // cryptographic randomness is not needed for backoff jitter
		},
	}
}

// retryWithBackoff retries op up to maxRetries times with exponential backoff
// and jitter, waiting on b. Returns nil on the first successful attempt or the
// last error after all attempts are exhausted. Respects context cancellation
// between attempts.
func retryWithBackoff(ctx context.Context, b backoff, maxRetries int, op func() error) error {
	var err error
	for attempt := range maxRetries + 1 {
		if err = op(); err == nil {
			return nil
		}

		if attempt < maxRetries {
			delay := time.Duration(math.Min(
				float64(retryBaseDelay)*math.Pow(2, float64(attempt)),
				float64(retryMaxDelay),
			))
			delay = time.Duration(float64(delay) * b.jitter())

			t := b.clock.NewTimer(delay)
			select {
			case <-ctx.Done():
				t.Stop()
				return ctx.Err()
			case <-t.C():
			}
		}
	}
	return err
}
