-- Reverse 0104: remove rows admitted by this migration before narrowing the CHECK.
DELETE FROM aura.agent_job_runs
WHERE task_id IN (
    SELECT id FROM aura.scheduler_tasks WHERE kind = 'observability_check'
);
DELETE FROM aura.scheduler_tasks WHERE kind = 'observability_check';

ALTER TABLE aura.scheduler_tasks DROP CONSTRAINT scheduler_tasks_kind_check;
ALTER TABLE aura.scheduler_tasks ADD CONSTRAINT scheduler_tasks_kind_check
    CHECK (kind IN ('reminder', 'agent_job', 'backup_postgres',
        'skill_ttl_sweep', 'identity_purge', 'sandbox_reap', 'share_expiry_sweep',
        'retention_sweep', 'memory_embed_backfill'));
