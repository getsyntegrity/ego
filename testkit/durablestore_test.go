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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/persistence"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
)

// newAccountState builds a *egopb.DurableState for persistenceID at
// versionNumber, for use as the payload of a conditional WriteState call.
func newAccountState(t *testing.T, persistenceID string, versionNumber uint64) *egopb.DurableState {
	t.Helper()
	anyState, err := anypb.New(&testpb.Account{AccountId: persistenceID, AccountBalance: 100})
	require.NoError(t, err)
	return &egopb.DurableState{PersistenceId: persistenceID, ResultingState: anyState, VersionNumber: versionNumber}
}

// ---------------------------------------------------------------------------
// WriteState: conditional-write contract (2.2x)
// ---------------------------------------------------------------------------

func TestDurableStore_WriteState_InvalidPreconditionIsRejected(t *testing.T) {
	ctx := context.TODO()
	store := NewDurableStore()
	require.NoError(t, store.Connect(ctx))

	var zero persistence.WritePrecondition
	err := store.WriteState(ctx, persistence.Unscoped(), newAccountState(t, "invalid-precondition", 1), zero)

	require.ErrorIs(t, err, persistence.ErrInvalidPrecondition)

	got, getErr := store.GetLatestState(ctx, persistence.Unscoped(), "invalid-precondition")
	require.NoError(t, getErr)
	assert.Nil(t, got, "a rejected precondition must not persist anything")
}

func TestDurableStore_WriteState_UnconditionalIsLegacyBehavior(t *testing.T) {
	ctx := context.TODO()
	store := NewDurableStore()
	require.NoError(t, store.Connect(ctx))

	require.NoError(t, store.WriteState(ctx, persistence.Unscoped(), newAccountState(t, "legacy", 1), persistence.Unconditional()))
	require.NoError(t, store.WriteState(ctx, persistence.Unscoped(), newAccountState(t, "legacy", 2), persistence.Unconditional()))

	got, err := store.GetLatestState(ctx, persistence.Unscoped(), "legacy")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.EqualValues(t, 2, got.GetVersionNumber())
}

func TestDurableStore_WriteState_ExactRevisionSucceedsWhenCurrent(t *testing.T) {
	ctx := context.TODO()
	store := NewDurableStore()
	require.NoError(t, store.Connect(ctx))

	require.NoError(t, store.WriteState(ctx, persistence.Unscoped(), newAccountState(t, "exact-ok", 1), persistence.ExpectGenesis()))
	err := store.WriteState(ctx, persistence.Unscoped(), newAccountState(t, "exact-ok", 2), persistence.ExpectRevision(1))
	require.NoError(t, err)

	got, err := store.GetLatestState(ctx, persistence.Unscoped(), "exact-ok")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.EqualValues(t, 2, got.GetVersionNumber())
}

func TestDurableStore_WriteState_StaleRevisionIsConflict(t *testing.T) {
	ctx := context.TODO()
	store := NewDurableStore()
	require.NoError(t, store.Connect(ctx))

	require.NoError(t, store.WriteState(ctx, persistence.Unscoped(), newAccountState(t, "stale", 1), persistence.ExpectGenesis()))

	err := store.WriteState(ctx, persistence.Unscoped(), newAccountState(t, "stale", 2), persistence.ExpectRevision(99))

	var conflict *persistence.ConflictError
	require.True(t, errors.As(err, &conflict))
	require.ErrorIs(t, err, persistence.ErrConcurrencyConflict)
	assert.Equal(t, "stale", conflict.PersistenceID())
	assert.Equal(t, persistence.ExpectRevision(99), conflict.Expected())

	actual, ok := conflict.ActualRevision()
	require.True(t, ok)
	assert.EqualValues(t, 1, actual)

	got, err := store.GetLatestState(ctx, persistence.Unscoped(), "stale")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.EqualValues(t, 1, got.GetVersionNumber(), "a rejected conditional write must not modify the persisted state")
}

func TestDurableStore_WriteState_GenesisSucceedsOnEmpty(t *testing.T) {
	ctx := context.TODO()
	store := NewDurableStore()
	require.NoError(t, store.Connect(ctx))

	err := store.WriteState(ctx, persistence.Unscoped(), newAccountState(t, "genesis-ok", 1), persistence.ExpectGenesis())
	require.NoError(t, err)

	got, err := store.GetLatestState(ctx, persistence.Unscoped(), "genesis-ok")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.EqualValues(t, 1, got.GetVersionNumber())
}

func TestDurableStore_WriteState_GenesisConflictsOnExisting(t *testing.T) {
	ctx := context.TODO()
	store := NewDurableStore()
	require.NoError(t, store.Connect(ctx))

	require.NoError(t, store.WriteState(ctx, persistence.Unscoped(), newAccountState(t, "genesis-taken", 1), persistence.ExpectGenesis()))

	err := store.WriteState(ctx, persistence.Unscoped(), newAccountState(t, "genesis-taken", 2), persistence.ExpectGenesis())

	var conflict *persistence.ConflictError
	require.True(t, errors.As(err, &conflict))
	actual, ok := conflict.ActualRevision()
	require.True(t, ok)
	assert.EqualValues(t, 1, actual)

	got, err := store.GetLatestState(ctx, persistence.Unscoped(), "genesis-taken")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.EqualValues(t, 1, got.GetVersionNumber())
}
