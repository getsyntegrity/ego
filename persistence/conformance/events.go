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
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/pablogore/ego/v4/persistence"
)

// RunEventsStoreConformance runs the full cross-tenant isolation matrix
// (EventsStoreChecks) against a persistence.EventsStore implementation.
// newStore MUST return a fresh, empty store on every call — see the package
// doc comment.
func RunEventsStoreConformance(t *testing.T, newStore func(t *testing.T) persistence.EventsStore) {
	t.Helper()
	runConformance(t, EventsStoreChecks, newStore)
}

// CaptureEventsStoreChecks runs EventsStoreChecks against a fresh store
// obtained from newStore for each check, capturing pass/fail without a
// *testing.T. See the package doc comment's Self-checking section.
func CaptureEventsStoreChecks(newStore func() persistence.EventsStore) []CheckResult {
	return captureChecks(EventsStoreChecks, newStore)
}

// EventsStoreChecks is the named isolation matrix RunEventsStoreConformance
// runs against a persistence.EventsStore. Exported so a permanent regression
// guard (testkit/conformance_test.go's TestConformanceCatchesNonIsolatingStore)
// can prove this suite actually fails against a store that ignores Scope.
var EventsStoreChecks = []Check[persistence.EventsStore]{
	{Name: "ReadIsolation/OtherTenantGetsNothing", Run: eventsOtherTenantGetsNothing},
	{Name: "ReadIsolation/UnscopedAndTenantDoNotCrossRead", Run: eventsUnscopedAndTenantDoNotCrossRead},
	{Name: "ReadIsolation/BothTenantsReadTheirOwnRecord", Run: eventsBothTenantsReadOwnRecord},
	{Name: "WriteIsolation/OtherTenantWriteLeavesRecordUntouched", Run: eventsOtherTenantWriteLeavesRecordUntouched},
	{Name: "WriteIsolation/DeleteIsScoped", Run: eventsDeleteIsScoped},
	{Name: "CAS/ExpectGenesisSucceedsForNewTenantOnEstablishedID", Run: eventsExpectGenesisSucceedsForNewTenant},
	{Name: "CAS/ExpectRevisionConflictCarriesItsScope", Run: eventsExpectRevisionConflictCarriesScope},
	{Name: "CAS/ConflictInOneScopeNotObservableInAnother", Run: eventsConflictNotObservableInAnotherScope},
	{Name: "Enumeration/PersistenceIDsScopedToOwnTenant", Run: eventsPersistenceIDsScopedToOwnTenant},
	{Name: "Unscoped/NeverCollidesWithTenantNamedUnscoped", Run: eventsUnscopedNeverCollidesWithForgedTenant},
}

func eventsOtherTenantGetsNothing(t require.TestingT, ctx context.Context, store persistence.EventsStore) {
	tenantA := mustTenantScope(t, "tenant-a")
	tenantB := mustTenantScope(t, "tenant-b")
	const id = "read-isolation"

	require.NoError(t, store.WriteEvents(ctx, tenantA, eventBatch(t, id, 1, 111), persistence.Unconditional()))

	got, err := store.GetLatestEvent(ctx, tenantB, id)
	require.NoError(t, err)
	require.Nil(t, got, "tenant B must not see tenant A's record")

	replayed, err := store.ReplayEvents(ctx, tenantB, id, 1, 1, 10)
	require.NoError(t, err)
	require.Empty(t, replayed, "tenant B must not replay tenant A's events")
}

func eventsUnscopedAndTenantDoNotCrossRead(t require.TestingT, ctx context.Context, store persistence.EventsStore) {
	tenantA := mustTenantScope(t, "tenant-a")

	const idWrittenByTenant = "unscoped-cross-read-tenant-wrote"
	require.NoError(t, store.WriteEvents(ctx, tenantA, eventBatch(t, idWrittenByTenant, 1, 111), persistence.Unconditional()))
	gotUnscoped, err := store.GetLatestEvent(ctx, persistence.Unscoped(), idWrittenByTenant)
	require.NoError(t, err)
	require.Nil(t, gotUnscoped, "Unscoped() must not see a record written only under a tenant scope")

	const idWrittenByUnscoped = "unscoped-cross-read-unscoped-wrote"
	require.NoError(t, store.WriteEvents(ctx, persistence.Unscoped(), eventBatch(t, idWrittenByUnscoped, 1, 222), persistence.Unconditional()))
	gotTenant, err := store.GetLatestEvent(ctx, tenantA, idWrittenByUnscoped)
	require.NoError(t, err)
	require.Nil(t, gotTenant, "a tenant scope must not see a record written only under Unscoped()")
}

func eventsBothTenantsReadOwnRecord(t require.TestingT, ctx context.Context, store persistence.EventsStore) {
	tenantA := mustTenantScope(t, "tenant-a")
	tenantB := mustTenantScope(t, "tenant-b")
	const id = "both-write-own-read"

	require.NoError(t, store.WriteEvents(ctx, tenantA, eventBatch(t, id, 1, 111), persistence.Unconditional()))
	require.NoError(t, store.WriteEvents(ctx, tenantB, eventBatch(t, id, 1, 222), persistence.Unconditional()))

	gotA, err := store.GetLatestEvent(ctx, tenantA, id)
	require.NoError(t, err)
	require.NotNil(t, gotA)
	require.Equal(t, float64(111), eventMarker(t, gotA), "tenant A must read back its own record, never tenant B's")

	gotB, err := store.GetLatestEvent(ctx, tenantB, id)
	require.NoError(t, err)
	require.NotNil(t, gotB)
	require.Equal(t, float64(222), eventMarker(t, gotB), "tenant B must read back its own record, never tenant A's")
}

func eventsOtherTenantWriteLeavesRecordUntouched(t require.TestingT, ctx context.Context, store persistence.EventsStore) {
	tenantA := mustTenantScope(t, "tenant-a")
	tenantB := mustTenantScope(t, "tenant-b")
	const id = "write-isolation"

	require.NoError(t, store.WriteEvents(ctx, tenantA, eventBatch(t, id, 1, 111), persistence.Unconditional()))
	before, err := store.GetLatestEvent(ctx, tenantA, id)
	require.NoError(t, err)
	require.NotNil(t, before)

	require.NoError(t, store.WriteEvents(ctx, tenantB, eventBatch(t, id, 1, 999), persistence.Unconditional()))

	after, err := store.GetLatestEvent(ctx, tenantA, id)
	require.NoError(t, err)
	require.NotNil(t, after)
	require.True(t, proto.Equal(before, after), "tenant B's write must not modify tenant A's record for the same persistence_id")
}

func eventsDeleteIsScoped(t require.TestingT, ctx context.Context, store persistence.EventsStore) {
	tenantA := mustTenantScope(t, "tenant-a")
	tenantB := mustTenantScope(t, "tenant-b")
	const id = "delete-isolation"

	require.NoError(t, store.WriteEvents(ctx, tenantA, eventBatch(t, id, 1, 111), persistence.Unconditional()))
	require.NoError(t, store.WriteEvents(ctx, tenantB, eventBatch(t, id, 1, 222), persistence.Unconditional()))

	require.NoError(t, store.DeleteEvents(ctx, tenantB, id, 100))

	gotA, err := store.GetLatestEvent(ctx, tenantA, id)
	require.NoError(t, err)
	require.NotNil(t, gotA, "deleting tenant B's events must not delete tenant A's")
	require.Equal(t, float64(111), eventMarker(t, gotA))

	gotB, err := store.GetLatestEvent(ctx, tenantB, id)
	require.NoError(t, err)
	require.Nil(t, gotB, "tenant B's own record must actually be gone after its own scoped delete")
}

// eventsExpectGenesisSucceedsForNewTenant is the sharpest check in the
// suite. A store that merely tags rows with tenant_metadata
// (ego-store-001's R7) but still keys its compare-and-swap by
// persistence_id alone gets this wrong: it would observe tenant A's
// already-established revision and reject tenant B's genesis write with a
// spurious conflict, even though tenant B has never written this
// persistence_id before. Correct isolation means tenant B's CAS state is
// entirely independent of tenant A's.
func eventsExpectGenesisSucceedsForNewTenant(t require.TestingT, ctx context.Context, store persistence.EventsStore) {
	tenantA := mustTenantScope(t, "tenant-a")
	tenantB := mustTenantScope(t, "tenant-b")
	const id = "genesis-cross-tenant"

	require.NoError(t, store.WriteEvents(ctx, tenantA, eventBatch(t, id, 1, 111), persistence.ExpectGenesis()))

	err := store.WriteEvents(ctx, tenantB, eventBatch(t, id, 1, 222), persistence.ExpectGenesis())
	require.NoError(t, err, "ExpectGenesis() must succeed for tenant B: tenant A having already established this id must never leak into tenant B's own CAS state")

	gotB, err := store.GetLatestEvent(ctx, tenantB, id)
	require.NoError(t, err)
	require.NotNil(t, gotB)
	require.Equal(t, float64(222), eventMarker(t, gotB))
}

func eventsExpectRevisionConflictCarriesScope(t require.TestingT, ctx context.Context, store persistence.EventsStore) {
	tenantA := mustTenantScope(t, "tenant-a")
	const id = "conflict-carries-scope"

	require.NoError(t, store.WriteEvents(ctx, tenantA, eventBatch(t, id, 1, 111), persistence.ExpectGenesis()))

	err := store.WriteEvents(ctx, tenantA, eventBatch(t, id, 2, 222), persistence.ExpectRevision(999))
	require.Error(t, err)
	require.ErrorIs(t, err, persistence.ErrConcurrencyConflict)

	var conflictErr *persistence.ConflictError
	require.True(t, errors.As(err, &conflictErr), "the returned error must be a *persistence.ConflictError")
	require.True(t, conflictErr.Scope().Equal(tenantA), "the conflict must carry the scope the failed write actually targeted")
}

func eventsConflictNotObservableInAnotherScope(t require.TestingT, ctx context.Context, store persistence.EventsStore) {
	tenantA := mustTenantScope(t, "tenant-a")
	tenantB := mustTenantScope(t, "tenant-b")
	const id = "conflict-not-cross-scope"

	require.NoError(t, store.WriteEvents(ctx, tenantA, eventBatch(t, id, 1, 111), persistence.ExpectGenesis()))
	// This attempt conflicts in tenant A's own scope: A already has a record.
	err := store.WriteEvents(ctx, tenantA, eventBatch(t, id, 1, 999), persistence.ExpectGenesis())
	require.Error(t, err)
	require.ErrorIs(t, err, persistence.ErrConcurrencyConflict)

	// Tenant B, using the exact same precondition that just conflicted in A,
	// must succeed: A's conflict must not have left any observable trace in
	// B's own CAS state.
	err = store.WriteEvents(ctx, tenantB, eventBatch(t, id, 1, 222), persistence.ExpectGenesis())
	require.NoError(t, err, "a conflict raised in tenant A's scope must not be observable in tenant B's scope")
}

func eventsPersistenceIDsScopedToOwnTenant(t require.TestingT, ctx context.Context, store persistence.EventsStore) {
	tenantA := mustTenantScope(t, "tenant-a")
	tenantB := mustTenantScope(t, "tenant-b")
	const sharedID = "shared-id"
	const aOnlyID = "a-only-id"

	require.NoError(t, store.WriteEvents(ctx, tenantA, eventBatch(t, sharedID, 1, 1), persistence.Unconditional()))
	require.NoError(t, store.WriteEvents(ctx, tenantB, eventBatch(t, sharedID, 1, 2), persistence.Unconditional()))
	require.NoError(t, store.WriteEvents(ctx, persistence.Unscoped(), eventBatch(t, sharedID, 1, 3), persistence.Unconditional()))
	require.NoError(t, store.WriteEvents(ctx, tenantA, eventBatch(t, aOnlyID, 1, 4), persistence.Unconditional()))

	idsA, _, err := store.PersistenceIDs(ctx, tenantA, 100, "")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{sharedID, aOnlyID}, idsA, "PersistenceIDs(tenantA) must list exactly tenant A's own ids, never tenant B's or Unscoped()'s, even for an identical persistence_id string")

	idsB, _, err := store.PersistenceIDs(ctx, tenantB, 100, "")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{sharedID}, idsB)
	require.NotContains(t, idsB, aOnlyID)
}

func eventsUnscopedNeverCollidesWithForgedTenant(t require.TestingT, ctx context.Context, store persistence.EventsStore) {
	forgedTenant := mustTenantScope(t, "unscoped")
	const id = "forging-guard"

	require.NoError(t, store.WriteEvents(ctx, persistence.Unscoped(), eventBatch(t, id, 1, 111), persistence.ExpectGenesis()))
	// The sharp assertion is the CAS success below: a store that reduced Scope
	// to its String() form ("unscoped" vs "tenant:unscoped") could still pass
	// the read/write checks above by accident, but would incorrectly conflict
	// here if it ever compared scopes by text rather than structurally.
	err := store.WriteEvents(ctx, forgedTenant, eventBatch(t, id, 1, 222), persistence.ExpectGenesis())
	require.NoError(t, err, `a tenant literally named "unscoped" must never forge Unscoped()'s own CAS state`)

	gotUnscoped, err := store.GetLatestEvent(ctx, persistence.Unscoped(), id)
	require.NoError(t, err)
	require.NotNil(t, gotUnscoped)
	require.Equal(t, float64(111), eventMarker(t, gotUnscoped))

	gotForged, err := store.GetLatestEvent(ctx, forgedTenant, id)
	require.NoError(t, err)
	require.NotNil(t, gotForged)
	require.Equal(t, float64(222), eventMarker(t, gotForged))
}
