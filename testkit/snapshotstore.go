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

package testkit

import (
	"context"
	"sort"
	"sync"

	"go.uber.org/atomic"

	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/persistence"
)

// SnapshotKey is a composite key for snapshot storage. Scope and
// PersistenceID together form the record's effective identity (see
// persistence.Scope's doc comment); SequenceNumber distinguishes multiple
// snapshots retained for the same (Scope, PersistenceID). This struct is
// used directly as the key of SnapshotStore's internal map — never a
// Scope.String()-derived or concatenated string — since persistence.Scope
// is a comparable value type.
type SnapshotKey struct {
	Scope          persistence.Scope
	PersistenceID  string
	SequenceNumber uint64
}

// SnapshotStore is an in-memory implementation of persistence.SnapshotStore for testing
type SnapshotStore struct {
	db        *sync.Map
	connected *atomic.Bool
}

var _ persistence.SnapshotStore = (*SnapshotStore)(nil)

// NewSnapshotStore creates an instance of SnapshotStore
func NewSnapshotStore() *SnapshotStore {
	return &SnapshotStore{
		db:        &sync.Map{},
		connected: atomic.NewBool(false),
	}
}

func (x *SnapshotStore) Connect(_ context.Context) error {
	if x.connected.Load() {
		return nil
	}
	x.connected.Store(true)
	return nil
}

func (x *SnapshotStore) Disconnect(_ context.Context) error {
	if !x.connected.Load() {
		return nil
	}
	x.db.Range(func(key interface{}, _ interface{}) bool {
		x.db.Delete(key)
		return true
	})
	x.connected.Store(false)
	return nil
}

func (x *SnapshotStore) Ping(ctx context.Context) error {
	if !x.connected.Load() {
		return x.Connect(ctx)
	}
	return nil
}

// WriteSnapshot persists a snapshot for a given (scope, persistenceID). An invalid scope is
// rejected with ErrInvalidScope before anything is written.
func (x *SnapshotStore) WriteSnapshot(_ context.Context, scope persistence.Scope, snapshot *egopb.Snapshot) error {
	if !scope.Valid() {
		return persistence.ErrInvalidScope
	}
	key := SnapshotKey{
		Scope:          scope,
		PersistenceID:  snapshot.GetPersistenceId(),
		SequenceNumber: snapshot.GetSequenceNumber(),
	}
	x.db.Store(key, snapshot)
	return nil
}

// GetLatestSnapshot fetches the latest snapshot for (scope, persistenceID). An invalid scope is
// rejected with ErrInvalidScope before anything is read, and this never returns a record that
// belongs to another scope.
func (x *SnapshotStore) GetLatestSnapshot(_ context.Context, scope persistence.Scope, persistenceID string) (*egopb.Snapshot, error) {
	if !scope.Valid() {
		return nil, persistence.ErrInvalidScope
	}
	var snapshots []*egopb.Snapshot
	x.db.Range(func(key any, value any) bool {
		k := key.(SnapshotKey)
		if k.Scope.Equal(scope) && k.PersistenceID == persistenceID {
			snapshots = append(snapshots, value.(*egopb.Snapshot))
		}
		return true
	})

	if len(snapshots) == 0 {
		return nil, nil
	}

	sort.SliceStable(snapshots, func(i, j int) bool {
		return snapshots[i].GetSequenceNumber() > snapshots[j].GetSequenceNumber()
	})

	return snapshots[0], nil
}

// DeleteSnapshots deletes all snapshots for (scope, persistenceID) up to toSequenceNumber
// (inclusive). An invalid scope is rejected with ErrInvalidScope before anything is deleted, and
// this never affects a record in another scope.
func (x *SnapshotStore) DeleteSnapshots(_ context.Context, scope persistence.Scope, persistenceID string, toSequenceNumber uint64) error {
	if !scope.Valid() {
		return persistence.ErrInvalidScope
	}
	x.db.Range(func(key interface{}, _ any) bool {
		k := key.(SnapshotKey)
		if k.Scope.Equal(scope) && k.PersistenceID == persistenceID && k.SequenceNumber <= toSequenceNumber {
			x.db.Delete(key)
		}
		return true
	})
	return nil
}
