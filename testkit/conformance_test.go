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

// This file wires EGO-TENANT-003 (T3)'s store-agnostic conformance suite
// (persistence/conformance) into the in-repo testkit stores, and carries the
// suite's own permanent self-check: proof that the suite actually fails
// against a store that does not isolate by persistence.Scope. See
// persistence/conformance's package doc comment for the full contract.
package testkit

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/persistence"
	"github.com/pablogore/ego/v4/persistence/conformance"
)

// ---------------------------------------------------------------------------
// Wiring: passing these three proves both that the suite is usable end to
// end by an adapter author, and that T2's EventStore/DurableStore/
// SnapshotStore actually satisfy EGO-TENANT-003's isolation requirements.
// ---------------------------------------------------------------------------

func TestEventStoreConformance(t *testing.T) {
	conformance.RunEventsStoreConformance(t, func(t *testing.T) persistence.EventsStore {
		return NewEventsStore()
	})
}

func TestDurableStoreConformance(t *testing.T) {
	conformance.RunStateStoreConformance(t, func(t *testing.T) persistence.StateStore {
		return NewDurableStore()
	})
}

func TestSnapshotStoreConformance(t *testing.T) {
	conformance.RunSnapshotStoreConformance(t, func(t *testing.T) persistence.SnapshotStore {
		return NewSnapshotStore()
	})
}

// ---------------------------------------------------------------------------
// TestConformanceCatchesNonIsolatingStore: the most important test in this
// file. A conformance suite that passes against a store with no real
// isolation is worse than none — this is the permanent regression guard
// proving conformance's checks actually fail when they should.
//
// Each wrapper below deliberately collapses every caller-supplied Scope to
// persistence.Unscoped() before delegating to a real, otherwise-correct
// in-repo store — exactly the shape of a naive adapter that merely tags rows
// with tenant_metadata (ego-store-001 R7) without actually keying storage by
// tenant. conformance.CaptureEventsStoreChecks/CaptureStateStoreChecks/
// CaptureSnapshotStoreChecks run the exact same named checks that
// RunEventsStoreConformance/RunStateStoreConformance/
// RunSnapshotStoreConformance run above, but capture pass/fail via a
// require.TestingT that records failures instead of calling t.FailNow(), so
// the detected (expected) failure can be asserted on here instead of
// propagating into this test's own result.
// ---------------------------------------------------------------------------

func TestConformanceCatchesNonIsolatingStore(t *testing.T) {
	t.Run("EventsStore", func(t *testing.T) {
		results := conformance.CaptureEventsStoreChecks(newNonIsolatingEventsStore)
		assertSuiteDetectedNonIsolation(t, results)
	})

	t.Run("StateStore", func(t *testing.T) {
		results := conformance.CaptureStateStoreChecks(newNonIsolatingDurableStore)
		assertSuiteDetectedNonIsolation(t, results)
	})

	t.Run("SnapshotStore", func(t *testing.T) {
		results := conformance.CaptureSnapshotStoreChecks(newNonIsolatingSnapshotStore)
		assertSuiteDetectedNonIsolation(t, results)
	})
}

// assertSuiteDetectedNonIsolation requires at least one captured check to
// have failed, and logs every result (with -v) so a future silent recovery
// to an always-passing suite is visible check-by-check, not just as a single
// boolean.
func assertSuiteDetectedNonIsolation(t *testing.T, results []conformance.CheckResult) {
	t.Helper()
	require.NotEmpty(t, results)

	var failed []string
	for _, r := range results {
		t.Logf("check %-65s failed=%v errors=%v", r.Name, r.Failed, r.Errors)
		if r.Failed {
			failed = append(failed, r.Name)
		}
	}
	require.NotEmpty(t, failed, "the conformance suite must detect at least one isolation violation against a non-isolating store")
}

// ---------------------------------------------------------------------------
// Non-isolating wrappers. Each ignores its Scope parameter entirely,
// delegating to Unscoped() regardless of what was actually passed, so every
// tenant collides with every other tenant (and with Unscoped()) exactly as
// a pre-TENANT-003 store would.
// ---------------------------------------------------------------------------

type nonIsolatingEventsStore struct {
	*EventStore
}

func newNonIsolatingEventsStore() persistence.EventsStore {
	return &nonIsolatingEventsStore{EventStore: NewEventsStore()}
}

func (x *nonIsolatingEventsStore) WriteEvents(ctx context.Context, _ persistence.Scope, events []*egopb.Event, precondition persistence.WritePrecondition) error {
	return x.EventStore.WriteEvents(ctx, persistence.Unscoped(), events, precondition)
}

func (x *nonIsolatingEventsStore) DeleteEvents(ctx context.Context, _ persistence.Scope, persistenceID string, toSequenceNumber uint64) error {
	return x.EventStore.DeleteEvents(ctx, persistence.Unscoped(), persistenceID, toSequenceNumber)
}

func (x *nonIsolatingEventsStore) ReplayEvents(ctx context.Context, _ persistence.Scope, persistenceID string, fromSequenceNumber, toSequenceNumber uint64, limit uint64) ([]*egopb.Event, error) {
	return x.EventStore.ReplayEvents(ctx, persistence.Unscoped(), persistenceID, fromSequenceNumber, toSequenceNumber, limit)
}

func (x *nonIsolatingEventsStore) GetLatestEvent(ctx context.Context, _ persistence.Scope, persistenceID string) (*egopb.Event, error) {
	return x.EventStore.GetLatestEvent(ctx, persistence.Unscoped(), persistenceID)
}

func (x *nonIsolatingEventsStore) PersistenceIDs(ctx context.Context, _ persistence.Scope, pageSize uint64, pageToken string) ([]string, string, error) {
	return x.EventStore.PersistenceIDs(ctx, persistence.Unscoped(), pageSize, pageToken)
}

var _ persistence.EventsStore = (*nonIsolatingEventsStore)(nil)

type nonIsolatingDurableStore struct {
	*DurableStore
}

func newNonIsolatingDurableStore() persistence.StateStore {
	return &nonIsolatingDurableStore{DurableStore: NewDurableStore()}
}

func (x *nonIsolatingDurableStore) WriteState(ctx context.Context, _ persistence.Scope, state *egopb.DurableState, precondition persistence.WritePrecondition) error {
	return x.DurableStore.WriteState(ctx, persistence.Unscoped(), state, precondition)
}

func (x *nonIsolatingDurableStore) GetLatestState(ctx context.Context, _ persistence.Scope, persistenceID string) (*egopb.DurableState, error) {
	return x.DurableStore.GetLatestState(ctx, persistence.Unscoped(), persistenceID)
}

var _ persistence.StateStore = (*nonIsolatingDurableStore)(nil)

type nonIsolatingSnapshotStore struct {
	*SnapshotStore
}

func newNonIsolatingSnapshotStore() persistence.SnapshotStore {
	return &nonIsolatingSnapshotStore{SnapshotStore: NewSnapshotStore()}
}

func (x *nonIsolatingSnapshotStore) WriteSnapshot(ctx context.Context, _ persistence.Scope, snapshot *egopb.Snapshot) error {
	return x.SnapshotStore.WriteSnapshot(ctx, persistence.Unscoped(), snapshot)
}

func (x *nonIsolatingSnapshotStore) GetLatestSnapshot(ctx context.Context, _ persistence.Scope, persistenceID string) (*egopb.Snapshot, error) {
	return x.SnapshotStore.GetLatestSnapshot(ctx, persistence.Unscoped(), persistenceID)
}

func (x *nonIsolatingSnapshotStore) DeleteSnapshots(ctx context.Context, _ persistence.Scope, persistenceID string, toSequenceNumber uint64) error {
	return x.SnapshotStore.DeleteSnapshots(ctx, persistence.Unscoped(), persistenceID, toSequenceNumber)
}

var _ persistence.SnapshotStore = (*nonIsolatingSnapshotStore)(nil)
