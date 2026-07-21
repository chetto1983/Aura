DROP INDEX aura.idempotency_operations_replay_expiry_idx;

ALTER TABLE aura.idempotency_operations
    DROP CONSTRAINT idempotency_operations_replay_headers_check,
    DROP CONSTRAINT idempotency_operations_replay_status_check,
    DROP COLUMN replay_cleared_at,
    DROP COLUMN replay_headers,
    DROP COLUMN replay_status_code;

CREATE INDEX idempotency_operations_replay_expiry_idx
    ON aura.idempotency_operations (replay_expires_at, identity_id, operation_scope, operation_key)
    WHERE state = 'completed'
      AND replay_expires_at IS NOT NULL
      AND (replay_body IS NOT NULL OR replay_preview IS NOT NULL OR replay_sidecar_ref IS NOT NULL);
