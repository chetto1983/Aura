ALTER TABLE aura.steer_queue
    ADD COLUMN target_worker_id text,
    ADD COLUMN target_run_id text,
    ADD CONSTRAINT steer_worker_scope CHECK (
        (target_worker_id IS NULL AND target_run_id IS NULL) OR
        (kind = 'steer' AND target_worker_id <> '' AND target_run_id <> ''
         AND target_worker_id IS NOT NULL AND target_run_id IS NOT NULL)
    );

CREATE INDEX steer_worker_history_idx ON aura.steer_queue
    (identity_id, conversation_id, target_worker_id, created_at DESC, id DESC)
    WHERE target_worker_id IS NOT NULL;
