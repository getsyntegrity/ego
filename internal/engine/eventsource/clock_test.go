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
	"sync"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
)

// manualClock adapts a go-specs ManualClock to the package clock and records
// the duration of every timer armed on it, so a test can assert the delay the
// code chose, not only that some timer fired. A test moves time with Advance
// and waits for the code under test to arm its timer (Pending) before it does,
// so an advance is never lost to a timer that does not exist yet.
//
// A clock made by newAutoClock instead advances by d the moment a timer for d
// is armed, so the timer fires at once. Code that waits on the same goroutine
// that runs the test then needs no second goroutine, and the test reads the
// delays it chose from timers.
type manualClock struct {
	*specs.ManualClock

	auto   bool
	mu     sync.Mutex
	delays []time.Duration
}

var _ clock = (*manualClock)(nil)

func newManualClock() *manualClock { return &manualClock{ManualClock: specs.NewManualClock()} }

func newAutoClock() *manualClock {
	c := newManualClock()
	c.auto = true
	return c
}

// NewTimer records d and arms a timer on the manual clock. The go-specs timer
// already has the method set of the package timer.
func (m *manualClock) NewTimer(d time.Duration) timer {
	m.mu.Lock()
	m.delays = append(m.delays, d)
	m.mu.Unlock()
	t := m.ManualClock.NewTimer(d)
	if m.auto {
		m.Advance(d)
	}
	return t
}

// timers returns the duration of every timer armed so far, in order.
func (m *manualClock) timers() []time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]time.Duration(nil), m.delays...)
}
