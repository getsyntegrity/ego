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
	"errors"
	"sync"

	"go.uber.org/atomic"

	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/persistence"
)

// durableStoreKey is the structural (scope, persistenceID) pair DurableStore
// keys its internal map by. persistence.Scope is a comparable value type
// (see its doc comment), so this struct is directly usable as a map key; no
// field is ever derived from Scope.String() or from concatenating scope and
// persistenceID into one string.
type durableStoreKey struct {
	scope         persistence.Scope
	persistenceID string
}

type DurableStore struct {
	// db maps durableStoreKey{scope, persistenceID} -> *egopb.DurableState.
	db        *sync.Map
	connected *atomic.Bool
}

// enforce compilation error
var _ persistence.StateStore = (*DurableStore)(nil)

// NewDurableStore creates an instance DurableStore
func NewDurableStore() *DurableStore {
	return &DurableStore{
		db:        &sync.Map{},
		connected: atomic.NewBool(false),
	}
}

// Connect connects the durable store
// nolint
func (d *DurableStore) Connect(ctx context.Context) error {
	if d.connected.Load() {
		return nil
	}
	d.connected.Store(true)
	return nil
}

// Disconnect disconnect the durable store
// nolint
func (d *DurableStore) Disconnect(ctx context.Context) error {
	if !d.connected.Load() {
		return nil
	}
	d.db.Range(func(key interface{}, value interface{}) bool {
		d.db.Delete(key)
		return true
	})
	d.connected.Store(false)
	return nil
}

// Ping verifies a connection to the database is still alive, establishing a connection if necessary.
func (d *DurableStore) Ping(ctx context.Context) error {
	if !d.connected.Load() {
		return d.Connect(ctx)
	}
	return nil
}

// WriteState persist durable state for a given (scope, persistenceID), subject to precondition.
// See persistence.StateStore for the full contract. The conditional path is decided by a single
// sync.Map CompareAndSwap (exact-revision) or LoadOrStore (genesis) attempt against the store
// itself: a failed attempt is a terminal conflict, never retried, since retrying would silently
// convert a declared, no-longer-valid expectation into success. An invalid scope is rejected with
// ErrInvalidScope before anything else is validated or touched.
// nolint
func (d *DurableStore) WriteState(_ context.Context, scope persistence.Scope, state *egopb.DurableState, precondition persistence.WritePrecondition) error {
	if !d.connected.Load() {
		return errors.New("durable store is not connected")
	}
	if !scope.Valid() {
		return persistence.ErrInvalidScope
	}
	if !precondition.Valid() {
		return persistence.ErrInvalidPrecondition
	}

	persistenceID := state.GetPersistenceId()
	key := durableStoreKey{scope: scope, persistenceID: persistenceID}

	if precondition.IsUnconditional() {
		d.db.Store(key, state)
		return nil
	}

	raw, exists := d.db.Load(key)
	var current *egopb.DurableState
	if exists {
		current = raw.(*egopb.DurableState)
	}

	if precondition.IsGenesis() {
		if exists {
			return persistence.NewConflictError(scope, persistenceID, precondition, persistence.WithActualRevision(current.GetVersionNumber()))
		}
		if actual, loaded := d.db.LoadOrStore(key, state); loaded {
			return persistence.NewConflictError(scope, persistenceID, precondition, persistence.WithActualRevision(actual.(*egopb.DurableState).GetVersionNumber()))
		}
		return nil
	}

	expectedRevision, _ := precondition.Revision()
	if !exists || current.GetVersionNumber() != expectedRevision {
		if exists {
			return persistence.NewConflictError(scope, persistenceID, precondition, persistence.WithActualRevision(current.GetVersionNumber()))
		}
		return persistence.NewConflictError(scope, persistenceID, precondition)
	}

	if !d.db.CompareAndSwap(key, current, state) {
		if actual, ok := d.db.Load(key); ok {
			return persistence.NewConflictError(scope, persistenceID, precondition, persistence.WithActualRevision(actual.(*egopb.DurableState).GetVersionNumber()))
		}
		return persistence.NewConflictError(scope, persistenceID, precondition)
	}
	return nil
}

// GetLatestState fetches the latest durable state for (scope, persistenceID). An invalid scope is
// rejected with ErrInvalidScope before anything is read, and this never returns a record that
// belongs to another scope.
// nolint
func (d *DurableStore) GetLatestState(_ context.Context, scope persistence.Scope, persistenceID string) (*egopb.DurableState, error) {
	if !d.connected.Load() {
		return nil, errors.New("durable store is not connected")
	}
	if !scope.Valid() {
		return nil, persistence.ErrInvalidScope
	}
	value, ok := d.db.Load(durableStoreKey{scope: scope, persistenceID: persistenceID})
	if !ok {
		return nil, nil
	}
	return value.(*egopb.DurableState), nil
}
