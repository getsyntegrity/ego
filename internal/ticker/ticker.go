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
	"sync"
	"time"
)

// Ticker defines time ticker that delivers ticks at intervals
type Ticker struct {
	Ticks     chan time.Time
	intervals time.Duration
	mutex     sync.Mutex
	ticking   bool
	stopCh    chan bool
	source    tickSource
}

// tickSource starts a periodic source of time and returns its channel and a function that releases it.
// It is the only seam between the ticker and the wall clock: production uses realSource, tests pass a
// source they fire by hand.
type tickSource func(interval time.Duration) (ticks <-chan time.Time, stop func())

func realSource(interval time.Duration) (<-chan time.Time, func()) {
	t := time.NewTicker(interval)
	return t.C, t.Stop
}

// New creates an instance of Ticker that ticks every intervals.
// It includes some kind of back-pressure for slow receivers
func New(intervals time.Duration) *Ticker {
	return newWithSource(intervals, realSource)
}

func newWithSource(intervals time.Duration, source tickSource) *Ticker {
	if intervals <= 0 {
		panic("intervals must be greater than zero")
	}
	return &Ticker{
		Ticks:     make(chan time.Time),
		intervals: intervals,
		stopCh:    make(chan bool),
		ticking:   false,
		source:    source,
	}
}

// Start the ticker. Ticks are delivered on the ticker's
// channel until Stop is called
func (t *Ticker) Start() {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if !t.ticking {
		go t.tickingLoop()
		t.ticking = true
	}
}

// Stop stops the ticker. No ticks will be delivered on ticker's channel
// after Stop returns and before Start is call again
func (t *Ticker) Stop() {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if t.ticking {
		t.ticking = false
		t.stopCh <- true
	}
}

// Ticking returns true when the ticker is ticking
// and false when it is stopped
func (t *Ticker) Ticking() bool {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return t.ticking
}

func (t *Ticker) tickingLoop() {
	ticks, stop := t.source(t.intervals)
	for {
		select {
		case tc := <-ticks:
			select {
			case t.Ticks <- tc:
			default:
			}
		case <-t.stopCh:
			stop()
			return
		}
	}
}
