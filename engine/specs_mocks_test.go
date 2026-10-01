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

package engine

import (
	"context"

	"github.com/getsyntegrity/go-specs/mock"

	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/offsetstore"
	"github.com/getsyntegrity/ego/persistence"
	"github.com/getsyntegrity/ego/port/publishing"
)

// This file holds the typed go-specs mock adapters used by the engine unit
// tests. Each adapter implements one port and forwards every method to a
// mock.Controller, so a case declares its expectations on the controller and
// the controller verifies them when the case ends. An adapter forwards every
// method of its port, so a call the case did not declare is reported as an
// unexpected call instead of reaching a nil interface.
//
// They are test-local on purpose: equivalent adapters exist on the unmerged
// refactor/engine-test-seams branch (internal/engine/enginetest). Folding both
// sets into one is a follow-up, see odd/tasks/engine-rest-go-specs-v033.md.

var (
	_ persistence.EventsStore   = eventsStoreMock{}
	_ persistence.SnapshotStore = snapshotStoreMock{}
	_ persistence.StateStore    = stateStoreMock{}
	_ offsetstore.OffsetStore   = offsetStoreMock{}
	_ publishing.EventPublisher = eventPublisherMock{}
	_ publishing.StatePublisher = statePublisherMock{}
)

type eventsStoreMock struct{ c *mock.Controller }

func (m eventsStoreMock) Connect(ctx context.Context) error {
	return m.c.Method("Connect").Call(ctx).Err(0)
}

func (m eventsStoreMock) Disconnect(ctx context.Context) error {
	return m.c.Method("Disconnect").Call(ctx).Err(0)
}

func (m eventsStoreMock) Ping(ctx context.Context) error {
	return m.c.Method("Ping").Call(ctx).Err(0)
}

func (m eventsStoreMock) WriteEvents(ctx context.Context, scope persistence.Scope, events []*egopb.Event,
	precondition persistence.WritePrecondition) error {
	return m.c.Method("WriteEvents").Call(ctx, scope, events, precondition).Err(0)
}

func (m eventsStoreMock) DeleteEvents(ctx context.Context, scope persistence.Scope, persistenceID string,
	toSequenceNumber uint64) error {
	return m.c.Method("DeleteEvents").Call(ctx, scope, persistenceID, toSequenceNumber).Err(0)
}

func (m eventsStoreMock) ReplayEvents(ctx context.Context, scope persistence.Scope, persistenceID string,
	fromSequenceNumber, toSequenceNumber uint64, limit uint64) ([]*egopb.Event, error) {
	r := m.c.Method("ReplayEvents").Call(ctx, scope, persistenceID, fromSequenceNumber, toSequenceNumber, limit)
	return mock.Value[[]*egopb.Event](r, 0), r.Err(1)
}

func (m eventsStoreMock) GetLatestEvent(ctx context.Context, scope persistence.Scope, persistenceID string) (*egopb.Event, error) {
	r := m.c.Method("GetLatestEvent").Call(ctx, scope, persistenceID)
	return mock.Value[*egopb.Event](r, 0), r.Err(1)
}

func (m eventsStoreMock) PersistenceIDs(ctx context.Context, scope persistence.Scope, pageSize uint64,
	pageToken string) ([]string, string, error) {
	r := m.c.Method("PersistenceIDs").Call(ctx, scope, pageSize, pageToken)
	return mock.Value[[]string](r, 0), mock.Value[string](r, 1), r.Err(2)
}

func (m eventsStoreMock) GetShardEvents(ctx context.Context, shardNumber uint64, offset int64,
	limit uint64) ([]*egopb.Event, int64, error) {
	r := m.c.Method("GetShardEvents").Call(ctx, shardNumber, offset, limit)
	return mock.Value[[]*egopb.Event](r, 0), mock.Value[int64](r, 1), r.Err(2)
}

func (m eventsStoreMock) ShardOffsets(ctx context.Context) (map[uint64]int64, error) {
	r := m.c.Method("ShardOffsets").Call(ctx)
	return mock.Value[map[uint64]int64](r, 0), r.Err(1)
}

type snapshotStoreMock struct{ c *mock.Controller }

func (m snapshotStoreMock) Connect(ctx context.Context) error {
	return m.c.Method("Connect").Call(ctx).Err(0)
}

func (m snapshotStoreMock) Disconnect(ctx context.Context) error {
	return m.c.Method("Disconnect").Call(ctx).Err(0)
}

func (m snapshotStoreMock) Ping(ctx context.Context) error {
	return m.c.Method("Ping").Call(ctx).Err(0)
}

func (m snapshotStoreMock) WriteSnapshot(ctx context.Context, scope persistence.Scope, snapshot *egopb.Snapshot) error {
	return m.c.Method("WriteSnapshot").Call(ctx, scope, snapshot).Err(0)
}

func (m snapshotStoreMock) GetLatestSnapshot(ctx context.Context, scope persistence.Scope, persistenceID string) (*egopb.Snapshot, error) {
	r := m.c.Method("GetLatestSnapshot").Call(ctx, scope, persistenceID)
	return mock.Value[*egopb.Snapshot](r, 0), r.Err(1)
}

func (m snapshotStoreMock) DeleteSnapshots(ctx context.Context, scope persistence.Scope, persistenceID string,
	toSequenceNumber uint64) error {
	return m.c.Method("DeleteSnapshots").Call(ctx, scope, persistenceID, toSequenceNumber).Err(0)
}

type stateStoreMock struct{ c *mock.Controller }

func (m stateStoreMock) Connect(ctx context.Context) error {
	return m.c.Method("Connect").Call(ctx).Err(0)
}

func (m stateStoreMock) Disconnect(ctx context.Context) error {
	return m.c.Method("Disconnect").Call(ctx).Err(0)
}

func (m stateStoreMock) Ping(ctx context.Context) error {
	return m.c.Method("Ping").Call(ctx).Err(0)
}

func (m stateStoreMock) WriteState(ctx context.Context, scope persistence.Scope, state *egopb.DurableState,
	precondition persistence.WritePrecondition) error {
	return m.c.Method("WriteState").Call(ctx, scope, state, precondition).Err(0)
}

func (m stateStoreMock) GetLatestState(ctx context.Context, scope persistence.Scope, persistenceID string) (*egopb.DurableState, error) {
	r := m.c.Method("GetLatestState").Call(ctx, scope, persistenceID)
	return mock.Value[*egopb.DurableState](r, 0), r.Err(1)
}

type offsetStoreMock struct{ c *mock.Controller }

func (m offsetStoreMock) Connect(ctx context.Context) error {
	return m.c.Method("Connect").Call(ctx).Err(0)
}

func (m offsetStoreMock) Disconnect(ctx context.Context) error {
	return m.c.Method("Disconnect").Call(ctx).Err(0)
}

func (m offsetStoreMock) Ping(ctx context.Context) error {
	return m.c.Method("Ping").Call(ctx).Err(0)
}

func (m offsetStoreMock) WriteOffset(ctx context.Context, offset *egopb.Offset) error {
	return m.c.Method("WriteOffset").Call(ctx, offset).Err(0)
}

func (m offsetStoreMock) GetCurrentOffset(ctx context.Context, projectionID *egopb.ProjectionId) (*egopb.Offset, error) {
	r := m.c.Method("GetCurrentOffset").Call(ctx, projectionID)
	return mock.Value[*egopb.Offset](r, 0), r.Err(1)
}

func (m offsetStoreMock) ResetOffset(ctx context.Context, projectionName string, value int64) error {
	return m.c.Method("ResetOffset").Call(ctx, projectionName, value).Err(0)
}

type eventPublisherMock struct{ c *mock.Controller }

func (m eventPublisherMock) ID() string {
	return mock.Value[string](m.c.Method("ID").Call(), 0)
}

func (m eventPublisherMock) Publish(ctx context.Context, event *egopb.Event) error {
	return m.c.Method("Publish").Call(ctx, event).Err(0)
}

func (m eventPublisherMock) Close(ctx context.Context) error {
	return m.c.Method("Close").Call(ctx).Err(0)
}

type statePublisherMock struct{ c *mock.Controller }

func (m statePublisherMock) ID() string {
	return mock.Value[string](m.c.Method("ID").Call(), 0)
}

func (m statePublisherMock) Publish(ctx context.Context, state *egopb.DurableState) error {
	return m.c.Method("Publish").Call(ctx, state).Err(0)
}

func (m statePublisherMock) Close(ctx context.Context) error {
	return m.c.Method("Close").Call(ctx).Err(0)
}

// Argument matchers that stand for "any non-nil message of this type".
var (
	anEvent = mock.MatchT("an event", func(e *egopb.Event) bool { return e != nil })
	aState  = mock.MatchT("a durable state", func(s *egopb.DurableState) bool { return s != nil })
)
