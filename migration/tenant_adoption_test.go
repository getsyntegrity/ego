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
	"github.com/pablogore/ego/v4/tenancy"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
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
	// The copy carries exactly the actor-style tenant stamp plus one
	// adoption receipt key, nothing else.
	for key, value := range wantMetadata {
		assert.Equal(t, value, evt.GetTenantMetadata()[key])
	}
	assert.Len(t, evt.GetTenantMetadata(), len(wantMetadata)+1)
	assert.NotEmpty(t, evt.GetTenantMetadata()[adoptionReceiptKey], "every adopted record carries an adoption receipt")

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

	// The target already holds a record under this id, but it is NOT the
	// record this adoption would write (timestamp 999, not the source's
	// 100): that is a collision, not a completed migration, so it fails
	// closed rather than being reported as already_present (#98 review).
	assert.Zero(t, report.AlreadyPresent, "a non-equivalent target must never be reported as already migrated")
	assert.Zero(t, report.Copied)
	assert.Equal(t, 1, report.Failed)
	require.Len(t, report.Failures, 1)
	assert.ErrorIs(t, report.Failures[0], errTargetNotEquivalent)

	// The pre-existing target record must be untouched (still timestamp 999,
	// not overwritten by the legacy copy's timestamp 100), and the source is
	// kept.
	evt, err := eventsStore.GetLatestEvent(ctx, target, id)
	require.NoError(t, err)
	require.NotNil(t, evt)
	assert.EqualValues(t, 999, evt.GetTimestamp())
	sourceEvt, err := eventsStore.GetLatestEvent(ctx, source, id)
	require.NoError(t, err)
	assert.NotNil(t, sourceEvt, "a failed classification must never delete the source")
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

// duplicatingEventsStore wraps a persistence.EventsStore and, for reads of
// duplicateScope only, prepends a corrupted duplicate of the first returned
// event. The distinct-sequence-number count and every sequence number still
// match what was written; only the raw row count reveals the extra row. It
// proves verifyEventsMatchBySequence rejects a read-back that carries more
// than one row for a sequence number.
type duplicatingEventsStore struct {
	persistence.EventsStore
	duplicateScope persistence.Scope
}

func (d *duplicatingEventsStore) ReplayEvents(ctx context.Context, scope persistence.Scope, persistenceID string, fromSequenceNumber, toSequenceNumber, maxNumber uint64) ([]*egopb.Event, error) {
	events, err := d.EventsStore.ReplayEvents(ctx, scope, persistenceID, fromSequenceNumber, toSequenceNumber, maxNumber)
	if err != nil || len(events) == 0 || !scope.Equal(d.duplicateScope) {
		return events, err
	}
	dup, ok := proto.Clone(events[0]).(*egopb.Event)
	if !ok {
		return nil, errors.New("duplicatingEventsStore: clone failed")
	}
	dup.Event = nil
	return append([]*egopb.Event{dup}, events...), nil
}

func TestTenantAdopterEventsVerificationRejectsDuplicateSequenceRows(t *testing.T) {
	ctx := context.Background()
	base := testkit.NewEventsStore()
	require.NoError(t, base.Connect(ctx))

	const id = "duplicate-rows-1"
	source := persistence.Unscoped()
	require.NoError(t, base.WriteEvents(ctx, source, []*egopb.Event{
		newLegacyEvent(t, id, 1, 100),
		newLegacyEvent(t, id, 2, 200),
	}, persistence.Unconditional()))

	target, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)

	adopter, err := NewTenantAdopter(
		fixedAssignment(map[string]tenancy.TenantID{id: "acme"}),
		WithEventsStore(&duplicatingEventsStore{EventsStore: base, duplicateScope: target}),
		WithWriteEnabled(),
		WithSourceDeletion(),
	)
	require.NoError(t, err)

	report, err := adopter.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, report.Failed, "a read-back with a duplicated sequence row must not verify")
	assert.Zero(t, report.SourceDeleted)

	sourceEvents, err := base.ReplayEvents(ctx, source, id, 1, 2, 10)
	require.NoError(t, err)
	assert.Len(t, sourceEvents, 2, "the source must remain intact after a failed verification")
}

// TestTenantAdopterSourceDeletingReRunIsIdempotent covers the #98 review
// finding that a second identical run after WithSourceDeletion reported
// every migrated aggregate as failed ("no source record") instead of as a
// no-op. testkit.EventStore keeps the persistence id's log entry after
// DeleteEvents, so the id is still enumerated with an empty source.
func TestTenantAdopterSourceDeletingReRunIsIdempotent(t *testing.T) {
	ctx := context.Background()
	eventsStore := testkit.NewEventsStore()
	require.NoError(t, eventsStore.Connect(ctx))
	snapshotStore := testkit.NewSnapshotStore()
	require.NoError(t, snapshotStore.Connect(ctx))

	const id = "delete-rerun-1"
	source := persistence.Unscoped()
	require.NoError(t, eventsStore.WriteEvents(ctx, source, []*egopb.Event{
		newLegacyEvent(t, id, 1, 100),
		newLegacyEvent(t, id, 2, 200),
	}, persistence.Unconditional()))
	require.NoError(t, snapshotStore.WriteSnapshot(ctx, source, newLegacySnapshot(t, id, 2, 200)))

	adopter, err := NewTenantAdopter(
		fixedAssignment(map[string]tenancy.TenantID{id: "acme"}),
		WithEventsStore(eventsStore),
		WithSnapshotStore(snapshotStore),
		WithWriteEnabled(),
		WithSourceDeletion(),
	)
	require.NoError(t, err)

	first, err := adopter.Run(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, first.Copied)
	require.Equal(t, 1, first.SourceDeleted)
	require.Zero(t, first.Failed)

	ids, _, err := eventsStore.PersistenceIDs(ctx, source, 10, "")
	require.NoError(t, err)
	require.Contains(t, ids, id, "precondition: this store keeps enumerating the emptied source id")

	second, err := adopter.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, second.Scanned)
	assert.Equal(t, 1, second.AlreadyPresent, "an equivalent target with a deleted source is already migrated")
	assert.Zero(t, second.Failed)
	assert.Empty(t, second.Failures)
	assert.Zero(t, second.Copied)
	assert.Zero(t, second.Verified)
	assert.Zero(t, second.SourceDeleted)
	require.Len(t, second.Aggregates, 1)
	assert.Equal(t, StatusAlreadyPresent, second.Aggregates[0].Events.Status)
	assert.Equal(t, StatusAlreadyPresent, second.Aggregates[0].Snapshot.Status)

	target, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)
	events, err := eventsStore.ReplayEvents(ctx, target, id, 1, 10, 10)
	require.NoError(t, err)
	assert.Len(t, events, 2, "the second run must not write anything")
}

func TestTenantAdopterSnapshotOnlyReRunAfterDeletionIsIdempotent(t *testing.T) {
	ctx := context.Background()
	snapshotStore := testkit.NewSnapshotStore()
	require.NoError(t, snapshotStore.Connect(ctx))

	const id = "snapshot-rerun-1"
	source := persistence.Unscoped()
	require.NoError(t, snapshotStore.WriteSnapshot(ctx, source, newLegacySnapshot(t, id, 3, 300)))

	adopter, err := NewTenantAdopter(
		fixedAssignment(map[string]tenancy.TenantID{id: "acme"}),
		WithSnapshotStore(snapshotStore),
		WithPersistenceIDs(id),
		WithWriteEnabled(),
		WithSourceDeletion(),
	)
	require.NoError(t, err)

	first, err := adopter.Run(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, first.SourceDeleted)

	second, err := adopter.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, second.AlreadyPresent)
	assert.Zero(t, second.Failed)
	assert.Empty(t, second.Failures)
}

// TestTenantAdopterMissingSourceClassification pins how an assigned
// aggregate whose source is absent is classified: an equivalent target is
// a no-op, a non-equivalent target fails closed, and neither side holding
// anything is the genuine missing-source failure.
func TestTenantAdopterMissingSourceClassification(t *testing.T) {
	ctx := context.Background()
	target, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)
	acme, err := tenancy.NewTenantContext("acme")
	require.NoError(t, err)
	globex, err := tenancy.NewTenantContext("globex")
	require.NoError(t, err)

	run := func(t *testing.T, eventsStore persistence.EventsStore, snapshotStore persistence.SnapshotStore, id string) *AdoptionReport {
		t.Helper()
		opts := []AdoptionOption{WithWriteEnabled(), WithSourceDeletion(), WithPersistenceIDs(id)}
		if eventsStore != nil {
			opts = append(opts, WithEventsStore(eventsStore))
		}
		if snapshotStore != nil {
			opts = append(opts, WithSnapshotStore(snapshotStore))
		}
		adopter, err := NewTenantAdopter(fixedAssignment(map[string]tenancy.TenantID{id: "acme"}), opts...)
		require.NoError(t, err)
		report, err := adopter.Run(ctx)
		require.NoError(t, err)
		return report
	}

	t.Run("target events owned by another tenant fail closed", func(t *testing.T) {
		eventsStore := testkit.NewEventsStore()
		require.NoError(t, eventsStore.Connect(ctx))
		const id = "foreign-target-events"
		evt := newLegacyEvent(t, id, 1, 100)
		evt.TenantMetadata = tenancy.MarshalMetadata(globex)
		require.NoError(t, eventsStore.WriteEvents(ctx, target, []*egopb.Event{evt}, persistence.Unconditional()))

		report := run(t, eventsStore, nil, id)
		assert.Equal(t, 1, report.Failed)
		assert.Zero(t, report.AlreadyPresent)
		require.Len(t, report.Failures, 1)
		assert.ErrorIs(t, report.Failures[0], errTargetNotEquivalent)
	})

	t.Run("target snapshot without tenant metadata fails closed", func(t *testing.T) {
		snapshotStore := testkit.NewSnapshotStore()
		require.NoError(t, snapshotStore.Connect(ctx))
		const id = "untagged-target-snapshot"
		require.NoError(t, snapshotStore.WriteSnapshot(ctx, target, newLegacySnapshot(t, id, 1, 100)))

		report := run(t, nil, snapshotStore, id)
		assert.Equal(t, 1, report.Failed)
		assert.Zero(t, report.AlreadyPresent)
		require.Len(t, report.Failures, 1)
		assert.ErrorIs(t, report.Failures[0], errTargetNotEquivalent)
	})

	t.Run("unrelated target owned by the assigned tenant is not proof of adoption", func(t *testing.T) {
		snapshotStore := testkit.NewSnapshotStore()
		require.NoError(t, snapshotStore.Connect(ctx))
		const id = "owned-target-snapshot"
		snap := newLegacySnapshot(t, id, 1, 100)
		snap.TenantMetadata = tenancy.MarshalMetadata(acme)
		require.NoError(t, snapshotStore.WriteSnapshot(ctx, target, snap))

		report := run(t, nil, snapshotStore, id)
		assert.Zero(t, report.AlreadyPresent, "same-tenant data with no adoption receipt must never count as adopted")
		assert.Equal(t, 1, report.Failed)
		require.Len(t, report.Failures, 1)
		assert.ErrorIs(t, report.Failures[0], errTargetNotEquivalent)
	})

	t.Run("owned target events without a receipt are not proof of adoption", func(t *testing.T) {
		eventsStore := testkit.NewEventsStore()
		require.NoError(t, eventsStore.Connect(ctx))
		const id = "owned-target-events"
		evt := newLegacyEvent(t, id, 1, 100)
		evt.TenantMetadata = tenancy.MarshalMetadata(acme)
		require.NoError(t, eventsStore.WriteEvents(ctx, target, []*egopb.Event{evt}, persistence.Unconditional()))

		report := run(t, eventsStore, nil, id)
		assert.Zero(t, report.AlreadyPresent)
		assert.Equal(t, 1, report.Failed)
		require.Len(t, report.Failures, 1)
		assert.ErrorIs(t, report.Failures[0], errTargetNotEquivalent)
	})

	t.Run("neither source nor target is a missing-source failure", func(t *testing.T) {
		eventsStore := testkit.NewEventsStore()
		require.NoError(t, eventsStore.Connect(ctx))
		snapshotStore := testkit.NewSnapshotStore()
		require.NoError(t, snapshotStore.Connect(ctx))

		report := run(t, eventsStore, snapshotStore, "nowhere")
		assert.Equal(t, 1, report.Failed)
		assert.Zero(t, report.AlreadyPresent)
		require.Len(t, report.Failures, 1)
		assert.ErrorIs(t, report.Failures[0], errNoSourceRecords)
	})
}

// TestTenantAdopterTargetExtendedByLiveWritesIsAlreadyPresent covers a
// re-run after the tenant-bound actor has already appended to the adopted
// stream: the target still contains the source records exactly and every
// target event carries the assigned tenant, so it is already migrated.
func TestTenantAdopterTargetExtendedByLiveWritesIsAlreadyPresent(t *testing.T) {
	ctx := context.Background()
	eventsStore := testkit.NewEventsStore()
	require.NoError(t, eventsStore.Connect(ctx))

	const id = "extended-1"
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

	target, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)
	acme, err := tenancy.NewTenantContext("acme")
	require.NoError(t, err)
	live := newLegacyEvent(t, id, 2, 200)
	live.TenantMetadata = tenancy.MarshalMetadata(acme)
	require.NoError(t, eventsStore.WriteEvents(ctx, target, []*egopb.Event{live}, persistence.Unconditional()))

	second, err := adopter.Run(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, second.AlreadyPresent)
	assert.Zero(t, second.Failed)
}

// TestTenantAdopterLaterSameTenantTargetIsNotEquivalent pins that a single
// latest snapshot or durable-state record cannot prove it descends from the
// source: a same-tenant target at a later position with an unrelated
// payload must fail closed, never count as already migrated.
func TestTenantAdopterLaterSameTenantTargetIsNotEquivalent(t *testing.T) {
	ctx := context.Background()
	target, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)
	acme, err := tenancy.NewTenantContext("acme")
	require.NoError(t, err)
	source := persistence.Unscoped()

	t.Run("snapshot", func(t *testing.T) {
		snapshotStore := testkit.NewSnapshotStore()
		require.NoError(t, snapshotStore.Connect(ctx))
		const id = "later-snapshot"
		require.NoError(t, snapshotStore.WriteSnapshot(ctx, source, newLegacySnapshot(t, id, 5, 500)))
		unrelated := newLegacySnapshot(t, id, 6, 999)
		unrelated.TenantMetadata = tenancy.MarshalMetadata(acme)
		require.NoError(t, snapshotStore.WriteSnapshot(ctx, target, unrelated))

		adopter, err := NewTenantAdopter(fixedAssignment(map[string]tenancy.TenantID{id: "acme"}),
			WithSnapshotStore(snapshotStore), WithPersistenceIDs(id), WithWriteEnabled(), WithSourceDeletion())
		require.NoError(t, err)
		report, err := adopter.Run(ctx)
		require.NoError(t, err)
		assert.Zero(t, report.AlreadyPresent)
		assert.Equal(t, 1, report.Failed)
		require.Len(t, report.Failures, 1)
		assert.ErrorIs(t, report.Failures[0], errTargetNotEquivalent)

		kept, err := snapshotStore.GetLatestSnapshot(ctx, source, id)
		require.NoError(t, err)
		assert.NotNil(t, kept, "the source must never be deleted on a failed classification")
	})

	t.Run("durable state", func(t *testing.T) {
		stateStore := testkit.NewDurableStore()
		require.NoError(t, stateStore.Connect(ctx))
		const id = "later-state"
		require.NoError(t, stateStore.WriteState(ctx, source, newLegacyDurableState(t, id, 5, 500), persistence.Unconditional()))
		unrelated := newLegacyDurableState(t, id, 6, 999)
		unrelated.TenantMetadata = tenancy.MarshalMetadata(acme)
		require.NoError(t, stateStore.WriteState(ctx, target, unrelated, persistence.Unconditional()))

		adopter, err := NewTenantAdopter(fixedAssignment(map[string]tenancy.TenantID{id: "acme"}),
			WithStateStore(stateStore), WithPersistenceIDs(id), WithWriteEnabled())
		require.NoError(t, err)
		report, err := adopter.Run(ctx)
		require.NoError(t, err)
		assert.Zero(t, report.AlreadyPresent)
		assert.Equal(t, 1, report.Failed)
		require.Len(t, report.Failures, 1)
		assert.ErrorIs(t, report.Failures[0], errTargetNotEquivalent)
	})
}

// TestTenantAdopterSamePositionTargetClassification pins that a target at
// the source's exact position is already migrated only when it is the exact
// record this adoption writes, and fails closed when anything differs.
func TestTenantAdopterSamePositionTargetClassification(t *testing.T) {
	ctx := context.Background()
	target, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)
	acme, err := tenancy.NewTenantContext("acme")
	require.NoError(t, err)
	source := persistence.Unscoped()

	t.Run("snapshot with a different payload at the same position fails", func(t *testing.T) {
		snapshotStore := testkit.NewSnapshotStore()
		require.NoError(t, snapshotStore.Connect(ctx))
		const id = "same-seq-snapshot"
		require.NoError(t, snapshotStore.WriteSnapshot(ctx, source, newLegacySnapshot(t, id, 5, 500)))
		different := newLegacySnapshot(t, id, 5, 999)
		different.TenantMetadata = tenancy.MarshalMetadata(acme)
		require.NoError(t, snapshotStore.WriteSnapshot(ctx, target, different))

		adopter, err := NewTenantAdopter(fixedAssignment(map[string]tenancy.TenantID{id: "acme"}),
			WithSnapshotStore(snapshotStore), WithPersistenceIDs(id), WithWriteEnabled())
		require.NoError(t, err)
		report, err := adopter.Run(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, report.Failed)
		assert.Zero(t, report.AlreadyPresent)
	})

	t.Run("durable state with a different payload at the same version fails", func(t *testing.T) {
		stateStore := testkit.NewDurableStore()
		require.NoError(t, stateStore.Connect(ctx))
		const id = "same-version-state"
		require.NoError(t, stateStore.WriteState(ctx, source, newLegacyDurableState(t, id, 5, 500), persistence.Unconditional()))
		different := newLegacyDurableState(t, id, 5, 999)
		different.TenantMetadata = tenancy.MarshalMetadata(acme)
		require.NoError(t, stateStore.WriteState(ctx, target, different, persistence.Unconditional()))

		adopter, err := NewTenantAdopter(fixedAssignment(map[string]tenancy.TenantID{id: "acme"}),
			WithStateStore(stateStore), WithPersistenceIDs(id), WithWriteEnabled())
		require.NoError(t, err)
		report, err := adopter.Run(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, report.Failed)
		assert.Zero(t, report.AlreadyPresent)
	})

	t.Run("exact snapshot and durable state records are already present", func(t *testing.T) {
		snapshotStore := testkit.NewSnapshotStore()
		require.NoError(t, snapshotStore.Connect(ctx))
		stateStore := testkit.NewDurableStore()
		require.NoError(t, stateStore.Connect(ctx))
		const id = "exact-records"
		require.NoError(t, snapshotStore.WriteSnapshot(ctx, source, newLegacySnapshot(t, id, 5, 500)))
		require.NoError(t, stateStore.WriteState(ctx, source, newLegacyDurableState(t, id, 5, 500), persistence.Unconditional()))

		adopter, err := NewTenantAdopter(fixedAssignment(map[string]tenancy.TenantID{id: "acme"}),
			WithSnapshotStore(snapshotStore), WithStateStore(stateStore), WithPersistenceIDs(id), WithWriteEnabled())
		require.NoError(t, err)
		first, err := adopter.Run(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, first.Copied)

		second, err := adopter.Run(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, second.AlreadyPresent)
		assert.Zero(t, second.Failed)
	})
}

// TestTenantAdopterReceiptProvesAdoptionAfterSourceDeletion pins the
// contract for a re-run whose source is gone: the target is already
// migrated only when it still carries a valid adoption receipt, which
// binds its exact content and the source scope it was adopted from. A
// receipt whose record was altered afterwards, or one written for a
// different source scope, is not proof.
func TestTenantAdopterReceiptProvesAdoptionAfterSourceDeletion(t *testing.T) {
	ctx := context.Background()
	target, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)
	source := persistence.Unscoped()

	adoptAndDelete := func(t *testing.T, snapshotStore *testkit.SnapshotStore, id string) {
		t.Helper()
		require.NoError(t, snapshotStore.WriteSnapshot(ctx, source, newLegacySnapshot(t, id, 5, 500)))
		adopter, err := NewTenantAdopter(fixedAssignment(map[string]tenancy.TenantID{id: "acme"}),
			WithSnapshotStore(snapshotStore), WithPersistenceIDs(id), WithWriteEnabled(), WithSourceDeletion())
		require.NoError(t, err)
		first, err := adopter.Run(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, first.SourceDeleted)
	}

	t.Run("a record altered after adoption no longer matches its receipt", func(t *testing.T) {
		snapshotStore := testkit.NewSnapshotStore()
		require.NoError(t, snapshotStore.Connect(ctx))
		const id = "tampered-receipt"
		adoptAndDelete(t, snapshotStore, id)

		adopted, err := snapshotStore.GetLatestSnapshot(ctx, target, id)
		require.NoError(t, err)
		tampered, ok := proto.Clone(adopted).(*egopb.Snapshot)
		require.True(t, ok)
		tampered.Timestamp = 999
		require.NoError(t, snapshotStore.WriteSnapshot(ctx, target, tampered))

		adopter, err := NewTenantAdopter(fixedAssignment(map[string]tenancy.TenantID{id: "acme"}),
			WithSnapshotStore(snapshotStore), WithPersistenceIDs(id), WithWriteEnabled(), WithSourceDeletion())
		require.NoError(t, err)
		report, err := adopter.Run(ctx)
		require.NoError(t, err)
		assert.Zero(t, report.AlreadyPresent)
		assert.Equal(t, 1, report.Failed)
	})

	t.Run("a receipt for a different source scope is not proof", func(t *testing.T) {
		snapshotStore := testkit.NewSnapshotStore()
		require.NoError(t, snapshotStore.Connect(ctx))
		const id = "other-source-scope"
		adoptAndDelete(t, snapshotStore, id)

		otherSource, err := persistence.NewTenantScope("legacy-partition")
		require.NoError(t, err)
		adopter, err := NewTenantAdopter(fixedAssignment(map[string]tenancy.TenantID{id: "acme"}),
			WithSnapshotStore(snapshotStore), WithPersistenceIDs(id), WithWriteEnabled(), WithSourceScope(otherSource))
		require.NoError(t, err)
		report, err := adopter.Run(ctx)
		require.NoError(t, err)
		assert.Zero(t, report.AlreadyPresent)
		assert.Equal(t, 1, report.Failed)
	})

	t.Run("adopted events followed by live writes are still proven", func(t *testing.T) {
		eventsStore := testkit.NewEventsStore()
		require.NoError(t, eventsStore.Connect(ctx))
		const id = "events-then-live"
		require.NoError(t, eventsStore.WriteEvents(ctx, source, []*egopb.Event{
			newLegacyEvent(t, id, 1, 100), newLegacyEvent(t, id, 2, 200),
		}, persistence.Unconditional()))
		adopter, err := NewTenantAdopter(fixedAssignment(map[string]tenancy.TenantID{id: "acme"}),
			WithEventsStore(eventsStore), WithWriteEnabled(), WithSourceDeletion())
		require.NoError(t, err)
		first, err := adopter.Run(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, first.SourceDeleted)

		acme, err := tenancy.NewTenantContext("acme")
		require.NoError(t, err)
		live := newLegacyEvent(t, id, 3, 300)
		live.TenantMetadata = tenancy.MarshalMetadata(acme)
		require.NoError(t, eventsStore.WriteEvents(ctx, target, []*egopb.Event{live}, persistence.Unconditional()))

		second, err := adopter.Run(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, second.AlreadyPresent)
		assert.Zero(t, second.Failed)
	})
}

func TestNewTenantAdopterRejectsZeroScanPageSize(t *testing.T) {
	_, err := NewTenantAdopter(fixedAssignment(nil), WithEventsStore(testkit.NewEventsStore()), WithScanPageSize(0))
	require.ErrorIs(t, err, ErrInvalidScanPageSize, "a zero page size would scan nothing and report success")
}

// racingEventsStore appends a new source event at a chosen moment, simulating
// a legacy writer that is still accepting writes while the adopter runs.
type racingEventsStore struct {
	persistence.EventsStore
	source persistence.Scope
	target persistence.Scope
	late   *egopb.Event
	// onTargetRead appends late during the target read-back verification;
	// otherwise it is appended at the start of the source DeleteEvents call.
	onTargetRead bool
	fired        bool
}

func (r *racingEventsStore) fire(ctx context.Context) error {
	if r.fired {
		return nil
	}
	r.fired = true
	return r.EventsStore.WriteEvents(ctx, r.source, []*egopb.Event{r.late}, persistence.Unconditional())
}

func (r *racingEventsStore) ReplayEvents(ctx context.Context, scope persistence.Scope, persistenceID string, from, to, maxNumber uint64) ([]*egopb.Event, error) {
	events, err := r.EventsStore.ReplayEvents(ctx, scope, persistenceID, from, to, maxNumber)
	if err == nil && r.onTargetRead && scope.Equal(r.target) && len(events) > 0 {
		if fireErr := r.fire(ctx); fireErr != nil {
			return nil, fireErr
		}
	}
	return events, err
}

func (r *racingEventsStore) DeleteEvents(ctx context.Context, scope persistence.Scope, persistenceID string, toSequenceNumber uint64) error {
	if !r.onTargetRead && scope.Equal(r.source) {
		if err := r.fire(ctx); err != nil {
			return err
		}
	}
	return r.EventsStore.DeleteEvents(ctx, scope, persistenceID, toSequenceNumber)
}

// TestTenantAdopterSourceDeletionRefusesSuccessUnderConcurrentWrites covers
// a source that keeps accepting writes during a deleting run: the run must
// never report source_deleted while an event exists only in the source.
func TestTenantAdopterSourceDeletionRefusesSuccessUnderConcurrentWrites(t *testing.T) {
	ctx := context.Background()
	source := persistence.Unscoped()
	target, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)

	for _, tc := range []struct {
		name         string
		onTargetRead bool
		wantSource   int
	}{
		{name: "a write after verification but before deletion prevents the deletion", onTargetRead: true, wantSource: 3},
		{name: "a write racing the deletion is detected and not reported as success", onTargetRead: false, wantSource: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := testkit.NewEventsStore()
			require.NoError(t, base.Connect(ctx))
			id := "racing-" + uuid.NewString()
			require.NoError(t, base.WriteEvents(ctx, source, []*egopb.Event{
				newLegacyEvent(t, id, 1, 100), newLegacyEvent(t, id, 2, 200),
			}, persistence.Unconditional()))

			store := &racingEventsStore{EventsStore: base, source: source, target: target,
				late: newLegacyEvent(t, id, 3, 300), onTargetRead: tc.onTargetRead}
			adopter, err := NewTenantAdopter(fixedAssignment(map[string]tenancy.TenantID{id: "acme"}),
				WithEventsStore(store), WithWriteEnabled(), WithSourceDeletion())
			require.NoError(t, err)

			report, err := adopter.Run(ctx)
			require.NoError(t, err)
			assert.Zero(t, report.SourceDeleted, "a source that changed during the run must not be reported as deleted")
			assert.Equal(t, 1, report.Failed)
			require.Len(t, report.Failures, 1)
			assert.ErrorIs(t, report.Failures[0], errSourceChangedDuringAdoption)

			remaining, err := base.ReplayEvents(ctx, source, id, 1, 10, 10)
			require.NoError(t, err)
			assert.Len(t, remaining, tc.wantSource, "the event written during the run must still exist in the source")
		})
	}
}

// racingSnapshotStore writes a newer source snapshot as the adopter deletes
// the adopted one.
type racingSnapshotStore struct {
	persistence.SnapshotStore
	source persistence.Scope
	late   *egopb.Snapshot
	fired  bool
}

func (r *racingSnapshotStore) DeleteSnapshots(ctx context.Context, scope persistence.Scope, persistenceID string, toSequenceNumber uint64) error {
	if !r.fired && scope.Equal(r.source) {
		r.fired = true
		if err := r.SnapshotStore.WriteSnapshot(ctx, r.source, r.late); err != nil {
			return err
		}
	}
	return r.SnapshotStore.DeleteSnapshots(ctx, scope, persistenceID, toSequenceNumber)
}

func TestTenantAdopterSnapshotDeletionRefusesSuccessUnderConcurrentWrites(t *testing.T) {
	ctx := context.Background()
	source := persistence.Unscoped()
	base := testkit.NewSnapshotStore()
	require.NoError(t, base.Connect(ctx))
	const id = "racing-snapshot"
	require.NoError(t, base.WriteSnapshot(ctx, source, newLegacySnapshot(t, id, 2, 200)))

	store := &racingSnapshotStore{SnapshotStore: base, source: source, late: newLegacySnapshot(t, id, 3, 300)}
	adopter, err := NewTenantAdopter(fixedAssignment(map[string]tenancy.TenantID{id: "acme"}),
		WithSnapshotStore(store), WithPersistenceIDs(id), WithWriteEnabled(), WithSourceDeletion())
	require.NoError(t, err)

	report, err := adopter.Run(ctx)
	require.NoError(t, err)
	assert.Zero(t, report.SourceDeleted)
	assert.Equal(t, 1, report.Failed)
	require.Len(t, report.Failures, 1)
	assert.ErrorIs(t, report.Failures[0], errSourceChangedDuringAdoption)

	latest, err := base.GetLatestSnapshot(ctx, source, id)
	require.NoError(t, err)
	require.NotNil(t, latest, "the snapshot written during the run must survive")
	assert.EqualValues(t, 3, latest.GetSequenceNumber())
}

// TestTenantAdopterDeletesSourceOfVerifiedExistingTarget covers a run that
// enables WithSourceDeletion after an earlier run copied without it: the
// target is proven to be the exact adoption, so the source is deleted under
// the same guard as a fresh copy, and nothing is reported as copied.
func TestTenantAdopterDeletesSourceOfVerifiedExistingTarget(t *testing.T) {
	ctx := context.Background()
	source := persistence.Unscoped()
	eventsStore := testkit.NewEventsStore()
	require.NoError(t, eventsStore.Connect(ctx))
	snapshotStore := testkit.NewSnapshotStore()
	require.NoError(t, snapshotStore.Connect(ctx))

	const id = "copy-then-delete"
	require.NoError(t, eventsStore.WriteEvents(ctx, source, []*egopb.Event{
		newLegacyEvent(t, id, 1, 100), newLegacyEvent(t, id, 2, 200),
	}, persistence.Unconditional()))
	require.NoError(t, snapshotStore.WriteSnapshot(ctx, source, newLegacySnapshot(t, id, 2, 200)))

	assignment := fixedAssignment(map[string]tenancy.TenantID{id: "acme"})
	keep, err := NewTenantAdopter(assignment, WithEventsStore(eventsStore), WithSnapshotStore(snapshotStore), WithWriteEnabled())
	require.NoError(t, err)
	first, err := keep.Run(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, first.Copied)
	require.Zero(t, first.SourceDeleted)

	deleting, err := NewTenantAdopter(assignment, WithEventsStore(eventsStore), WithSnapshotStore(snapshotStore), WithWriteEnabled(), WithSourceDeletion())
	require.NoError(t, err)
	second, err := deleting.Run(ctx)
	require.NoError(t, err)
	assert.Zero(t, second.Failed)
	assert.Equal(t, 1, second.AlreadyPresent, "nothing was copied: the target was already the exact adoption")
	assert.Zero(t, second.Copied)
	assert.Zero(t, second.Verified)
	assert.Equal(t, 1, second.SourceDeleted, "the verified source must now be deleted")
	require.Len(t, second.Aggregates, 1)
	assert.Equal(t, StatusSourceDeleted, second.Aggregates[0].Events.Status)
	assert.Equal(t, StatusSourceDeleted, second.Aggregates[0].Snapshot.Status)

	events, err := eventsStore.ReplayEvents(ctx, source, id, 1, 10, 10)
	require.NoError(t, err)
	assert.Empty(t, events)
	snap, err := snapshotStore.GetLatestSnapshot(ctx, source, id)
	require.NoError(t, err)
	assert.Nil(t, snap)
}

// replacingSnapshotStore overwrites the source snapshot at the SAME sequence
// number while the adopter reads back the target, the way a concurrent
// writer can with a store that keys snapshots by (scope, id, sequence).
type replacingSnapshotStore struct {
	persistence.SnapshotStore
	source, target persistence.Scope
	replacement    *egopb.Snapshot
	targetReads    int
}

func (r *replacingSnapshotStore) GetLatestSnapshot(ctx context.Context, scope persistence.Scope, persistenceID string) (*egopb.Snapshot, error) {
	snapshot, err := r.SnapshotStore.GetLatestSnapshot(ctx, scope, persistenceID)
	if err == nil && scope.Equal(r.target) && snapshot != nil {
		r.targetReads++
		if r.targetReads == 1 {
			if writeErr := r.SnapshotStore.WriteSnapshot(ctx, r.source, r.replacement); writeErr != nil {
				return nil, writeErr
			}
		}
	}
	return snapshot, err
}

func TestTenantAdopterRefusesDeletionOfReplacedSameSequenceSnapshot(t *testing.T) {
	ctx := context.Background()
	source := persistence.Unscoped()
	target, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)
	base := testkit.NewSnapshotStore()
	require.NoError(t, base.Connect(ctx))

	const id = "replaced-snapshot"
	require.NoError(t, base.WriteSnapshot(ctx, source, newLegacySnapshot(t, id, 4, 400)))
	replacement := newLegacySnapshot(t, id, 4, 444)

	adopter, err := NewTenantAdopter(fixedAssignment(map[string]tenancy.TenantID{id: "acme"}),
		WithSnapshotStore(&replacingSnapshotStore{SnapshotStore: base, source: source, target: target, replacement: replacement}),
		WithPersistenceIDs(id), WithWriteEnabled(), WithSourceDeletion())
	require.NoError(t, err)
	report, err := adopter.Run(ctx)
	require.NoError(t, err)
	assert.Zero(t, report.SourceDeleted)
	assert.Equal(t, 1, report.Failed)
	require.Len(t, report.Failures, 1)
	assert.ErrorIs(t, report.Failures[0], errSourceChangedDuringAdoption)

	kept, err := base.GetLatestSnapshot(ctx, source, id)
	require.NoError(t, err)
	require.NotNil(t, kept)
	assert.True(t, proto.Equal(replacement, kept), "the replacement snapshot must survive")
}

// replacingEventsStore rewrites an existing source event in place (same
// sequence number) while the adopter reads back the target.
type replacingEventsStore struct {
	persistence.EventsStore
	source, target persistence.Scope
	replacement    *egopb.Event
	fired          bool
}

func (r *replacingEventsStore) ReplayEvents(ctx context.Context, scope persistence.Scope, persistenceID string, from, to, maxNumber uint64) ([]*egopb.Event, error) {
	events, err := r.EventsStore.ReplayEvents(ctx, scope, persistenceID, from, to, maxNumber)
	if err == nil && !r.fired && scope.Equal(r.target) && len(events) > 0 {
		r.fired = true
		if writeErr := r.EventsStore.WriteEvents(ctx, r.source, []*egopb.Event{r.replacement}, persistence.Unconditional()); writeErr != nil {
			return nil, writeErr
		}
	}
	return events, err
}

func TestTenantAdopterRefusesDeletionOfRewrittenSourceEvent(t *testing.T) {
	ctx := context.Background()
	source := persistence.Unscoped()
	target, err := persistence.NewTenantScope("acme")
	require.NoError(t, err)
	base := testkit.NewEventsStore()
	require.NoError(t, base.Connect(ctx))

	const id = "rewritten-event"
	require.NoError(t, base.WriteEvents(ctx, source, []*egopb.Event{
		newLegacyEvent(t, id, 1, 100), newLegacyEvent(t, id, 2, 200),
	}, persistence.Unconditional()))

	adopter, err := NewTenantAdopter(fixedAssignment(map[string]tenancy.TenantID{id: "acme"}),
		WithEventsStore(&replacingEventsStore{EventsStore: base, source: source, target: target, replacement: newLegacyEvent(t, id, 2, 222)}),
		WithWriteEnabled(), WithSourceDeletion())
	require.NoError(t, err)
	report, err := adopter.Run(ctx)
	require.NoError(t, err)
	assert.Zero(t, report.SourceDeleted)
	assert.Equal(t, 1, report.Failed)
	require.Len(t, report.Failures, 1)
	assert.ErrorIs(t, report.Failures[0], errSourceChangedDuringAdoption)

	remaining, err := base.ReplayEvents(ctx, source, id, 1, 10, 10)
	require.NoError(t, err)
	assert.Len(t, remaining, 2, "a source that was rewritten must not be deleted")
}
