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

package enginetest

import (
	"github.com/getsyntegrity/go-specs/mock"

	"github.com/getsyntegrity/ego/eventstream"
)

// EventStreamMock is an eventstream.Stream backed by a go-specs
// mock.Controller. Every method forwards its call to the controller under the
// method's own name, and the case declares the answer with
// c.Method("Name").Expect(...).Return(...). Build the controller with
// mock.NewController(ctx) so its expectations are verified when the case ends,
// and an unexpected call is reported at once.
type EventStreamMock struct{ c *mock.Controller }

var _ eventstream.Stream = (*EventStreamMock)(nil)

// NewEventStreamMock returns an EventStreamMock that forwards to c.
func NewEventStreamMock(c *mock.Controller) *EventStreamMock { return &EventStreamMock{c: c} }

// AddSubscriber forwards to the controller.
func (m *EventStreamMock) AddSubscriber() eventstream.Subscriber {
	return mock.Value[eventstream.Subscriber](m.c.Method("AddSubscriber").Call(), 0)
}

// RemoveSubscriber forwards to the controller.
func (m *EventStreamMock) RemoveSubscriber(sub eventstream.Subscriber) {
	m.c.Method("RemoveSubscriber").Call(sub)
}

// SubscribersCount forwards to the controller.
func (m *EventStreamMock) SubscribersCount(topic string) int {
	return mock.Value[int](m.c.Method("SubscribersCount").Call(topic), 0)
}

// Subscribe forwards to the controller.
func (m *EventStreamMock) Subscribe(sub eventstream.Subscriber, topic string) {
	m.c.Method("Subscribe").Call(sub, topic)
}

// Unsubscribe forwards to the controller.
func (m *EventStreamMock) Unsubscribe(sub eventstream.Subscriber, topic string) {
	m.c.Method("Unsubscribe").Call(sub, topic)
}

// Publish forwards to the controller.
func (m *EventStreamMock) Publish(topic string, msg any) {
	m.c.Method("Publish").Call(topic, msg)
}

// Broadcast forwards to the controller.
func (m *EventStreamMock) Broadcast(msg any, topics []string) {
	m.c.Method("Broadcast").Call(msg, topics)
}

// Close forwards to the controller.
func (m *EventStreamMock) Close() {
	m.c.Method("Close").Call()
}
