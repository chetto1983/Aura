-- Reverse 0033: restore the 0010 scheduler_tasks.kind CHECK (drop the 'identity_purge'
-- widening), leaving 'skill_ttl_sweep' in place (the 0010 list).
--
-- A down that narrows the kind CHECK must first remove the rows the widening admitted:
-- a live identity_purge sweep task (seeded at boot by seedIdentityPurgeSweep since 36-14)
-- violates the restored 0010 CHECK and aborts the whole down mid-chain (dirty database).
-- agent_job_runs FKs scheduler_tasks(id) ON DELETE CASCADE, so delete its purge-task run
-- rows first (explicit here for parity with the 0010 down).
DELETE FROM aura.agent_job_runs
    WHERE task_id IN (SELECT id FROM aura.scheduler_tasks WHERE kind = 'identity_purge');
DELETE FROM aura.scheduler_tasks WHERE kind = 'identity_purge';

ALTER TABLE aura.scheduler_tasks DROP CONSTRAINT scheduler_tasks_kind_check;
ALTER TABLE aura.scheduler_tasks ADD  CONSTRAINT scheduler_tasks_kind_check
    CHECK (kind IN ('reminder', 'agent_job', 'backup_postgres', 'backup_neo4j', 'skill_ttl_sweep'));
