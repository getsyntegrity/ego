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

// RunStateStoreConformance runs the full cross-tenant isolation matrix
// (StateStoreChecks) against a persistence.StateStore implementation.
// newStore MUST return a fresh, empty store on every call — see the package
// doc comment.
func RunStateStoreConformance(t *testing.T, newStore func(t *testing.T) persistence.StateStore) {
	t.Helper()
	runConformance(t, StateStoreChecks, newStore)
}

// CaptureStateStoreChecks runs StateStoreChecks against a fresh store
// obtained from newStore for each check, capturing pass/fail without a
// *testing.T. See the package doc comment's Self-checking section.
func CaptureStateStoreChecks(newStore func() persistence.StateStore) []CheckResult {
	return captureChecks(StateStoreChecks, newStore)
}

// StateStoreChecks is the named isolation matrix RunStateStoreConformance
// runs against a persistence.StateStore. persistence.StateStore has no
// delete or enumeration method, so — unlike EventsStoreChecks — there is no
// DeleteIsScoped or Enumeration check here; every other part of the matrix
// (read isolation, write isolation, and WRITE-004 CAS semantics preserved
// per scope) applies identically.
var StateStoreChecks = []Check[persistence.StateStore]{
	{Name: "ReadIsolation/OtherTenantGetsNothing", Run: stateOtherTenantGetsNothing},
	{Name: "ReadIsolation/UnscopedAndTenantDoNotCrossRead", Run: stateUnscopedAndTenantDoNotCrossRead},
	{Name: "ReadIsolation/BothTenantsReadTheirOwnRecord", Run: stateBothTenantsReadOwnRecord},
	{Name: "WriteIsolation/OtherTenantWriteLeavesRecordUntouched", Run: stateOtherTenantWriteLeavesRecordUntouched},
	{Name: "CAS/ExpectGenesisSucceedsForNewTenantOnEstablishedID", Run: stateExpectGenesisSucceedsForNewTenant},
	{Name: "CAS/ExpectRevisionConflictCarriesItsScope", Run: stateExpectRevisionConflictCarriesScope},
	{Name: "CAS/ConflictInOneScopeNotObservableInAnother", Run: stateConflictNotObservableInAnotherScope},
	{Name: "Unscoped/NeverCollidesWithTenantNamedUnscoped", Run: stateUnscopedNeverCollidesWithForgedTenant},
}

func stateOtherTenantGetsNothing(t require.TestingT, ctx context.Context, store persistence.StateStore) {
	tenantA := mustTenantScope(t, "tenant-a")
	tenantB := mustTenantScope(t, "tenant-b")
	const id = "read-isolation"

	require.NoError(t, store.WriteState(ctx, tenantA, stateRecord(t, id, 1, 111), persistence.Unconditional()))

	got, err := store.GetLatestState(ctx, tenantB, id)
	require.NoError(t, err)
	require.Nil(t, got, "tenant B must not see tenant A's record")
}

func stateUnscopedAndTenantDoNotCrossRead(t require.TestingT, ctx context.Context, store persistence.StateStore) {
	tenantA := mustTenantScope(t, "tenant-a")

	const idWrittenByTenant = "unscoped-cross-read-tenant-wrote"
	require.NoError(t, store.WriteState(ctx, tenantA, stateRecord(t, idWrittenByTenant, 1, 111), persistence.Unconditional()))
	gotUnscoped, err := store.GetLatestState(ctx, persistence.Unscoped(), idWrittenByTenant)
	require.NoError(t, err)
	require.Nil(t, gotUnscoped, "Unscoped() must not see a record written only under a tenant scope")

	const idWrittenByUnscoped = "unscoped-cross-read-unscoped-wrote"
	require.NoError(t, store.WriteState(ctx, persistence.Unscoped(), stateRecord(t, idWrittenByUnscoped, 1, 222), persistence.Unconditional()))
	gotTenant, err := store.GetLatestState(ctx, tenantA, idWrittenByUnscoped)
	require.NoError(t, err)
	require.Nil(t, gotTenant, "a tenant scope must not see a record written only under Unscoped()")
}

func stateBothTenantsReadOwnRecord(t require.TestingT, ctx context.Context, store persistence.StateStore) {
	tenantA := mustTenantScope(t, "tenant-a")
	tenantB := mustTenantScope(t, "tenant-b")
	const id = "both-write-own-read"

	require.NoError(t, store.WriteState(ctx, tenantA, stateRecord(t, id, 1, 111), persistence.Unconditional()))
	require.NoError(t, store.WriteState(ctx, tenantB, stateRecord(t, id, 1, 222), persistence.Unconditional()))

	gotA, err := store.GetLatestState(ctx, tenantA, id)
	require.NoError(t, err)
	require.NotNil(t, gotA)
	require.Equal(t, float64(111), stateMarker(t, gotA), "tenant A must read back its own record, never tenant B's")

	gotB, err := store.GetLatestState(ctx, tenantB, id)
	require.NoError(t, err)
	require.NotNil(t, gotB)
	require.Equal(t, float64(222), stateMarker(t, gotB), "tenant B must read back its own record, never tenant A's")
}

func stateOtherTenantWriteLeavesRecordUntouched(t require.TestingT, ctx context.Context, store persistence.StateStore) {
	tenantA := mustTenantScope(t, "tenant-a")
	tenantB := mustTenantScope(t, "tenant-b")
	const id = "write-isolation"

	require.NoError(t, store.WriteState(ctx, tenantA, stateRecord(t, id, 1, 111), persistence.Unconditional()))
	before, err := store.GetLatestState(ctx, tenantA, id)
	require.NoError(t, err)
	require.NotNil(t, before)

	require.NoError(t, store.WriteState(ctx, tenantB, stateRecord(t, id, 1, 999), persistence.Unconditional()))

	after, err := store.GetLatestState(ctx, tenantA, id)
	require.NoError(t, err)
	require.NotNil(t, after)
	require.True(t, proto.Equal(before, after), "tenant B's write must not modify tenant A's record for the same persistence_id")
}

// stateExpectGenesisSucceedsForNewTenant mirrors
// eventsExpectGenesisSucceedsForNewTenant: it is the sharpest check for
// StateStore too, for the identical reason — see that function's comment.
func stateExpectGenesisSucceedsForNewTenant(t require.TestingT, ctx context.Context, store persistence.StateStore) {
	tenantA := mustTenantScope(t, "tenant-a")
	tenantB := mustTenantScope(t, "tenant-b")
	const id = "genesis-cross-tenant"

	require.NoError(t, store.WriteState(ctx, tenantA, stateRecord(t, id, 1, 111), persistence.ExpectGenesis()))

	err := store.WriteState(ctx, tenantB, stateRecord(t, id, 1, 222), persistence.ExpectGenesis())
	require.NoError(t, err, "ExpectGenesis() must succeed for tenant B: tenant A having already established this id must never leak into tenant B's own CAS state")

	gotB, err := store.GetLatestState(ctx, tenantB, id)
	require.NoError(t, err)
	require.NotNil(t, gotB)
	require.Equal(t, float64(222), stateMarker(t, gotB))
}

func stateExpectRevisionConflictCarriesScope(t require.TestingT, ctx context.Context, store persistence.StateStore) {
	tenantA := mustTenantScope(t, "tenant-a")
	const id = "conflict-carries-scope"

	require.NoError(t, store.WriteState(ctx, tenantA, stateRecord(t, id, 1, 111), persistence.ExpectGenesis()))

	err := store.WriteState(ctx, tenantA, stateRecord(t, id, 2, 222), persistence.ExpectRevision(999))
	require.Error(t, err)
	require.ErrorIs(t, err, persistence.ErrConcurrencyConflict)

	var conflictErr *persistence.ConflictError
	require.True(t, errors.As(err, &conflictErr), "the returned error must be a *persistence.ConflictError")
	require.True(t, conflictErr.Scope().Equal(tenantA), "the conflict must carry the scope the failed write actually targeted")
}

func stateConflictNotObservableInAnotherScope(t require.TestingT, ctx context.Context, store persistence.StateStore) {
	tenantA := mustTenantScope(t, "tenant-a")
	tenantB := mustTenantScope(t, "tenant-b")
	const id = "conflict-not-cross-scope"

	require.NoError(t, store.WriteState(ctx, tenantA, stateRecord(t, id, 1, 111), persistence.ExpectGenesis()))
	err := store.WriteState(ctx, tenantA, stateRecord(t, id, 1, 999), persistence.ExpectGenesis())
	require.Error(t, err)
	require.ErrorIs(t, err, persistence.ErrConcurrencyConflict)

	err = store.WriteState(ctx, tenantB, stateRecord(t, id, 1, 222), persistence.ExpectGenesis())
	require.NoError(t, err, "a conflict raised in tenant A's scope must not be observable in tenant B's scope")
}

func stateUnscopedNeverCollidesWithForgedTenant(t require.TestingT, ctx context.Context, store persistence.StateStore) {
	forgedTenant := mustTenantScope(t, "unscoped")
	const id = "forging-guard"

	require.NoError(t, store.WriteState(ctx, persistence.Unscoped(), stateRecord(t, id, 1, 111), persistence.ExpectGenesis()))
	err := store.WriteState(ctx, forgedTenant, stateRecord(t, id, 1, 222), persistence.ExpectGenesis())
	require.NoError(t, err, `a tenant literally named "unscoped" must never forge Unscoped()'s own CAS state`)

	gotUnscoped, err := store.GetLatestState(ctx, persistence.Unscoped(), id)
	require.NoError(t, err)
	require.NotNil(t, gotUnscoped)
	require.Equal(t, float64(111), stateMarker(t, gotUnscoped))

	gotForged, err := store.GetLatestState(ctx, forgedTenant, id)
	require.NoError(t, err)
	require.NotNil(t, gotForged)
	require.Equal(t, float64(222), stateMarker(t, gotForged))
}
