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
	"runtime"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
)

// manualSource is a tick source the test drives by hand: nothing ticks until fire is called. It has
// no goroutine and no real timer, so the tests below never wait on wall-clock time.
type manualSource struct {
	ticks chan time.Time
}

func newManualSource() *manualSource { return &manualSource{ticks: make(chan time.Time)} }

func (m *manualSource) source(time.Duration) (<-chan time.Time, func()) {
	return m.ticks, func() {}
}

// fire hands one tick to the ticker's loop. It returns once the loop has taken it, which does not
// wait on real time.
func (m *manualSource) fire(at time.Time) { m.ticks <- at }

// panicValue returns what fn panics with, or nil when it returns normally.
func panicValue(fn func()) (recovered any) {
	defer func() { recovered = recover() }()
	fn()
	return nil
}

func TestTicker(t *testing.T) {
	specs.Describe(t, "Ticker delivers ticks until stopped", func(s *specs.Spec) {
		s.It("stops ticking after five ticks and Stop", func(ctx *specs.Context) {
			src := newManualSource()
			ticker := newWithSource(100*time.Millisecond, src.source)
			ctx.Cleanup(ticker.Stop) // Stop is idempotent; this releases the loop if a check fails
			ticker.Start()
			ctx.Expect(ticker.Ticking()).To(specs.BeTrue())

			// Ticks is unbuffered and the ticker drops a tick nobody is waiting for. A receiver
			// goroutine forwards what arrives, and each attempt fires one source tick and counts at
			// most one delivered tick, so the count can only reach five by passing through every value.
			// The poll runs on a manual clock that the callback advances, so it never sleeps.
			delivered := make(chan time.Time, 64)
			done := make(chan struct{})
			ctx.Cleanup(func() { close(done) })
			go func() {
				for {
					select {
					case at := <-ticker.Ticks:
						delivered <- at
					case <-done:
						return
					}
				}
			}()

			clock := specs.NewManualClock()
			ticks := 0
			ctx.Eventually(func() any {
				src.fire(clock.Now())
				runtime.Gosched() // let the receiver goroutine forward the tick
				select {
				case <-delivered:
					ticks++
				default:
				}
				clock.Advance(time.Millisecond)
				return ticks
			}, specs.Equal(5), specs.WithClock(clock), specs.WithTimeout(time.Minute), specs.WithInterval(time.Millisecond))

			ticker.Stop()
			ctx.Expect(ticker.Ticking()).To(specs.BeFalse())
		})

		s.It("rejects a non-positive interval", func(ctx *specs.Context) {
			ctx.Expect(panicValue(func() { New(0) })).To(specs.Equal("intervals must be greater than zero"))
			ctx.Expect(panicValue(func() { New(-time.Second) })).To(specs.Equal("intervals must be greater than zero"))
		})

		s.It("starts and stops on the real clock without waiting for a tick", func(ctx *specs.Context) {
			// The interval is far longer than the case, so no tick arrives; this only covers the
			// production wiring that New adds on top of the tick source.
			ticker := New(time.Hour)
			ctx.Cleanup(ticker.Stop)
			ticker.Start()
			ctx.Expect(ticker.Ticking()).To(specs.BeTrue())
			ticker.Stop()
			ctx.Expect(ticker.Ticking()).To(specs.BeFalse())
		})
	})
}
