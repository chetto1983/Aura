-- A downgrade must not turn undelivered child instructions into parent steers.
DELETE FROM aura.steer_queue WHERE target_worker_id IS NOT NULL;
DROP INDEX aura.steer_worker_history_idx;
ALTER TABLE aura.steer_queue
    DROP CONSTRAINT steer_worker_scope,
    DROP COLUMN target_worker_id,
    DROP COLUMN target_run_id;
