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
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
)

const (
	// waitTimeout bounds every wait on the asynchronous fan-out of Publish and
	// Broadcast.
	waitTimeout = 10 * time.Second
	// waitInterval is how often a waited-on condition is polled.
	waitInterval = time.Millisecond
)

// awaitQueued waits until sub has n messages queued. Publish and Broadcast
// signal each subscriber from its own goroutine, so delivery is observable
// only through the subscriber's queue.
func awaitQueued(ctx *specs.Context, sub Subscriber, n int) {
	queued := sub.(*subscriber).messages
	ctx.Eventually(func() any { return queued.Length() }, specs.BeGreaterThanOrEqual(uint64(n)),
		specs.WithTimeout(waitTimeout), specs.WithInterval(waitInterval))
}

// fired reports, without blocking, whether ch has a value or is closed.
func fired(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// awaitFired waits until ch has a value or is closed.
func awaitFired(ctx *specs.Context, ch <-chan struct{}) {
	ctx.Eventually(func() any { return fired(ch) }, specs.BeTrue(),
		specs.WithTimeout(waitTimeout), specs.WithInterval(waitInterval))
}

// drain returns every message the subscriber's iterator yields.
func drain(sub Subscriber) []*Message {
	var got []*Message
	for msg := range sub.Iterator() {
		got = append(got, msg)
	}
	return got
}

func TestStream(t *testing.T) {
	specs.Describe(t, "the events stream delivers published messages to its active subscribers", func(s *specs.Spec) {
		s.It("Message accessors", func(ctx *specs.Context) {
			msg := NewMessage("topic", "payload")
			ctx.Expect(msg.Topic()).ToEqual("topic")
			ctx.Expect(msg.Payload()).ToEqual("payload")
		})

		s.It("With Subscriber lifecycle", func(ctx *specs.Context) {
			sub := newSubscriber()
			ctx.Expect(sub.ID()).To(specs.Not(specs.Equal("")))
			ctx.Expect(sub.Active()).To(specs.BeTrue())

			// empty iterator should close immediately
			ctx.Expect(drain(sub)).To(specs.BeEmpty())

			sub.subscribe("a")
			sub.subscribe("b")
			ctx.Expect(sub.Topics()).To(specs.HaveLen(2))

			sub.signal(NewMessage("a", "one"))
			sub.signal(NewMessage("b", "two"))

			ctx.Expect(drain(sub)).To(specs.HaveLen(2))

			sub.unsubscribe("a")
			ctx.Expect(sub.Topics()).To(specs.HaveLen(1))

			sub.Shutdown()
			ctx.Expect(sub.Active()).To(specs.BeFalse())

			// signals after shutdown should be ignored
			sub.signal(NewMessage("b", "three"))
			ctx.Expect(drain(sub)).To(specs.BeEmpty())

			// enqueue nil while active to cover nil dequeue branch before shutdown
			activeSub := newSubscriber()
			activeSub.messages.Enqueue(nil)
			ctx.Expect(drain(activeSub)).To(specs.BeEmpty())

			// cover nil dequeue path: manually drop a nil into the queue
			sub.messages.Enqueue(nil)
			ctx.Expect(drain(sub)).To(specs.EveryElement(specs.BeNil()))
		})

		s.It("With Ready signaling", func(ctx *specs.Context) {
			sub := newSubscriber()

			// no message yet: Ready must not fire
			ctx.Expect(fired(sub.Ready())).To(specs.BeFalse())

			// a signal wakes a consumer blocked on Ready
			sub.signal(NewMessage("a", "one"))
			awaitFired(ctx, sub.Ready())

			// coalescing: many signals while no one is draining keep at most one
			// pending wake-up, and a single drain still sees every message
			sub.signal(NewMessage("a", "two"))
			sub.signal(NewMessage("a", "three"))
			<-sub.Ready()
			ctx.Expect(drain(sub)).To(specs.HaveLen(3))
			ctx.Expect(fired(sub.Ready())).To(specs.BeFalse())

			// Shutdown wakes a consumer blocked on Ready
			done := make(chan struct{})
			go func() {
				<-sub.Ready()
				close(done)
			}()
			sub.Shutdown()
			awaitFired(ctx, done)
			ctx.Expect(sub.Active()).To(specs.BeFalse())
		})

		s.It("With Subscription", func(ctx *specs.Context) {
			broker := New()

			// add consumer
			cons := broker.AddSubscriber()
			ctx.Expect(cons).To(specs.Not(specs.BeNil()))
			broker.Subscribe(cons, "t1")
			broker.Subscribe(cons, "t2")

			ctx.Expect(broker.SubscribersCount("t1")).ToEqual(1)
			ctx.Expect(broker.SubscribersCount("t2")).ToEqual(1)

			// remove the consumer
			broker.RemoveSubscriber(cons)
			ctx.Expect(broker.SubscribersCount("t1")).ToEqual(0)
			ctx.Expect(broker.SubscribersCount("t2")).ToEqual(0)

			broker.Subscribe(cons, "t3")
			ctx.Expect(broker.SubscribersCount("t3")).ToEqual(0)

			broker.Close()
		})

		s.It("With Unsubscription", func(ctx *specs.Context) {
			broker := New()

			// add consumer
			cons := broker.AddSubscriber()
			ctx.Expect(cons).To(specs.Not(specs.BeNil()))
			broker.Subscribe(cons, "t1")
			broker.Subscribe(cons, "t2")

			sub2 := broker.AddSubscriber()
			ctx.Expect(sub2).To(specs.Not(specs.BeNil()))
			broker.Subscribe(sub2, "t1")

			ctx.Expect(broker.SubscribersCount("t1")).ToEqual(2)
			ctx.Expect(broker.SubscribersCount("t2")).ToEqual(1)

			sub2.Shutdown()

			// Unsubscribe the consumer
			broker.Unsubscribe(cons, "t1")
			broker.Unsubscribe(sub2, "t1")
			ctx.Expect(broker.SubscribersCount("t1")).ToEqual(0)
			ctx.Expect(broker.SubscribersCount("t2")).ToEqual(1)

			broker.Subscribe(cons, "t3")
			ctx.Expect(broker.SubscribersCount("t3")).ToEqual(1)

			// remove the consumer
			broker.RemoveSubscriber(cons)
			broker.Subscribe(cons, "t4")
			ctx.Expect(broker.SubscribersCount("t4")).ToEqual(0)

			broker.Close()
		})

		s.It("Publish skips inactive subscribers and topics with no subscribers", func(ctx *specs.Context) {
			broker := New()

			// publish without subscribers should be a no-op
			broker.Publish("unused", "ignored")

			active := broker.AddSubscriber()
			inactive := broker.AddSubscriber()
			broker.Subscribe(active, "t1")
			broker.Subscribe(inactive, "t1")
			inactive.Shutdown()

			broker.Publish("t1", "hi")
			// The inactive subscriber is filtered synchronously inside Publish, so
			// once the active one has its message nothing else can still arrive.
			awaitQueued(ctx, active, 1)

			ctx.Expect(drain(active)).To(specs.HaveLen(1))
			ctx.Expect(drain(inactive)).To(specs.BeEmpty())

			broker.Close()
		})

		s.It("Close shuts down all subscribers", func(ctx *specs.Context) {
			broker := New()
			sub1 := broker.AddSubscriber()
			sub2 := broker.AddSubscriber()
			broker.Subscribe(sub1, "t1")
			broker.Subscribe(sub2, "t1")

			broker.Close()

			ctx.Expect(sub1.Active()).To(specs.BeFalse())
			ctx.Expect(sub2.Active()).To(specs.BeFalse())
		})

		s.It("With Publication", func(ctx *specs.Context) {
			broker := New()

			// add consumer
			cons := broker.AddSubscriber()
			ctx.Expect(cons).To(specs.Not(specs.BeNil()))
			broker.Subscribe(cons, "t1")
			broker.Subscribe(cons, "t2")

			sub2 := broker.AddSubscriber()
			ctx.Expect(sub2).To(specs.Not(specs.BeNil()))
			broker.Subscribe(sub2, "t1")

			ctx.Expect(broker.SubscribersCount("t1")).ToEqual(2)
			ctx.Expect(broker.SubscribersCount("t2")).ToEqual(1)

			sub2.Shutdown()

			broker.Publish("t1", "hi")
			broker.Publish("t2", "hello")

			awaitQueued(ctx, cons, 2)

			messages := drain(cons)
			ctx.Expect(messages).To(specs.HaveLen(2))
			ctx.Expect(messages).To(specs.EveryElement(specs.All(
				specs.Not(specs.BeNil()),
				specs.Project("Topic", (*Message).Topic, specs.Not(specs.Equal(""))),
				specs.Project("Payload", (*Message).Payload, specs.Not(specs.BeNil())),
			)))
			ctx.Expect(cons.Topics()).To(specs.HaveLen(2))

			broker.Close()
		})

		s.It("With Broadcast", func(ctx *specs.Context) {
			broker := New()

			// add consumer
			cons := broker.AddSubscriber()
			ctx.Expect(cons).To(specs.Not(specs.BeNil()))
			broker.Subscribe(cons, "t1")
			broker.Subscribe(cons, "t2")

			sub2 := broker.AddSubscriber()
			ctx.Expect(sub2).To(specs.Not(specs.BeNil()))
			broker.Subscribe(sub2, "t1")

			ctx.Expect(broker.SubscribersCount("t1")).ToEqual(2)
			ctx.Expect(broker.SubscribersCount("t2")).ToEqual(1)

			sub2.Shutdown()

			broker.Broadcast("hi", []string{"t1", "t2"})

			awaitQueued(ctx, cons, 2)

			ctx.Expect(drain(cons)).To(specs.HaveLen(2))
			ctx.Expect(cons.Topics()).To(specs.HaveLen(2))

			broker.Close()
		})
	})
}
