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

package ticker

import (
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
)

func TestTicker(t *testing.T) {
	specs.Describe(t, "Ticker delivers ticks until stopped", func(s *specs.Spec) {
		s.It("stops ticking after five ticks and Stop", func(ctx *specs.Context) {
			ticker := New(100 * time.Millisecond)
			ctx.Cleanup(ticker.Stop) // Stop is idempotent; this releases the loop if a check fails
			ticker.Start()
			ctx.Expect(ticker.Ticking()).To(specs.BeTrue())

			// Ticks is unbuffered and the ticker drops a tick nobody is waiting for, so
			// each poll waits for one tick. A ticker that never ticks then fails after
			// the timeout instead of hanging the case.
			ticks := 0
			ctx.Eventually(func() any {
				select {
				case <-ticker.Ticks:
					ticks++
				case <-time.After(time.Second):
				}
				return ticks
			}, specs.Equal(5), specs.WithTimeout(5*time.Second), specs.WithInterval(time.Millisecond))

			ticker.Stop()
			ctx.Expect(ticker.Ticking()).To(specs.BeFalse())
		})
	})
}
