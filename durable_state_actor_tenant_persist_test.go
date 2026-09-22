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
	"encoding/json"
	"errors"
	"sync"
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
	"github.com/pablogore/ego/v4/persistence"
	"github.com/pablogore/ego/v4/tenancy"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
	"github.com/pablogore/ego/v4/testkit"
)

// flakyFirstCommandDurableStateBehavior fails the very first HandleCommand
// invocation it ever receives, as any real behavior might on invalid input,
// and behaves like a normal account behavior on every subsequent call. It
// proves that a failed first command must not appropriate the actor for the
// tenant that sent it (DS2 P1 #1 regression): a later tenant's valid command
// must still be free to claim the actor.
type flakyFirstCommandDurableStateBehavior struct {
	id string

	mu    sync.Mutex
	calls int
}

var _ DurableStateBehavior = (*flakyFirstCommandDurableStateBehavior)(nil)

func newFlakyFirstCommandDurableStateBehavior(id string) *flakyFirstCommandDurableStateBehavior {
	return &flakyFirstCommandDurableStateBehavior{id: id}
}

func (x *flakyFirstCommandDurableStateBehavior) ID() string {
	return x.id
}

func (x *flakyFirstCommandDurableStateBehavior) InitialState() State {
	return new(testpb.Account)
}

// nolint
func (x *flakyFirstCommandDurableStateBehavior) HandleCommand(_ context.Context, command Command, priorVersion uint64, _ State) (State, uint64, error) {
	x.mu.Lock()
	x.calls++
	isFirstCall := x.calls == 1
	x.mu.Unlock()

	if isFirstCall {
		return nil, 0, errors.New("simulated failure on the first command")
	}

	switch cmd := command.(type) {
	case *testpb.CreateAccount:
		return &testpb.Account{
			AccountId:      x.id,
			AccountBalance: cmd.GetAccountBalance(),
		}, priorVersion + 1, nil
	default:
		return nil, 0, errors.New("unhandled command")
	}
}

// callCount reports how many times HandleCommand has run so far.
func (x *flakyFirstCommandDurableStateBehavior) callCount() int {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.calls
}

func (x *flakyFirstCommandDurableStateBehavior) MarshalBinary() ([]byte, error) {
	return json.Marshal(struct {
		ID string `json:"id"`
	}{ID: x.id})
}

func (x *flakyFirstCommandDurableStateBehavior) UnmarshalBinary(data []byte) error {
	aux := struct {
		ID string `json:"id"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	x.id = aux.ID
	return nil
}

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
		require.NoError(t, durableStore.WriteState(ctx, persistence.Unscoped(), newDurableState(tenancy.MarshalMetadata(tenantA)), persistence.Unconditional()))

		entity := &DurableStateActor{
			persistenceID: persistenceID,
			behavior:      NewAccountDurableStateBehavior(persistenceID),
			stateStore:    durableStore,
			tenantAware:   true,
			scope:         persistence.Unscoped(),
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
			scope:         persistence.Unscoped(),
		}

		require.NoError(t, entity.recoverFromStore(ctx))
		assert.Equal(t, noTenantContext, entity.actorTenant)
	})

	t.Run("fails closed when tenant-aware and persisted metadata is absent", func(t *testing.T) {
		durableStore := testkit.NewDurableStore()
		require.NoError(t, durableStore.Connect(ctx))
		require.NoError(t, durableStore.WriteState(ctx, persistence.Unscoped(), newDurableState(nil), persistence.Unconditional()))

		entity := &DurableStateActor{
			persistenceID: persistenceID,
			behavior:      NewAccountDurableStateBehavior(persistenceID),
			stateStore:    durableStore,
			tenantAware:   true,
			scope:         persistence.Unscoped(),
		}

		err := entity.recoverFromStore(ctx)
		require.Error(t, err)
		assert.True(t, errors.Is(err, tenancy.ErrInvalid))
	})

	t.Run("fails closed when tenant-aware and persisted metadata is malformed", func(t *testing.T) {
		durableStore := testkit.NewDurableStore()
		require.NoError(t, durableStore.Connect(ctx))
		require.NoError(t, durableStore.WriteState(ctx, persistence.Unscoped(), newDurableState(tenancy.Metadata{"ego.tenant.scope": "not-a-real-scope"}), persistence.Unconditional()))

		entity := &DurableStateActor{
			persistenceID: persistenceID,
			behavior:      NewAccountDurableStateBehavior(persistenceID),
			stateStore:    durableStore,
			tenantAware:   true,
			scope:         persistence.Unscoped(),
		}

		err := entity.recoverFromStore(ctx)
		require.Error(t, err)
		assert.True(t, errors.Is(err, tenancy.ErrInvalid))
	})

	t.Run("legacy mode never seeds actorTenant, even when metadata is present", func(t *testing.T) {
		durableStore := testkit.NewDurableStore()
		require.NoError(t, durableStore.Connect(ctx))
		require.NoError(t, durableStore.WriteState(ctx, persistence.Unscoped(), newDurableState(tenancy.MarshalMetadata(tenantA)), persistence.Unconditional()))

		entity := &DurableStateActor{
			persistenceID: persistenceID,
			behavior:      NewAccountDurableStateBehavior(persistenceID),
			stateStore:    durableStore,
			scope:         persistence.Unscoped(),
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
	pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
		goakt.WithDependencies(behavior, extensions.NewEntityTenantScope("acme")), goakt.WithLongLived())
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

	newEntity := func(tenantAware bool, tc tenancy.TenantContext, scope persistence.Scope, store *testkit.DurableStore, stream eventstream.Stream) *DurableStateActor {
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
			scope:           scope,
		}
	}

	t.Run("legacy mode writes no tenant metadata", func(t *testing.T) {
		durableStore := testkit.NewDurableStore()
		require.NoError(t, durableStore.Connect(ctx))
		eventStream := eventstream.New()

		entity := newEntity(false, noTenantContext, persistence.Unscoped(), durableStore, eventStream)
		require.NoError(t, entity.persistStateAndPublish(ctx))

		latest, err := durableStore.GetLatestState(ctx, persistence.Unscoped(), persistenceID)
		require.NoError(t, err)
		require.NotNil(t, latest)
		assert.Empty(t, latest.GetTenantMetadata())

		eventStream.Close()
		require.NoError(t, durableStore.Disconnect(ctx))
	})

	t.Run("tenant-aware mode writes the actor's established tenant", func(t *testing.T) {
		tenantA, err := tenancy.NewTenantContext("acme")
		require.NoError(t, err)
		scopeA, err := persistence.NewTenantScope("acme")
		require.NoError(t, err)

		durableStore := testkit.NewDurableStore()
		require.NoError(t, durableStore.Connect(ctx))
		eventStream := eventstream.New()

		entity := newEntity(true, tenantA, scopeA, durableStore, eventStream)
		require.NoError(t, entity.persistStateAndPublish(ctx))

		latest, err := durableStore.GetLatestState(ctx, scopeA, persistenceID)
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
		scopeA, err := persistence.NewTenantScope("acme")
		require.NoError(t, err)

		durableStore := new(mocks.StateStore)
		durableStore.EXPECT().Ping(mock.Anything).Return(nil)
		durableStore.EXPECT().GetLatestState(mock.Anything, scopeA, behavior.ID()).Return(nil, nil)

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
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, extensions.NewEntityTenantScope("acme")), goakt.WithLongLived())
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
		scopeA, err := persistence.NewTenantScope("acme")
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
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, extensions.NewEntityTenantScope("acme")), goakt.WithLongLived())
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

		latest, err := durableStore.GetLatestState(ctx, scopeA, persistenceID)
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

		latest, err := durableStore.GetLatestState(ctx, persistence.Unscoped(), persistenceID)
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

		scopeA, err := persistence.NewTenantScope("acme")
		require.NoError(t, err)

		durableStore := new(mocks.StateStore)
		durableStore.EXPECT().Ping(mock.Anything).Return(nil)
		durableStore.EXPECT().GetLatestState(mock.Anything, scopeA, behavior.ID()).Return(latestState, nil)

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
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, extensions.NewEntityTenantScope("acme")), goakt.WithLongLived())
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
	pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
		goakt.WithDependencies(behavior, extensions.NewEntityTenantScope("acme")), goakt.WithLongLived())
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

// TestDurableStateActorFailedFirstCommandDoesNotAppropriateActor covers PR2
// review round 2's P1 #1, updated for TENANT-003 T4's spawn-time binding:
// establishActorTenant used to run before HandleCommand ever executed or
// validated a command, so a tenant whose first command failed HandleCommand
// (or produced an invalid state/version) would appropriate the actor
// without ever committing a mutation. Under the commitState fix, ownership
// only lands together with a successfully committed state and version.
//
// TENANT-003 T4 changes WHERE ownership comes from: an actor is now bound
// to its tenant at SPAWN (via the injected extensions.EntityTenantScope
// dependency, pre-seeding entity.actorTenant before any command runs), not
// lazily on first successful commit. So a DIFFERENT tenant can no longer
// "claim" this actor after the spawn-bound tenant's first command fails —
// that tenant was never eligible to begin with, regardless of how the
// spawn-bound tenant's commands turn out. This test now proves: the
// spawn-bound tenant's failed first command does not corrupt in-memory
// state or write a durable record, the SAME spawn-bound tenant's retry
// still succeeds afterward, and a foreign tenant is rejected throughout —
// before AND after the spawn-bound tenant's successful commit, and again
// after a restart.
func TestDurableStateActorFailedFirstCommandDoesNotAppropriateActor(t *testing.T) {
	ctx := context.TODO()

	durableStore := testkit.NewDurableStore()
	persistenceID := uuid.NewString()
	behavior := newFlakyFirstCommandDurableStateBehavior(persistenceID)
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
	pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
		goakt.WithDependencies(behavior, extensions.NewEntityTenantScope("acme")), goakt.WithLongLived())
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
	require.NoError(t, err, "Ask itself must not fail; the failure is carried in the CommandReply")
	commandReply, ok := reply.(*egopb.CommandReply)
	require.True(t, ok)
	_, ok = commandReply.GetReply().(*egopb.CommandReply_ErrorReply)
	require.True(t, ok, "tenant A's first command must fail HandleCommand")
	require.EqualValues(t, 1, behavior.callCount())

	scopeA, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)

	latest, err := durableStore.GetLatestState(ctx, scopeA, persistenceID)
	require.NoError(t, err)
	assert.Nil(t, latest, "a failed first command must never write a durable record")

	// Tenant B was never the spawn-bound tenant, so it is rejected here too
	// — this actor was already bound to tenant A at spawn, independent of
	// whether tenant A's own commands succeed.
	ctxB, err := tenancy.Attach(ctx, tenantB)
	require.NoError(t, err)
	reply, err = goakt.Ask(ctxB, pid, &testpb.CreateAccount{AccountBalance: 700}, 5*time.Second)
	require.NoError(t, err, "Ask itself must not fail; the rejection is carried in the CommandReply")
	commandReply, ok = reply.(*egopb.CommandReply)
	require.True(t, ok)
	_, ok = commandReply.GetReply().(*egopb.CommandReply_ErrorReply)
	require.True(t, ok, "tenant B must be rejected: it was never the spawn-bound tenant")
	require.EqualValues(t, 1, behavior.callCount(), "a rejected cross-tenant command must never reach HandleCommand")

	// The spawn-bound tenant (A) retries and succeeds.
	reply, err = goakt.Ask(ctxA, pid, &testpb.CreateAccount{AccountBalance: 900}, 5*time.Second)
	require.NoError(t, err)
	commandReply, ok = reply.(*egopb.CommandReply)
	require.True(t, ok)
	require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply(),
		"the spawn-bound tenant must still be able to claim the actor after its own earlier failed command")
	require.EqualValues(t, 2, behavior.callCount())

	// Tenant B remains rejected after tenant A's successful commit too.
	reply, err = goakt.Ask(ctxB, pid, &testpb.CreateAccount{AccountBalance: 1100}, 5*time.Second)
	require.NoError(t, err, "Ask itself must not fail; the rejection is carried in the CommandReply")
	commandReply, ok = reply.(*egopb.CommandReply)
	require.True(t, ok)
	_, ok = commandReply.GetReply().(*egopb.CommandReply_ErrorReply)
	require.True(t, ok, "tenant B must still be rejected once tenant A owns the actor")
	require.EqualValues(t, 2, behavior.callCount(), "a rejected cross-tenant command must never reach HandleCommand")

	// Restart: PostStop must persist tenant A's ownership (currentVersion >
	// 0, established together with the committed state), and recovery must
	// cross-check the recovered metadata against the restarted instance's
	// own spawn-bound tenant (also A here).
	require.NoError(t, actorSystem.Kill(ctx, behavior.ID()))
	pause.For(time.Second)

	latest, err = durableStore.GetLatestState(ctx, scopeA, persistenceID)
	require.NoError(t, err)
	require.NotNil(t, latest, "tenant A's committed state must survive PostStop")
	assert.EqualValues(t, 1, latest.GetVersionNumber())
	assert.Equal(t, map[string]string(tenancy.MarshalMetadata(tenantA)), latest.GetTenantMetadata())

	restarted := newDurableStateActor()
	pid, err = actorSystem.Spawn(ctx, behavior.ID(), restarted,
		goakt.WithDependencies(behavior, extensions.NewEntityTenantScope("acme")), goakt.WithLongLived())
	require.NoError(t, err)
	require.NotNil(t, pid)
	pause.For(time.Second)

	reply, err = goakt.Ask(ctxB, pid, &testpb.CreateAccount{AccountBalance: 1300}, 5*time.Second)
	require.NoError(t, err)
	commandReply, ok = reply.(*egopb.CommandReply)
	require.True(t, ok)
	_, ok = commandReply.GetReply().(*egopb.CommandReply_ErrorReply)
	require.True(t, ok, "tenant B must still be rejected after restart: recovery cross-checked tenant A as the owner")

	require.NoError(t, durableStore.Disconnect(ctx))
	eventStream.Close()
	pause.For(time.Second)
	require.NoError(t, actorSystem.Stop(ctx))
}

// TestDurableStateActorRecoverFromStoreLegacyVersionZeroGenesis covers PR2
// review round 2's P1 #2: a legacy installation's PostStop used to flush
// InitialState() unconditionally even when the actor never handled a
// command, so a version-0 record can carry a real (non-empty) payload with
// no tenant metadata at all. In tenant-aware mode this must be treated as
// genesis without an owner — not a compromised record that permanently
// blocks recovery — while a committed (version > 0) record must still fail
// closed on missing or invalid metadata. This also exercises the full
// restart path: after recovering as genesis, the first command must be free
// to claim the actor for whichever tenant sends it, and that ownership must
// itself survive a further restart.
func TestDurableStateActorRecoverFromStoreLegacyVersionZeroGenesis(t *testing.T) {
	ctx := context.TODO()
	persistenceID := uuid.NewString()

	// newLegacyRecord builds a fresh record each call — VersionNumber is
	// mutated per-subtest below, and the underlying store keeps whatever
	// pointer it is handed, so sharing one instance across subtests would
	// let an earlier subtest's mutation leak into a later one.
	newLegacyRecord := func(version uint64) *egopb.DurableState {
		legacyStateAny, err := anypb.New(&testpb.Account{AccountId: persistenceID, AccountBalance: 100})
		require.NoError(t, err)
		return &egopb.DurableState{
			PersistenceId:  persistenceID,
			VersionNumber:  version,
			ResultingState: legacyStateAny,
			Timestamp:      time.Now().UnixNano(),
			// TenantMetadata deliberately absent: this is exactly what a
			// pre-tenancy PostStop used to persist unconditionally.
		}
	}

	t.Run("recoverFromStore treats it as genesis, not a fail-closed rejection", func(t *testing.T) {
		durableStore := testkit.NewDurableStore()
		require.NoError(t, durableStore.Connect(ctx))
		require.NoError(t, durableStore.WriteState(ctx, persistence.Unscoped(), newLegacyRecord(0), persistence.Unconditional()))

		entity := &DurableStateActor{
			persistenceID: persistenceID,
			behavior:      NewAccountDurableStateBehavior(persistenceID),
			stateStore:    durableStore,
			tenantAware:   true,
			scope:         persistence.Unscoped(),
		}

		require.NoError(t, entity.recoverFromStore(ctx))
		assert.Equal(t, noTenantContext, entity.actorTenant)
		assert.EqualValues(t, 0, entity.currentVersion)
		assert.Equal(t, entity.behavior.InitialState(), entity.currentState,
			"the legacy payload must be discarded, not unmarshaled, at version 0")
	})

	t.Run("a committed (version > 0) record still fails closed on missing metadata", func(t *testing.T) {
		durableStore := testkit.NewDurableStore()
		require.NoError(t, durableStore.Connect(ctx))
		require.NoError(t, durableStore.WriteState(ctx, persistence.Unscoped(), newLegacyRecord(1), persistence.Unconditional()))

		entity := &DurableStateActor{
			persistenceID: persistenceID,
			behavior:      NewAccountDurableStateBehavior(persistenceID),
			stateStore:    durableStore,
			tenantAware:   true,
			scope:         persistence.Unscoped(),
		}

		err := entity.recoverFromStore(ctx)
		require.Error(t, err)
		assert.True(t, errors.Is(err, tenancy.ErrInvalid))
	})

	t.Run("end-to-end: a full actor spawns clean off a legacy version-0 record and a command still claims it", func(t *testing.T) {
		durableStore := testkit.NewDurableStore()
		behavior := newTenancyProbeDurableStateBehavior(persistenceID)
		require.NoError(t, durableStore.Connect(ctx))
		require.NoError(t, durableStore.WriteState(ctx, persistence.Unscoped(), newLegacyRecord(0), persistence.Unconditional()))

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
		pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
			goakt.WithDependencies(behavior, extensions.NewEntityTenantScope("acme")), goakt.WithLongLived())
		require.NoError(t, err, "recovering a legacy version-0 record must not block Spawn/PreStart in tenant-aware mode")
		require.NotNil(t, pid)
		pause.For(time.Second)

		tenantA, err := tenancy.NewTenantContext("acme")
		require.NoError(t, err)
		ctxA, err := tenancy.Attach(ctx, tenantA)
		require.NoError(t, err)
		reply, err := goakt.Ask(ctxA, pid, &testpb.CreateAccount{AccountBalance: 500}, 5*time.Second)
		require.NoError(t, err)
		commandReply, ok := reply.(*egopb.CommandReply)
		require.True(t, ok)
		require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply(),
			"a genesis actor recovered from a legacy version-0 record must still accept its first command")

		require.NoError(t, actorSystem.Kill(ctx, behavior.ID()))
		pause.For(time.Second)

		scopeA, err := persistence.NewTenantScope("acme")
		require.NoError(t, err)

		latest, err := durableStore.GetLatestState(ctx, scopeA, persistenceID)
		require.NoError(t, err)
		require.NotNil(t, latest)
		assert.EqualValues(t, 1, latest.GetVersionNumber())
		assert.Equal(t, map[string]string(tenancy.MarshalMetadata(tenantA)), latest.GetTenantMetadata(),
			"the newly committed ownership must survive PostStop after recovering from legacy genesis")

		require.NoError(t, durableStore.Disconnect(ctx))
		eventStream.Close()
		pause.For(time.Second)
		require.NoError(t, actorSystem.Stop(ctx))
	})
}
