-- Indexes of events_store. The last one, idx_events_store_shard, is the marker
-- schema.go uses to recognise this version in a database that was created by
-- hand before versions were recorded.
CREATE INDEX IF NOT EXISTS idx_events_store_persistence_id ON events_store(tenant_id, persistence_id);
CREATE INDEX IF NOT EXISTS idx_events_store_seqnumber ON events_store(sequence_number);
CREATE INDEX IF NOT EXISTS idx_events_store_timestamp ON events_store(timestamp);
CREATE INDEX IF NOT EXISTS idx_events_store_shard ON events_store(shard_number);
