-- tenant_metadata carries egopb.Event's TenantMetadata as a JSON object. It is
-- nullable rather than NOT NULL DEFAULT '{}': proto3 cannot tell a nil map from
-- an empty one on the wire, so the store writes NULL for both and reads NULL
-- back as a nil map. Existing rows get NULL, which is already the correct "no
-- tenant metadata" reading for a row nothing ever attached an identity to, so
-- there is deliberately no backfill.
ALTER TABLE events_store ADD COLUMN IF NOT EXISTS tenant_metadata JSONB;
