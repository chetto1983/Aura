-- Durable export-delete lifecycle. The reservation identifies the only operation
-- allowed to resume, while the phase distinguishes a releasable fence from work
-- that has crossed the irreversible teardown boundary.
ALTER TABLE aura.conversations
    ADD COLUMN delete_phase text,
    ADD COLUMN delete_reserved_at timestamptz,
    ADD COLUMN delete_worker text,
    ADD COLUMN delete_lease_expires_at timestamptz;

UPDATE aura.conversations
SET delete_phase = 'reserved',
    delete_reserved_at = now()
WHERE delete_reservation IS NOT NULL;

ALTER TABLE aura.conversations
    ADD CONSTRAINT conversations_delete_lifecycle_shape CHECK (
        (delete_reservation IS NULL AND delete_phase IS NULL AND delete_reserved_at IS NULL
         AND delete_worker IS NULL AND delete_lease_expires_at IS NULL)
        OR
        (delete_reservation IS NOT NULL
         AND delete_phase IN ('reserved', 'teardown_started')
         AND delete_reserved_at IS NOT NULL
         AND ((delete_worker IS NULL AND delete_lease_expires_at IS NULL)
              OR (delete_worker IS NOT NULL AND delete_lease_expires_at IS NOT NULL)))
    );

CREATE INDEX conversations_pending_export_delete_idx
    ON aura.conversations (delete_reserved_at, id)
    WHERE delete_reservation IS NOT NULL;
