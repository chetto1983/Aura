ALTER TABLE aura.idempotency_operations
    ADD COLUMN replay_status_code smallint,
    ADD COLUMN replay_headers jsonb,
    ADD COLUMN replay_cleared_at timestamptz,
    ADD CONSTRAINT idempotency_operations_replay_status_check CHECK (
        replay_status_code IS NULL OR replay_status_code BETWEEN 100 AND 599
    ),
    ADD CONSTRAINT idempotency_operations_replay_headers_check CHECK (
        replay_headers IS NULL OR octet_length(replay_headers::text) <= 4096
    );

DROP INDEX aura.idempotency_operations_replay_expiry_idx;
CREATE INDEX idempotency_operations_replay_expiry_idx
    ON aura.idempotency_operations (replay_expires_at, identity_id, operation_scope, operation_key)
    WHERE state = 'completed'
      AND replay_expires_at IS NOT NULL
      AND (replay_body IS NOT NULL OR replay_preview IS NOT NULL OR replay_sidecar_ref IS NOT NULL
           OR replay_status_code IS NOT NULL OR replay_headers IS NOT NULL);

COMMENT ON COLUMN aura.idempotency_operations.replay_cleared_at IS
    'Terminal marker distinguishing expired replay material from a valid completed response.';
