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

// These tests exercise PostgresEventStore against a real PostgreSQL instance.
// They are gated by EGO_EXAMPLE_POSTGRES_DSN and t.Skip when it is unset, so
// `go test ./...` stays green with no database available (e.g. plain CI).
//
// Run a local Postgres and export the DSN before running these tests:
//
//	docker run -d --rm --name ego115-pg -e POSTGRES_PASSWORD=pg -p 55432:5432 postgres:16-alpine
//	export EGO_EXAMPLE_POSTGRES_DSN="postgres://postgres:pg@localhost:55432/postgres?sslmode=disable"
//	go test -count=1 -v ./...
package main

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/persistence"
	"github.com/pablogore/ego/v4/persistence/conformance"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
)

// postgresTestDSN returns the DSN configured for these tests, skipping the
// calling test when it is unset.
func postgresTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("EGO_EXAMPLE_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("EGO_EXAMPLE_POSTGRES_DSN not set, skipping Postgres-backed test")
	}
	return dsn
}

// newPostgresTestStore provisions the events_store schema (creating it if
// missing) and truncates it, so every call returns a store backed by a
// fresh, empty table — the contract persistence/conformance.RunEventsStoreConformance
// requires of newStore. The returned store is not yet Connect()-ed; the
// conformance runner and the tests below do that themselves.
func newPostgresTestStore(t *testing.T, dsn string) *PostgresEventStore {
	t.Helper()
	ctx := context.Background()

	setupPool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	defer setupPool.Close()

	_, err = setupPool.Exec(ctx, eventsStoreSchemaDDL)
	require.NoError(t, err)
	_, err = setupPool.Exec(ctx, "TRUNCATE TABLE events_store")
	require.NoError(t, err)

	return NewPostgresEventStore(dsn)
}

func TestPostgresEventStore_Conformance(t *testing.T) {
	dsn := postgresTestDSN(t)
	conformance.RunEventsStoreConformance(t, func(t *testing.T) persistence.EventsStore {
		return newPostgresTestStore(t, dsn)
	})
}

// pgMarkedEvent builds a single-event batch carrying marker in its payload,
// so the winner of a race between two writers can be identified afterward
// from the persisted event alone. Modeled on testkit/concurrency_test.go's
// markedEvent.
func pgMarkedEvent(t *testing.T, persistenceID string, sequenceNumber uint64, marker float64) []*egopb.Event {
	t.Helper()
	payload, err := anypb.New(&testpb.AccountCreated{AccountId: persistenceID, AccountBalance: marker})
	require.NoError(t, err)
	return []*egopb.Event{
		{PersistenceId: persistenceID, SequenceNumber: sequenceNumber, Event: payload, Timestamp: time.Now().UnixMilli(), Shard: 1},
	}
}

// pgEventMarker recovers the marker written by pgMarkedEvent.
func pgEventMarker(t *testing.T, event *egopb.Event) float64 {
	t.Helper()
	var msg testpb.AccountCreated
	require.NoError(t, event.GetEvent().UnmarshalTo(&msg))
	return msg.GetAccountBalance()
}

// raceTwoWriters runs a and b as independent goroutines released
// simultaneously by a closed start channel, so the two calls genuinely
// contend against the store rather than against the test goroutine's own
// ordering. Modeled on testkit/concurrency_test.go's raceTwoWriters.
func raceTwoWriters(a, b func() error) (errA, errB error) {
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		<-start
		errA = a()
	}()
	go func() {
		defer wg.Done()
		<-start
		errB = b()
	}()

	close(start)
	wg.Wait()
	return errA, errB
}

// countOutcomes classifies two conditional-write results into (successes,
// conflicts), failing the test if either error is a non-conflict failure.
func countOutcomes(t *testing.T, errA, errB error) (successes, conflicts int) {
	t.Helper()
	for _, err := range []error{errA, errB} {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, persistence.ErrConcurrencyConflict):
			conflicts++
		default:
			t.Fatalf("unexpected non-conflict error: %v", err)
		}
	}
	return successes, conflicts
}

// TestPostgresEventStore_ConcurrentExpectRevisionHasExactlyOneWinner mirrors
// testkit/concurrency_test.go's T8: two independent writers race
// ExpectRevision(1) against the same persistence id, driving the store
// directly (no actor, no mailbox), and exactly one of them must commit.
func TestPostgresEventStore_ConcurrentExpectRevisionHasExactlyOneWinner(t *testing.T) {
	dsn := postgresTestDSN(t)
	store := newPostgresTestStore(t, dsn)
	ctx := context.Background()
	require.NoError(t, store.Connect(ctx))
	defer store.Disconnect(ctx)

	scope := persistence.Unscoped()
	const persistenceID = "pg-t8-event-race"
	require.NoError(t, store.WriteEvents(ctx, scope, pgMarkedEvent(t, persistenceID, 1, 0), persistence.ExpectGenesis()))

	errA, errB := raceTwoWriters(
		func() error {
			return store.WriteEvents(ctx, scope, pgMarkedEvent(t, persistenceID, 2, 111), persistence.ExpectRevision(1))
		},
		func() error {
			return store.WriteEvents(ctx, scope, pgMarkedEvent(t, persistenceID, 2, 222), persistence.ExpectRevision(1))
		},
	)

	successes, conflicts := countOutcomes(t, errA, errB)
	assert.Equal(t, 1, successes, "exactly one of the two racing writers must commit")
	assert.Equal(t, 1, conflicts, "the losing writer must observe a typed concurrency conflict")

	latest, err := store.GetLatestEvent(ctx, scope, persistenceID)
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.EqualValues(t, 2, latest.GetSequenceNumber())

	winnerMarker := pgEventMarker(t, latest)
	if errA == nil {
		assert.EqualValues(t, 111, winnerMarker, "final revision must reflect writer A, the one that actually succeeded")
	} else {
		assert.EqualValues(t, 222, winnerMarker, "final revision must reflect writer B, the one that actually succeeded")
	}

	// Only the winner's row is persisted: a losing conditional writer must
	// never have inserted its own row before observing the conflict.
	var count int
	require.NoError(t, store.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM events_store WHERE tenant_id=$1 AND persistence_id=$2 AND sequence_number=$3`,
		"", persistenceID, 2,
	).Scan(&count))
	assert.Equal(t, 1, count, "exactly one row must exist at the contested sequence number")
}

// TestPostgresEventStore_ExpectGenesisConflictsOnExistingID proves
// ExpectGenesis() against an already-established persistence id fails
// closed with a *persistence.ConflictError carrying the actual revision,
// without touching the existing row.
func TestPostgresEventStore_ExpectGenesisConflictsOnExistingID(t *testing.T) {
	dsn := postgresTestDSN(t)
	store := newPostgresTestStore(t, dsn)
	ctx := context.Background()
	require.NoError(t, store.Connect(ctx))
	defer store.Disconnect(ctx)

	scope := persistence.Unscoped()
	const persistenceID = "pg-genesis-conflict"
	require.NoError(t, store.WriteEvents(ctx, scope, pgMarkedEvent(t, persistenceID, 1, 111), persistence.ExpectGenesis()))

	err := store.WriteEvents(ctx, scope, pgMarkedEvent(t, persistenceID, 1, 999), persistence.ExpectGenesis())
	require.Error(t, err)
	require.ErrorIs(t, err, persistence.ErrConcurrencyConflict)

	var conflictErr *persistence.ConflictError
	require.True(t, errors.As(err, &conflictErr))
	actual, ok := conflictErr.ActualRevision()
	require.True(t, ok)
	assert.EqualValues(t, 1, actual)

	latest, err := store.GetLatestEvent(ctx, scope, persistenceID)
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.EqualValues(t, 111, pgEventMarker(t, latest), "the existing record must be untouched by the failed genesis write")
}

// TestPostgresEventStore_TenantScopeIsolatesRecords proves two different
// tenant scopes writing the same persistence_id keep independent
// StorageRevision and never observe each other's records, directly against
// Postgres (persistence/conformance's own matrix already covers this in
// depth; this test pins it once more at the SQL layer specifically).
func TestPostgresEventStore_TenantScopeIsolatesRecords(t *testing.T) {
	dsn := postgresTestDSN(t)
	store := newPostgresTestStore(t, dsn)
	ctx := context.Background()
	require.NoError(t, store.Connect(ctx))
	defer store.Disconnect(ctx)

	tenantA, err := persistence.NewTenantScope("tenant-a")
	require.NoError(t, err)
	tenantB, err := persistence.NewTenantScope("tenant-b")
	require.NoError(t, err)
	const persistenceID = "pg-tenant-isolation"

	require.NoError(t, store.WriteEvents(ctx, tenantA, pgMarkedEvent(t, persistenceID, 1, 111), persistence.ExpectGenesis()))
	// Tenant B must be able to genesis the exact same persistence_id: its CAS
	// state is entirely independent of tenant A's.
	require.NoError(t, store.WriteEvents(ctx, tenantB, pgMarkedEvent(t, persistenceID, 1, 222), persistence.ExpectGenesis()))

	gotA, err := store.GetLatestEvent(ctx, tenantA, persistenceID)
	require.NoError(t, err)
	require.NotNil(t, gotA)
	assert.EqualValues(t, 111, pgEventMarker(t, gotA))

	gotB, err := store.GetLatestEvent(ctx, tenantB, persistenceID)
	require.NoError(t, err)
	require.NotNil(t, gotB)
	assert.EqualValues(t, 222, pgEventMarker(t, gotB))
}
