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
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
)

// manualClock adapts a go-specs ManualClock to the runner's clock. A test moves
// time with Advance and waits for the runner to arm a timer with Pending, so an
// advance is never lost to a timer that does not exist yet.
type manualClock struct{ *specs.ManualClock }

var _ clock = manualClock{}

func newManualClock() manualClock { return manualClock{specs.NewManualClock()} }

// NewTimer arms a timer on the manual clock. The go-specs timer already has the
// method set of the runner's timer.
func (m manualClock) NewTimer(d time.Duration) timer { return m.ManualClock.NewTimer(d) }

func TestWithClock(t *testing.T) {
	specs.Describe(t, "the runner reads time through an injectable clock", func(s *specs.Spec) {
		s.It("uses the clock given to WithClock", func(ctx *specs.Context) {
			manual := newManualClock()
			var r Runner
			WithClock(manual).Apply(&r)
			ctx.Expect(r.clock).To(specs.Equal(manual))
		})
		s.It("defaults New to the real clock", func(ctx *specs.Context) {
			runner := New("clock-default", nil, nil, nil)
			ctx.Expect(runner.clock).To(specs.Equal(realClock{}))
		})
		s.It("keeps the real clock when WithClock is given nil", func(ctx *specs.Context) {
			runner := New("clock-nil", nil, nil, nil, WithClock(nil))
			ctx.Expect(runner.clock).To(specs.Equal(realClock{}))
		})
		s.It("has a real clock that tells the wall time and fires its timers", func(ctx *specs.Context) {
			before := time.Now()
			now := realClock{}.Now()
			ctx.Expect(now.Before(before)).To(specs.BeFalse())

			fired := realClock{}.NewTimer(time.Millisecond)
			defer fired.Stop()
			ctx.Eventually(func() any {
				select {
				case <-fired.C():
					return true
				default:
					return false
				}
			}, specs.BeTrue(), poll...)

			stopped := realClock{}.NewTimer(time.Hour)
			ctx.Expect(stopped.Stop()).To(specs.BeTrue())
		})
	})
}
