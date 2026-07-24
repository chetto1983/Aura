-- Remove the retired pre-policy learning stack's scheduler and settings state.
-- The explicit run deletion precedes task deletion so the destructive target is
-- auditable even though the task foreign key also cascades.
DELETE FROM aura.agent_job_runs
WHERE task_id IN (
    SELECT id FROM aura.scheduler_tasks WHERE kind = 'learning_compaction'
);

DELETE FROM aura.scheduler_tasks
WHERE kind = 'learning_compaction';

DELETE FROM aura.settings
WHERE key = 'AURA_LLM_REASONING_LEARNING'
   OR key LIKE 'AURA\_LEARNING\_%' ESCAPE '\';

ALTER TABLE aura.scheduler_tasks DROP CONSTRAINT scheduler_tasks_kind_check;
ALTER TABLE aura.scheduler_tasks ADD CONSTRAINT scheduler_tasks_kind_check
    CHECK (kind IN ('reminder', 'agent_job', 'backup_postgres', 'backup_neo4j',
        'skill_ttl_sweep', 'identity_purge', 'sandbox_reap', 'share_expiry_sweep',
        'retention_sweep'));
