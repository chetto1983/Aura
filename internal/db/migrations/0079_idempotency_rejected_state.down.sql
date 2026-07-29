DROP INDEX aura.idempotency_operations_replay_expiry_idx;

ALTER TABLE aura.idempotency_operations
    DROP CONSTRAINT idempotency_operations_terminal_shape_check,
    DROP CONSTRAINT idempotency_operations_state_check;

UPDATE aura.idempotency_operations
SET state = 'indeterminate',
    indeterminate_at = COALESCE(rejected_at, updated_at)
WHERE state = 'rejected';

ALTER TABLE aura.idempotency_operations
    DROP COLUMN rejected_at,
    ADD CONSTRAINT idempotency_operations_state_check
        CHECK (state IN ('in_progress', 'completed', 'indeterminate')),
    ADD CONSTRAINT idempotency_operations_terminal_shape_check CHECK (
        (state = 'in_progress' AND completed_at IS NULL AND indeterminate_at IS NULL)
        OR
        (state = 'completed' AND completed_at IS NOT NULL AND indeterminate_at IS NULL)
        OR
        (state = 'indeterminate' AND completed_at IS NULL AND indeterminate_at IS NOT NULL)
    );

CREATE INDEX idempotency_operations_replay_expiry_idx
    ON aura.idempotency_operations
        (replay_expires_at, identity_id, operation_scope, operation_key)
    WHERE state = 'completed'
      AND replay_expires_at IS NOT NULL
      AND (
          replay_body IS NOT NULL
          OR replay_preview IS NOT NULL
          OR replay_sidecar_ref IS NOT NULL
          OR replay_status_code IS NOT NULL
          OR replay_headers IS NOT NULL
      );
