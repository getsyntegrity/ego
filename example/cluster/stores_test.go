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

package main

// These tests exercise PostgresEventStore's argument validation without a
// database: every scoped method MUST reject an invalid persistence.Scope or
// persistence.WritePrecondition (and a conditional batch that violates the
// single-persistence-id rule) before it ever touches s.pool. A
// *PostgresEventStore with a nil pool is used deliberately: if validation
// were to fall through to a query, these tests would panic on the nil pool
// instead of returning the expected sentinel error, making the ordering
// itself testable.

import (
	"context"
	"testing"

	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/persistence"
)

// unvalidatedStore returns a *PostgresEventStore with a nil pool, so any test
// that reaches an actual query panics instead of silently passing.
func unvalidatedStore() *PostgresEventStore {
	return &PostgresEventStore{}
}

func singleEvent(persistenceID string, sequenceNumber uint64) *egopb.Event {
	return &egopb.Event{PersistenceId: persistenceID, SequenceNumber: sequenceNumber}
}

func TestPostgresEventStore_WriteEvents_InvalidScope(t *testing.T) {
	store := unvalidatedStore()
	err := store.WriteEvents(context.Background(), persistence.Scope{}, []*egopb.Event{singleEvent("a", 1)}, persistence.Unconditional())
	mustErrorIs(t, err, persistence.ErrInvalidScope)
}

func TestPostgresEventStore_WriteEvents_InvalidPrecondition(t *testing.T) {
	store := unvalidatedStore()
	err := store.WriteEvents(context.Background(), persistence.Unscoped(), []*egopb.Event{singleEvent("a", 1)}, persistence.WritePrecondition{})
	mustErrorIs(t, err, persistence.ErrInvalidPrecondition)
}

func TestPostgresEventStore_WriteEvents_EmptyBatchConditional(t *testing.T) {
	store := unvalidatedStore()
	err := store.WriteEvents(context.Background(), persistence.Unscoped(), nil, persistence.ExpectGenesis())
	mustErrorIs(t, err, persistence.ErrPreconditionScope)
}

func TestPostgresEventStore_WriteEvents_MixedIDBatchConditional(t *testing.T) {
	store := unvalidatedStore()
	events := []*egopb.Event{singleEvent("a", 1), singleEvent("b", 2)}
	err := store.WriteEvents(context.Background(), persistence.Unscoped(), events, persistence.ExpectRevision(1))
	mustErrorIs(t, err, persistence.ErrPreconditionScope)
}

func TestPostgresEventStore_WriteEvents_EmptyBatchUnconditionalSucceeds(t *testing.T) {
	// Unconditional() never declares a per-persistence-id expectation, so an
	// empty batch is a legitimate no-op rather than ErrPreconditionScope. This
	// also exercises the nil-pool guard: writeUnconditional must return before
	// touching s.pool for an empty batch.
	store := unvalidatedStore()
	err := store.WriteEvents(context.Background(), persistence.Unscoped(), nil, persistence.Unconditional())
	mustNoError(t, err)
}

func TestPostgresEventStore_DeleteEvents_InvalidScope(t *testing.T) {
	store := unvalidatedStore()
	err := store.DeleteEvents(context.Background(), persistence.Scope{}, "a", 1)
	mustErrorIs(t, err, persistence.ErrInvalidScope)
}

func TestPostgresEventStore_ReplayEvents_InvalidScope(t *testing.T) {
	store := unvalidatedStore()
	events, err := store.ReplayEvents(context.Background(), persistence.Scope{}, "a", 1, 10, 10)
	mustErrorIs(t, err, persistence.ErrInvalidScope)
	mustNil(t, events)
}

func TestPostgresEventStore_GetLatestEvent_InvalidScope(t *testing.T) {
	store := unvalidatedStore()
	event, err := store.GetLatestEvent(context.Background(), persistence.Scope{}, "a")
	mustErrorIs(t, err, persistence.ErrInvalidScope)
	mustNil(t, event)
}

func TestPostgresEventStore_PersistenceIDs_InvalidScope(t *testing.T) {
	store := unvalidatedStore()
	ids, next, err := store.PersistenceIDs(context.Background(), persistence.Scope{}, 10, "")
	mustErrorIs(t, err, persistence.ErrInvalidScope)
	mustNil(t, ids)
	mustEmpty(t, next)
}

// A zero pageSize is a degenerate page: it lists nothing and returns an empty
// nextPageToken, so an iteration that passes it terminates instead of looping
// on the same empty page. This mirrors testkit's in-memory EventStore. The
// store's nil pool proves no query runs.
func TestPostgresEventStore_PersistenceIDs_ZeroPageSize(t *testing.T) {
	store := unvalidatedStore()
	ids, next, err := store.PersistenceIDs(context.Background(), persistence.Unscoped(), 0, "")
	mustNoError(t, err)
	mustEmpty(t, ids)
	mustEmpty(t, next)

	ids, next, err = store.PersistenceIDs(context.Background(), persistence.Unscoped(), 0, "some-id")
	mustNoError(t, err)
	mustEmpty(t, ids)
	mustEmpty(t, next)
}

func TestPostgresEventStore_ImplementsEventsStore(t *testing.T) {
	var _ persistence.EventsStore = (*PostgresEventStore)(nil)
}
