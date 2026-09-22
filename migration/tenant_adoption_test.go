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

package migration

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	ego "github.com/pablogore/ego/v4"
	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/persistence"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
	"github.com/pablogore/ego/v4/tenancy"
	"github.com/pablogore/ego/v4/testkit"
)

// corruptingEventsStore wraps a persistence.EventsStore and, for writes
// targeting corruptScope only, runs mangle on a clone of every event before
// delegating the write. It exists to prove the events verification in
// adoptEvents (tenant_adoption.go) catches a corrupted write that the OLD
// verification (a bare len(written) != len(sourceEvents) count check) could
// not: mangle preserves the event count and every SequenceNumber, changing
// only what a count-only check cannot see.
type corruptingEventsStore struct {
	persistence.EventsStore
	corruptScope persistence.Scope
	mangle       func(*egopb.Event)
}

func (c *corruptingEventsStore) WriteEvents(ctx context.Context, scope persistence.Scope, events []*egopb.Event, precondition persistence.WritePrecondition) error {
	if !scope.Equal(c.corruptScope) {
		return c.EventsStore.WriteEvents(ctx, scope, events, precondition)
	}
	corrupted := make([]*egopb.Event, len(events))
	for i, e := range events {
		clone, ok := proto.Clone(e).(*egopb.Event)
		if !ok {
			return errors.New("corruptingEventsStore: clone failed")
		}
		c.mangle(clone)
		corrupted[i] = clone
	}
	return c.EventsStore.WriteEvents(ctx, scope, corrupted, precondition)
}

// corruptingSnapshotStore is corruptingEventsStore's snapshot-store
// counterpart: it corrupts a write's SNAPSHOT while preserving its
// SequenceNumber, which is all the OLD verification
// (written.GetSequenceNumber() != snapshot.GetSequenceNumber()) ever
// checked.
type corruptingSnapshotStore struct {
	persistence.SnapshotStore
	corruptScope persistence.Scope
	mangle       func(*egopb.Snapshot)
}

func (c *corruptingSnapshotStore) WriteSnapshot(ctx context.Context, scope persistence.Scope, snapshot *egopb.Snapshot) error {
	if !scope.Equal(c.corruptScope) {
		return c.SnapshotStore.WriteSnapshot(ctx, scope, snapshot)
	}
	clone, ok := proto.Clone(snapshot).(*egopb.Snapshot)
	if !ok {
		return errors.New("corruptingSnapshotStore: clone failed")
	}
	c.mangle(clone)
	return c.SnapshotStore.WriteSnapshot(ctx, scope, clone)
}

// corruptingStateStore is corruptingEventsStore's durable-state-store
// counterpart: it corrupts a write's STATE while preserving its
// VersionNumber, which is all the OLD verification
// (written.GetVersionNumber() != state.GetVersionNumber()) ever checked.
type corruptingStateStore struct {
	persistence.StateStore
	corruptScope persistence.Scope
	mangle       func(*egopb.DurableState)
}

func (c *corruptingStateStore) WriteState(ctx context.Context, scope persistence.Scope, state *egopb.DurableState, precondition persistence.WritePrecondition) error {
	if !scope.Equal(c.corruptScope) {
		return c.StateStore.WriteState(ctx, scope, state, precondition)
	}
	clone, ok := proto.Clone(state).(*egopb.DurableState)
	if !ok {
		return errors.New("corruptingStateStore: clone failed")
	}
	c.mangle(clone)
	return c.StateStore.WriteState(ctx, scope, clone, precondition)
}

// newLegacyEvent builds a plain (non-tenant) event for persistenceID at
// seqNr, exactly as pre-tenancy code would have written it: no
// TenantMetadata at all.
func newLegacyEvent(t *testing.T, persistenceID string, seqNr uint64, ts int64) *egopb.Event {
	t.Helper()
	payload, err := anypb.New(timestamppb.New(time.Unix(ts, 0)))
	require.NoError(t, err)
	return &egopb.Event{
		PersistenceId:  persistenceID,
		SequenceNumber: seqNr,
		Event:          payload,
		Timestamp:      ts,
	}
}

func newLegacySnapshot(t *testing.T, persistenceID string, seqNr uint64, ts int64) *egopb.Snapshot {
	t.Helper()
	payload, err := anypb.New(timestamppb.New(time.Unix(ts, 0)))
	require.NoError(t, err)
	return &egopb.Snapshot{
		PersistenceId:  persistenceID,
		SequenceNumber: seqNr,
		State:          payload,
		Timestamp:      ts,
	}
}

func newLegacyDurableState(t *testing.T, persistenceID string, version uint64, ts int64) *egopb.DurableState {
	t.Helper()
	payload, err := anypb.New(timestamppb.New(time.Unix(ts, 0)))
	require.NoError(t, err)
	return &egopb.DurableState{
		PersistenceId:  persistenceID,
		VersionNumber:  version,
		ResultingState: payload,
		Timestamp:      ts,
	}
}

// fixedAssignment returns a TenantAssignment that maps every id present in
// assignments to its tenant, and reports ok=false for anything else.
func fixedAssignment(assignments map[string]tenancy.TenantID) TenantAssignment {
	return func(_ context.Context, persistenceID string) (tenancy.TenantID, bool, error) {
		id, ok := assignments[persistenceID]
		return id, ok, nil
	}
}

// adoptionAccountBehavior is a minimal ego.EventSourcedBehavior used only by
// TestTenantAdopterEndToEndRecoveryThroughRealActor to prove a real,
// tenant-bound EventSourcedActor recovers migrated data. It is not exported
// from the root package, so this test defines its own copy rather than
// reusing the root package's internal test helper.
type adoptionAccountBehavior struct {
	id string
}

var _ ego.EventSourcedBehavior = (*adoptionAccountBehavior)(nil)

func (x *adoptionAccountBehavior) ID() string { return x.id }

func (x *adoptionAccountBehavior) InitialState() ego.State { return new(testpb.Account) }

func (x *adoptionAccountBehavior) HandleCommand(_ context.Context, command ego.Command, _ ego.State) ([]ego.Event, error) {
	cmd, ok := command.(*testpb.CreditAccount)
	if !ok || cmd.GetAccountId() != x.id {
		return nil, errors.New("unhandled command")
	}
	return []ego.Event{
		&testpb.AccountCredited{AccountId: cmd.GetAccountId(), AccountBalance: cmd.GetBalance()},
	}, nil
}

func (x *adoptionAccountBehavior) HandleEvent(_ context.Context, event ego.Event, priorState ego.State) (ego.State, error) {
	switch evt := event.(type) {
	case *testpb.AccountCreated:
		return &testpb.Account{AccountId: evt.GetAccountId(), AccountBalance: evt.GetAccountBalance()}, nil
	case *testpb.AccountCredited:
		account, _ := priorState.(*testpb.Account)
		return &testpb.Account{
			AccountId:      evt.GetAccountId(),
			AccountBalance: account.GetAccountBalance() + evt.GetAccountBalance(),
		}, nil
	default:
		return nil, errors.New("unhandled event")
	}
}

func (x *adoptionAccountBehavior) MarshalBinary() ([]byte, error) {
	return []byte(x.id), nil
}

func (x *adoptionAccountBehavior) UnmarshalBinary(data []byte) error {
	x.id = string(data)
	return nil
}

func TestNewTenantAdopter(t *testing.T) {
	t.Run("requires a TenantAssignment", func(t *testing.T) {
		_, err := NewTenantAdopter(nil, WithEventsStore(testkit.NewEventsStore()))
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrAssignmentRequired)
	})

	t.Run("requires at least one store", func(t *testing.T) {
		_, err := NewTenantAdopter(fixedAssignment(nil))
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrNoStoresConfigured)
	})

	t.Run("defaults to dry-run, Unscoped source, and a resolved logger", func(t *testing.T) {
		a, err := NewTenantAdopter(fixedAssignment(nil), WithEventsStore(testkit.NewEventsStore()))
		require.NoError(t, err)
		assert.False(t, a.write)
		assert.True(t, a.sourceScope.IsUnscoped())
		assert.NotNil(t, a.logger)
	})
}

func TestTenantAdopterDryRunWritesNothing(t *testing.T) {
	ctx := context.Background()
	eventsStore := testkit.NewEventsStore()
	require.NoError(t, eventsStore.Connect(ctx))
	snapshotStore := testkit.NewSnapshotStore()
	require.NoError(t, snapshotStore.Connect(ctx))
	stateStore := testkit.NewDurableStore()
	require.NoError(t, stateStore.Connect(ctx))

	const id = "dry-run-1"
	require.NoError(t, eventsStore.WriteEvents(ctx, persistence.Unscoped(), []*egopb.Event{newLegacyEvent(t, id, 1, 100)}, persistence.Unconditional()))
	require.NoError(t, snapshotStore.WriteSnapshot(ctx, persistence.Unscoped(), newLegacySnapshot(t, id, 1, 100)))
	require.NoError(t, stateStore.WriteState(ctx, persistence.Unscoped(), newLegacyDurableState(t, id, 1, 100), persistence.Unconditional()))

	adopter, err := NewTenantAdopter(
		fixedAssignment(map[string]tenancy.TenantID{id: "acme"}),
		WithEventsStore(eventsStore),
		WithSnapshotStore(snapshotStore),
		WithStateStore(stateStore),
	)
	require.NoError(t, err)

	report, err := adopter.Run(ctx)
	require.NoError(t, err)
	require.NotNil(t, report)

	assert.True(t, report.DryRun)
	assert.Equal(t, 1, report.Scanned)
	assert.Equal(t, 1, report.Assigned)
	assert.Equal(t, 1, report.Copied, "dry-run must report exactly what a real run would do")
	assert.Zero(t, report.Verified, "dry-run performs no write, so nothing was actually verified")
	assert.Zero(t, report.SourceDeleted)
	assert.Zero(t, report.Failed)

	target, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)

	evt, err := eventsStore.GetLatestEvent(ctx, target, id)
	require.NoError(t, err)
	assert.Nil(t, evt, "dry-run must write nothing to the target scope")

	snap, err := snapshotStore.GetLatestSnapshot(ctx, target, id)
	require.NoError(t, err)
	assert.Nil(t, snap)

	state, err := stateStore.GetLatestState(ctx, target, id)
	require.NoError(t, err)
	assert.Nil(t, state)
}

func TestTenantAdopterRealRunCopiesAndKeepsSource(t *testing.T) {
	ctx := context.Background()
	eventsStore := testkit.NewEventsStore()
	require.NoError(t, eventsStore.Connect(ctx))
	snapshotStore := testkit.NewSnapshotStore()
	require.NoError(t, snapshotStore.Connect(ctx))
	stateStore := testkit.NewDurableStore()
	require.NoError(t, stateStore.Connect(ctx))

	const id = "real-run-1"
	source := persistence.Unscoped()
	require.NoError(t, eventsStore.WriteEvents(ctx, source, []*egopb.Event{
		newLegacyEvent(t, id, 1, 100),
		newLegacyEvent(t, id, 2, 200),
	}, persistence.Unconditional()))
	require.NoError(t, snapshotStore.WriteSnapshot(ctx, source, newLegacySnapshot(t, id, 2, 200)))
	require.NoError(t, stateStore.WriteState(ctx, source, newLegacyDurableState(t, id, 1, 200), persistence.Unconditional()))

	adopter, err := NewTenantAdopter(
		fixedAssignment(map[string]tenancy.TenantID{id: "acme"}),
		WithEventsStore(eventsStore),
		WithSnapshotStore(snapshotStore),
		WithStateStore(stateStore),
		WithWriteEnabled(),
	)
	require.NoError(t, err)

	report, err := adopter.Run(ctx)
	require.NoError(t, err)

	assert.False(t, report.DryRun)
	assert.Equal(t, 1, report.Copied)
	assert.Equal(t, 1, report.Verified)
	assert.Zero(t, report.SourceDeleted, "no delete option was requested")
	assert.Zero(t, report.Failed)

	target, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)

	targetEvents, err := eventsStore.ReplayEvents(ctx, target, id, 1, 2, 10)
	require.NoError(t, err)
	assert.Len(t, targetEvents, 2)

	targetSnap, err := snapshotStore.GetLatestSnapshot(ctx, target, id)
	require.NoError(t, err)
	require.NotNil(t, targetSnap)
	assert.EqualValues(t, 2, targetSnap.GetSequenceNumber())

	targetState, err := stateStore.GetLatestState(ctx, target, id)
	require.NoError(t, err)
	require.NotNil(t, targetState)
	assert.EqualValues(t, 1, targetState.GetVersionNumber())

	// The source scope keeps its originals: no delete by default.
	sourceEvents, err := eventsStore.ReplayEvents(ctx, source, id, 1, 2, 10)
	require.NoError(t, err)
	assert.Len(t, sourceEvents, 2)

	sourceSnap, err := snapshotStore.GetLatestSnapshot(ctx, source, id)
	require.NoError(t, err)
	assert.NotNil(t, sourceSnap)

	sourceState, err := stateStore.GetLatestState(ctx, source, id)
	require.NoError(t, err)
	assert.NotNil(t, sourceState)
}

func TestTenantAdopterStampsTargetTenantMetadata(t *testing.T) {
	ctx := context.Background()
	eventsStore := testkit.NewEventsStore()
	require.NoError(t, eventsStore.Connect(ctx))
	snapshotStore := testkit.NewSnapshotStore()
	require.NoError(t, snapshotStore.Connect(ctx))
	stateStore := testkit.NewDurableStore()
	require.NoError(t, stateStore.Connect(ctx))

	const id = "stamp-1"
	source := persistence.Unscoped()
	require.NoError(t, eventsStore.WriteEvents(ctx, source, []*egopb.Event{newLegacyEvent(t, id, 1, 100)}, persistence.Unconditional()))
	require.NoError(t, snapshotStore.WriteSnapshot(ctx, source, newLegacySnapshot(t, id, 1, 100)))
	require.NoError(t, stateStore.WriteState(ctx, source, newLegacyDurableState(t, id, 1, 100), persistence.Unconditional()))

	adopter, err := NewTenantAdopter(
		fixedAssignment(map[string]tenancy.TenantID{id: "acme"}),
		WithEventsStore(eventsStore),
		WithSnapshotStore(snapshotStore),
		WithStateStore(stateStore),
		WithWriteEnabled(),
	)
	require.NoError(t, err)
	_, err = adopter.Run(ctx)
	require.NoError(t, err)

	target, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)

	wantTenant, err := tenancy.NewTenantID("acme")
	require.NoError(t, err)
	wantContext, err := tenancy.NewTenantContext(wantTenant)
	require.NoError(t, err)
	wantMetadata := tenancy.MarshalMetadata(wantContext)

	evt, err := eventsStore.GetLatestEvent(ctx, target, id)
	require.NoError(t, err)
	require.NotNil(t, evt)
	gotEventTenant, err := tenancy.UnmarshalMetadata(tenancy.Metadata(evt.GetTenantMetadata()))
	require.NoError(t, err)
	assert.Equal(t, wantContext, gotEventTenant, "the copied event must carry tenant_metadata stamped exactly like the actors stamp it")
	assert.Equal(t, map[string]string(wantMetadata), evt.GetTenantMetadata())

	snap, err := snapshotStore.GetLatestSnapshot(ctx, target, id)
	require.NoError(t, err)
	require.NotNil(t, snap)
	gotSnapTenant, err := tenancy.UnmarshalMetadata(tenancy.Metadata(snap.GetTenantMetadata()))
	require.NoError(t, err)
	assert.Equal(t, wantContext, gotSnapTenant)

	state, err := stateStore.GetLatestState(ctx, target, id)
	require.NoError(t, err)
	require.NotNil(t, state)
	gotStateTenant, err := tenancy.UnmarshalMetadata(tenancy.Metadata(state.GetTenantMetadata()))
	require.NoError(t, err)
	assert.Equal(t, wantContext, gotStateTenant)
}

// TestTenantAdopterEndToEndRecoveryThroughRealActor is the test that proves
// the migration actually produces usable data: it writes legacy (unscoped,
// no tenant_metadata) events directly to the store, adopts the aggregate
// into tenant "acme", then spawns a REAL tenant-aware EventSourcedActor
// (through a real Engine, exactly as production code does) bound to "acme"
// and proves it recovers the migrated event and can keep applying commands
// on top of it. If the migrated tenant_metadata were missing or wrong, the
// actor's own seedActorTenant/tenancy.VerifyUnchanged cross-check (T4) would
// refuse to recover at all.
func TestTenantAdopterEndToEndRecoveryThroughRealActor(t *testing.T) {
	ctx := context.Background()
	eventsStore := testkit.NewEventsStore()
	require.NoError(t, eventsStore.Connect(ctx))
	t.Cleanup(func() { _ = eventsStore.Disconnect(ctx) })

	entityID := "acct-" + uuid.NewString()
	source := persistence.Unscoped()

	// Legacy data: written exactly as a pre-tenancy deployment would have,
	// with no tenant_metadata at all.
	createdPayload, err := anypb.New(&testpb.AccountCreated{AccountId: entityID, AccountBalance: 100})
	require.NoError(t, err)
	legacyEvent := &egopb.Event{
		PersistenceId:  entityID,
		SequenceNumber: 1,
		Event:          createdPayload,
		Timestamp:      time.Now().Unix(),
	}
	require.NoError(t, eventsStore.WriteEvents(ctx, source, []*egopb.Event{legacyEvent}, persistence.Unconditional()))

	adopter, err := NewTenantAdopter(
		fixedAssignment(map[string]tenancy.TenantID{entityID: "acme"}),
		WithEventsStore(eventsStore),
		WithWriteEnabled(),
	)
	require.NoError(t, err)
	report, err := adopter.Run(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, report.Copied)
	require.Equal(t, 1, report.Verified)

	resolver, err := tenancy.WithSingleTenant("acme")
	require.NoError(t, err)

	cfg := ego.NewConfig(eventsStore, ego.WithTenantResolver(resolver))
	sys, err := goakt.NewActorSystem("TenantAdoptionE2E-"+uuid.NewString(), cfg.GoaktOptions()...)
	require.NoError(t, err)
	require.NoError(t, sys.Start(ctx))
	t.Cleanup(func() { _ = sys.Stop(context.Background()) })

	engine, err := ego.NewEngine(sys, cfg)
	require.NoError(t, err)
	require.NoError(t, engine.Start(ctx))
	t.Cleanup(func() { _ = engine.Stop(context.Background()) })

	behavior := &adoptionAccountBehavior{id: entityID}
	require.NoError(t, engine.Entity(ctx, behavior))

	// The recovered state must reflect the migrated event (balance 100)
	// BEFORE any new command is applied: crediting 50 on top of it must
	// yield 150, which is only possible if recovery actually replayed the
	// migrated AccountCreated event rather than starting from a blank slate.
	resultingState, _, err := engine.SendCommand(ctx, entityID, &testpb.CreditAccount{AccountId: entityID, Balance: 50}, time.Minute)
	require.NoError(t, err, "the tenant-bound actor must recover the migrated event without a tenant_metadata cross-check failure")

	account, ok := resultingState.(*testpb.Account)
	require.True(t, ok)
	assert.EqualValues(t, 150, account.GetAccountBalance(), "150 == migrated balance (100) + credited amount (50), proving recovery used the migrated data")
}

func TestTenantAdopterTwoTenantsAreIsolated(t *testing.T) {
	ctx := context.Background()
	eventsStore := testkit.NewEventsStore()
	require.NoError(t, eventsStore.Connect(ctx))

	const idA = "multi-a"
	const idB = "multi-b"
	source := persistence.Unscoped()
	require.NoError(t, eventsStore.WriteEvents(ctx, source, []*egopb.Event{newLegacyEvent(t, idA, 1, 100)}, persistence.Unconditional()))
	require.NoError(t, eventsStore.WriteEvents(ctx, source, []*egopb.Event{newLegacyEvent(t, idB, 1, 200)}, persistence.Unconditional()))

	adopter, err := NewTenantAdopter(
		fixedAssignment(map[string]tenancy.TenantID{idA: "acme", idB: "globex"}),
		WithEventsStore(eventsStore),
		WithWriteEnabled(),
	)
	require.NoError(t, err)
	report, err := adopter.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, report.Copied)

	acme, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)
	globex, err := persistence.NewTenantScope("globex")
	require.NoError(t, err)

	// acme must see its own aggregate, never globex's.
	evt, err := eventsStore.GetLatestEvent(ctx, acme, idA)
	require.NoError(t, err)
	assert.NotNil(t, evt)
	evt, err = eventsStore.GetLatestEvent(ctx, acme, idB)
	require.NoError(t, err)
	assert.Nil(t, evt, "acme must never see globex's aggregate")

	evt, err = eventsStore.GetLatestEvent(ctx, globex, idB)
	require.NoError(t, err)
	assert.NotNil(t, evt)
	evt, err = eventsStore.GetLatestEvent(ctx, globex, idA)
	require.NoError(t, err)
	assert.Nil(t, evt, "globex must never see acme's aggregate")
}

func TestTenantAdopterAssignmentOkFalseLeavesUntouched(t *testing.T) {
	ctx := context.Background()
	eventsStore := testkit.NewEventsStore()
	require.NoError(t, eventsStore.Connect(ctx))

	const id = "unassigned-1"
	source := persistence.Unscoped()
	require.NoError(t, eventsStore.WriteEvents(ctx, source, []*egopb.Event{newLegacyEvent(t, id, 1, 100)}, persistence.Unconditional()))

	adopter, err := NewTenantAdopter(
		fixedAssignment(nil), // ok=false for everything
		WithEventsStore(eventsStore),
		WithWriteEnabled(),
	)
	require.NoError(t, err)
	report, err := adopter.Run(ctx)
	require.NoError(t, err)

	assert.Equal(t, 1, report.Scanned)
	assert.Equal(t, 1, report.SkippedByAssignment)
	assert.Zero(t, report.Assigned)
	assert.Zero(t, report.Copied)

	// The source event is untouched.
	evt, err := eventsStore.GetLatestEvent(ctx, source, id)
	require.NoError(t, err)
	assert.NotNil(t, evt)
}

func TestTenantAdopterReRunIsANoOp(t *testing.T) {
	ctx := context.Background()
	eventsStore := testkit.NewEventsStore()
	require.NoError(t, eventsStore.Connect(ctx))

	const id = "idempotent-1"
	source := persistence.Unscoped()
	require.NoError(t, eventsStore.WriteEvents(ctx, source, []*egopb.Event{newLegacyEvent(t, id, 1, 100)}, persistence.Unconditional()))

	adopter, err := NewTenantAdopter(
		fixedAssignment(map[string]tenancy.TenantID{id: "acme"}),
		WithEventsStore(eventsStore),
		WithWriteEnabled(),
	)
	require.NoError(t, err)

	first, err := adopter.Run(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, first.Copied)
	require.Zero(t, first.Failed)

	second, err := adopter.Run(ctx)
	require.NoError(t, err)
	assert.Zero(t, second.Failed, "re-running must not be an error")
	assert.Zero(t, second.Copied, "re-running must not duplicate the write")
	assert.Equal(t, 1, second.AlreadyPresent, "re-running must report the aggregate as already migrated")

	target, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)
	events, err := eventsStore.ReplayEvents(ctx, target, id, 1, 10, 10)
	require.NoError(t, err)
	assert.Len(t, events, 1, "no duplicate event was written")
}

func TestTenantAdopterAlreadyPresentInTargetIsNeverOverwritten(t *testing.T) {
	ctx := context.Background()
	eventsStore := testkit.NewEventsStore()
	require.NoError(t, eventsStore.Connect(ctx))

	const id = "conflict-1"
	source := persistence.Unscoped()
	require.NoError(t, eventsStore.WriteEvents(ctx, source, []*egopb.Event{newLegacyEvent(t, id, 1, 100)}, persistence.Unconditional()))

	// Some other process already wrote a DIFFERENT record for this id under
	// the target tenant, before adoption ran.
	target, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)
	preexisting, err := tenancy.NewTenantContext("acme")
	require.NoError(t, err)
	preexistingEvent := newLegacyEvent(t, id, 1, 999)
	preexistingEvent.TenantMetadata = tenancy.MarshalMetadata(preexisting)
	require.NoError(t, eventsStore.WriteEvents(ctx, target, []*egopb.Event{preexistingEvent}, persistence.Unconditional()))

	adopter, err := NewTenantAdopter(
		fixedAssignment(map[string]tenancy.TenantID{id: "acme"}),
		WithEventsStore(eventsStore),
		WithWriteEnabled(),
	)
	require.NoError(t, err)
	report, err := adopter.Run(ctx)
	require.NoError(t, err)

	assert.Equal(t, 1, report.AlreadyPresent)
	assert.Zero(t, report.Copied)
	assert.Zero(t, report.Failed)

	// The pre-existing target record must be untouched (still timestamp 999,
	// not overwritten by the legacy copy's timestamp 100).
	evt, err := eventsStore.GetLatestEvent(ctx, target, id)
	require.NoError(t, err)
	require.NotNil(t, evt)
	assert.EqualValues(t, 999, evt.GetTimestamp())
}

func TestTenantAdopterSourceDeletionOnlyAfterVerification(t *testing.T) {
	ctx := context.Background()
	eventsStore := testkit.NewEventsStore()
	require.NoError(t, eventsStore.Connect(ctx))

	const id = "delete-1"
	source := persistence.Unscoped()
	require.NoError(t, eventsStore.WriteEvents(ctx, source, []*egopb.Event{newLegacyEvent(t, id, 1, 100)}, persistence.Unconditional()))

	t.Run("without the opt-in, source is kept", func(t *testing.T) {
		adopter, err := NewTenantAdopter(
			fixedAssignment(map[string]tenancy.TenantID{id: "keepme"}),
			WithEventsStore(eventsStore),
			WithWriteEnabled(),
		)
		require.NoError(t, err)
		report, err := adopter.Run(ctx)
		require.NoError(t, err)
		assert.Zero(t, report.SourceDeleted)

		evt, err := eventsStore.GetLatestEvent(ctx, source, id)
		require.NoError(t, err)
		assert.NotNil(t, evt, "source must be kept without the explicit opt-in")
	})

	t.Run("with the opt-in, source is removed only after a verified copy", func(t *testing.T) {
		adopter, err := NewTenantAdopter(
			fixedAssignment(map[string]tenancy.TenantID{id: "acme"}),
			WithEventsStore(eventsStore),
			WithWriteEnabled(),
			WithSourceDeletion(),
		)
		require.NoError(t, err)
		report, err := adopter.Run(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, report.Copied)
		assert.Equal(t, 1, report.Verified)
		assert.Equal(t, 1, report.SourceDeleted)

		evt, err := eventsStore.GetLatestEvent(ctx, source, id)
		require.NoError(t, err)
		assert.Nil(t, evt, "source must be removed once the copy was verified")

		target, err := persistence.NewTenantScope("acme")
		require.NoError(t, err)
		targetEvt, err := eventsStore.GetLatestEvent(ctx, target, id)
		require.NoError(t, err)
		assert.NotNil(t, targetEvt, "the target copy must remain")
	})
}

func TestTenantAdopterPerAggregateFailureDoesNotAbortRun(t *testing.T) {
	ctx := context.Background()
	eventsStore := testkit.NewEventsStore()
	require.NoError(t, eventsStore.Connect(ctx))

	const goodID = "good-1"
	const badID = "bad-1"
	source := persistence.Unscoped()
	require.NoError(t, eventsStore.WriteEvents(ctx, source, []*egopb.Event{newLegacyEvent(t, goodID, 1, 100)}, persistence.Unconditional()))
	require.NoError(t, eventsStore.WriteEvents(ctx, source, []*egopb.Event{newLegacyEvent(t, badID, 1, 200)}, persistence.Unconditional()))

	assignErr := errors.New("boom: cannot decide tenant for bad-1")
	assign := func(_ context.Context, persistenceID string) (tenancy.TenantID, bool, error) {
		if persistenceID == badID {
			return "", false, assignErr
		}
		return "acme", true, nil
	}

	adopter, err := NewTenantAdopter(assign, WithEventsStore(eventsStore), WithWriteEnabled())
	require.NoError(t, err)

	report, err := adopter.Run(ctx)
	require.NoError(t, err, "a per-aggregate failure must not abort the whole run")

	assert.Equal(t, 2, report.Scanned)
	assert.Equal(t, 1, report.Copied, "the good aggregate must still be migrated")
	assert.Equal(t, 1, report.Failed)
	require.Len(t, report.Failures, 1)
	assert.Equal(t, badID, report.Failures[0].PersistenceID)
	assert.ErrorIs(t, report.Failures[0].Err, assignErr)

	target, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)
	evt, err := eventsStore.GetLatestEvent(ctx, target, goodID)
	require.NoError(t, err)
	assert.NotNil(t, evt)
}

func TestTenantAdopterExplicitPersistenceIDsForDurableStateOnly(t *testing.T) {
	ctx := context.Background()
	stateStore := testkit.NewDurableStore()
	require.NoError(t, stateStore.Connect(ctx))

	const id = "state-only-1"
	source := persistence.Unscoped()
	require.NoError(t, stateStore.WriteState(ctx, source, newLegacyDurableState(t, id, 1, 100), persistence.Unconditional()))

	// No events store at all: StateStore has no enumeration method, so the
	// operator must supply the id explicitly.
	adopter, err := NewTenantAdopter(
		fixedAssignment(map[string]tenancy.TenantID{id: "acme"}),
		WithStateStore(stateStore),
		WithPersistenceIDs(id),
		WithWriteEnabled(),
	)
	require.NoError(t, err)
	report, err := adopter.Run(ctx)
	require.NoError(t, err)

	assert.Equal(t, 1, report.Scanned)
	assert.Equal(t, 1, report.Copied)

	target, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)
	state, err := stateStore.GetLatestState(ctx, target, id)
	require.NoError(t, err)
	assert.NotNil(t, state)
}

// TestTenantAdopterEventsVerificationCatchesCorruptedWrite is the
// adversarial proof for the second review defect: a verification that only
// compares len(written) != len(sourceEvents) cannot detect a write that
// dropped the payload and tenant_metadata while preserving the event count
// and every sequence number. corruptingEventsStore simulates exactly that
// faulty adapter. The fixed verification (verifyEventsMatchBySequence) must
// fail this aggregate, name its persistence id, and — critically — never
// delete the source, even though WithSourceDeletion was requested.
func TestTenantAdopterEventsVerificationCatchesCorruptedWrite(t *testing.T) {
	ctx := context.Background()
	base := testkit.NewEventsStore()
	require.NoError(t, base.Connect(ctx))

	const id = "corrupt-events-1"
	source := persistence.Unscoped()
	require.NoError(t, base.WriteEvents(ctx, source, []*egopb.Event{
		newLegacyEvent(t, id, 1, 100),
		newLegacyEvent(t, id, 2, 200),
	}, persistence.Unconditional()))

	target, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)

	corrupting := &corruptingEventsStore{
		EventsStore:  base,
		corruptScope: target,
		mangle: func(e *egopb.Event) {
			// Right count, right sequence number, wrong everything else:
			// the payload is dropped and tenant_metadata never lands.
			e.TenantMetadata = nil
			e.Event = nil
		},
	}

	adopter, err := NewTenantAdopter(
		fixedAssignment(map[string]tenancy.TenantID{id: "acme"}),
		WithEventsStore(corrupting),
		WithWriteEnabled(),
		WithSourceDeletion(),
	)
	require.NoError(t, err)

	report, err := adopter.Run(ctx)
	require.NoError(t, err, "a per-aggregate verification failure must not abort the whole run")

	assert.Equal(t, 1, report.Failed, "a corrupted target write must be reported as a failure, not silently verified")
	require.Len(t, report.Failures, 1)
	assert.Equal(t, id, report.Failures[0].PersistenceID)
	assert.Contains(t, report.Failures[0].Err.Error(), id, "the failure must name the persistence id that differed")
	assert.Zero(t, report.SourceDeleted, "a failed verification must never delete the source, even with WithSourceDeletion")

	sourceEvents, err := base.ReplayEvents(ctx, source, id, 1, 2, 10)
	require.NoError(t, err)
	assert.Len(t, sourceEvents, 2, "the source copy must remain fully intact after a failed verification")
}

// TestTenantAdopterSnapshotVerificationCatchesCorruptedWrite is the
// snapshot-store counterpart of the events test above: a verification that
// only compares SequenceNumber cannot detect a snapshot whose STATE payload
// was corrupted while its sequence number was preserved.
func TestTenantAdopterSnapshotVerificationCatchesCorruptedWrite(t *testing.T) {
	ctx := context.Background()
	base := testkit.NewSnapshotStore()
	require.NoError(t, base.Connect(ctx))

	const id = "corrupt-snapshot-1"
	source := persistence.Unscoped()
	require.NoError(t, base.WriteSnapshot(ctx, source, newLegacySnapshot(t, id, 5, 100)))

	target, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)

	corrupting := &corruptingSnapshotStore{
		SnapshotStore: base,
		corruptScope:  target,
		mangle: func(s *egopb.Snapshot) {
			// Right sequence number, wrong state payload entirely.
			payload, err := anypb.New(&testpb.Account{AccountId: id, AccountBalance: 999})
			require.NoError(t, err)
			s.State = payload
		},
	}

	adopter, err := NewTenantAdopter(
		fixedAssignment(map[string]tenancy.TenantID{id: "acme"}),
		WithSnapshotStore(corrupting),
		WithPersistenceIDs(id), // no events store: SnapshotStore has no enumeration method
		WithWriteEnabled(),
		WithSourceDeletion(),
	)
	require.NoError(t, err)

	report, err := adopter.Run(ctx)
	require.NoError(t, err)

	assert.Equal(t, 1, report.Failed, "a snapshot whose payload differs from the source must fail verification even though its sequence number matches")
	require.Len(t, report.Failures, 1)
	assert.Equal(t, id, report.Failures[0].PersistenceID)
	assert.Contains(t, report.Failures[0].Err.Error(), id)
	assert.Zero(t, report.SourceDeleted, "a failed verification must never delete the source")

	sourceSnap, err := base.GetLatestSnapshot(ctx, source, id)
	require.NoError(t, err)
	require.NotNil(t, sourceSnap, "the source snapshot must remain intact after a failed verification")
}

// TestTenantAdopterStateVerificationCatchesCorruptedWrite is the
// durable-state counterpart: a verification that only compares
// VersionNumber cannot detect a durable state whose payload was corrupted
// while its version number was preserved. adoptState never deletes (there
// is no delete method on persistence.StateStore), but its verification
// still feeds AdoptionReport.Verified/Failed, so it must be held to the
// same standard.
func TestTenantAdopterStateVerificationCatchesCorruptedWrite(t *testing.T) {
	ctx := context.Background()
	base := testkit.NewDurableStore()
	require.NoError(t, base.Connect(ctx))

	const id = "corrupt-state-1"
	source := persistence.Unscoped()
	require.NoError(t, base.WriteState(ctx, source, newLegacyDurableState(t, id, 3, 100), persistence.Unconditional()))

	target, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)

	corrupting := &corruptingStateStore{
		StateStore:   base,
		corruptScope: target,
		mangle: func(s *egopb.DurableState) {
			// Right version number, wrong resulting-state payload entirely.
			payload, err := anypb.New(&testpb.Account{AccountId: id, AccountBalance: 999})
			require.NoError(t, err)
			s.ResultingState = payload
		},
	}

	adopter, err := NewTenantAdopter(
		fixedAssignment(map[string]tenancy.TenantID{id: "acme"}),
		WithStateStore(corrupting),
		WithPersistenceIDs(id), // no events store: StateStore has no enumeration method
		WithWriteEnabled(),
	)
	require.NoError(t, err)

	report, err := adopter.Run(ctx)
	require.NoError(t, err)

	assert.Equal(t, 1, report.Failed, "a durable state whose payload differs from the source must fail verification even though its version number matches")
	require.Len(t, report.Failures, 1)
	assert.Equal(t, id, report.Failures[0].PersistenceID)
	assert.Contains(t, report.Failures[0].Err.Error(), id)
}

// TestTenantAdopterAdoptsEveryAggregateAcrossMultiplePages is the
// migration-level regression for the first review defect: PersistenceIDs
// pagination used to silently skip one id at every page boundary (see
// testkit/eventstore.go's PersistenceIDs doc comment), and
// collectPersistenceIDs (tenant_adoption.go) drives this tool's entire scan
// off that enumeration — a real run could report success while leaving
// aggregates unadopted. This writes more aggregates than fit in one page at
// a small configured page size and asserts every single one is scanned and
// adopted, none skipped at a page boundary.
func TestTenantAdopterAdoptsEveryAggregateAcrossMultiplePages(t *testing.T) {
	ctx := context.Background()
	eventsStore := testkit.NewEventsStore()
	require.NoError(t, eventsStore.Connect(ctx))

	const pageSize = 4
	const total = 3*pageSize + 1 // forces at least four PersistenceIDs pages
	source := persistence.Unscoped()
	assignments := make(map[string]tenancy.TenantID, total)
	for i := 0; i < total; i++ {
		id := fmt.Sprintf("adopt-page-%03d", i)
		require.NoError(t, eventsStore.WriteEvents(ctx, source, []*egopb.Event{newLegacyEvent(t, id, 1, int64(i))}, persistence.Unconditional()))
		assignments[id] = "acme"
	}

	adopter, err := NewTenantAdopter(
		fixedAssignment(assignments),
		WithEventsStore(eventsStore),
		WithScanPageSize(pageSize),
		WithWriteEnabled(),
	)
	require.NoError(t, err)

	report, err := adopter.Run(ctx)
	require.NoError(t, err)

	assert.Equal(t, total, report.Scanned, "every persistence id must be scanned exactly once; none skipped at a PersistenceIDs page boundary")
	assert.Equal(t, total, report.Copied)
	assert.Zero(t, report.Failed)

	target, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)
	for id := range assignments {
		evt, err := eventsStore.GetLatestEvent(ctx, target, id)
		require.NoError(t, err)
		assert.NotNil(t, evt, "persistence id %q must have been adopted, not skipped at a page boundary", id)
	}
}
