-- offsets_store: the position each projection has reached, per shard.
CREATE TABLE IF NOT EXISTS offsets_store
(
    projection_name VARCHAR(255) NOT NULL,
    shard_number    BIGINT       NOT NULL,
    current_offset  BIGINT       NOT NULL,
    timestamp       BIGINT       NOT NULL,
    PRIMARY KEY (projection_name, shard_number)
);

CREATE INDEX IF NOT EXISTS idx_offsets_store_name ON offsets_store (projection_name);
CREATE INDEX IF NOT EXISTS idx_offsets_store_shard ON offsets_store (shard_number);
