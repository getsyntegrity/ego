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

// EventsStore defines the API to write to the events store
//
// # Breaking change: WriteEvents gained a required precondition parameter
//
// WRITE-004 changed WriteEvents's signature from
// WriteEvents(ctx, events []*egopb.Event) error to
// WriteEvents(ctx, events []*egopb.Event, precondition WritePrecondition) error
// (design.md M-1/M-4). Any external implementation of EventsStore written
// against the pre-WRITE-004 signature fails to compile against this
// interface. Manual verification: temporarily assign a value of such an
// implementation to a var of type EventsStore (e.g.
// var _ EventsStore = (*oldStyleStore)(nil) where oldStyleStore's
// WriteEvents takes only (ctx, events)) and observe the compiler reject it
// with "missing method WriteEvents" / a wrong-signature error. M-3 documents
// the upgrade recipe.
type EventsStore interface {
	// Connect connects to the journal store
	Connect(ctx context.Context) error
	// Disconnect disconnect the journal store
	Disconnect(ctx context.Context) error
	// WriteEvents persist event in batches for a given persistenceID, subject to precondition.
	// Note: persistence id and the sequence number make a record in the journal store unique. Failure to ensure that
	// can lead to some un-wanted behaviors and data inconsistency
	//
	// The precondition is evaluated against the persisted revision for the batch's target
	// persistenceID (see WritePrecondition) and the batch is committed as one atomic operation:
	// there is no observable window in which another writer's commit can interleave between the
	// precondition check and the commit. Implementations MUST NOT implement this as a separate
	// read-then-compare followed by an unconditional write.
	//
	// When precondition is not Unconditional(), every event in events MUST share one
	// PersistenceId (read from events[0]); a batch that violates this, including an empty slice,
	// returns ErrPreconditionScope. An invalid precondition (the zero value of WritePrecondition)
	// returns ErrInvalidPrecondition. When the precondition does not hold against the persisted
	// revision, no event in the batch is committed and a *ConflictError is returned, identifiable
	// via errors.As or errors.Is(err, ErrConcurrencyConflict).
	WriteEvents(ctx context.Context, events []*egopb.Event, precondition WritePrecondition) error
	// Ping verifies a connection to the database is still alive, establishing a connection if necessary.
	Ping(ctx context.Context) error
	// DeleteEvents deletes store from the store up to a given sequence number (inclusive)
	DeleteEvents(ctx context.Context, persistenceID string, toSequenceNumber uint64) error
	// ReplayEvents fetches store for a given persistence ID from a given sequence number(inclusive) to a given sequence number(inclusive) with a maximum of journals to be replayed.
	ReplayEvents(ctx context.Context, persistenceID string, fromSequenceNumber, toSequenceNumber uint64, limit uint64) ([]*egopb.Event, error)
	// GetLatestEvent fetches the latest event
	GetLatestEvent(ctx context.Context, persistenceID string) (*egopb.Event, error)
	// PersistenceIDs returns the distinct list of all the persistence ids in the journal store
	PersistenceIDs(ctx context.Context, pageSize uint64, pageToken string) (persistenceIDs []string, nextPageToken string, err error)
	// GetShardEvents returns the next (limit) events after the offset in the journal for a given shard
	GetShardEvents(ctx context.Context, shardNumber uint64, offset int64, limit uint64) ([]*egopb.Event, int64, error)
	// ShardOffsets returns every distinct shard in the journal mapped to the
	// offset (timestamp) of its most recent event. Compared against the
	// offsets a projection has committed, it tells which shards have pending
	// events without scanning every shard. An empty journal yields an empty
	// map. SQL-backed stores implement it with a single query:
	//
	//	SELECT shard_number, MAX(timestamp) FROM events_store GROUP BY shard_number
	ShardOffsets(ctx context.Context) (map[uint64]int64, error)
}
