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

// SnapshotStoreMock is a persistence.SnapshotStore backed by a go-specs
// mock.Controller. Every method forwards its call to the controller under the
// method's own name, and the case declares the answer with
// c.Method("Name").Expect(...).Return(...). Build the controller with
// mock.NewController(ctx) so its expectations are verified when the case ends.
type SnapshotStoreMock struct{ c *mock.Controller }

var _ persistence.SnapshotStore = (*SnapshotStoreMock)(nil)

// NewSnapshotStoreMock returns a SnapshotStoreMock that forwards to c.
func NewSnapshotStoreMock(c *mock.Controller) *SnapshotStoreMock { return &SnapshotStoreMock{c: c} }

// Connect forwards to the controller.
func (m *SnapshotStoreMock) Connect(ctx context.Context) error {
	return m.c.Method("Connect").Call(ctx).Err(0)
}

// Disconnect forwards to the controller.
func (m *SnapshotStoreMock) Disconnect(ctx context.Context) error {
	return m.c.Method("Disconnect").Call(ctx).Err(0)
}

// Ping forwards to the controller.
func (m *SnapshotStoreMock) Ping(ctx context.Context) error {
	return m.c.Method("Ping").Call(ctx).Err(0)
}

// WriteSnapshot forwards to the controller.
func (m *SnapshotStoreMock) WriteSnapshot(ctx context.Context, scope persistence.Scope, snapshot *egopb.Snapshot) error {
	return m.c.Method("WriteSnapshot").Call(ctx, scope, snapshot).Err(0)
}

// GetLatestSnapshot forwards to the controller.
func (m *SnapshotStoreMock) GetLatestSnapshot(ctx context.Context, scope persistence.Scope, persistenceID string) (*egopb.Snapshot, error) {
	r := m.c.Method("GetLatestSnapshot").Call(ctx, scope, persistenceID)
	return mock.Value[*egopb.Snapshot](r, 0), r.Err(1)
}

// DeleteSnapshots forwards to the controller.
func (m *SnapshotStoreMock) DeleteSnapshots(ctx context.Context, scope persistence.Scope, persistenceID string, toSequenceNumber uint64) error {
	return m.c.Method("DeleteSnapshots").Call(ctx, scope, persistenceID, toSequenceNumber).Err(0)
}
