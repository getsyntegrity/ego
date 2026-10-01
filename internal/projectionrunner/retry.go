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
	"time"
)

const (
	// defaultRetryTries is the attempt count used when the configured maximum
	// is not positive, the same default the retry library applied.
	defaultRetryTries = 5
	// defaultRetryDelay is the wait between attempts used when the configured
	// delay is not positive, the same default the retry library applied.
	defaultRetryDelay = 200 * time.Millisecond
)

// retryOn runs fn until it succeeds or has failed maxTries times, waiting delay
// between two attempts on clk. It returns nil on the first success, and the
// error of the last attempt once attempts are exhausted; there is no wait after
// the last attempt. The wait is cut short when ctx ends, and the error of the
// attempt that preceded it is returned. fn is always attempted at least once,
// even on a context that has already ended, and is expected to honor ctx itself.
//
// A maxTries or delay that is not positive falls back to its default. The delay
// is constant: unlike the exponential, jittered schedule of the retry library
// it replaces, it does not grow or vary, which keeps the schedule driven by the
// clock predictable.
func retryOn(ctx context.Context, clk clock, maxTries int, delay time.Duration, fn func(context.Context) error) error {
	if maxTries <= 0 {
		maxTries = defaultRetryTries
	}
	if delay <= 0 {
		delay = defaultRetryDelay
	}

	for attempt := 1; ; attempt++ {
		err := fn(ctx)
		if err == nil {
			return nil
		}
		if attempt >= maxTries {
			return err
		}

		wait := clk.NewTimer(delay)
		select {
		case <-wait.C():
		case <-ctx.Done():
			wait.Stop()
			return err
		}
	}
}
