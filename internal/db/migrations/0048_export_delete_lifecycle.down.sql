DROP INDEX IF EXISTS aura.conversations_pending_export_delete_idx;
ALTER TABLE aura.conversations
    DROP CONSTRAINT IF EXISTS conversations_delete_lifecycle_shape,
    DROP COLUMN IF EXISTS delete_lease_expires_at,
    DROP COLUMN IF EXISTS delete_worker,
    DROP COLUMN IF EXISTS delete_reserved_at,
    DROP COLUMN IF EXISTS delete_phase;
