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

package ego

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/persistence"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
	"github.com/pablogore/ego/v4/testkit"
)

// TestEngineEraseEntityCannotEraseAnotherTenantsRecord covers TENANT-003
// T4's EraseEntity fix (engine.go's EraseEntity, ~line 1284): before this
// fix, EraseEntity called the stores with persistence.Unscoped()
// unconditionally regardless of who called it, so any caller who knew a
// persistenceID could erase ANY tenant's events — the exact isolation hole
// this ticket closes. EraseEntity now resolves the caller's own tenant
// scope from ctx (via the same tenancy.TenantResolver as every other
// engine method) and confines its read/delete to that scope alone.
//
// The two records below share the SAME persistenceID but live under two
// different tenant scopes at the store layer — the composite (scope,
// persistenceID) key testkit.EventStore uses internally — simulating what
// the "Known limitation" section of design.md documents: two tenants can
// collide on the same logical id at the storage layer even though the
// actor system itself would only ever let one of them claim a live actor
// for that id.
func TestEngineEraseEntityCannotEraseAnotherTenantsRecord(t *testing.T) {
	ctx := context.Background()
	persistenceID := uuid.NewString()

	store := testkit.NewEventsStore()
	require.NoError(t, store.Connect(ctx))
	t.Cleanup(func() { _ = store.Disconnect(ctx) })

	scopeA, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)
	scopeB, err := persistence.NewTenantScope("globex")
	require.NoError(t, err)

	newEvent := func() *egopb.Event {
		eventAny, err := anypb.New(&testpb.AccountCreated{AccountId: persistenceID, AccountBalance: 100})
		require.NoError(t, err)
		return &egopb.Event{
			PersistenceId:  persistenceID,
			SequenceNumber: 1,
			Event:          eventAny,
			Timestamp:      time.Now().UnixNano(),
		}
	}

	require.NoError(t, store.WriteEvents(ctx, scopeA, []*egopb.Event{newEvent()}, persistence.Unconditional()))
	require.NoError(t, store.WriteEvents(ctx, scopeB, []*egopb.Event{newEvent()}, persistence.Unconditional()))

	// perCallerTenantResolver (option_test.go) resolves whichever tenant id
	// the caller placed on ctx under perCallerTenantKey.
	engine := newTestEngine(t, "Sample", store, WithTenantResolver(perCallerTenantResolver{}))
	require.NoError(t, engine.Start(ctx))

	ctxA := context.WithValue(ctx, perCallerTenantKey{}, "acme")
	require.NoError(t, engine.EraseEntity(ctxA, persistenceID, true))

	latestA, err := store.GetLatestEvent(ctx, scopeA, persistenceID)
	require.NoError(t, err)
	require.Nil(t, latestA, "tenant A's own record must be erased by its own EraseEntity call")

	latestB, err := store.GetLatestEvent(ctx, scopeB, persistenceID)
	require.NoError(t, err)
	require.NotNil(t, latestB, "tenant B's record at the same persistenceID must survive tenant A's erasure call")

	require.NoError(t, engine.Stop(ctx))
}
