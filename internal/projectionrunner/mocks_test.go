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

package projectionrunner

import (
	"context"

	"github.com/getsyntegrity/go-specs/mock"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/persistence"
)

// The adapters below let a go-specs mock.Controller stand in for the runner's
// collaborators. Each method forwards its call to the controller, and the
// case declares what the call answers with ctrl.Method("Name").Expect(...).
// A controller is created per case with mock.NewController(ctx), so its
// expectations are verified when the case ends.

// offsetStoreMock implements offsetstore.OffsetStore.
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

// eventsStoreMock implements persistence.EventsStore.
type eventsStoreMock struct{ c *mock.Controller }

func (m eventsStoreMock) Connect(ctx context.Context) error {
	return m.c.Method("Connect").Call(ctx).Err(0)
}

func (m eventsStoreMock) Disconnect(ctx context.Context) error {
	return m.c.Method("Disconnect").Call(ctx).Err(0)
}

func (m eventsStoreMock) WriteEvents(ctx context.Context, scope persistence.Scope, events []*egopb.Event, precondition persistence.WritePrecondition) error {
	return m.c.Method("WriteEvents").Call(ctx, scope, events, precondition).Err(0)
}

func (m eventsStoreMock) Ping(ctx context.Context) error {
	return m.c.Method("Ping").Call(ctx).Err(0)
}

func (m eventsStoreMock) DeleteEvents(ctx context.Context, scope persistence.Scope, persistenceID string, toSequenceNumber uint64) error {
	return m.c.Method("DeleteEvents").Call(ctx, scope, persistenceID, toSequenceNumber).Err(0)
}

func (m eventsStoreMock) ReplayEvents(ctx context.Context, scope persistence.Scope, persistenceID string, fromSequenceNumber, toSequenceNumber, limit uint64) ([]*egopb.Event, error) {
	r := m.c.Method("ReplayEvents").Call(ctx, scope, persistenceID, fromSequenceNumber, toSequenceNumber, limit)
	return mock.Value[[]*egopb.Event](r, 0), r.Err(1)
}

func (m eventsStoreMock) GetLatestEvent(ctx context.Context, scope persistence.Scope, persistenceID string) (*egopb.Event, error) {
	r := m.c.Method("GetLatestEvent").Call(ctx, scope, persistenceID)
	return mock.Value[*egopb.Event](r, 0), r.Err(1)
}

func (m eventsStoreMock) PersistenceIDs(ctx context.Context, scope persistence.Scope, pageSize uint64, pageToken string) ([]string, string, error) {
	r := m.c.Method("PersistenceIDs").Call(ctx, scope, pageSize, pageToken)
	return mock.Value[[]string](r, 0), mock.Value[string](r, 1), r.Err(2)
}

func (m eventsStoreMock) GetShardEvents(ctx context.Context, shardNumber uint64, offset int64, limit uint64) ([]*egopb.Event, int64, error) {
	r := m.c.Method("GetShardEvents").Call(ctx, shardNumber, offset, limit)
	return mock.Value[[]*egopb.Event](r, 0), mock.Value[int64](r, 1), r.Err(2)
}

func (m eventsStoreMock) ShardOffsets(ctx context.Context) (map[uint64]int64, error) {
	r := m.c.Method("ShardOffsets").Call(ctx)
	return mock.Value[map[uint64]int64](r, 0), r.Err(1)
}

// encryptorMock implements encryption.Encryptor.
type encryptorMock struct{ c *mock.Controller }

func (m encryptorMock) Encrypt(ctx context.Context, persistenceID string, plaintext []byte) ([]byte, string, error) {
	r := m.c.Method("Encrypt").Call(ctx, persistenceID, plaintext)
	return mock.Value[[]byte](r, 0), mock.Value[string](r, 1), r.Err(2)
}

func (m encryptorMock) Decrypt(ctx context.Context, persistenceID string, ciphertext []byte, keyID string) ([]byte, error) {
	r := m.c.Method("Decrypt").Call(ctx, persistenceID, ciphertext, keyID)
	return mock.Value[[]byte](r, 0), r.Err(1)
}

// eventAdapterMock implements eventadapter.EventAdapter.
type eventAdapterMock struct{ c *mock.Controller }

func (m eventAdapterMock) Adapt(event *anypb.Any, revision uint64) (*anypb.Any, error) {
	r := m.c.Method("Adapt").Call(event, revision)
	return mock.Value[*anypb.Any](r, 0), r.Err(1)
}
