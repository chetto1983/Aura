-- Reverse 0010: restore the original 0009 scheduler_tasks.kind CHECK (drop the
-- skill_ttl_sweep widening), then drop the triggers + function + the audit table.
-- The two indexes drop with their table. Triggers must drop before the function
-- they call (or be dropped via the table drop — explicit here for clarity).

ALTER TABLE aura.scheduler_tasks DROP CONSTRAINT scheduler_tasks_kind_check;
ALTER TABLE aura.scheduler_tasks ADD  CONSTRAINT scheduler_tasks_kind_check
    CHECK (kind IN ('reminder', 'agent_job', 'backup_postgres', 'backup_neo4j'));

DROP TRIGGER IF EXISTS skill_audit_no_truncate      ON aura.skill_audit;
DROP TRIGGER IF EXISTS skill_audit_no_update_delete ON aura.skill_audit;
DROP TABLE   IF EXISTS aura.skill_audit;
DROP FUNCTION IF EXISTS aura.reject_skill_audit_mutation();
