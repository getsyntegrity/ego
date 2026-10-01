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

import "time"

// clock is the runner's only source of time. The pull loop, the start-up ping
// retry, the recovery retry, the store backoff and the offset timestamps all
// read it, so a test can replace real time with a manual clock and move it
// explicitly instead of sleeping. It is package-private on purpose: the
// WithClock option exists for the tests of this package.
type clock interface {
	// Now returns the current time.
	Now() time.Time
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

// Now returns the wall-clock time.
func (realClock) Now() time.Time { return time.Now() }

// NewTimer returns a timer backed by time.Timer.
func (realClock) NewTimer(d time.Duration) timer { return realTimer{time.NewTimer(d)} }

// realTimer adapts time.Timer, whose channel is a field, to the timer interface.
type realTimer struct{ t *time.Timer }

// C returns the channel the timer fires on.
func (r realTimer) C() <-chan time.Time { return r.t.C }

// Stop stops the timer and reports whether it was still pending.
func (r realTimer) Stop() bool { return r.t.Stop() }
