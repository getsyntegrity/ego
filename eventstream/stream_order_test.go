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

package eventstream

import (
	"runtime"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

// drainInOrder collects n payloads from the subscriber, yielding the processor
// between drains so any delivery goroutine still pending can run. It uses no
// real time: it is bounded by the number of yields, not by a deadline.
func drainInOrder(sub Subscriber, n int) []int {
	got := make([]int, 0, n)
	for yields := 0; len(got) < n && yields < 1000; yields++ {
		for msg := range sub.Iterator() {
			got = append(got, msg.Payload().(int))
		}
		runtime.Gosched()
	}
	return got
}

func TestPublishKeepsTheOrderOfOneProducer(t *testing.T) {
	specs.Describe(t, "Publish delivers messages from one producer in publish order", func(s *specs.Spec) {
		s.It("delivers 1..N in order when each Publish returns before the next starts", func(ctx *specs.Context) {
			// One processor makes the Go scheduler deterministic: the most
			// recently started goroutine runs first, so a Publish that hands
			// delivery to a goroutine reorders consecutive messages every time.
			previous := runtime.GOMAXPROCS(1)
			defer runtime.GOMAXPROCS(previous)

			const n = 50
			stream := New()
			sub := stream.AddSubscriber()
			stream.Subscribe(sub, "events")

			want := make([]int, n)
			for i := 0; i < n; i++ {
				want[i] = i + 1
				stream.Publish("events", i+1)
			}

			ctx.Expect(drainInOrder(sub, n)).To(specs.Equal(want))
		})
	})
}
