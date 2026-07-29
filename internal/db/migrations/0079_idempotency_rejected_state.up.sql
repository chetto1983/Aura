DROP INDEX aura.idempotency_operations_replay_expiry_idx;

ALTER TABLE aura.idempotency_operations
    DROP CONSTRAINT idempotency_operations_terminal_shape_check,
    DROP CONSTRAINT idempotency_operations_state_check,
    ADD COLUMN rejected_at timestamptz,
    ADD CONSTRAINT idempotency_operations_state_check
        CHECK (state IN ('in_progress', 'completed', 'indeterminate', 'rejected')),
    ADD CONSTRAINT idempotency_operations_terminal_shape_check CHECK (
        (state = 'in_progress'
            AND completed_at IS NULL AND indeterminate_at IS NULL AND rejected_at IS NULL)
        OR
        (state = 'completed'
            AND completed_at IS NOT NULL AND indeterminate_at IS NULL AND rejected_at IS NULL)
        OR
        (state = 'indeterminate'
            AND completed_at IS NULL AND indeterminate_at IS NOT NULL AND rejected_at IS NULL)
        OR
        (state = 'rejected'
            AND completed_at IS NULL AND indeterminate_at IS NULL AND rejected_at IS NOT NULL)
    );

CREATE INDEX idempotency_operations_replay_expiry_idx
    ON aura.idempotency_operations
        (replay_expires_at, identity_id, operation_scope, operation_key)
    WHERE state IN ('completed', 'rejected')
      AND replay_expires_at IS NOT NULL
      AND (
          replay_body IS NOT NULL
          OR replay_preview IS NOT NULL
          OR replay_sidecar_ref IS NOT NULL
          OR replay_status_code IS NOT NULL
          OR replay_headers IS NOT NULL
      );

COMMENT ON COLUMN aura.idempotency_operations.rejected_at IS
    'Terminal time for a deterministic no-effect domain rejection; unlike indeterminate, it is safely replayed as an error.';
