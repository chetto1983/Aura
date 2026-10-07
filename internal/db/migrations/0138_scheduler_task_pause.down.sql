UPDATE aura.scheduler_tasks SET status = 'active' WHERE status = 'paused';

ALTER TABLE aura.scheduler_tasks
    DROP CONSTRAINT scheduler_tasks_paused_reason_iff_paused_chk,
    DROP COLUMN paused_reason,
    DROP COLUMN consecutive_failures;
