-- Source: Phase 37 (SBX-03/D-08 idle-suspend reaper) + 37-03-PLAN Task 2.
-- The D-08 idle-suspend reap sweep (internal/sandbox SuspendIdle, driven by
-- internal/cron/handlers.SandboxReapHandler) ships as a scheduler TaskKind, exactly like
-- the 0010 skill_ttl_sweep and 0033 identity_purge — NOT a bespoke goroutine. A reap task
-- therefore has to be INSERTable into aura.scheduler_tasks, but the kind CHECK (0009, widened
-- at 0010 then 0033) does not admit 'sandbox_reap', so the reap sweep is structurally
-- unschedulable until this widen lands.
--
-- Mirror 0033's widen verbatim: the 0009 constraint is unnamed (inline column CHECK), so
-- Postgres auto-named it `scheduler_tasks_kind_check`; drop + re-add it with the extra
-- 'sandbox_reap' member. 0009 GRANTed aura_app DML on scheduler_tasks already, so no grant
-- change is needed here (aura_migrate owns DDL).

ALTER TABLE aura.scheduler_tasks DROP CONSTRAINT scheduler_tasks_kind_check;
ALTER TABLE aura.scheduler_tasks ADD  CONSTRAINT scheduler_tasks_kind_check
    CHECK (kind IN ('reminder', 'agent_job', 'backup_postgres', 'backup_neo4j', 'skill_ttl_sweep', 'identity_purge', 'sandbox_reap'));
