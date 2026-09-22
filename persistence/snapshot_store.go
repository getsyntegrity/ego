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

package persistence

import (
	"context"

	"github.com/pablogore/ego/v4/egopb"
)

// SnapshotStore defines the API to persist and retrieve entity state snapshots.
// Snapshots are used to speed up recovery of event-sourced entities by avoiding
// full event replay from the beginning of time.
//
// # Breaking change: every record-addressing method gained a required Scope parameter
//
// TENANT-003 (T2) changed every record-addressing method's signature to
// take a persistence.Scope as the parameter immediately after ctx:
//
//	WriteSnapshot(ctx, snapshot *egopb.Snapshot) error ->
//	WriteSnapshot(ctx, scope Scope, snapshot *egopb.Snapshot) error
//	GetLatestSnapshot(ctx, persistenceID string) (*egopb.Snapshot, error) ->
//	GetLatestSnapshot(ctx, scope Scope, persistenceID string) (*egopb.Snapshot, error)
//	DeleteSnapshots(ctx, persistenceID string, toSequenceNumber uint64) error ->
//	DeleteSnapshots(ctx, scope Scope, persistenceID string, toSequenceNumber uint64) error
//
// Any external implementation of SnapshotStore fails to compile against this
// interface. Manual verification: temporarily assign a value of such an
// implementation to a var of type SnapshotStore (e.g.
// var _ SnapshotStore = (*oldStyleStore)(nil) where oldStyleStore's methods
// still take the pre-TENANT-003 argument lists) and observe the compiler
// reject it with "missing method WriteSnapshot" / a wrong-signature error
// for each changed method.
//
// Upgrade recipe: accept the new scope Scope parameter and include it
// structurally in the record key (e.g. a Go map or table key built from
// the pair (scope, persistenceID), never from Scope.String() or a
// persistenceID transformation — see persistence.Scope's doc comment). An
// adapter that wants pre-TENANT-003 behavior for existing rows maps
// Unscoped() to its current key layout unchanged, so no data migration is
// needed for non-tenant deployments: a store that always receives
// Unscoped() behaves exactly as it did before Scope existed.
type SnapshotStore interface {
	// Connect connects to the snapshot store
	Connect(ctx context.Context) error
	// Disconnect disconnects the snapshot store
	Disconnect(ctx context.Context) error
	// Ping verifies a connection to the snapshot store is still alive, establishing a connection
	// if necessary. Deliberately unscoped: connection lifecycle is not record-addressing, so it
	// carries no tenant boundary.
	Ping(ctx context.Context) error
	// WriteSnapshot persists a snapshot for a given (scope, persistenceID). scope and
	// persistenceID together form the record's effective identity: an invalid (zero-value) scope
	// returns ErrInvalidScope and nothing is written, and a write performed in one scope MUST NOT
	// modify a record that belongs to another scope.
	WriteSnapshot(ctx context.Context, scope Scope, snapshot *egopb.Snapshot) error
	// GetLatestSnapshot fetches the latest snapshot for a given (scope, persistenceID).
	// Returns nil when no snapshot is found. scope and persistenceID together form the record's
	// effective identity: an invalid (zero-value) scope returns ErrInvalidScope and nothing is
	// read, and a read performed in one scope MUST NOT return a record that belongs to another
	// scope.
	GetLatestSnapshot(ctx context.Context, scope Scope, persistenceID string) (*egopb.Snapshot, error)
	// DeleteSnapshots deletes all snapshots for a given (scope, persistenceID) up to a given
	// sequence number (inclusive). scope and persistenceID together form the record's effective
	// identity: an invalid (zero-value) scope returns ErrInvalidScope and nothing is deleted, and
	// a delete performed in one scope MUST NOT affect a record in another scope.
	DeleteSnapshots(ctx context.Context, scope Scope, persistenceID string, toSequenceNumber uint64) error
}
