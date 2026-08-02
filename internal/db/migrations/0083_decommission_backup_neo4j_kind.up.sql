-- Retire the nightly Neo4j backup. The `neo4j` compose service, its aura-neo4j
-- volume and the AURA_NEO4J_BOLT_URL/NEO4J_* credentials the handler read were all
-- deleted with the graph store, so every future run of this kind can only fail
-- against a host that is not there. Aura's graph plane is ArcadeDB now.
--
-- Runs are deleted before tasks so the destructive target is auditable even though
-- the task foreign key also cascades (same shape as 0059).
DELETE FROM aura.agent_job_runs
WHERE task_id IN (
    SELECT id FROM aura.scheduler_tasks WHERE kind = 'backup_neo4j'
);

DELETE FROM aura.scheduler_tasks
WHERE kind = 'backup_neo4j';

ALTER TABLE aura.scheduler_tasks DROP CONSTRAINT scheduler_tasks_kind_check;
ALTER TABLE aura.scheduler_tasks ADD CONSTRAINT scheduler_tasks_kind_check
    CHECK (kind IN ('reminder', 'agent_job', 'backup_postgres',
        'skill_ttl_sweep', 'identity_purge', 'sandbox_reap', 'share_expiry_sweep',
        'retention_sweep'));
