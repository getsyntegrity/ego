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
	"github.com/getsyntegrity/ego/persistence"
)

// StateStoreMock is a persistence.StateStore backed by a go-specs
// mock.Controller. Every method forwards its call to the controller under the
// method's own name, and the case declares the answer with
// c.Method("Name").Expect(...).Return(...). Build the controller with
// mock.NewController(ctx) so its expectations are verified when the case ends.
type StateStoreMock struct{ c *mock.Controller }

var _ persistence.StateStore = (*StateStoreMock)(nil)

// NewStateStoreMock returns a StateStoreMock that forwards to c.
func NewStateStoreMock(c *mock.Controller) *StateStoreMock { return &StateStoreMock{c: c} }

// Connect forwards to the controller.
func (m *StateStoreMock) Connect(ctx context.Context) error {
	return m.c.Method("Connect").Call(ctx).Err(0)
}

// Disconnect forwards to the controller.
func (m *StateStoreMock) Disconnect(ctx context.Context) error {
	return m.c.Method("Disconnect").Call(ctx).Err(0)
}

// Ping forwards to the controller.
func (m *StateStoreMock) Ping(ctx context.Context) error {
	return m.c.Method("Ping").Call(ctx).Err(0)
}

// WriteState forwards to the controller.
func (m *StateStoreMock) WriteState(ctx context.Context, scope persistence.Scope, state *egopb.DurableState, precondition persistence.WritePrecondition) error {
	return m.c.Method("WriteState").Call(ctx, scope, state, precondition).Err(0)
}

// GetLatestState forwards to the controller.
func (m *StateStoreMock) GetLatestState(ctx context.Context, scope persistence.Scope, persistenceID string) (*egopb.DurableState, error) {
	r := m.c.Method("GetLatestState").Call(ctx, scope, persistenceID)
	return mock.Value[*egopb.DurableState](r, 0), r.Err(1)
}
