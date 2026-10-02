-- events_store_revisions holds one row per (tenant_id, persistence_id): the
-- StorageRevision, i.e. the highest sequence number ever committed for that
-- record. It is separate from events_store because DeleteEvents removes
-- replayable events for retention but must never lower the revision, or
-- ExpectGenesis() would succeed again and a stale ExpectRevision would win.
-- The row is also the per-record lock: every write and every delete takes it
-- before touching events_store.
CREATE TABLE IF NOT EXISTS events_store_revisions
(
    tenant_id      VARCHAR(255) DEFAULT '' NOT NULL,
    persistence_id VARCHAR(255)            NOT NULL,
    revision       BIGINT                  NOT NULL,
    PRIMARY KEY (tenant_id, persistence_id)
);

-- Backfill for a database created before this table existed: each record's
-- revision starts at its highest retained sequence number, and an existing
-- revision is never lowered.
INSERT INTO events_store_revisions (tenant_id, persistence_id, revision)
SELECT tenant_id, persistence_id, MAX(sequence_number)
FROM events_store
GROUP BY tenant_id, persistence_id
ON CONFLICT (tenant_id, persistence_id)
DO UPDATE SET revision = GREATEST(events_store_revisions.revision, EXCLUDED.revision);
