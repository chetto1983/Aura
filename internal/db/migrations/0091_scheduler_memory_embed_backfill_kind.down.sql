-- Reverse 0091: restore the 0083 scheduler_tasks.kind CHECK.
--
-- A down that NARROWS the kind CHECK must first remove the rows the widening admitted:
-- the seeded memory_embed_backfill task violates the restored list and would abort the
-- whole down mid-chain (dirty database). agent_job_runs FKs scheduler_tasks(id) ON
-- DELETE CASCADE, but the run rows are deleted explicitly so the destructive target is
-- auditable (same shape as 0034 and 0083).
DELETE FROM aura.agent_job_runs
WHERE task_id IN (
    SELECT id FROM aura.scheduler_tasks WHERE kind = 'memory_embed_backfill'
);

DELETE FROM aura.scheduler_tasks WHERE kind = 'memory_embed_backfill';

ALTER TABLE aura.scheduler_tasks DROP CONSTRAINT scheduler_tasks_kind_check;
ALTER TABLE aura.scheduler_tasks ADD CONSTRAINT scheduler_tasks_kind_check
    CHECK (kind IN ('reminder', 'agent_job', 'backup_postgres',
        'skill_ttl_sweep', 'identity_purge', 'sandbox_reap', 'share_expiry_sweep',
        'retention_sweep'));
