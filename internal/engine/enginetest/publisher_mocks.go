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
	"context"

	"github.com/getsyntegrity/go-specs/mock"

	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/port/publishing"
)

// EventPublisherMock is a publishing.EventPublisher backed by a go-specs
// mock.Controller. Every method forwards its call to the controller under the
// method's own name, and the case declares the answer with
// c.Method("Name").Expect(...).Return(...). Build the controller with
// mock.NewController(ctx) so its expectations are verified when the case ends.
type EventPublisherMock struct{ c *mock.Controller }

var _ publishing.EventPublisher = (*EventPublisherMock)(nil)

// NewEventPublisherMock returns an EventPublisherMock that forwards to c.
func NewEventPublisherMock(c *mock.Controller) *EventPublisherMock {
	return &EventPublisherMock{c: c}
}

// ID forwards to the controller.
func (m *EventPublisherMock) ID() string {
	return mock.Value[string](m.c.Method("ID").Call(), 0)
}

// Publish forwards to the controller.
func (m *EventPublisherMock) Publish(ctx context.Context, event *egopb.Event) error {
	return m.c.Method("Publish").Call(ctx, event).Err(0)
}

// Close forwards to the controller.
func (m *EventPublisherMock) Close(ctx context.Context) error {
	return m.c.Method("Close").Call(ctx).Err(0)
}

// StatePublisherMock is a publishing.StatePublisher backed by a go-specs
// mock.Controller, with the same forwarding rules as EventPublisherMock.
type StatePublisherMock struct{ c *mock.Controller }

var _ publishing.StatePublisher = (*StatePublisherMock)(nil)

// NewStatePublisherMock returns a StatePublisherMock that forwards to c.
func NewStatePublisherMock(c *mock.Controller) *StatePublisherMock {
	return &StatePublisherMock{c: c}
}

// ID forwards to the controller.
func (m *StatePublisherMock) ID() string {
	return mock.Value[string](m.c.Method("ID").Call(), 0)
}

// Publish forwards to the controller.
func (m *StatePublisherMock) Publish(ctx context.Context, state *egopb.DurableState) error {
	return m.c.Method("Publish").Call(ctx, state).Err(0)
}

// Close forwards to the controller.
func (m *StatePublisherMock) Close(ctx context.Context) error {
	return m.c.Method("Close").Call(ctx).Err(0)
}
