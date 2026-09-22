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

// This file proves EGO-TENANT-003 T2's plumbing: every store method that now
// takes a persistence.Scope rejects the invalid zero-value Scope with
// persistence.ErrInvalidScope before touching any state, and Unscoped()
// continues to behave exactly as the pre-TENANT-003 stores did (regression
// guard). The deeper cross-tenant isolation matrix (two different valid
// scopes never seeing each other's records) is deliberately out of scope
// here — that is T3's dedicated conformance suite.
package testkit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/persistence"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
)

// ---------------------------------------------------------------------------
// EventStore: invalid scope rejection
// ---------------------------------------------------------------------------

func TestEventStore_InvalidScopeRejected(t *testing.T) {
	ctx := context.TODO()
	var invalid persistence.Scope

	anyEvent, err := anypb.New(&testpb.AccountCreated{AccountId: "acc-1", AccountBalance: 100})
	require.NoError(t, err)
	events := []*egopb.Event{
		{PersistenceId: "scope-guard", SequenceNumber: 1, Event: anyEvent, Timestamp: time.Now().UnixMilli(), Shard: 1},
	}

	t.Run("WriteEvents", func(t *testing.T) {
		store := NewEventsStore()
		require.NoError(t, store.Connect(ctx))
		err := store.WriteEvents(ctx, invalid, events, persistence.Unconditional())
		require.ErrorIs(t, err, persistence.ErrInvalidScope)

		ids, _, listErr := store.PersistenceIDs(ctx, persistence.Unscoped(), 10, "")
		require.NoError(t, listErr)
		assert.Empty(t, ids, "an invalid scope must not have written anything")
	})

	t.Run("DeleteEvents", func(t *testing.T) {
		store := NewEventsStore()
		require.NoError(t, store.Connect(ctx))
		require.NoError(t, store.WriteEvents(ctx, persistence.Unscoped(), events, persistence.Unconditional()))

		err := store.DeleteEvents(ctx, invalid, "scope-guard", 1)
		require.ErrorIs(t, err, persistence.ErrInvalidScope)

		replayed, replayErr := store.ReplayEvents(ctx, persistence.Unscoped(), "scope-guard", 1, 1, 10)
		require.NoError(t, replayErr)
		assert.Len(t, replayed, 1, "the invalid-scope delete must not have touched the unscoped record")
	})

	t.Run("ReplayEvents", func(t *testing.T) {
		store := NewEventsStore()
		require.NoError(t, store.Connect(ctx))
		require.NoError(t, store.WriteEvents(ctx, persistence.Unscoped(), events, persistence.Unconditional()))

		got, err := store.ReplayEvents(ctx, invalid, "scope-guard", 1, 1, 10)
		require.ErrorIs(t, err, persistence.ErrInvalidScope)
		assert.Nil(t, got)
	})

	t.Run("GetLatestEvent", func(t *testing.T) {
		store := NewEventsStore()
		require.NoError(t, store.Connect(ctx))
		require.NoError(t, store.WriteEvents(ctx, persistence.Unscoped(), events, persistence.Unconditional()))

		got, err := store.GetLatestEvent(ctx, invalid, "scope-guard")
		require.ErrorIs(t, err, persistence.ErrInvalidScope)
		assert.Nil(t, got)
	})

	t.Run("PersistenceIDs", func(t *testing.T) {
		store := NewEventsStore()
		require.NoError(t, store.Connect(ctx))
		require.NoError(t, store.WriteEvents(ctx, persistence.Unscoped(), events, persistence.Unconditional()))

		ids, token, err := store.PersistenceIDs(ctx, invalid, 10, "")
		require.ErrorIs(t, err, persistence.ErrInvalidScope)
		assert.Empty(t, ids)
		assert.Empty(t, token)
	})
}

// ---------------------------------------------------------------------------
// DurableStore: invalid scope rejection
// ---------------------------------------------------------------------------

func TestDurableStore_InvalidScopeRejected(t *testing.T) {
	ctx := context.TODO()
	var invalid persistence.Scope

	anyState, err := anypb.New(&testpb.Account{AccountId: "acc-1", AccountBalance: 500})
	require.NoError(t, err)
	state := &egopb.DurableState{PersistenceId: "scope-guard-ds", ResultingState: anyState, VersionNumber: 1}

	t.Run("WriteState", func(t *testing.T) {
		store := NewDurableStore()
		require.NoError(t, store.Connect(ctx))
		err := store.WriteState(ctx, invalid, state, persistence.Unconditional())
		require.ErrorIs(t, err, persistence.ErrInvalidScope)

		got, getErr := store.GetLatestState(ctx, persistence.Unscoped(), "scope-guard-ds")
		require.NoError(t, getErr)
		assert.Nil(t, got, "an invalid scope must not have written anything")
	})

	t.Run("GetLatestState", func(t *testing.T) {
		store := NewDurableStore()
		require.NoError(t, store.Connect(ctx))
		require.NoError(t, store.WriteState(ctx, persistence.Unscoped(), state, persistence.Unconditional()))

		got, err := store.GetLatestState(ctx, invalid, "scope-guard-ds")
		require.ErrorIs(t, err, persistence.ErrInvalidScope)
		assert.Nil(t, got)
	})
}

// ---------------------------------------------------------------------------
// SnapshotStore: invalid scope rejection
// ---------------------------------------------------------------------------

func TestSnapshotStore_InvalidScopeRejected(t *testing.T) {
	ctx := context.TODO()
	var invalid persistence.Scope

	anyState, err := anypb.New(&testpb.Account{AccountId: "acc-1", AccountBalance: 500})
	require.NoError(t, err)
	snapshot := &egopb.Snapshot{PersistenceId: "scope-guard-snap", SequenceNumber: 1, State: anyState}

	t.Run("WriteSnapshot", func(t *testing.T) {
		store := NewSnapshotStore()
		require.NoError(t, store.Connect(ctx))
		err := store.WriteSnapshot(ctx, invalid, snapshot)
		require.ErrorIs(t, err, persistence.ErrInvalidScope)

		got, getErr := store.GetLatestSnapshot(ctx, persistence.Unscoped(), "scope-guard-snap")
		require.NoError(t, getErr)
		assert.Nil(t, got, "an invalid scope must not have written anything")
	})

	t.Run("GetLatestSnapshot", func(t *testing.T) {
		store := NewSnapshotStore()
		require.NoError(t, store.Connect(ctx))
		require.NoError(t, store.WriteSnapshot(ctx, persistence.Unscoped(), snapshot))

		got, err := store.GetLatestSnapshot(ctx, invalid, "scope-guard-snap")
		require.ErrorIs(t, err, persistence.ErrInvalidScope)
		assert.Nil(t, got)
	})

	t.Run("DeleteSnapshots", func(t *testing.T) {
		store := NewSnapshotStore()
		require.NoError(t, store.Connect(ctx))
		require.NoError(t, store.WriteSnapshot(ctx, persistence.Unscoped(), snapshot))

		err := store.DeleteSnapshots(ctx, invalid, "scope-guard-snap", 1)
		require.ErrorIs(t, err, persistence.ErrInvalidScope)

		got, getErr := store.GetLatestSnapshot(ctx, persistence.Unscoped(), "scope-guard-snap")
		require.NoError(t, getErr)
		assert.NotNil(t, got, "the invalid-scope delete must not have touched the unscoped record")
	})
}

// ---------------------------------------------------------------------------
// Unscoped() regression guard: a single writer under Unscoped() behaves
// exactly as the pre-TENANT-003 stores did (the existing suites in
// stores_test.go, eventstore_test.go, durablestore_test.go and
// concurrency_test.go already exercise this for every method by always
// passing persistence.Unscoped(); this test is a focused, explicit
// same-persistenceID-different-scope check that a second, independent
// tenant scope does not collide with Unscoped()'s own records).
// ---------------------------------------------------------------------------

func TestEventStore_UnscopedDoesNotCollideWithTenantScope(t *testing.T) {
	ctx := context.TODO()
	store := NewEventsStore()
	require.NoError(t, store.Connect(ctx))

	tenantA, err := persistence.NewTenantScope("tenant-a")
	require.NoError(t, err)

	anyEvent, err := anypb.New(&testpb.AccountCreated{AccountId: "acc-1", AccountBalance: 100})
	require.NoError(t, err)

	unscopedEvents := []*egopb.Event{
		{PersistenceId: "shared-id", SequenceNumber: 1, Event: anyEvent, Timestamp: time.Now().UnixMilli(), Shard: 1},
	}
	require.NoError(t, store.WriteEvents(ctx, persistence.Unscoped(), unscopedEvents, persistence.Unconditional()))

	// tenantA writing the SAME persistenceID with ExpectGenesis() must
	// succeed, proving the precondition's persisted revision is scoped to
	// (scope, persistenceID), not persistenceID alone.
	tenantEvents := []*egopb.Event{
		{PersistenceId: "shared-id", SequenceNumber: 1, Event: anyEvent, Timestamp: time.Now().UnixMilli(), Shard: 1},
	}
	require.NoError(t, store.WriteEvents(ctx, tenantA, tenantEvents, persistence.ExpectGenesis()))

	unscopedLatest, err := store.GetLatestEvent(ctx, persistence.Unscoped(), "shared-id")
	require.NoError(t, err)
	require.NotNil(t, unscopedLatest)

	tenantLatest, err := store.GetLatestEvent(ctx, tenantA, "shared-id")
	require.NoError(t, err)
	require.NotNil(t, tenantLatest)
}
