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

package adaptertest

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/urd/port/adapter"
)

// The AT-4 self-check needs a release that never returns, so the harness has
// to wait out stallDeadline+grace (1.2 s of real time). The harness reads
// time through the unexported clk, and this file swaps it for a clock that
// fires the AT-4 wait at once. It lives in the package, not in
// adaptertest_test, because clk is not part of the public API.

// instantClock reads the real time but fires the timer of the AT-4 wait
// (stallDeadline+grace) as soon as it is created. Every other timer never
// fires, so an ordinary call that returns is never cut short.
type instantClock struct{ real realClock }

func (c instantClock) Now() time.Time { return c.real.Now() }

func (c instantClock) NewTimer(d time.Duration) (<-chan time.Time, func()) {
	if d != stallDeadline+grace {
		return make(chan time.Time), func() {}
	}
	fired := make(chan time.Time, 1)
	fired <- c.real.Now()
	return fired, func() {}
}

// useClock swaps clk for the duration of the test.
func useClock(t *testing.T, c clock) {
	t.Helper()
	prev := clk
	clk = c
	t.Cleanup(func() { clk = prev })
}

// stuckRelease is an owned adapter whose Close ignores its context once the
// backend is stalled.
type stuckRelease struct {
	stalled *atomic.Bool
	block   chan struct{}
}

func (stuckRelease) Describe() adapter.Descriptor {
	return adapter.Descriptor{Ports: []adapter.Port{"publishing.EventPublisher"}, Name: "stuck"}
}

func (s stuckRelease) Close(context.Context) error {
	if s.stalled.Load() {
		<-s.block
	}
	return nil
}

func TestCapture_CloseIgnoringTheDeadlineFailsAT4WithoutWaiting(t *testing.T) {
	specs.Describe(t, "Capture fails AT-4 for a release that ignores its deadline", func(s *specs.Spec) {
		s.It("fails only AT-4, mentions the deadline and does not wait for the real grace", func(ctx *specs.Context) {
			useClock(ctx.T, instantClock{})
			block := make(chan struct{})
			ctx.T.Cleanup(func() { close(block) })
			stalled := &atomic.Bool{}
			target := Target{
				Port:      "publishing.EventPublisher",
				Ownership: Owned,
				Stall:     func(*testing.T) { stalled.Store(true) },
				New: func(*testing.T) (any, error) {
					return stuckRelease{stalled: stalled, block: block}, nil
				},
			}
			start := time.Now()
			results := Capture(ctx.T, target)
			elapsed := time.Since(start)

			byName := map[string]Result{}
			var failed []string
			for _, r := range results {
				byName[r.Check] = r
				if r.Outcome == Failed {
					failed = append(failed, r.Check)
				}
			}
			ctx.Expect(failed).To(specs.Equal([]string{"AT-4"}))
			ctx.Expect(byName["AT-4"].Detail).To(specs.Contain("deadline"))
			ctx.Expect(elapsed < grace/2).To(specs.Equal(true))
		})
	})
}

func TestRealClockReadsTheWallClockAndFiresItsTimer(t *testing.T) {
	specs.Describe(t, "realClock", func(s *specs.Spec) {
		s.It("reports the current time and fires a timer after its duration", func(ctx *specs.Context) {
			var c clock = realClock{}
			before := time.Now()
			ctx.Expect(c.Now().Before(before)).To(specs.Equal(false))
			fired, stop := c.NewTimer(time.Millisecond)
			defer stop()
			select {
			case <-fired:
			case <-time.After(5 * time.Second):
				ctx.T.Fatal("the timer did not fire")
			}
		})
		s.It("stops a timer before it fires", func(ctx *specs.Context) {
			var c clock = realClock{}
			fired, stop := c.NewTimer(time.Hour)
			stop()
			select {
			case <-fired:
				ctx.T.Fatal("a stopped timer fired")
			default:
			}
		})
	})
}
