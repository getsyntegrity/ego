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
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/getsyntegrity/ego/eventadapter"
)

// EventAdapterMock is an eventadapter.EventAdapter backed by a go-specs
// mock.Controller. Adapt forwards its call to the controller, and the case
// declares the answer with c.Method("Adapt").Expect(...).Return(...). Build the
// controller with mock.NewController(ctx) so its expectations are verified when
// the case ends.
type EventAdapterMock struct{ c *mock.Controller }

var _ eventadapter.EventAdapter = (*EventAdapterMock)(nil)

// NewEventAdapterMock returns an EventAdapterMock that forwards to c.
func NewEventAdapterMock(c *mock.Controller) *EventAdapterMock { return &EventAdapterMock{c: c} }

// Adapt forwards to the controller. Its results are the adapted event and the
// error, in that order.
func (m *EventAdapterMock) Adapt(event *anypb.Any, revision uint64) (*anypb.Any, error) {
	r := m.c.Method("Adapt").Call(event, revision)
	return mock.Value[*anypb.Any](r, 0), r.Err(1)
}
