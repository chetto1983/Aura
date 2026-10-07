-- Pause for scheduled tasks (prd.md §15). Slot measured with
-- `ls internal/db/migrations/ | tail -1`: 0137 was the head.
--
-- 'paused' has been an allowed status since 0009, but nothing wrote it. paused_reason
-- says who paused a task: the operator, or the scheduler after repeated failed runs.
-- consecutive_failures counts failed runs since the last success; the scheduler pauses a
-- task when it reaches AURA_SCHEDULER_PAUSE_AFTER_FAILURES.

UPDATE aura.scheduler_tasks SET status = 'active' WHERE status = 'paused';

ALTER TABLE aura.scheduler_tasks
    ADD COLUMN consecutive_failures integer NOT NULL DEFAULT 0
        CONSTRAINT scheduler_tasks_consecutive_failures_chk CHECK (consecutive_failures >= 0),
    ADD COLUMN paused_reason text
        CONSTRAINT scheduler_tasks_paused_reason_chk CHECK (paused_reason IN ('operator', 'failures')),
    ADD CONSTRAINT scheduler_tasks_paused_reason_iff_paused_chk
        CHECK ((status = 'paused') = (paused_reason IS NOT NULL));
