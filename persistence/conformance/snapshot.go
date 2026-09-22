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

package conformance

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/pablogore/ego/v4/persistence"
)

// RunSnapshotStoreConformance runs the full cross-tenant isolation matrix
// (SnapshotStoreChecks) against a persistence.SnapshotStore implementation.
// newStore MUST return a fresh, empty store on every call — see the package
// doc comment.
func RunSnapshotStoreConformance(t *testing.T, newStore func(t *testing.T) persistence.SnapshotStore) {
	t.Helper()
	runConformance(t, SnapshotStoreChecks, newStore)
}

// CaptureSnapshotStoreChecks runs SnapshotStoreChecks against a fresh store
// obtained from newStore for each check, capturing pass/fail without a
// *testing.T. See the package doc comment's Self-checking section.
func CaptureSnapshotStoreChecks(newStore func() persistence.SnapshotStore) []CheckResult {
	return captureChecks(SnapshotStoreChecks, newStore)
}

// SnapshotStoreChecks is the named isolation matrix
// RunSnapshotStoreConformance runs against a persistence.SnapshotStore.
// SnapshotStore has no WritePrecondition parameter and no PersistenceIDs
// method, so — unlike EventsStoreChecks — there is no CAS or Enumeration
// check here; read isolation, write isolation (including scoped delete),
// and Unscoped compatibility apply identically.
var SnapshotStoreChecks = []Check[persistence.SnapshotStore]{
	{Name: "ReadIsolation/OtherTenantGetsNothing", Run: snapshotOtherTenantGetsNothing},
	{Name: "ReadIsolation/UnscopedAndTenantDoNotCrossRead", Run: snapshotUnscopedAndTenantDoNotCrossRead},
	{Name: "ReadIsolation/BothTenantsReadTheirOwnRecord", Run: snapshotBothTenantsReadOwnRecord},
	{Name: "WriteIsolation/OtherTenantWriteLeavesRecordUntouched", Run: snapshotOtherTenantWriteLeavesRecordUntouched},
	{Name: "WriteIsolation/DeleteIsScoped", Run: snapshotDeleteIsScoped},
	{Name: "Unscoped/NeverCollidesWithTenantNamedUnscoped", Run: snapshotUnscopedNeverCollidesWithForgedTenant},
}

func snapshotOtherTenantGetsNothing(ctx context.Context, t require.TestingT, store persistence.SnapshotStore) {
	tenantA := mustTenantScope(t, "tenant-a")
	tenantB := mustTenantScope(t, "tenant-b")
	const id = "read-isolation"

	require.NoError(t, store.WriteSnapshot(ctx, tenantA, snapshotRecord(t, id, 1, 111)))

	got, err := store.GetLatestSnapshot(ctx, tenantB, id)
	require.NoError(t, err)
	require.Nil(t, got, "tenant B must not see tenant A's snapshot")
}

func snapshotUnscopedAndTenantDoNotCrossRead(ctx context.Context, t require.TestingT, store persistence.SnapshotStore) {
	tenantA := mustTenantScope(t, "tenant-a")

	const idWrittenByTenant = "unscoped-cross-read-tenant-wrote"
	require.NoError(t, store.WriteSnapshot(ctx, tenantA, snapshotRecord(t, idWrittenByTenant, 1, 111)))
	gotUnscoped, err := store.GetLatestSnapshot(ctx, persistence.Unscoped(), idWrittenByTenant)
	require.NoError(t, err)
	require.Nil(t, gotUnscoped, "Unscoped() must not see a snapshot written only under a tenant scope")

	const idWrittenByUnscoped = "unscoped-cross-read-unscoped-wrote"
	require.NoError(t, store.WriteSnapshot(ctx, persistence.Unscoped(), snapshotRecord(t, idWrittenByUnscoped, 1, 222)))
	gotTenant, err := store.GetLatestSnapshot(ctx, tenantA, idWrittenByUnscoped)
	require.NoError(t, err)
	require.Nil(t, gotTenant, "a tenant scope must not see a snapshot written only under Unscoped()")
}

func snapshotBothTenantsReadOwnRecord(ctx context.Context, t require.TestingT, store persistence.SnapshotStore) {
	tenantA := mustTenantScope(t, "tenant-a")
	tenantB := mustTenantScope(t, "tenant-b")
	const id = "both-write-own-read"

	require.NoError(t, store.WriteSnapshot(ctx, tenantA, snapshotRecord(t, id, 1, 111)))
	require.NoError(t, store.WriteSnapshot(ctx, tenantB, snapshotRecord(t, id, 1, 222)))

	gotA, err := store.GetLatestSnapshot(ctx, tenantA, id)
	require.NoError(t, err)
	require.NotNil(t, gotA)
	require.Equal(t, float64(111), snapshotMarker(t, gotA), "tenant A must read back its own snapshot, never tenant B's")

	gotB, err := store.GetLatestSnapshot(ctx, tenantB, id)
	require.NoError(t, err)
	require.NotNil(t, gotB)
	require.Equal(t, float64(222), snapshotMarker(t, gotB), "tenant B must read back its own snapshot, never tenant A's")
}

func snapshotOtherTenantWriteLeavesRecordUntouched(ctx context.Context, t require.TestingT, store persistence.SnapshotStore) {
	tenantA := mustTenantScope(t, "tenant-a")
	tenantB := mustTenantScope(t, "tenant-b")
	const id = "write-isolation"

	require.NoError(t, store.WriteSnapshot(ctx, tenantA, snapshotRecord(t, id, 1, 111)))
	before, err := store.GetLatestSnapshot(ctx, tenantA, id)
	require.NoError(t, err)
	require.NotNil(t, before)

	// Same persistence_id AND the same sequence number, deliberately: a
	// store keying only by (persistenceID, sequenceNumber) would let this
	// overwrite tenant A's snapshot outright.
	require.NoError(t, store.WriteSnapshot(ctx, tenantB, snapshotRecord(t, id, 1, 999)))

	after, err := store.GetLatestSnapshot(ctx, tenantA, id)
	require.NoError(t, err)
	require.NotNil(t, after)
	require.True(t, proto.Equal(before, after), "tenant B's write must not modify tenant A's snapshot for the same persistence_id and sequence number")
}

func snapshotDeleteIsScoped(ctx context.Context, t require.TestingT, store persistence.SnapshotStore) {
	tenantA := mustTenantScope(t, "tenant-a")
	tenantB := mustTenantScope(t, "tenant-b")
	const id = "delete-isolation"

	require.NoError(t, store.WriteSnapshot(ctx, tenantA, snapshotRecord(t, id, 1, 111)))
	require.NoError(t, store.WriteSnapshot(ctx, tenantB, snapshotRecord(t, id, 1, 222)))

	require.NoError(t, store.DeleteSnapshots(ctx, tenantB, id, 100))

	gotA, err := store.GetLatestSnapshot(ctx, tenantA, id)
	require.NoError(t, err)
	require.NotNil(t, gotA, "deleting tenant B's snapshots must not delete tenant A's")
	require.Equal(t, float64(111), snapshotMarker(t, gotA))

	gotB, err := store.GetLatestSnapshot(ctx, tenantB, id)
	require.NoError(t, err)
	require.Nil(t, gotB, "tenant B's own snapshot must actually be gone after its own scoped delete")
}

func snapshotUnscopedNeverCollidesWithForgedTenant(ctx context.Context, t require.TestingT, store persistence.SnapshotStore) {
	forgedTenant := mustTenantScope(t, "unscoped")
	const id = "forging-guard"

	require.NoError(t, store.WriteSnapshot(ctx, persistence.Unscoped(), snapshotRecord(t, id, 1, 111)))
	require.NoError(t, store.WriteSnapshot(ctx, forgedTenant, snapshotRecord(t, id, 1, 222)))

	gotUnscoped, err := store.GetLatestSnapshot(ctx, persistence.Unscoped(), id)
	require.NoError(t, err)
	require.NotNil(t, gotUnscoped)
	require.Equal(t, float64(111), snapshotMarker(t, gotUnscoped))

	gotForged, err := store.GetLatestSnapshot(ctx, forgedTenant, id)
	require.NoError(t, err)
	require.NotNil(t, gotForged)
	require.Equal(t, float64(222), snapshotMarker(t, gotForged))
}
