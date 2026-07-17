-- Reverse 0040: a down that narrows the scheduler_tasks.kind CHECK must FIRST
-- remove the rows the widening admitted — a live share_expiry_sweep task row
-- violates the restored CHECK and aborts the whole down mid-chain, leaving a
-- dirty database.

-- FK children first. agent_job_runs FKs scheduler_tasks(id) ON DELETE CASCADE, so
-- this delete is explicit here for parity with 0034's down, not strictly required.
DELETE FROM aura.agent_job_runs
    WHERE task_id IN (SELECT id FROM aura.scheduler_tasks WHERE kind = 'share_expiry_sweep');
DELETE FROM aura.scheduler_tasks WHERE kind = 'share_expiry_sweep';

-- Restore the pre-37F kind CHECK (the list read from disk in Task 1, WITHOUT
-- 'share_expiry_sweep').
ALTER TABLE aura.scheduler_tasks DROP CONSTRAINT scheduler_tasks_kind_check;
ALTER TABLE aura.scheduler_tasks ADD  CONSTRAINT scheduler_tasks_kind_check
    CHECK (kind IN ('reminder', 'agent_job', 'backup_postgres', 'backup_neo4j', 'skill_ttl_sweep', 'identity_purge', 'sandbox_reap'));

-- Children before parents. Neither table is referenced BY another table, so this
-- order is stylistic parity with 0004's stated drop-order rule rather than an FK
-- requirement.
DROP TABLE IF EXISTS aura.share_audit;
DROP TABLE IF EXISTS aura.shared_links;
