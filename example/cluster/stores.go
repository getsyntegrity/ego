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

package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/offsetstore"
	"github.com/pablogore/ego/v4/persistence"
)

// eventsStoreSchemaDDL is the CREATE TABLE statement for events_store, kept
// in sync by hand with the ConfigMap init.sql in k8s/postgres.yaml (the
// deployment source of truth). It is used by the Postgres-backed tests
// (stores_postgres_test.go) to provision a fresh schema without depending on
// a running cluster's init container.
//
// tenant_id defaults to ” (the empty string), which scopeKey encodes as
// persistence.Unscoped(): a valid tenancy.TenantID is never empty
// (tenancy.NewTenantID rejects ""), so ” unambiguously means "no tenant".
// Existing rows from before this column existed read back as ”, so an
// existing non-tenant deployment needs no data rewrite — see README.md's
// migration note for the ALTER TABLE recipe on an existing database.
//
// events_store_revisions holds one row per (tenant_id, persistence_id): the
// StorageRevision, i.e. the highest sequence number ever committed for that
// record. It is separate from events_store because DeleteEvents removes
// replayable events for retention but must never lower the revision, or
// ExpectGenesis() would succeed again and a stale ExpectRevision would win.
// The row is also the per-record lock every write takes (see WriteEvents).
//
// The script is idempotent and doubles as the migration for a database
// created before events_store_revisions existed: the final INSERT backfills
// each record's revision from its highest retained sequence number, and never
// lowers a revision that is already stored.
const eventsStoreSchemaDDL = `
CREATE TABLE IF NOT EXISTS events_store
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
CREATE INDEX IF NOT EXISTS idx_events_store_persistence_id ON events_store(tenant_id, persistence_id);
CREATE INDEX IF NOT EXISTS idx_events_store_seqnumber ON events_store(sequence_number);
CREATE INDEX IF NOT EXISTS idx_events_store_timestamp ON events_store(timestamp);
CREATE INDEX IF NOT EXISTS idx_events_store_shard ON events_store(shard_number);

CREATE TABLE IF NOT EXISTS events_store_revisions
(
    tenant_id      VARCHAR(255) DEFAULT '' NOT NULL,
    persistence_id VARCHAR(255)            NOT NULL,
    revision       BIGINT                  NOT NULL,
    PRIMARY KEY (tenant_id, persistence_id)
);
INSERT INTO events_store_revisions (tenant_id, persistence_id, revision)
SELECT tenant_id, persistence_id, MAX(sequence_number)
FROM events_store
GROUP BY tenant_id, persistence_id
ON CONFLICT (tenant_id, persistence_id)
DO UPDATE SET revision = GREATEST(events_store_revisions.revision, EXCLUDED.revision);
`

// scopeKey validates scope and returns the tenant_id column value it maps
// to: "" for persistence.Unscoped(), or the tenant's id for a tenant scope.
// It returns persistence.ErrInvalidScope for the invalid zero-value Scope,
// before any caller touches the database. Every record-addressing method
// keys structurally on scope.IsUnscoped()/scope.TenantID(), NEVER on
// scope.String() — see persistence.Scope's doc comment on why String() must
// never be used to build a storage key.
func scopeKey(scope persistence.Scope) (string, error) {
	if !scope.Valid() {
		return "", persistence.ErrInvalidScope
	}
	if scope.IsUnscoped() {
		return "", nil
	}
	return string(scope.TenantID()), nil
}

// PostgresEventStore implements persistence.EventsStore using PostgreSQL.
type PostgresEventStore struct {
	pool *pgxpool.Pool
	dsn  string
}

var _ persistence.EventsStore = (*PostgresEventStore)(nil)

// NewPostgresEventStore creates a new PostgreSQL-backed event store.
func NewPostgresEventStore(dsn string) *PostgresEventStore {
	return &PostgresEventStore{dsn: dsn}
}

func (s *PostgresEventStore) Connect(ctx context.Context) error {
	cfg, err := pgxpool.ParseConfig(s.dsn)
	if err != nil {
		return fmt.Errorf("event store config: %w", err)
	}
	cfg.MaxConns = 20
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("event store connect: %w", err)
	}
	s.pool = pool
	return nil
}

func (s *PostgresEventStore) Disconnect(_ context.Context) error {
	if s.pool != nil {
		s.pool.Close()
	}
	return nil
}

func (s *PostgresEventStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// WriteEvents implements persistence.EventsStore. See that interface's doc
// comment for the full contract. scope is validated first, then precondition
// (persistence.ErrInvalidScope / persistence.ErrInvalidPrecondition), before
// anything is read or written. For a conditional write (anything but
// Unconditional()), every event in the batch MUST share one PersistenceId,
// including that the batch must be non-empty, or persistence.ErrPreconditionScope
// is returned — mirroring testkit's in-memory EventStore.
func (s *PostgresEventStore) WriteEvents(ctx context.Context, scope persistence.Scope, events []*egopb.Event, precondition persistence.WritePrecondition) error {
	tenantID, err := scopeKey(scope)
	if err != nil {
		return err
	}

	if !precondition.Valid() {
		return persistence.ErrInvalidPrecondition
	}

	if precondition.IsUnconditional() {
		return s.writeUnconditional(ctx, tenantID, events)
	}

	if len(events) == 0 {
		return persistence.ErrPreconditionScope
	}
	persistenceID := events[0].GetPersistenceId()
	for _, event := range events[1:] {
		if event.GetPersistenceId() != persistenceID {
			return persistence.ErrPreconditionScope
		}
	}
	return s.writeConditional(ctx, scope, tenantID, persistenceID, events, precondition)
}

// insertEventSQL inserts one event row. A conditional write uses it as is, so
// a primary-key clash fails the transaction; an unconditional write appends
// insertEventIgnoreDuplicateSQL to keep its legacy "duplicates are ignored"
// behavior.
const (
	insertEventSQL = `
		INSERT INTO events_store
			(tenant_id, persistence_id, sequence_number, is_deleted, event_payload, event_manifest,
			 timestamp, shard_number, encryption_key_id, is_encrypted)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`
	insertEventIgnoreDuplicateSQL = insertEventSQL + `
		ON CONFLICT (tenant_id, persistence_id, sequence_number) DO NOTHING`
)

// insertEvent writes event under tenantID inside tx using query, which is
// insertEventSQL or insertEventIgnoreDuplicateSQL.
func insertEvent(ctx context.Context, tx pgx.Tx, query, tenantID string, event *egopb.Event) error {
	payload, err := proto.Marshal(event.GetEvent())
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	if _, err := tx.Exec(ctx, query,
		tenantID,
		event.GetPersistenceId(),
		event.GetSequenceNumber(),
		event.GetIsDeleted(),
		payload,
		event.GetEvent().GetTypeUrl(),
		event.GetTimestamp(),
		event.GetShard(),
		event.GetEncryptionKeyId(),
		event.GetIsEncrypted(),
	); err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

// writeUnconditional preserves legacy, precondition-free write semantics:
// every event commits, keyed by (tenantID, its own PersistenceId,
// SequenceNumber), and a primary-key clash is silently ignored.
//
// It still takes part in the per-record lock protocol shared with
// writeConditional. Before inserting any event, it upserts the
// events_store_revisions row of every persistence id in the batch, raising
// the revision to the batch's highest sequence number for that id (never
// lowering it). The upsert row-locks each revision row until commit, so an
// unconditional write can never commit between a conditional writer's
// revision check and its insert. The ids are locked in sorted order, so two
// batches that share ids always acquire their locks in the same order and
// cannot deadlock each other.
func (s *PostgresEventStore) writeUnconditional(ctx context.Context, tenantID string, events []*egopb.Event) error {
	if len(events) == 0 {
		return nil
	}

	highest := make(map[string]uint64)
	for _, event := range events {
		id := event.GetPersistenceId()
		if seq, seen := highest[id]; !seen || event.GetSequenceNumber() > seq {
			highest[id] = event.GetSequenceNumber()
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once Commit has succeeded

	for _, persistenceID := range slices.Sorted(maps.Keys(highest)) {
		if _, err := tx.Exec(ctx, `
			INSERT INTO events_store_revisions (tenant_id, persistence_id, revision)
			VALUES ($1, $2, $3)
			ON CONFLICT (tenant_id, persistence_id)
			DO UPDATE SET revision = GREATEST(events_store_revisions.revision, EXCLUDED.revision)`,
			tenantID, persistenceID, highest[persistenceID],
		); err != nil {
			return fmt.Errorf("advance revision: %w", err)
		}
	}

	for _, event := range events {
		if err := insertEvent(ctx, tx, insertEventIgnoreDuplicateSQL, tenantID, event); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// writeConditional evaluates precondition for (scope, persistenceID) and
// commits events as one atomic operation. It first locks the record's
// events_store_revisions row (lockRevision), which every writer of that
// record takes before touching events_store, so the revision check, the
// inserts and the revision update happen with no other writer's commit in
// between. The insert does NOT use ON CONFLICT DO NOTHING: a primary-key
// clash under the held lock means two callers disagree about the target
// sequence numbers, and that must fail the transaction rather than silently
// drop the write.
//
// The revision compared against is the stored StorageRevision, not
// MAX(sequence_number): DeleteEvents removes events for retention without
// touching the revision, so deleted events never reopen ExpectGenesis() or
// let a stale ExpectRevision win.
func (s *PostgresEventStore) writeConditional(ctx context.Context, scope persistence.Scope, tenantID, persistenceID string, events []*egopb.Event, precondition persistence.WritePrecondition) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once Commit has succeeded

	revision, exists, err := lockRevision(ctx, tx, tenantID, persistenceID)
	if err != nil {
		return err
	}

	if precondition.IsGenesis() {
		if exists {
			return persistence.NewConflictError(scope, persistenceID, precondition, persistence.WithActualRevision(revision))
		}
	} else {
		expectedRevision, _ := precondition.Revision()
		if !exists {
			return persistence.NewConflictError(scope, persistenceID, precondition)
		}
		if revision != expectedRevision {
			return persistence.NewConflictError(scope, persistenceID, precondition, persistence.WithActualRevision(revision))
		}
	}

	highest := revision
	for _, event := range events {
		if err := insertEvent(ctx, tx, insertEventSQL, tenantID, event); err != nil {
			return err
		}
		highest = max(highest, event.GetSequenceNumber())
	}

	if _, err := tx.Exec(ctx,
		`UPDATE events_store_revisions SET revision = $3 WHERE tenant_id = $1 AND persistence_id = $2`,
		tenantID, persistenceID, highest,
	); err != nil {
		return fmt.Errorf("advance revision: %w", err)
	}
	return tx.Commit(ctx)
}

// lockRevision row-locks (tenantID, persistenceID)'s events_store_revisions
// row for the rest of tx and returns its revision. exists is false when the
// record has never been committed; in that case lockRevision inserts a
// placeholder row, which holds the same lock (a concurrent writer's insert or
// upsert of that key waits on it) and disappears if tx rolls back. When the
// placeholder insert finds a row that another transaction committed in the
// meantime, the row is locked and read again.
func lockRevision(ctx context.Context, tx pgx.Tx, tenantID, persistenceID string) (revision uint64, exists bool, err error) {
	for {
		err := tx.QueryRow(ctx,
			`SELECT revision FROM events_store_revisions WHERE tenant_id = $1 AND persistence_id = $2 FOR UPDATE`,
			tenantID, persistenceID,
		).Scan(&revision)
		switch {
		case err == nil:
			return revision, true, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return 0, false, fmt.Errorf("lock revision: %w", err)
		}

		tag, err := tx.Exec(ctx, `
			INSERT INTO events_store_revisions (tenant_id, persistence_id, revision)
			VALUES ($1, $2, 0)
			ON CONFLICT (tenant_id, persistence_id) DO NOTHING`,
			tenantID, persistenceID,
		)
		if err != nil {
			return 0, false, fmt.Errorf("claim revision: %w", err)
		}
		if tag.RowsAffected() == 1 {
			return 0, false, nil
		}
	}
}

// DeleteEvents implements persistence.EventsStore. scope is validated before
// anything is touched, and this never affects a record in another scope.
// Only replayable events are removed: the record's events_store_revisions
// row is left as is, so its StorageRevision survives retention.
func (s *PostgresEventStore) DeleteEvents(ctx context.Context, scope persistence.Scope, persistenceID string, toSequenceNumber uint64) error {
	tenantID, err := scopeKey(scope)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`DELETE FROM events_store WHERE tenant_id=$1 AND persistence_id=$2 AND sequence_number<=$3`,
		tenantID, persistenceID, toSequenceNumber)
	return err
}

// ReplayEvents implements persistence.EventsStore. scope is validated before
// anything is read, and this never returns a record that belongs to another
// scope.
func (s *PostgresEventStore) ReplayEvents(ctx context.Context, scope persistence.Scope, persistenceID string, fromSequenceNumber, toSequenceNumber, limit uint64) ([]*egopb.Event, error) {
	tenantID, err := scopeKey(scope)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT persistence_id, sequence_number, is_deleted, event_payload, event_manifest,
		       timestamp, shard_number, encryption_key_id, is_encrypted
		FROM events_store
		WHERE tenant_id=$1 AND persistence_id=$2 AND sequence_number>=$3 AND sequence_number<=$4
		ORDER BY sequence_number ASC
		LIMIT $5`,
		tenantID, persistenceID, fromSequenceNumber, toSequenceNumber, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

// GetLatestEvent implements persistence.EventsStore. scope is validated
// before anything is read, and this never returns a record that belongs to
// another scope.
func (s *PostgresEventStore) GetLatestEvent(ctx context.Context, scope persistence.Scope, persistenceID string) (*egopb.Event, error) {
	tenantID, err := scopeKey(scope)
	if err != nil {
		return nil, err
	}
	events, err := s.pool.Query(ctx, `
		SELECT persistence_id, sequence_number, is_deleted, event_payload, event_manifest,
		       timestamp, shard_number, encryption_key_id, is_encrypted
		FROM events_store
		WHERE tenant_id=$1 AND persistence_id=$2
		ORDER BY sequence_number DESC
		LIMIT 1`, tenantID, persistenceID)
	if err != nil {
		return nil, err
	}
	defer events.Close()

	result, err := scanEvents(events)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result[0], nil
}

// PersistenceIDs implements persistence.EventsStore. scope is validated
// before anything is read. nextPageToken is the last persistence id actually
// RETURNED on this page (a cursor over what the caller has consumed), and
// the next call resumes strictly after it (persistence_id > pageToken):
// those two facts agree, so iterating from "" until an empty token is
// returned yields every persistence id in scope exactly once, matching
// persistence.EventsStore.PersistenceIDs's pagination contract.
//
// A zero pageSize is a degenerate page: it returns no ids and an empty
// nextPageToken without querying, like testkit's in-memory EventStore. An
// empty token there means "iteration complete", which stops a caller that
// passes 0 instead of handing it a token that repeats the same empty page.
func (s *PostgresEventStore) PersistenceIDs(ctx context.Context, scope persistence.Scope, pageSize uint64, pageToken string) ([]string, string, error) {
	tenantID, err := scopeKey(scope)
	if err != nil {
		return nil, "", err
	}
	if pageSize == 0 {
		return []string{}, "", nil
	}

	var rows pgx.Rows
	if pageToken == "" {
		rows, err = s.pool.Query(ctx,
			`SELECT DISTINCT persistence_id FROM events_store WHERE tenant_id=$1 ORDER BY persistence_id LIMIT $2`,
			tenantID, pageSize)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT DISTINCT persistence_id FROM events_store WHERE tenant_id=$1 AND persistence_id > $2 ORDER BY persistence_id LIMIT $3`,
			tenantID, pageToken, pageSize)
	}
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, "", err
		}
		ids = append(ids, id)
	}

	var next string
	if len(ids) == int(pageSize) {
		next = ids[len(ids)-1]
	}
	return ids, next, rows.Err()
}

func (s *PostgresEventStore) GetShardEvents(ctx context.Context, shardNumber uint64, offset int64, limit uint64) ([]*egopb.Event, int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT persistence_id, sequence_number, is_deleted, event_payload, event_manifest,
		       timestamp, shard_number, encryption_key_id, is_encrypted
		FROM events_store
		WHERE shard_number=$1 AND timestamp > $2
		ORDER BY timestamp ASC
		LIMIT $3`,
		shardNumber, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	events, err := scanEvents(rows)
	if err != nil {
		return nil, 0, err
	}
	if len(events) == 0 {
		return nil, 0, nil
	}
	nextOffset := events[len(events)-1].GetTimestamp()
	return events, nextOffset, nil
}

func (s *PostgresEventStore) ShardOffsets(ctx context.Context) (map[uint64]int64, error) {
	rows, err := s.pool.Query(ctx, `SELECT shard_number, MAX(timestamp) FROM events_store GROUP BY shard_number`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	offsets := make(map[uint64]int64)
	for rows.Next() {
		var (
			shard  uint64
			offset int64
		)
		if err := rows.Scan(&shard, &offset); err != nil {
			return nil, err
		}
		offsets[shard] = offset
	}
	return offsets, rows.Err()
}

// scanEvents reads rows into egopb.Event slices.
func scanEvents(rows pgx.Rows) ([]*egopb.Event, error) {
	var events []*egopb.Event
	for rows.Next() {
		var (
			persistenceID   string
			sequenceNumber  uint64
			isDeleted       bool
			payload         []byte
			manifest        string
			timestamp       int64
			shardNumber     uint64
			encryptionKeyID string
			isEncrypted     bool
		)
		if err := rows.Scan(&persistenceID, &sequenceNumber, &isDeleted, &payload, &manifest,
			&timestamp, &shardNumber, &encryptionKeyID, &isEncrypted); err != nil {
			return nil, err
		}

		eventAny := &anypb.Any{TypeUrl: manifest}
		if err := proto.Unmarshal(payload, eventAny); err != nil {
			return nil, fmt.Errorf("unmarshal event payload: %w", err)
		}

		events = append(events, &egopb.Event{
			PersistenceId:   persistenceID,
			SequenceNumber:  sequenceNumber,
			IsDeleted:       isDeleted,
			Event:           eventAny,
			Timestamp:       timestamp,
			Shard:           shardNumber,
			EncryptionKeyId: encryptionKeyID,
			IsEncrypted:     isEncrypted,
		})
	}
	return events, rows.Err()
}

// PostgresOffsetStore implements offsetstore.OffsetStore using PostgreSQL.
type PostgresOffsetStore struct {
	pool *pgxpool.Pool
	dsn  string
}

var _ offsetstore.OffsetStore = (*PostgresOffsetStore)(nil)

// NewPostgresOffsetStore creates a new PostgreSQL-backed offset store.
func NewPostgresOffsetStore(dsn string) *PostgresOffsetStore {
	return &PostgresOffsetStore{dsn: dsn}
}

func (s *PostgresOffsetStore) Connect(ctx context.Context) error {
	cfg, err := pgxpool.ParseConfig(s.dsn)
	if err != nil {
		return fmt.Errorf("offset store config: %w", err)
	}
	cfg.MaxConns = 20
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("offset store connect: %w", err)
	}
	s.pool = pool
	return nil
}

func (s *PostgresOffsetStore) Disconnect(_ context.Context) error {
	if s.pool != nil {
		s.pool.Close()
	}
	return nil
}

func (s *PostgresOffsetStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *PostgresOffsetStore) WriteOffset(ctx context.Context, offset *egopb.Offset) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO offsets_store (projection_name, shard_number, current_offset, timestamp)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (projection_name, shard_number)
		DO UPDATE SET current_offset = EXCLUDED.current_offset, timestamp = EXCLUDED.timestamp`,
		offset.GetProjectionName(),
		offset.GetShardNumber(),
		offset.GetValue(),
		offset.GetTimestamp(),
	)
	return err
}

func (s *PostgresOffsetStore) GetCurrentOffset(ctx context.Context, projectionID *egopb.ProjectionId) (*egopb.Offset, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT projection_name, shard_number, current_offset, timestamp
		FROM offsets_store
		WHERE projection_name=$1 AND shard_number=$2`,
		projectionID.GetProjectionName(),
		projectionID.GetShardNumber(),
	)

	var offset egopb.Offset
	err := row.Scan(
		&offset.ProjectionName,
		&offset.ShardNumber,
		&offset.Value,
		&offset.Timestamp,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return &offset, nil
}

func (s *PostgresOffsetStore) ResetOffset(ctx context.Context, projectionName string, value int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE offsets_store SET current_offset=$1 WHERE projection_name=$2`,
		value, projectionName)
	return err
}
