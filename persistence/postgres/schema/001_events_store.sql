-- events_store: the journal of every event, keyed by (tenant_id, persistence_id,
-- sequence_number). tenant_id defaults to '' (the empty string), which the store
-- encodes as persistence.Unscoped(): a valid tenancy.TenantID is never empty, so
-- '' unambiguously means "no tenant".
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
