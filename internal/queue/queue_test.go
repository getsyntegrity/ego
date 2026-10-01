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

package queue

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

func TestQueueDequeueEmpty(t *testing.T) {
	specs.Describe(t, "Dequeue on an empty queue returns nothing", func(s *specs.Spec) {
		s.It("returns nil", func(ctx *specs.Context) {
			q := NewQueue()
			ctx.Expect(q.Dequeue()).To(specs.BeNil())
		})
	})
}

func TestQueueLength(t *testing.T) {
	specs.Describe(t, "Length tracks the number of queued items", func(s *specs.Spec) {
		s.It("is zero when new, grows on enqueue and shrinks on dequeue", func(ctx *specs.Context) {
			q := NewQueue()
			ctx.Expect(q.Length()).ToEqual(uint64(0))

			q.Enqueue(1)
			ctx.Expect(q.Length()).ToEqual(uint64(1))

			q.Dequeue()
			ctx.Expect(q.Length()).ToEqual(uint64(0))
		})
	})
}

func TestQueueIsEmpty(t *testing.T) {
	specs.Describe(t, "IsEmpty reports whether the queue holds items", func(s *specs.Spec) {
		s.It("is true when new, false after enqueue and true again after dequeue", func(ctx *specs.Context) {
			q := NewQueue()
			ctx.Expect(q.IsEmpty()).To(specs.BeTrue())

			q.Enqueue(1)
			ctx.Expect(q.IsEmpty()).To(specs.BeFalse())

			q.Dequeue()
			ctx.Expect(q.IsEmpty()).To(specs.BeTrue())
		})
	})
}
