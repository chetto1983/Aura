-- Source: Phase 36 (MUSR-01/D-27 grace-window de-provisioning) + 36-14-PLAN Task 1.
-- The D-27 soft-delete purge (internal/agui/deprovision.go PurgeExpired, driven by
-- internal/cron/handlers.IdentityPurgeHandler) ships as a scheduler TaskKind, exactly
-- like the 0010 skill_ttl_sweep — NOT a bespoke goroutine. A purge task therefore has
-- to be INSERTable into aura.scheduler_tasks, but the kind CHECK (0009, widened once at
-- 0010) does not admit 'identity_purge', so the grace-window purge is structurally
-- unschedulable until this widen lands.
--
-- Mirror 0010's A2-landmine widen verbatim: the 0009 constraint is unnamed (inline
-- column CHECK), so Postgres auto-named it `scheduler_tasks_kind_check`; drop + re-add
-- it with the extra 'identity_purge' member. 0009 GRANTed aura_app DML on
-- scheduler_tasks already, so no grant change is needed here (aura_migrate owns DDL).

ALTER TABLE aura.scheduler_tasks DROP CONSTRAINT scheduler_tasks_kind_check;
ALTER TABLE aura.scheduler_tasks ADD  CONSTRAINT scheduler_tasks_kind_check
    CHECK (kind IN ('reminder', 'agent_job', 'backup_postgres', 'backup_neo4j', 'skill_ttl_sweep', 'identity_purge'));
