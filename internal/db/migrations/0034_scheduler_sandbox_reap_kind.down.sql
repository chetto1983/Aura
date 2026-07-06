-- Reverse 0034: restore the 0033 scheduler_tasks.kind CHECK (drop the 'sandbox_reap'
-- widening), leaving 'identity_purge' in place (the 0033 list).
--
-- A down that narrows the kind CHECK must first remove the rows the widening admitted:
-- a live sandbox_reap sweep task (seeded at boot once plan 37-05 wires seedSandboxReapSweep)
-- violates the restored 0033 CHECK and aborts the whole down mid-chain (dirty database).
-- agent_job_runs FKs scheduler_tasks(id) ON DELETE CASCADE, so delete its reap-task run rows
-- first (explicit here for parity with the 0033 down).
DELETE FROM aura.agent_job_runs
    WHERE task_id IN (SELECT id FROM aura.scheduler_tasks WHERE kind = 'sandbox_reap');
DELETE FROM aura.scheduler_tasks WHERE kind = 'sandbox_reap';

ALTER TABLE aura.scheduler_tasks DROP CONSTRAINT scheduler_tasks_kind_check;
ALTER TABLE aura.scheduler_tasks ADD  CONSTRAINT scheduler_tasks_kind_check
    CHECK (kind IN ('reminder', 'agent_job', 'backup_postgres', 'backup_neo4j', 'skill_ttl_sweep', 'identity_purge'));
