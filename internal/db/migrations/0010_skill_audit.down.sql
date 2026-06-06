-- Reverse 0010: restore the original 0009 scheduler_tasks.kind CHECK (drop the
-- skill_ttl_sweep widening), then drop the triggers + function + the audit table.
-- The two indexes drop with their table. Triggers must drop before the function
-- they call (or be dropped via the table drop — explicit here for clarity).

-- A down that narrows the kind CHECK must first remove the rows the widening
-- admitted: a live skill_ttl_sweep task (registered at boot since 7e) violates
-- the restored 0009 CHECK and aborts the whole down mid-chain (dirty database).
-- agent_job_runs FKs scheduler_tasks(id), so its rows go first.
DELETE FROM aura.agent_job_runs
    WHERE task_id IN (SELECT id FROM aura.scheduler_tasks WHERE kind = 'skill_ttl_sweep');
DELETE FROM aura.scheduler_tasks WHERE kind = 'skill_ttl_sweep';

ALTER TABLE aura.scheduler_tasks DROP CONSTRAINT scheduler_tasks_kind_check;
ALTER TABLE aura.scheduler_tasks ADD  CONSTRAINT scheduler_tasks_kind_check
    CHECK (kind IN ('reminder', 'agent_job', 'backup_postgres', 'backup_neo4j'));

DROP TRIGGER IF EXISTS skill_audit_no_truncate      ON aura.skill_audit;
DROP TRIGGER IF EXISTS skill_audit_no_update_delete ON aura.skill_audit;
DROP TABLE   IF EXISTS aura.skill_audit;
DROP FUNCTION IF EXISTS aura.reject_skill_audit_mutation();
