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

package postgres_test

import (
	"context"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/getsyntegrity/ego/persistence"
	"github.com/getsyntegrity/ego/persistence/postgres"
)

// postgresLatestSchemaVersion is the version of the newest file in persistence/postgres/schema. Adding a file
// means raising it here.
const postgresLatestSchemaVersion = 5

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
	t.Parallel()
	dsn := shared.NewDatabase(t)
	specs.Describe(t, "PostgresEventStore against a real database", func(s *specs.Spec) {
		s.It("schema migrates legacy database", func(sc *specs.Context) {
			ctx := context.Background()

			pool, err := pgxpool.New(ctx, dsn)
			sc.Expect(err).To(specs.BeNil())
			defer pool.Close()

			_, err = pool.Exec(ctx, legacyEventsStoreDDL)
			sc.Expect(err).To(specs.BeNil())
			for _, row := range []struct {
				tenantID string
				seq      int
			}{{"", 1}, {"", 2}, {"", 3}, {"tenant-a", 1}} {
				_, err = pool.Exec(ctx, `
					INSERT INTO events_store (tenant_id, persistence_id, sequence_number, event_payload, event_manifest, timestamp, shard_number)
					VALUES ($1, 'pg-legacy', $2, ''::bytea, '', 0, 1)`, row.tenantID, row.seq)
				sc.Expect(err).To(specs.BeNil())
			}

			store := postgres.NewEventStore(dsn)
			sc.Expect(store.Connect(ctx)).To(specs.BeNil())
			defer store.Disconnect(ctx)
			sc.Expect(store.Migrate(ctx)).To(specs.BeNil())
			sc.Expect(store.Migrate(ctx)).To(specs.BeNil()) // Migrate must be idempotent
			version, err := store.SchemaVersion(ctx)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(version).To(specs.Equal(uint(postgresLatestSchemaVersion)))

			tenantA, err := persistence.NewTenantScope("tenant-a")
			sc.Expect(err).To(specs.BeNil())

			requireConflictAt(sc, store.WriteEvents(ctx, persistence.Unscoped(), pgMarkedEvent("pg-legacy", 1, 0), persistence.ExpectGenesis()), 3)
			sc.Expect(store.WriteEvents(ctx, persistence.Unscoped(), pgMarkedEvent("pg-legacy", 4, 4), persistence.ExpectRevision(3))).To(specs.BeNil())
			requireConflictAt(sc, store.WriteEvents(ctx, tenantA, pgMarkedEvent("pg-legacy", 1, 0), persistence.ExpectGenesis()), 1)
			sc.Expect(store.WriteEvents(ctx, tenantA, pgMarkedEvent("pg-legacy", 2, 2), persistence.ExpectRevision(1))).To(specs.BeNil())
		})
	})
}

// legacyEventsStoreDDLBeforeTenantMetadata is events_store and
// events_store_revisions as they existed right after #115's scoped-store
// migration but before tenant_metadata existed: everything
// eventsStoreSchemaDDL creates today except that column.
const legacyEventsStoreDDLBeforeTenantMetadata = `
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
CREATE TABLE events_store_revisions
(
    tenant_id      VARCHAR(255) DEFAULT '' NOT NULL,
    persistence_id VARCHAR(255)            NOT NULL,
    revision       BIGINT                  NOT NULL,
    PRIMARY KEY (tenant_id, persistence_id)
);
`

// TestPostgresEventStore_SchemaMigratesLegacyTenantMetadata starts from a
// database that has events_store and events_store_revisions but no
// tenant_metadata column, inserts an event row the old way with plain SQL
// (no such column to write to), applies eventsStoreSchemaDDL (the same
// idempotent script the k8s init and README use), and proves the column is
// added with NO backfill: the pre-existing row's tenant_metadata is NULL —
// never an invented tenant identity — and the row is otherwise unchanged.
// Applying the script twice is a no-op (#115 Codex P2).
func TestPostgresEventStore_SchemaMigratesLegacyTenantMetadata(t *testing.T) {
	t.Parallel()
	dsn := shared.NewDatabase(t)
	specs.Describe(t, "PostgresEventStore against a real database", func(s *specs.Spec) {
		s.It("schema migrates legacy tenant metadata", func(sc *specs.Context) {
			ctx := context.Background()

			pool, err := pgxpool.New(ctx, dsn)
			sc.Expect(err).To(specs.BeNil())
			defer pool.Close()

			_, err = pool.Exec(ctx, legacyEventsStoreDDLBeforeTenantMetadata)
			sc.Expect(err).To(specs.BeNil())

			const persistenceID = "pg-legacy-tenant-metadata"
			_, err = pool.Exec(ctx, `
				INSERT INTO events_store (tenant_id, persistence_id, sequence_number, event_payload, event_manifest, timestamp, shard_number)
				VALUES ('', $1, 1, ''::bytea, '', 1000, 1)`, persistenceID)
			sc.Expect(err).To(specs.BeNil())

			store := postgres.NewEventStore(dsn)
			sc.Expect(store.Connect(ctx)).To(specs.BeNil())
			defer store.Disconnect(ctx)
			sc.Expect(store.Migrate(ctx)).To(specs.BeNil())
			sc.Expect(store.Migrate(ctx)).To(specs.BeNil()) // Migrate must be idempotent
			version, err := store.SchemaVersion(ctx)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(version).To(specs.Equal(uint(postgresLatestSchemaVersion)))

			var tenantMetadataIsNull bool
			sc.Expect(pool.QueryRow(ctx,
				`SELECT tenant_metadata IS NULL FROM events_store WHERE tenant_id='' AND persistence_id=$1 AND sequence_number=1`,
				persistenceID,
			).Scan(&tenantMetadataIsNull)).To(specs.BeNil())
			sc.Expect(tenantMetadataIsNull).To(specs.BeTrue()) // the migration must not backfill or invent tenant metadata for a pre-existing row

			latest, err := store.GetLatestEvent(ctx, persistence.Unscoped(), persistenceID)
			sc.Expect(err).To(specs.BeNil())
			sc.Expect(latest).To(specs.Not(specs.BeNil()))
			sc.Expect(latest.GetTenantMetadata()).To(specs.BeEmpty())     // a pre-existing row must read back with no tenant metadata, never an invented identity
			sc.Expect(latest.GetTimestamp()).To(specs.Equal(int64(1000))) // the legacy row's other columns must be unaffected by the migration
		})
	})
}
