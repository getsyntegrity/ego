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
	"fmt"
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
	_, err = setupPool.Exec(ctx, "TRUNCATE TABLE events_store, events_store_revisions")
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

// TestPostgresEventStore_UnconditionalWriteCannotBreakExpectRevision races an
// ExpectRevision(1) writer against an unconditional writer that targets the
// same (scope, persistenceID) and the same sequence number. Both writers must
// take the same per-key lock, so exactly one of two orders is observable:
//
//   - the conditional writer commits first, and the unconditional insert of
//     sequence 2 is a no-op (its primary key already exists), or
//   - the unconditional writer commits first, the revision becomes 2, and the
//     conditional writer fails with a typed conflict carrying revision 2.
//
// Without a shared lock the unconditional commit can land between the
// conditional writer's revision read and its insert, which surfaces as a
// primary-key error instead of a conflict. The race is repeated to make that
// window observable.
func TestPostgresEventStore_UnconditionalWriteCannotBreakExpectRevision(t *testing.T) {
	dsn := postgresTestDSN(t)
	store := newPostgresTestStore(t, dsn)
	ctx := context.Background()
	require.NoError(t, store.Connect(ctx))
	defer store.Disconnect(ctx)

	scope := persistence.Unscoped()
	const (
		iterations        = 200
		conditionalMark   = 111
		unconditionalMark = 222
	)
	for i := range iterations {
		persistenceID := fmt.Sprintf("pg-mixed-race-%d", i)
		require.NoError(t, store.WriteEvents(ctx, scope, pgMarkedEvent(t, persistenceID, 1, 0), persistence.ExpectGenesis()))

		errConditional, errUnconditional := raceTwoWriters(
			func() error {
				return store.WriteEvents(ctx, scope, pgMarkedEvent(t, persistenceID, 2, conditionalMark), persistence.ExpectRevision(1))
			},
			func() error {
				return store.WriteEvents(ctx, scope, pgMarkedEvent(t, persistenceID, 2, unconditionalMark), persistence.Unconditional())
			},
		)
		require.NoError(t, errUnconditional, "iteration %d: the unconditional write must always succeed", i)

		latest, err := store.GetLatestEvent(ctx, scope, persistenceID)
		require.NoError(t, err)
		require.NotNil(t, latest)
		require.EqualValues(t, 2, latest.GetSequenceNumber())

		if errConditional == nil {
			require.EqualValues(t, conditionalMark, pgEventMarker(t, latest),
				"iteration %d: a successful ExpectRevision(1) write must own sequence 2", i)
			continue
		}
		var conflictErr *persistence.ConflictError
		require.True(t, errors.As(errConditional, &conflictErr),
			"iteration %d: the conditional write must fail only with a typed conflict, got %v", i, errConditional)
		actual, ok := conflictErr.ActualRevision()
		require.True(t, ok)
		require.EqualValues(t, 2, actual)
		require.EqualValues(t, unconditionalMark, pgEventMarker(t, latest))
	}
}

// TestPostgresEventStore_UnconditionalMixedBatchesDoNotDeadlock runs two
// unconditional batches that touch the same two persistence ids in opposite
// orders, many times concurrently. Locks are acquired in a deterministic
// order, so neither batch can wait on the other in a cycle; PostgreSQL would
// otherwise abort one of them with a deadlock error.
func TestPostgresEventStore_UnconditionalMixedBatchesDoNotDeadlock(t *testing.T) {
	dsn := postgresTestDSN(t)
	store := newPostgresTestStore(t, dsn)
	ctx := context.Background()
	require.NoError(t, store.Connect(ctx))
	defer store.Disconnect(ctx)

	scope := persistence.Unscoped()
	for i := range 100 {
		seq := uint64(i + 1)
		forward := append(pgMarkedEvent(t, "pg-batch-a", seq, 1), pgMarkedEvent(t, "pg-batch-b", seq, 1)...)
		backward := append(pgMarkedEvent(t, "pg-batch-b", seq, 2), pgMarkedEvent(t, "pg-batch-a", seq, 2)...)
		errA, errB := raceTwoWriters(
			func() error { return store.WriteEvents(ctx, scope, forward, persistence.Unconditional()) },
			func() error { return store.WriteEvents(ctx, scope, backward, persistence.Unconditional()) },
		)
		require.NoError(t, errA, "iteration %d", i)
		require.NoError(t, errB, "iteration %d", i)
	}

	for _, persistenceID := range []string{"pg-batch-a", "pg-batch-b"} {
		err := store.WriteEvents(ctx, scope, pgMarkedEvent(t, persistenceID, 101, 0), persistence.ExpectRevision(100))
		require.NoError(t, err, "unconditional batches must advance %s's revision to 100", persistenceID)
	}
}

// writeRevisions commits events 1..n for persistenceID, one ExpectRevision
// step at a time, so the test also exercises the revision each write leaves.
func writeRevisions(t *testing.T, store *PostgresEventStore, scope persistence.Scope, persistenceID string, n uint64) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, store.WriteEvents(ctx, scope, pgMarkedEvent(t, persistenceID, 1, 1), persistence.ExpectGenesis()))
	for seq := uint64(2); seq <= n; seq++ {
		require.NoError(t, store.WriteEvents(ctx, scope, pgMarkedEvent(t, persistenceID, seq, float64(seq)), persistence.ExpectRevision(seq-1)))
	}
}

// requireConflictAt asserts err is a typed conflict that observed revision.
func requireConflictAt(t *testing.T, err error, revision uint64) {
	t.Helper()
	var conflictErr *persistence.ConflictError
	require.True(t, errors.As(err, &conflictErr), "expected a *persistence.ConflictError, got %v", err)
	actual, ok := conflictErr.ActualRevision()
	require.True(t, ok)
	require.Equal(t, revision, actual)
}

// TestPostgresEventStore_PartialDeleteKeepsRevision deletes the oldest events
// and proves the revision a conditional write compares against is still the
// highest committed sequence number.
func TestPostgresEventStore_PartialDeleteKeepsRevision(t *testing.T) {
	dsn := postgresTestDSN(t)
	store := newPostgresTestStore(t, dsn)
	ctx := context.Background()
	require.NoError(t, store.Connect(ctx))
	defer store.Disconnect(ctx)

	scope := persistence.Unscoped()
	const persistenceID = "pg-partial-delete"
	writeRevisions(t, store, scope, persistenceID, 5)

	require.NoError(t, store.DeleteEvents(ctx, scope, persistenceID, 3))

	replayed, err := store.ReplayEvents(ctx, scope, persistenceID, 1, 5, 10)
	require.NoError(t, err)
	require.Len(t, replayed, 2, "events 1..3 must no longer be replayable")

	requireConflictAt(t, store.WriteEvents(ctx, scope, pgMarkedEvent(t, persistenceID, 4, 0), persistence.ExpectRevision(3)), 5)
	requireConflictAt(t, store.WriteEvents(ctx, scope, pgMarkedEvent(t, persistenceID, 1, 0), persistence.ExpectGenesis()), 5)
	require.NoError(t, store.WriteEvents(ctx, scope, pgMarkedEvent(t, persistenceID, 6, 6), persistence.ExpectRevision(5)))
}

// TestPostgresEventStore_TotalDeleteKeepsRevision deletes every event of a
// persistence id. ExpectGenesis() must not become valid again and a stale
// revision must not be accepted: the revision stays at the highest sequence
// number ever committed.
func TestPostgresEventStore_TotalDeleteKeepsRevision(t *testing.T) {
	dsn := postgresTestDSN(t)
	store := newPostgresTestStore(t, dsn)
	ctx := context.Background()
	require.NoError(t, store.Connect(ctx))
	defer store.Disconnect(ctx)

	scope := persistence.Unscoped()
	const persistenceID = "pg-total-delete"
	writeRevisions(t, store, scope, persistenceID, 5)

	require.NoError(t, store.DeleteEvents(ctx, scope, persistenceID, 5))

	latest, err := store.GetLatestEvent(ctx, scope, persistenceID)
	require.NoError(t, err)
	require.Nil(t, latest, "every event must be deleted")

	requireConflictAt(t, store.WriteEvents(ctx, scope, pgMarkedEvent(t, persistenceID, 1, 0), persistence.ExpectGenesis()), 5)
	requireConflictAt(t, store.WriteEvents(ctx, scope, pgMarkedEvent(t, persistenceID, 3, 0), persistence.ExpectRevision(2)), 5)
	require.NoError(t, store.WriteEvents(ctx, scope, pgMarkedEvent(t, persistenceID, 6, 6), persistence.ExpectRevision(5)))

	// An unconditional write of an older sequence number must not move the
	// revision backwards either.
	require.NoError(t, store.WriteEvents(ctx, scope, pgMarkedEvent(t, persistenceID, 2, 2), persistence.Unconditional()))
	requireConflictAt(t, store.WriteEvents(ctx, scope, pgMarkedEvent(t, persistenceID, 3, 0), persistence.ExpectRevision(2)), 6)
}

// TestPostgresEventStore_DeleteKeepsRevisionPerTenant proves the revision
// marker is scoped: deleting a tenant's events keeps that tenant's revision
// and leaves another tenant's same persistence id untouched.
func TestPostgresEventStore_DeleteKeepsRevisionPerTenant(t *testing.T) {
	dsn := postgresTestDSN(t)
	store := newPostgresTestStore(t, dsn)
	ctx := context.Background()
	require.NoError(t, store.Connect(ctx))
	defer store.Disconnect(ctx)

	tenantA, err := persistence.NewTenantScope("tenant-a")
	require.NoError(t, err)
	tenantB, err := persistence.NewTenantScope("tenant-b")
	require.NoError(t, err)
	const persistenceID = "pg-tenant-delete"
	writeRevisions(t, store, tenantA, persistenceID, 3)
	writeRevisions(t, store, tenantB, persistenceID, 2)

	require.NoError(t, store.DeleteEvents(ctx, tenantA, persistenceID, 3))

	requireConflictAt(t, store.WriteEvents(ctx, tenantA, pgMarkedEvent(t, persistenceID, 1, 0), persistence.ExpectGenesis()), 3)
	require.NoError(t, store.WriteEvents(ctx, tenantB, pgMarkedEvent(t, persistenceID, 3, 3), persistence.ExpectRevision(2)))
	requireConflictAt(t, store.WriteEvents(ctx, tenantA, pgMarkedEvent(t, persistenceID, 4, 0), persistence.ExpectRevision(2)), 3)
}

// legacyEventsStoreDDL is events_store as it existed before
// events_store_revisions: the revision was derived from MAX(sequence_number).
const legacyEventsStoreDDL = `
CREATE TABLE events_store
(
    tenant_id         VARCHAR(255) DEFAULT '' NOT NULL,
    persistence_id    VARCHAR(255)          NOT NULL,
    sequence_number   BIGINT                NOT NULL,
    is_deleted        BOOLEAN DEFAULT FALSE NOT NULL,
    event_payload     BYTEA                 NOT NULL,
    event_manifest    VARCHAR(255)          NOT NULL,
    timestamp         BIGINT                NOT NULL,
    shard_number      BIGINT                NOT NULL,
    encryption_key_id VARCHAR(255) DEFAULT '' NOT NULL,
    is_encrypted      BOOLEAN DEFAULT FALSE NOT NULL,
    PRIMARY KEY (tenant_id, persistence_id, sequence_number)
);
`

// TestPostgresEventStore_SchemaMigratesLegacyDatabase starts from a database
// that has events but no revision table, applies eventsStoreSchemaDDL (the
// same idempotent script the k8s init and README use), and proves each
// existing (tenant_id, persistence_id) gets its revision backfilled from its
// highest retained sequence number. Applying the script twice is a no-op.
func TestPostgresEventStore_SchemaMigratesLegacyDatabase(t *testing.T) {
	dsn := postgresTestDSN(t)
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	_, err = pool.Exec(ctx, `DROP TABLE IF EXISTS events_store_revisions; DROP TABLE IF EXISTS events_store;`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, legacyEventsStoreDDL)
	require.NoError(t, err)
	for _, row := range []struct {
		tenantID string
		seq      int
	}{{"", 1}, {"", 2}, {"", 3}, {"tenant-a", 1}} {
		_, err = pool.Exec(ctx, `
			INSERT INTO events_store (tenant_id, persistence_id, sequence_number, event_payload, event_manifest, timestamp, shard_number)
			VALUES ($1, 'pg-legacy', $2, ''::bytea, '', 0, 1)`, row.tenantID, row.seq)
		require.NoError(t, err)
	}

	_, err = pool.Exec(ctx, eventsStoreSchemaDDL)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, eventsStoreSchemaDDL)
	require.NoError(t, err, "the schema script must be idempotent")

	store := NewPostgresEventStore(dsn)
	require.NoError(t, store.Connect(ctx))
	defer store.Disconnect(ctx)

	tenantA, err := persistence.NewTenantScope("tenant-a")
	require.NoError(t, err)

	requireConflictAt(t, store.WriteEvents(ctx, persistence.Unscoped(), pgMarkedEvent(t, "pg-legacy", 1, 0), persistence.ExpectGenesis()), 3)
	require.NoError(t, store.WriteEvents(ctx, persistence.Unscoped(), pgMarkedEvent(t, "pg-legacy", 4, 4), persistence.ExpectRevision(3)))
	requireConflictAt(t, store.WriteEvents(ctx, tenantA, pgMarkedEvent(t, "pg-legacy", 1, 0), persistence.ExpectGenesis()), 1)
	require.NoError(t, store.WriteEvents(ctx, tenantA, pgMarkedEvent(t, "pg-legacy", 2, 2), persistence.ExpectRevision(1)))
}
