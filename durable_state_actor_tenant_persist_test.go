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
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/eventstream"
	"github.com/pablogore/ego/v4/internal/extensions"
	"github.com/pablogore/ego/v4/internal/pause"
	mocks "github.com/pablogore/ego/v4/mocks/persistence"
	"github.com/pablogore/ego/v4/tenancy"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
	"github.com/pablogore/ego/v4/testkit"
)

// TestDurableStateActorRecoverFromStoreSeedsActorTenant covers tasks.md
// Phase 1 (DS2): recoverFromStore must seed entity.actorTenant from the
// recovered record's carried tenant metadata before its state payload is
// touched, leave a genesis actor unseeded without error, and fail closed
// (ErrInvalid) when a tenant-aware actor recovers a non-genesis record whose
// tenant metadata is absent or malformed. Legacy mode never seeds.
func TestDurableStateActorRecoverFromStoreSeedsActorTenant(t *testing.T) {
	ctx := context.Background()
	persistenceID := "acct-1"

	tenantA, err := tenancy.NewTenantContext("acme")
	require.NoError(t, err)

	newDurableState := func(md tenancy.Metadata) *egopb.DurableState {
		stateAny, err := anypb.New(&testpb.Account{AccountId: persistenceID, AccountBalance: 100})
		require.NoError(t, err)
		return &egopb.DurableState{
			PersistenceId:  persistenceID,
			VersionNumber:  1,
			ResultingState: stateAny,
			Timestamp:      time.Now().UnixNano(),
			TenantMetadata: md,
		}
	}

	t.Run("seeds actorTenant from valid persisted metadata", func(t *testing.T) {
		durableStore := testkit.NewDurableStore()
		require.NoError(t, durableStore.Connect(ctx))
		require.NoError(t, durableStore.WriteState(ctx, newDurableState(tenancy.MarshalMetadata(tenantA))))

		entity := &DurableStateActor{
			persistenceID: persistenceID,
			behavior:      NewAccountDurableStateBehavior(persistenceID),
			stateStore:    durableStore,
			tenantAware:   true,
		}

		require.NoError(t, entity.recoverFromStore(ctx))
		assert.Equal(t, tenantA, entity.actorTenant)
	})

	t.Run("genesis leaves actorTenant unseeded without error", func(t *testing.T) {
		durableStore := testkit.NewDurableStore()
		require.NoError(t, durableStore.Connect(ctx))

		entity := &DurableStateActor{
			persistenceID: persistenceID,
			behavior:      NewAccountDurableStateBehavior(persistenceID),
			stateStore:    durableStore,
			tenantAware:   true,
		}

		require.NoError(t, entity.recoverFromStore(ctx))
		assert.Equal(t, noTenantContext, entity.actorTenant)
	})

	t.Run("fails closed when tenant-aware and persisted metadata is absent", func(t *testing.T) {
		durableStore := testkit.NewDurableStore()
		require.NoError(t, durableStore.Connect(ctx))
		require.NoError(t, durableStore.WriteState(ctx, newDurableState(nil)))

		entity := &DurableStateActor{
			persistenceID: persistenceID,
			behavior:      NewAccountDurableStateBehavior(persistenceID),
			stateStore:    durableStore,
			tenantAware:   true,
		}

		err := entity.recoverFromStore(ctx)
		require.Error(t, err)
		assert.True(t, errors.Is(err, tenancy.ErrInvalid))
	})

	t.Run("fails closed when tenant-aware and persisted metadata is malformed", func(t *testing.T) {
		durableStore := testkit.NewDurableStore()
		require.NoError(t, durableStore.Connect(ctx))
		require.NoError(t, durableStore.WriteState(ctx, newDurableState(tenancy.Metadata{"ego.tenant.scope": "not-a-real-scope"})))

		entity := &DurableStateActor{
			persistenceID: persistenceID,
			behavior:      NewAccountDurableStateBehavior(persistenceID),
			stateStore:    durableStore,
			tenantAware:   true,
		}

		err := entity.recoverFromStore(ctx)
		require.Error(t, err)
		assert.True(t, errors.Is(err, tenancy.ErrInvalid))
	})

	t.Run("legacy mode never seeds actorTenant, even when metadata is present", func(t *testing.T) {
		durableStore := testkit.NewDurableStore()
		require.NoError(t, durableStore.Connect(ctx))
		require.NoError(t, durableStore.WriteState(ctx, newDurableState(tenancy.MarshalMetadata(tenantA))))

		entity := &DurableStateActor{
			persistenceID: persistenceID,
			behavior:      NewAccountDurableStateBehavior(persistenceID),
			stateStore:    durableStore,
		}

		require.NoError(t, entity.recoverFromStore(ctx))
		assert.Equal(t, noTenantContext, entity.actorTenant)
	})
}

// TestDurableStateActorProcessCommandRejectsCrossTenant covers tasks.md
// Phase 2 (DS1): the T4-A pre-handler gate must reject a command whose
// resolved TenantContext differs from this actor's already-established
// actorTenant, before HandleCommand ever runs — DurableStateActor had no
// such identity check before PR2, only tenancy.Require's presence-only
// proof (see TestDurableStateActorTenancyGate).
func TestDurableStateActorProcessCommandRejectsCrossTenant(t *testing.T) {
	ctx := context.TODO()

	durableStore := testkit.NewDurableStore()
	persistenceID := uuid.NewString()
	behavior := newTenancyProbeDurableStateBehavior(persistenceID)
	require.NoError(t, durableStore.Connect(ctx))

	eventStream := eventstream.New()

	actorSystem, err := goakt.NewActorSystem("TestActorSystem",
		goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
		goakt.WithExtensions(
			extensions.NewDurableStateStore(durableStore),
			extensions.NewEventsStream(eventStream),
			extensions.NewTenancyMarker(),
		),
		goakt.WithActorInitMaxRetries(3))
	require.NoError(t, err)
	require.NoError(t, actorSystem.Start(ctx))
	pause.For(time.Second)

	actor := newDurableStateActor()
	pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived())
	require.NoError(t, err)
	require.NotNil(t, pid)
	pause.For(time.Second)

	tenantA, err := tenancy.NewTenantContext("acme")
	require.NoError(t, err)
	tenantB, err := tenancy.NewTenantContext("globex")
	require.NoError(t, err)

	ctxA, err := tenancy.Attach(ctx, tenantA)
	require.NoError(t, err)
	reply, err := goakt.Ask(ctxA, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
	require.NoError(t, err)
	commandReply, ok := reply.(*egopb.CommandReply)
	require.True(t, ok)
	require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply(),
		"tenant A's first command must succeed and establish actorTenant")

	ctxB, err := tenancy.Attach(ctx, tenantB)
	require.NoError(t, err)
	reply, err = goakt.Ask(ctxB, pid, &testpb.CreditAccount{AccountId: persistenceID, Balance: 10}, 5*time.Second)
	require.NoError(t, err, "Ask itself must not fail; the rejection is carried in the CommandReply")
	commandReply, ok = reply.(*egopb.CommandReply)
	require.True(t, ok)
	errorReply, ok := commandReply.GetReply().(*egopb.CommandReply_ErrorReply)
	require.True(t, ok, "a cross-tenant command must be rejected before HandleCommand runs")

	wantErr := tenancy.VerifyUnchanged(tenantA, tenantB)
	assert.Equal(t, wantErr.Error(), errorReply.ErrorReply.GetMessage(),
		"rejection must be the fail-closed tenant error VerifyUnchanged produces, not an invented error type")
	assert.EqualValues(t, 1, behavior.invocationCount(), "HandleCommand must not run for the rejected cross-tenant command")

	require.NoError(t, durableStore.Disconnect(ctx))
	eventStream.Close()
	pause.For(time.Second)
	require.NoError(t, actorSystem.Stop(ctx))
}

// TestDurableStateActorPersistStateAndPublishWritesTenantMetadata covers
// tasks.md Phase 3 (DS3): persistStateAndPublish must write
// entity.actorTenant's metadata onto the persisted DurableState using
// tenancy.MarshalMetadata's exact ego.tenant.* keys, and write nothing in
// legacy mode.
func TestDurableStateActorPersistStateAndPublishWritesTenantMetadata(t *testing.T) {
	ctx := context.TODO()
	persistenceID := "acct-1"

	newEntity := func(tenantAware bool, tc tenancy.TenantContext, store *testkit.DurableStore, stream eventstream.Stream) *DurableStateActor {
		state := &testpb.Account{AccountId: persistenceID, AccountBalance: 100}
		cachedAny, err := anypb.New(state)
		require.NoError(t, err)
		return &DurableStateActor{
			persistenceID:   persistenceID,
			currentState:    state,
			cachedStateAny:  cachedAny,
			currentVersion:  1,
			lastCommandTime: time.Now(),
			stateStore:      store,
			eventsStream:    stream,
			tenantAware:     tenantAware,
			actorTenant:     tc,
		}
	}

	t.Run("legacy mode writes no tenant metadata", func(t *testing.T) {
		durableStore := testkit.NewDurableStore()
		require.NoError(t, durableStore.Connect(ctx))
		eventStream := eventstream.New()

		entity := newEntity(false, noTenantContext, durableStore, eventStream)
		require.NoError(t, entity.persistStateAndPublish(ctx))

		latest, err := durableStore.GetLatestState(ctx, persistenceID)
		require.NoError(t, err)
		require.NotNil(t, latest)
		assert.Empty(t, latest.GetTenantMetadata())

		eventStream.Close()
		require.NoError(t, durableStore.Disconnect(ctx))
	})

	t.Run("tenant-aware mode writes the actor's established tenant", func(t *testing.T) {
		tenantA, err := tenancy.NewTenantContext("acme")
		require.NoError(t, err)

		durableStore := testkit.NewDurableStore()
		require.NoError(t, durableStore.Connect(ctx))
		eventStream := eventstream.New()

		entity := newEntity(true, tenantA, durableStore, eventStream)
		require.NoError(t, entity.persistStateAndPublish(ctx))

		latest, err := durableStore.GetLatestState(ctx, persistenceID)
		require.NoError(t, err)
		require.NotNil(t, latest)
		require.NotEmpty(t, latest.GetTenantMetadata())
		assert.Equal(t, map[string]string(tenancy.MarshalMetadata(tenantA)), latest.GetTenantMetadata())

		roundTripped, err := tenancy.UnmarshalMetadata(latest.GetTenantMetadata())
		require.NoError(t, err)
		assert.Equal(t, tenantA, roundTripped)

		eventStream.Close()
		require.NoError(t, durableStore.Disconnect(ctx))
	})
}

// TestDurableStateActorPostStopTenantPersist covers tasks.md Phase 3's four
// mandatory DS3 tests for the lifecycle flush: a tenant-aware actor that
// never established a tenant identity (genesis, no command ever handled)
// must skip PostStop's persist entirely rather than write a DurableState
// whose tenant_metadata is empty (which recoverFromStore/DS2 would then
// refuse to recover from — a bricking bug); a tenant-aware, seeded actor
// must have PostStop persist with the established tenant's metadata;
// legacy mode keeps today's unconditional flush even for a never-touched
// genesis actor (regression); and an actor that fails recovery because its
// persisted tenant metadata was invalid never reaches PostStop's persist at
// all, because it never finishes PreStart.
func TestDurableStateActorPostStopTenantPersist(t *testing.T) {
	ctx := context.TODO()

	t.Run("tenant-aware and never-seeded: PostStop does not persist", func(t *testing.T) {
		persistenceID := uuid.NewString()
		behavior := NewAccountDurableStateBehavior(persistenceID)

		durableStore := new(mocks.StateStore)
		durableStore.EXPECT().Ping(mock.Anything).Return(nil)
		durableStore.EXPECT().GetLatestState(mock.Anything, behavior.ID()).Return(nil, nil)

		eventStream := eventstream.New()

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewDurableStateStore(durableStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewTenancyMarker(),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newDurableStateActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived())
		require.NoError(t, err)
		require.NotNil(t, pid)
		pause.For(time.Second)

		require.NoError(t, actorSystem.Kill(ctx, behavior.ID()))
		pause.For(time.Second)

		// No WriteState expectation was ever registered on this mock: if
		// PostStop had called persistStateAndPublish, the mock would have
		// panicked on the unexpected call and failed this test.
		durableStore.AssertExpectations(t)
		durableStore.AssertNotCalled(t, "WriteState", mock.Anything, mock.Anything)

		eventStream.Close()
		pause.For(time.Second)
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("tenant-aware and seeded: PostStop writes with the established tenant", func(t *testing.T) {
		tenantA, err := tenancy.NewTenantContext("acme")
		require.NoError(t, err)

		durableStore := testkit.NewDurableStore()
		persistenceID := uuid.NewString()
		behavior := newTenancyProbeDurableStateBehavior(persistenceID)
		require.NoError(t, durableStore.Connect(ctx))

		eventStream := eventstream.New()

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewDurableStateStore(durableStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewTenancyMarker(),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newDurableStateActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived())
		require.NoError(t, err)
		require.NotNil(t, pid)
		pause.For(time.Second)

		ctxA, err := tenancy.Attach(ctx, tenantA)
		require.NoError(t, err)
		reply, err := goakt.Ask(ctxA, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
		require.NoError(t, err)
		commandReply, ok := reply.(*egopb.CommandReply)
		require.True(t, ok)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

		require.NoError(t, actorSystem.Kill(ctx, behavior.ID()))
		pause.For(time.Second)

		latest, err := durableStore.GetLatestState(ctx, persistenceID)
		require.NoError(t, err)
		require.NotNil(t, latest)
		require.NotEmpty(t, latest.GetTenantMetadata())
		assert.Equal(t, map[string]string(tenancy.MarshalMetadata(tenantA)), latest.GetTenantMetadata())

		eventStream.Close()
		pause.For(time.Second)
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("legacy mode keeps the unconditional flush, even for a never-touched genesis actor", func(t *testing.T) {
		durableStore := testkit.NewDurableStore()
		persistenceID := uuid.NewString()
		behavior := NewAccountDurableStateBehavior(persistenceID)
		require.NoError(t, durableStore.Connect(ctx))

		eventStream := eventstream.New()

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewDurableStateStore(durableStore),
				extensions.NewEventsStream(eventStream),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newDurableStateActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived())
		require.NoError(t, err)
		require.NotNil(t, pid)
		pause.For(time.Second)

		require.NoError(t, actorSystem.Kill(ctx, behavior.ID()))
		pause.For(time.Second)

		latest, err := durableStore.GetLatestState(ctx, persistenceID)
		require.NoError(t, err)
		require.NotNil(t, latest, "legacy mode must flush on PostStop even without ever handling a command")
		assert.Empty(t, latest.GetTenantMetadata())

		eventStream.Close()
		pause.For(time.Second)
		require.NoError(t, actorSystem.Stop(ctx))
	})

	t.Run("an actor that fails recovery on invalid tenant metadata never reaches PostStop's persist", func(t *testing.T) {
		persistenceID := uuid.NewString()
		behavior := NewAccountDurableStateBehavior(persistenceID)

		stateAny, err := anypb.New(&testpb.Account{AccountId: persistenceID, AccountBalance: 100})
		require.NoError(t, err)
		latestState := &egopb.DurableState{
			PersistenceId:  persistenceID,
			VersionNumber:  1,
			ResultingState: stateAny,
			Timestamp:      time.Now().UnixNano(),
			// TenantMetadata deliberately absent: a non-genesis record on a
			// tenant-aware actor must be refused, not silently recovered.
		}

		durableStore := new(mocks.StateStore)
		durableStore.EXPECT().Ping(mock.Anything).Return(nil)
		durableStore.EXPECT().GetLatestState(mock.Anything, behavior.ID()).Return(latestState, nil)

		eventStream := eventstream.New()

		actorSystem, err := goakt.NewActorSystem("TestActorSystem",
			goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
			goakt.WithExtensions(
				extensions.NewDurableStateStore(durableStore),
				extensions.NewEventsStream(eventStream),
				extensions.NewTenancyMarker(),
			),
			goakt.WithActorInitMaxRetries(3))
		require.NoError(t, err)
		require.NoError(t, actorSystem.Start(ctx))
		pause.For(time.Second)

		actor := newDurableStateActor()
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived())
		require.Error(t, err, "PreStart must fail closed on invalid persisted tenant metadata")
		require.Nil(t, pid)
		pause.For(time.Second)

		// No WriteState expectation was ever registered: PostStop is never
		// invoked for an actor whose PreStart never completed.
		durableStore.AssertExpectations(t)
		durableStore.AssertNotCalled(t, "WriteState", mock.Anything, mock.Anything)

		eventStream.Close()
		pause.For(time.Second)
		require.NoError(t, actorSystem.Stop(ctx))
	})
}

// TestDurableStateActorGetStateCommandTenancyGate covers tasks.md Phase 4
// (DS4): getStateAndReply must apply the same gate processCommand's T4-A
// does, since Receive dispatches *egopb.GetStateCommand directly, bypassing
// processCommand entirely — without this, a resolved tenant could read
// another tenant's full committed durable state.
func TestDurableStateActorGetStateCommandTenancyGate(t *testing.T) {
	ctx := context.TODO()

	durableStore := testkit.NewDurableStore()
	persistenceID := uuid.NewString()
	behavior := newTenancyProbeDurableStateBehavior(persistenceID)
	require.NoError(t, durableStore.Connect(ctx))

	eventStream := eventstream.New()

	actorSystem, err := goakt.NewActorSystem("TestActorSystem",
		goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
		goakt.WithExtensions(
			extensions.NewDurableStateStore(durableStore),
			extensions.NewEventsStream(eventStream),
			extensions.NewTenancyMarker(),
		),
		goakt.WithActorInitMaxRetries(3))
	require.NoError(t, err)
	require.NoError(t, actorSystem.Start(ctx))
	pause.For(time.Second)

	actor := newDurableStateActor()
	pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor, goakt.WithDependencies(behavior), goakt.WithLongLived())
	require.NoError(t, err)
	require.NotNil(t, pid)
	pause.For(time.Second)

	tenantA, err := tenancy.NewTenantContext("acme")
	require.NoError(t, err)
	tenantB, err := tenancy.NewTenantContext("globex")
	require.NoError(t, err)

	ctxA, err := tenancy.Attach(ctx, tenantA)
	require.NoError(t, err)
	reply, err := goakt.Ask(ctxA, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
	require.NoError(t, err)
	commandReply, ok := reply.(*egopb.CommandReply)
	require.True(t, ok)
	require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply(),
		"tenant A's command must succeed and establish actorTenant")

	t.Run("foreign tenant is rejected", func(t *testing.T) {
		ctxB, err := tenancy.Attach(ctx, tenantB)
		require.NoError(t, err)
		reply, err := goakt.Ask(ctxB, pid, &egopb.GetStateCommand{}, 5*time.Second)
		require.NoError(t, err, "Ask itself must not fail; the rejection is carried in the CommandReply")
		commandReply, ok := reply.(*egopb.CommandReply)
		require.True(t, ok)
		errorReply, ok := commandReply.GetReply().(*egopb.CommandReply_ErrorReply)
		require.True(t, ok, "GetStateCommand from a different tenant must be rejected, not return tenant A's state")

		wantErr := tenancy.VerifyUnchanged(tenantA, tenantB)
		assert.Equal(t, wantErr.Error(), errorReply.ErrorReply.GetMessage(),
			"rejection must be the fail-closed tenant error VerifyUnchanged produces, not an invented error type")
	})

	t.Run("missing tenant context is rejected", func(t *testing.T) {
		reply, err := goakt.Ask(ctx, pid, &egopb.GetStateCommand{}, 5*time.Second)
		require.NoError(t, err, "Ask itself must not fail; the rejection is carried in the CommandReply")
		commandReply, ok := reply.(*egopb.CommandReply)
		require.True(t, ok)
		_, ok = commandReply.GetReply().(*egopb.CommandReply_ErrorReply)
		require.True(t, ok, "GetStateCommand with no resolved tenant must be rejected on a tenant-aware actor")
	})

	t.Run("matching tenant succeeds", func(t *testing.T) {
		reply, err := goakt.Ask(ctxA, pid, &egopb.GetStateCommand{}, 5*time.Second)
		require.NoError(t, err)
		commandReply, ok := reply.(*egopb.CommandReply)
		require.True(t, ok)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())
	})

	require.NoError(t, durableStore.Disconnect(ctx))
	eventStream.Close()
	pause.For(time.Second)
	require.NoError(t, actorSystem.Stop(ctx))
}
