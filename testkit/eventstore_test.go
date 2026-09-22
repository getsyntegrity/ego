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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/persistence"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
)

// newAccountEvent builds a single-event batch for persistenceID at
// sequenceNumber, for use as the payload of a conditional WriteEvents call.
func newAccountEvent(t *testing.T, persistenceID string, sequenceNumber uint64) []*egopb.Event {
	t.Helper()
	anyEvent, err := anypb.New(&testpb.AccountCreated{AccountId: persistenceID, AccountBalance: 100})
	require.NoError(t, err)
	return []*egopb.Event{
		{PersistenceId: persistenceID, SequenceNumber: sequenceNumber, Event: anyEvent, Timestamp: time.Now().UnixMilli(), Shard: 1},
	}
}

// ---------------------------------------------------------------------------
// WriteEvents: conditional-write contract (2.2x)
// ---------------------------------------------------------------------------

func TestEventStore_WriteEvents_InvalidPreconditionIsRejected(t *testing.T) {
	ctx := context.TODO()
	store := NewEventsStore()
	require.NoError(t, store.Connect(ctx))

	var zero persistence.WritePrecondition
	err := store.WriteEvents(ctx, persistence.Unscoped(), newAccountEvent(t, "invalid-precondition", 1), zero)

	require.ErrorIs(t, err, persistence.ErrInvalidPrecondition)

	latest, getErr := store.GetLatestEvent(ctx, persistence.Unscoped(), "invalid-precondition")
	require.NoError(t, getErr)
	assert.Nil(t, latest, "a rejected precondition must not persist anything")
}

func TestEventStore_WriteEvents_UnconditionalIsLegacyBehavior(t *testing.T) {
	ctx := context.TODO()
	store := NewEventsStore()
	require.NoError(t, store.Connect(ctx))

	require.NoError(t, store.WriteEvents(ctx, persistence.Unscoped(), newAccountEvent(t, "legacy", 1), persistence.Unconditional()))
	require.NoError(t, store.WriteEvents(ctx, persistence.Unscoped(), newAccountEvent(t, "legacy", 2), persistence.Unconditional()))

	replayed, err := store.ReplayEvents(ctx, persistence.Unscoped(), "legacy", 1, 2, 10)
	require.NoError(t, err)
	assert.Len(t, replayed, 2)
}

func TestEventStore_WriteEvents_ExactRevisionSucceedsWhenCurrent(t *testing.T) {
	ctx := context.TODO()
	store := NewEventsStore()
	require.NoError(t, store.Connect(ctx))

	require.NoError(t, store.WriteEvents(ctx, persistence.Unscoped(), newAccountEvent(t, "exact-ok", 1), persistence.ExpectGenesis()))
	err := store.WriteEvents(ctx, persistence.Unscoped(), newAccountEvent(t, "exact-ok", 2), persistence.ExpectRevision(1))
	require.NoError(t, err)

	latest, err := store.GetLatestEvent(ctx, persistence.Unscoped(), "exact-ok")
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.EqualValues(t, 2, latest.GetSequenceNumber())
}

func TestEventStore_WriteEvents_StaleRevisionIsConflict(t *testing.T) {
	ctx := context.TODO()
	store := NewEventsStore()
	require.NoError(t, store.Connect(ctx))

	require.NoError(t, store.WriteEvents(ctx, persistence.Unscoped(), newAccountEvent(t, "stale", 1), persistence.ExpectGenesis()))

	err := store.WriteEvents(ctx, persistence.Unscoped(), newAccountEvent(t, "stale", 2), persistence.ExpectRevision(99))

	var conflict *persistence.ConflictError
	require.True(t, errors.As(err, &conflict))
	require.ErrorIs(t, err, persistence.ErrConcurrencyConflict)
	assert.Equal(t, "stale", conflict.PersistenceID())
	assert.Equal(t, persistence.ExpectRevision(99), conflict.Expected())

	actual, ok := conflict.ActualRevision()
	require.True(t, ok)
	assert.EqualValues(t, 1, actual)

	latest, err := store.GetLatestEvent(ctx, persistence.Unscoped(), "stale")
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.EqualValues(t, 1, latest.GetSequenceNumber(), "a rejected conditional write must not modify the persisted log")
}

func TestEventStore_WriteEvents_GenesisSucceedsOnEmpty(t *testing.T) {
	ctx := context.TODO()
	store := NewEventsStore()
	require.NoError(t, store.Connect(ctx))

	err := store.WriteEvents(ctx, persistence.Unscoped(), newAccountEvent(t, "genesis-ok", 1), persistence.ExpectGenesis())
	require.NoError(t, err)

	latest, err := store.GetLatestEvent(ctx, persistence.Unscoped(), "genesis-ok")
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.EqualValues(t, 1, latest.GetSequenceNumber())
}

func TestEventStore_WriteEvents_GenesisConflictsOnExisting(t *testing.T) {
	ctx := context.TODO()
	store := NewEventsStore()
	require.NoError(t, store.Connect(ctx))

	require.NoError(t, store.WriteEvents(ctx, persistence.Unscoped(), newAccountEvent(t, "genesis-taken", 1), persistence.ExpectGenesis()))

	err := store.WriteEvents(ctx, persistence.Unscoped(), newAccountEvent(t, "genesis-taken", 2), persistence.ExpectGenesis())

	var conflict *persistence.ConflictError
	require.True(t, errors.As(err, &conflict))
	actual, ok := conflict.ActualRevision()
	require.True(t, ok)
	assert.EqualValues(t, 1, actual)

	latest, err := store.GetLatestEvent(ctx, persistence.Unscoped(), "genesis-taken")
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.EqualValues(t, 1, latest.GetSequenceNumber())
}

// ---------------------------------------------------------------------------
// newEventLog: duplicate SequenceNumber must not accumulate (regression)
// ---------------------------------------------------------------------------
//
// Before the eventLog restructuring, events were keyed by
// EventKey{PersistenceID, SequenceNumber} in a sync.Map: writing the same key
// again silently overwrote the prior entry, so every read path observed
// exactly one event per (PersistenceID, SequenceNumber) pair. These tests
// prove the eventLog-based representation preserves that observable
// semantic through ReplayEvents, GetLatestEvent, GetShardEvents,
// ShardOffsets, and DeleteEvents.

func TestEventStore_WriteEvents_DuplicateSequenceNumberOverwritesNotAccumulates(t *testing.T) {
	ctx := context.TODO()
	store := NewEventsStore()
	require.NoError(t, store.Connect(ctx))

	require.NoError(t, store.WriteEvents(ctx, persistence.Unscoped(), newAccountEvent(t, "dup-sn", 1), persistence.Unconditional()))
	require.NoError(t, store.WriteEvents(ctx, persistence.Unscoped(), newAccountEvent(t, "dup-sn", 1), persistence.Unconditional()))

	replayed, err := store.ReplayEvents(ctx, persistence.Unscoped(), "dup-sn", 1, 10, 100)
	require.NoError(t, err)
	assert.Len(t, replayed, 1, "rewriting SequenceNumber 1 must not produce two visible events")

	latest, err := store.GetLatestEvent(ctx, persistence.Unscoped(), "dup-sn")
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.EqualValues(t, 1, latest.GetSequenceNumber())
}

func TestEventStore_WriteEvents_DuplicateSequenceNumberWithinConditionalWriteOverwrites(t *testing.T) {
	ctx := context.TODO()
	store := NewEventsStore()
	require.NoError(t, store.Connect(ctx))

	require.NoError(t, store.WriteEvents(ctx, persistence.Unscoped(), newAccountEvent(t, "dup-sn-conditional", 1), persistence.ExpectGenesis()))
	require.NoError(t, store.WriteEvents(ctx, persistence.Unscoped(), newAccountEvent(t, "dup-sn-conditional", 1), persistence.ExpectRevision(1)))

	replayed, err := store.ReplayEvents(ctx, persistence.Unscoped(), "dup-sn-conditional", 1, 10, 100)
	require.NoError(t, err)
	assert.Len(t, replayed, 1, "a conditional rewrite of SequenceNumber 1 must not produce two visible events")

	latest, err := store.GetLatestEvent(ctx, persistence.Unscoped(), "dup-sn-conditional")
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.EqualValues(t, 1, latest.GetSequenceNumber())
}

func TestEventStore_WriteEvents_DuplicateSequenceNumberDoesNotDuplicateShardEventsOrOffsets(t *testing.T) {
	ctx := context.TODO()
	store := NewEventsStore()
	require.NoError(t, store.Connect(ctx))

	require.NoError(t, store.WriteEvents(ctx, persistence.Unscoped(), newAccountEvent(t, "dup-sn-shard", 1), persistence.Unconditional()))
	require.NoError(t, store.WriteEvents(ctx, persistence.Unscoped(), newAccountEvent(t, "dup-sn-shard", 1), persistence.Unconditional()))

	shardEvents, _, err := store.GetShardEvents(ctx, 1, 0, 100)
	require.NoError(t, err)
	assert.Len(t, shardEvents, 1, "rewriting SequenceNumber 1 must not duplicate its shard's events")

	offsets, err := store.ShardOffsets(ctx)
	require.NoError(t, err)
	require.Contains(t, offsets, uint64(1))
	assert.Equal(t, shardEvents[0].GetTimestamp(), offsets[1], "the duplicate rewrite must not skew the shard's offset beyond its single surviving event")
}

func TestEventStore_WriteEvents_DuplicateSequenceNumberThenDeleteEventsLeavesNoResidual(t *testing.T) {
	ctx := context.TODO()
	store := NewEventsStore()
	require.NoError(t, store.Connect(ctx))

	require.NoError(t, store.WriteEvents(ctx, persistence.Unscoped(), newAccountEvent(t, "dup-sn-delete", 1), persistence.Unconditional()))
	require.NoError(t, store.WriteEvents(ctx, persistence.Unscoped(), newAccountEvent(t, "dup-sn-delete", 1), persistence.Unconditional()))
	require.NoError(t, store.WriteEvents(ctx, persistence.Unscoped(), newAccountEvent(t, "dup-sn-delete", 2), persistence.Unconditional()))

	require.NoError(t, store.DeleteEvents(ctx, persistence.Unscoped(), "dup-sn-delete", 1))

	replayed, err := store.ReplayEvents(ctx, persistence.Unscoped(), "dup-sn-delete", 1, 10, 100)
	require.NoError(t, err)
	require.Len(t, replayed, 1, "deleting up to SequenceNumber 1 must leave exactly the surviving event, not a residual duplicate")
	assert.EqualValues(t, 2, replayed[0].GetSequenceNumber())
}

func TestEventStore_WriteEvents_ConditionalBatchMustShareOnePersistenceID(t *testing.T) {
	ctx := context.TODO()
	store := NewEventsStore()
	require.NoError(t, store.Connect(ctx))

	mixed := append(newAccountEvent(t, "batch-a", 1), newAccountEvent(t, "batch-b", 1)...)

	err := store.WriteEvents(ctx, persistence.Unscoped(), mixed, persistence.ExpectGenesis())
	require.ErrorIs(t, err, persistence.ErrPreconditionScope)

	err = store.WriteEvents(ctx, persistence.Unscoped(), nil, persistence.ExpectGenesis())
	require.ErrorIs(t, err, persistence.ErrPreconditionScope)
}
