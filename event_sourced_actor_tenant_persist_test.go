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
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/eventstream"
	"github.com/pablogore/ego/v4/internal/extensions"
	"github.com/pablogore/ego/v4/internal/pause"
	"github.com/pablogore/ego/v4/persistence"
	"github.com/pablogore/ego/v4/tenancy"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
	"github.com/pablogore/ego/v4/testkit"
)

// TestEventSourcedActorMarshalEventWritesTenantMetadata covers tasks
// 2.1/2.2 (sdd/ego-tenant-002/tasks Phase 2): marshalEvent must serialize
// the per-command TenantContext it is given into the built *egopb.Event's
// TenantMetadata field, using tenancy.MarshalMetadata's exact ego.tenant.*
// keys (D9 carrier reuse), round-tripping through tenancy.UnmarshalMetadata
// for both a tenant-scoped and an administrative-scoped context. Legacy
// mode (tenantAware == false) must write no tenant metadata at all (D2).
func TestEventSourcedActorMarshalEventWritesTenantMetadata(t *testing.T) {
	t.Run("legacy mode writes no tenant metadata", func(t *testing.T) {
		entity := &EventSourcedActor{persistenceID: "acct-1"}

		envelope, err := entity.marshalEvent(context.Background(), &testpb.AccountCreated{AccountId: "acct-1"}, tenancy.TenantContext{}, 1, time.Now(), 0)
		require.NoError(t, err)
		assert.Empty(t, envelope.GetTenantMetadata())
	})

	t.Run("tenant-scoped context round-trips via tenancy.UnmarshalMetadata", func(t *testing.T) {
		entity := &EventSourcedActor{persistenceID: "acct-1", tenantAware: true}
		tc, err := tenancy.NewTenantContext("acme")
		require.NoError(t, err)

		envelope, err := entity.marshalEvent(context.Background(), &testpb.AccountCreated{AccountId: "acct-1"}, tc, 1, time.Now(), 0)
		require.NoError(t, err)

		require.NotEmpty(t, envelope.GetTenantMetadata())
		assert.Equal(t, map[string]string(tenancy.MarshalMetadata(tc)), envelope.GetTenantMetadata())

		roundTripped, err := tenancy.UnmarshalMetadata(envelope.GetTenantMetadata())
		require.NoError(t, err)
		assert.Equal(t, tc, roundTripped)
	})

	t.Run("administrative-scoped context round-trips via tenancy.UnmarshalMetadata", func(t *testing.T) {
		entity := &EventSourcedActor{persistenceID: "acct-1", tenantAware: true}
		admin, err := tenancy.NewAdministrative("ops-team", "crypto-shred")
		require.NoError(t, err)
		tc, err := tenancy.NewAdministrativeContext(admin)
		require.NoError(t, err)

		envelope, err := entity.marshalEvent(context.Background(), &testpb.AccountCreated{AccountId: "acct-1"}, tc, 1, time.Now(), 0)
		require.NoError(t, err)

		roundTripped, err := tenancy.UnmarshalMetadata(envelope.GetTenantMetadata())
		require.NoError(t, err)
		assert.Equal(t, tc, roundTripped)
	})
}

// TestEventSourcedActorNewSnapshotEnvelopeWritesTenantMetadata covers
// tasks 2.3/2.4 (Phase 2, D5): newSnapshotEnvelope must write the actor's
// established tenant identity (entity.actorTenant) onto the built
// *egopb.Snapshot, so a snapshot taken without any in-flight command
// context (e.g. after a batch flush) still carries tenant identity for
// recover() (Phase 3) to seed from. Legacy mode writes nothing.
func TestEventSourcedActorNewSnapshotEnvelopeWritesTenantMetadata(t *testing.T) {
	t.Run("legacy mode writes no tenant metadata", func(t *testing.T) {
		entity := &EventSourcedActor{persistenceID: "acct-1", eventsCounter: 3, lastCommandTime: time.Now()}
		snapshot := entity.newSnapshotEnvelope(nil)
		assert.Empty(t, snapshot.GetTenantMetadata())
	})

	t.Run("tenant-aware mode writes the actor's established tenant", func(t *testing.T) {
		tc, err := tenancy.NewTenantContext("acme")
		require.NoError(t, err)

		entity := &EventSourcedActor{
			persistenceID:   "acct-1",
			eventsCounter:   3,
			lastCommandTime: time.Now(),
			tenantAware:     true,
			actorTenant:     tc,
		}

		snapshot := entity.newSnapshotEnvelope(nil)
		require.NotEmpty(t, snapshot.GetTenantMetadata())

		roundTripped, err := tenancy.UnmarshalMetadata(snapshot.GetTenantMetadata())
		require.NoError(t, err)
		assert.Equal(t, tc, roundTripped)
	})
}

// TestEventSourcedActorRecoverSeedsActorTenant covers tasks 3.1-3.6
// (Phase 3, D5/D6): recover() must seed entity.actorTenant from persisted
// tenant metadata before this actor accepts any command — from the latest
// event, from the snapshot when events are retention-deleted, cross-check
// the two when both exist, and fail closed (not open) when tenant-aware and
// metadata is absent or malformed on persisted data that exists.
func TestEventSourcedActorRecoverSeedsActorTenant(t *testing.T) {
	ctx := context.Background()
	persistenceID := "acct-1"

	tenantA, err := tenancy.NewTenantContext("acme")
	require.NoError(t, err)
	tenantB, err := tenancy.NewTenantContext("globex")
	require.NoError(t, err)

	newEvent := func(seqNr uint64, tc tenancy.TenantContext) *egopb.Event {
		eventAny, err := anypb.New(&testpb.AccountCreated{AccountId: persistenceID, AccountBalance: 100})
		require.NoError(t, err)
		evt := &egopb.Event{
			PersistenceId:  persistenceID,
			SequenceNumber: seqNr,
			Event:          eventAny,
			Timestamp:      time.Now().UnixNano(),
		}
		if tc != noTenantContext {
			evt.TenantMetadata = tenancy.MarshalMetadata(tc)
		}
		return evt
	}

	newSnapshot := func(seqNr uint64, tc tenancy.TenantContext) *egopb.Snapshot {
		stateAny, err := anypb.New(&testpb.Account{AccountId: persistenceID, AccountBalance: 100})
		require.NoError(t, err)
		snap := &egopb.Snapshot{
			PersistenceId:  persistenceID,
			SequenceNumber: seqNr,
			State:          stateAny,
			Timestamp:      time.Now().Unix(),
		}
		if tc != noTenantContext {
			snap.TenantMetadata = tenancy.MarshalMetadata(tc)
		}
		return snap
	}

	t.Run("3.1/3.2: seeds actorTenant from the latest event's metadata", func(t *testing.T) {
		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		require.NoError(t, eventStore.WriteEvents(ctx, persistence.Unscoped(), []*egopb.Event{newEvent(1, tenantA)}, persistence.Unconditional()))

		entity := &EventSourcedActor{
			persistenceID: persistenceID,
			behavior:      NewAccountEventSourcedBehavior(persistenceID),
			eventsStore:   eventStore,
			tenantAware:   true,
		}

		require.NoError(t, entity.recover(ctx))
		assert.Equal(t, tenantA, entity.actorTenant)
	})

	t.Run("3.3/3.4: seeds actorTenant from the snapshot when events are retention-deleted", func(t *testing.T) {
		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		snapshotStore := testkit.NewSnapshotStore()
		require.NoError(t, snapshotStore.Connect(ctx))
		require.NoError(t, snapshotStore.WriteSnapshot(ctx, persistence.Unscoped(), newSnapshot(5, tenantA)))

		entity := &EventSourcedActor{
			persistenceID: persistenceID,
			behavior:      NewAccountEventSourcedBehavior(persistenceID),
			eventsStore:   eventStore,
			snapshotStore: snapshotStore,
			tenantAware:   true,
		}

		require.NoError(t, entity.recover(ctx))
		assert.Equal(t, tenantA, entity.actorTenant)
	})

	t.Run("3.3/3.4: snapshot and latest event agreeing on tenant both succeed and match", func(t *testing.T) {
		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		require.NoError(t, eventStore.WriteEvents(ctx, persistence.Unscoped(), []*egopb.Event{newEvent(6, tenantA)}, persistence.Unconditional()))
		snapshotStore := testkit.NewSnapshotStore()
		require.NoError(t, snapshotStore.Connect(ctx))
		require.NoError(t, snapshotStore.WriteSnapshot(ctx, persistence.Unscoped(), newSnapshot(5, tenantA)))

		entity := &EventSourcedActor{
			persistenceID: persistenceID,
			behavior:      NewAccountEventSourcedBehavior(persistenceID),
			eventsStore:   eventStore,
			snapshotStore: snapshotStore,
			tenantAware:   true,
		}

		require.NoError(t, entity.recover(ctx))
		assert.Equal(t, tenantA, entity.actorTenant)
	})

	t.Run("3.3/3.4: snapshot and latest event disagreeing on tenant fails closed with ErrDenied", func(t *testing.T) {
		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		require.NoError(t, eventStore.WriteEvents(ctx, persistence.Unscoped(), []*egopb.Event{newEvent(6, tenantB)}, persistence.Unconditional()))
		snapshotStore := testkit.NewSnapshotStore()
		require.NoError(t, snapshotStore.Connect(ctx))
		require.NoError(t, snapshotStore.WriteSnapshot(ctx, persistence.Unscoped(), newSnapshot(5, tenantA)))

		entity := &EventSourcedActor{
			persistenceID: persistenceID,
			behavior:      NewAccountEventSourcedBehavior(persistenceID),
			eventsStore:   eventStore,
			snapshotStore: snapshotStore,
			tenantAware:   true,
		}

		err := entity.recover(ctx)
		require.Error(t, err)
		assert.True(t, errors.Is(err, tenancy.ErrDenied))
	})

	t.Run("3.5/3.6: fails closed when tenant-aware and the latest event carries no tenant metadata", func(t *testing.T) {
		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		require.NoError(t, eventStore.WriteEvents(ctx, persistence.Unscoped(), []*egopb.Event{newEvent(1, noTenantContext)}, persistence.Unconditional()))

		entity := &EventSourcedActor{
			persistenceID: persistenceID,
			behavior:      NewAccountEventSourcedBehavior(persistenceID),
			eventsStore:   eventStore,
			tenantAware:   true,
		}

		err := entity.recover(ctx)
		require.Error(t, err)
		assert.True(t, errors.Is(err, tenancy.ErrInvalid))
	})

	t.Run("3.5/3.6: fails closed when tenant-aware and the snapshot carries no tenant metadata", func(t *testing.T) {
		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		snapshotStore := testkit.NewSnapshotStore()
		require.NoError(t, snapshotStore.Connect(ctx))
		require.NoError(t, snapshotStore.WriteSnapshot(ctx, persistence.Unscoped(), newSnapshot(5, noTenantContext)))

		entity := &EventSourcedActor{
			persistenceID: persistenceID,
			behavior:      NewAccountEventSourcedBehavior(persistenceID),
			eventsStore:   eventStore,
			snapshotStore: snapshotStore,
			tenantAware:   true,
		}

		err := entity.recover(ctx)
		require.Error(t, err)
		assert.True(t, errors.Is(err, tenancy.ErrInvalid))
	})

	t.Run("legacy mode never seeds actorTenant, even when metadata is present", func(t *testing.T) {
		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		require.NoError(t, eventStore.WriteEvents(ctx, persistence.Unscoped(), []*egopb.Event{newEvent(1, tenantA)}, persistence.Unconditional()))

		entity := &EventSourcedActor{
			persistenceID: persistenceID,
			behavior:      NewAccountEventSourcedBehavior(persistenceID),
			eventsStore:   eventStore,
		}

		require.NoError(t, entity.recover(ctx))
		assert.Equal(t, noTenantContext, entity.actorTenant)
	})

	t.Run("a brand new actor with no persisted data recovers without a tenant identity yet", func(t *testing.T) {
		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))

		entity := &EventSourcedActor{
			persistenceID: persistenceID,
			behavior:      NewAccountEventSourcedBehavior(persistenceID),
			eventsStore:   eventStore,
			tenantAware:   true,
		}

		require.NoError(t, entity.recover(ctx))
		assert.Equal(t, noTenantContext, entity.actorTenant)
	})

	// The next two subtests are PR1 review-comment regressions: recover()
	// only validated latestEvent's tenant metadata before this fix (D3's
	// fail-closed intent applied to the wrong event). replayEvents replayed
	// every event strictly between the snapshot point and latestSeqNr
	// unchecked, so an intermediate event belonging to another tenant, or
	// missing tenant metadata altogether, would be silently applied to
	// state as long as the *latest* event still carried the actor's own
	// tenant.
	t.Run("an intermediate event belonging to a different tenant fails closed with ErrDenied", func(t *testing.T) {
		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		require.NoError(t, eventStore.WriteEvents(ctx, persistence.Unscoped(), []*egopb.Event{
			newEvent(1, tenantA),
			newEvent(2, tenantB),
			newEvent(3, tenantA),
		}, persistence.Unconditional()))

		entity := &EventSourcedActor{
			persistenceID: persistenceID,
			behavior:      NewAccountEventSourcedBehavior(persistenceID),
			eventsStore:   eventStore,
			tenantAware:   true,
		}

		err := entity.recover(ctx)
		require.Error(t, err)
		assert.True(t, errors.Is(err, tenancy.ErrDenied))
	})

	t.Run("an intermediate event with no tenant metadata fails closed with ErrInvalid", func(t *testing.T) {
		eventStore := testkit.NewEventsStore()
		require.NoError(t, eventStore.Connect(ctx))
		require.NoError(t, eventStore.WriteEvents(ctx, persistence.Unscoped(), []*egopb.Event{
			newEvent(1, tenantA),
			newEvent(2, noTenantContext),
			newEvent(3, tenantA),
		}, persistence.Unconditional()))

		entity := &EventSourcedActor{
			persistenceID: persistenceID,
			behavior:      NewAccountEventSourcedBehavior(persistenceID),
			eventsStore:   eventStore,
			tenantAware:   true,
		}

		err := entity.recover(ctx)
		require.Error(t, err)
		assert.True(t, errors.Is(err, tenancy.ErrInvalid))
	})
}

// TestEventSourcedActorSeedActorTenant covers the seedActorTenant helper
// (design.md Interfaces/Contracts, EGO-TENANT-002) in isolation: a no-op in
// legacy mode, an unconditional first seed, and a VerifyUnchanged
// cross-check once already seeded.
func TestEventSourcedActorSeedActorTenant(t *testing.T) {
	tenantA, err := tenancy.NewTenantContext("acme")
	require.NoError(t, err)
	tenantB, err := tenancy.NewTenantContext("globex")
	require.NoError(t, err)

	t.Run("legacy mode is a no-op", func(t *testing.T) {
		entity := &EventSourcedActor{}
		require.NoError(t, entity.seedActorTenant(tenantA))
		assert.Equal(t, noTenantContext, entity.actorTenant)
	})

	t.Run("first call unconditionally seeds", func(t *testing.T) {
		entity := &EventSourcedActor{tenantAware: true}
		require.NoError(t, entity.seedActorTenant(tenantA))
		assert.Equal(t, tenantA, entity.actorTenant)
	})

	t.Run("second call with the same tenant is a no-op success", func(t *testing.T) {
		entity := &EventSourcedActor{tenantAware: true, actorTenant: tenantA}
		require.NoError(t, entity.seedActorTenant(tenantA))
		assert.Equal(t, tenantA, entity.actorTenant)
	})

	t.Run("second call with a different tenant fails with ErrDenied", func(t *testing.T) {
		entity := &EventSourcedActor{tenantAware: true, actorTenant: tenantA}
		err := entity.seedActorTenant(tenantB)
		require.Error(t, err)
		assert.True(t, errors.Is(err, tenancy.ErrDenied))
		assert.Equal(t, tenantA, entity.actorTenant, "a rejected re-seed must not overwrite the original")
	})
}

// TestEventSourcedActorProcessCommandAndReplyRejectsCrossTenant covers
// tasks 4.4/4.5 (Phase 4): the non-batched path (processCommandAndReply)
// must reject a command whose resolved TenantContext differs from this
// actor's already-seeded actorTenant, mirroring processAndBatch's gate —
// net-new enforcement per design.md risk #3 (this gate previously only
// proved presence via tenancy.Require, never identity match).
func TestEventSourcedActorProcessCommandAndReplyRejectsCrossTenant(t *testing.T) {
	ctx := context.TODO()

	eventStore := testkit.NewEventsStore()
	persistenceID := uuid.NewString()
	behavior := NewAccountEventSourcedBehavior(persistenceID)

	require.NoError(t, eventStore.Connect(ctx))
	pause.For(time.Second)

	eventStream := eventstream.New()

	actorSystem, err := goakt.NewActorSystem("TestActorSystem",
		goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
		goakt.WithExtensions(
			extensions.NewEventsStore(eventStore),
			extensions.NewEventsStream(eventStream),
			extensions.NewTenancyMarker(),
		),
		goakt.WithActorInitMaxRetries(3))
	require.NoError(t, err)
	require.NoError(t, actorSystem.Start(ctx))
	pause.For(time.Second)

	actor := newEventSourcedActor()
	pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
		goakt.WithDependencies(behavior),
		goakt.WithLongLived(), goakt.WithStashing())
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
	require.True(t, ok, "a cross-tenant command on the non-batched path must be rejected")

	wantErr := tenancy.VerifyUnchanged(tenantA, tenantB)
	assert.Equal(t, wantErr.Error(), errorReply.ErrorReply.GetMessage(),
		"rejection must be the fail-closed tenant error VerifyUnchanged produces, not an invented error type")

	require.NoError(t, eventStore.Disconnect(ctx))
	eventStream.Close()
	pause.For(time.Second)
	require.NoError(t, actorSystem.Stop(ctx))
}

// TestEventSourcedActorGetStateCommandRejectsCrossTenant is a PR1
// review-comment regression: Receive dispatched *egopb.GetStateCommand
// straight to getStateAndReply, bypassing every tenant gate that
// processCommandAndReply/processAndBatch enforce. A resolved tenant B could
// read tenant A's full committed state by sending GetStateCommand instead
// of a real command. getStateAndReply must apply the same T4-A style gate:
// require a resolved TenantContext and reject one that mismatches the
// actor's already-established actorTenant.
func TestEventSourcedActorGetStateCommandRejectsCrossTenant(t *testing.T) {
	ctx := context.TODO()

	eventStore := testkit.NewEventsStore()
	persistenceID := uuid.NewString()
	behavior := NewAccountEventSourcedBehavior(persistenceID)

	require.NoError(t, eventStore.Connect(ctx))
	pause.For(time.Second)

	eventStream := eventstream.New()

	actorSystem, err := goakt.NewActorSystem("TestActorSystem",
		goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
		goakt.WithExtensions(
			extensions.NewEventsStore(eventStore),
			extensions.NewEventsStream(eventStream),
			extensions.NewTenancyMarker(),
		),
		goakt.WithActorInitMaxRetries(3))
	require.NoError(t, err)
	require.NoError(t, actorSystem.Start(ctx))
	pause.For(time.Second)

	actor := newEventSourcedActor()
	pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
		goakt.WithDependencies(behavior),
		goakt.WithLongLived(), goakt.WithStashing())
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

	ctxB, err := tenancy.Attach(ctx, tenantB)
	require.NoError(t, err)
	reply, err = goakt.Ask(ctxB, pid, &egopb.GetStateCommand{}, 5*time.Second)
	require.NoError(t, err, "Ask itself must not fail; the rejection is carried in the CommandReply")
	commandReply, ok = reply.(*egopb.CommandReply)
	require.True(t, ok)
	errorReply, ok := commandReply.GetReply().(*egopb.CommandReply_ErrorReply)
	require.True(t, ok, "GetStateCommand from a different tenant must be rejected, not return tenant A's state")

	wantErr := tenancy.VerifyUnchanged(tenantA, tenantB)
	assert.Equal(t, wantErr.Error(), errorReply.ErrorReply.GetMessage(),
		"rejection must be the fail-closed tenant error VerifyUnchanged produces, not an invented error type")

	require.NoError(t, eventStore.Disconnect(ctx))
	eventStream.Close()
	pause.For(time.Second)
	require.NoError(t, actorSystem.Stop(ctx))
}

// TestEventSourcedActorGetStateCommandRequiresTenantWhenTenantAware is a
// companion to the cross-tenant regression above: in tenant-aware mode, a
// GetStateCommand with no resolved TenantContext at all must also be
// rejected (tenancy.Require's absence path), matching
// processCommandAndReply's T4-A gate rather than silently returning state.
func TestEventSourcedActorGetStateCommandRequiresTenantWhenTenantAware(t *testing.T) {
	ctx := context.TODO()

	eventStore := testkit.NewEventsStore()
	persistenceID := uuid.NewString()
	behavior := NewAccountEventSourcedBehavior(persistenceID)

	require.NoError(t, eventStore.Connect(ctx))
	pause.For(time.Second)

	eventStream := eventstream.New()

	actorSystem, err := goakt.NewActorSystem("TestActorSystem",
		goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
		goakt.WithExtensions(
			extensions.NewEventsStore(eventStore),
			extensions.NewEventsStream(eventStream),
			extensions.NewTenancyMarker(),
		),
		goakt.WithActorInitMaxRetries(3))
	require.NoError(t, err)
	require.NoError(t, actorSystem.Start(ctx))
	pause.For(time.Second)

	actor := newEventSourcedActor()
	pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
		goakt.WithDependencies(behavior),
		goakt.WithLongLived(), goakt.WithStashing())
	require.NoError(t, err)
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
	require.IsType(t, new(egopb.CommandReply_StateReply), commandReply.GetReply())

	reply, err = goakt.Ask(ctx, pid, &egopb.GetStateCommand{}, 5*time.Second)
	require.NoError(t, err, "Ask itself must not fail; the rejection is carried in the CommandReply")
	commandReply, ok = reply.(*egopb.CommandReply)
	require.True(t, ok)
	_, ok = commandReply.GetReply().(*egopb.CommandReply_ErrorReply)
	require.True(t, ok, "GetStateCommand with no resolved tenant must be rejected on a tenant-aware actor")

	require.NoError(t, eventStore.Disconnect(ctx))
	eventStream.Close()
	pause.For(time.Second)
	require.NoError(t, actorSystem.Stop(ctx))
}

// TestEventSourcedActorTenantIdentitySurvivesRestart covers task 4.6: a
// cross-tenant command must still be rejected after this actor restarts and
// recovers its tenant identity from persisted event metadata (Phase 3),
// not only while the original in-memory actorTenant is still warm.
func TestEventSourcedActorTenantIdentitySurvivesRestart(t *testing.T) {
	ctx := context.TODO()

	eventStore := testkit.NewEventsStore()
	persistenceID := uuid.NewString()
	behavior := NewAccountEventSourcedBehavior(persistenceID)

	require.NoError(t, eventStore.Connect(ctx))
	pause.For(time.Second)

	eventStream := eventstream.New()

	actorSystem, err := goakt.NewActorSystem("TestActorSystem",
		goakt.WithLogger(newLoggerAdapter(DiscardLogger)),
		goakt.WithExtensions(
			extensions.NewEventsStore(eventStore),
			extensions.NewEventsStream(eventStream),
			extensions.NewTenancyMarker(),
		),
		goakt.WithActorInitMaxRetries(3))
	require.NoError(t, err)
	require.NoError(t, actorSystem.Start(ctx))
	pause.For(time.Second)

	tenantA, err := tenancy.NewTenantContext("acme")
	require.NoError(t, err)
	tenantB, err := tenancy.NewTenantContext("globex")
	require.NoError(t, err)

	// First actor instance: tenant A persists, establishing actorTenant,
	// then is stopped so no in-memory state survives.
	actor := newEventSourcedActor()
	pid, err := actorSystem.Spawn(ctx, behavior.ID(), actor,
		goakt.WithDependencies(behavior),
		goakt.WithLongLived(), goakt.WithStashing())
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

	// Second actor instance under the same persistence ID: recover() must
	// seed actorTenant from the persisted event before this new in-process
	// actor accepts any command.
	restarted := newEventSourcedActor()
	pid, err = actorSystem.Spawn(ctx, behavior.ID(), restarted,
		goakt.WithDependencies(behavior),
		goakt.WithLongLived(), goakt.WithStashing())
	require.NoError(t, err)
	require.NotNil(t, pid)
	pause.For(time.Second)

	ctxB, err := tenancy.Attach(ctx, tenantB)
	require.NoError(t, err)
	reply, err = goakt.Ask(ctxB, pid, &testpb.CreditAccount{AccountId: persistenceID, Balance: 10}, 5*time.Second)
	require.NoError(t, err, "Ask itself must not fail; the rejection is carried in the CommandReply")
	commandReply, ok = reply.(*egopb.CommandReply)
	require.True(t, ok)
	errorReply, ok := commandReply.GetReply().(*egopb.CommandReply_ErrorReply)
	require.True(t, ok, "tenant identity recovered from persisted event metadata must survive actor restart")

	wantErr := tenancy.VerifyUnchanged(tenantA, tenantB)
	assert.Equal(t, wantErr.Error(), errorReply.ErrorReply.GetMessage(),
		"rejection must be the fail-closed tenant error VerifyUnchanged produces, not an invented error type")

	require.NoError(t, eventStore.Disconnect(ctx))
	eventStream.Close()
	pause.For(time.Second)
	require.NoError(t, actorSystem.Stop(ctx))
}
