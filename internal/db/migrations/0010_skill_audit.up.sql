-- Source: 11-CONTEXT (D-29 audit-coherence matrix, D-05 identity-in-audit-only,
-- D-23 content_hash recovery path, D-19 NO last_used_at/use_count ALTER — sidecar
-- instead), 11-RESEARCH §"Pattern 3" (append-only + role separation), §Pitfall 1
-- (TRUNCATE bypasses row triggers → a SEPARATE statement trigger), §Pitfall 6
-- (golang-migrate forbids a CONCURRENT index build inside the implicit migration
-- tx). The
-- migration floor is 0009 (scheduler); skills land at 0010+ (amendment #48 / D-32).
--
-- aura.skill_audit is APPEND-ONLY (T-11-04-R1, Repudiation defense, belt-and-
-- suspenders): aura_app holds SELECT + INSERT ONLY (no UPDATE/DELETE/TRUNCATE
-- grant), AND a BEFORE UPDATE OR DELETE row trigger raises, AND a BEFORE TRUNCATE
-- statement trigger raises (a row trigger never fires for TRUNCATE — Pitfall 1).
--
-- The D-29 coherence CHECK constrains (approval_source, paused_state_token,
-- gate_recommended, gate_taken) to the FIVE allowed event tuples (the matrix is a
-- disjunction; rows 2+3 share the same shape so the CHECK encodes four distinct
-- tuples). content_hash is recorded on EVERY row (D-23 recovery path).
--
-- The A2 landmine: the 0009 scheduler_tasks.kind CHECK is ALTERed here to admit
-- 'skill_ttl_sweep' (D-16 — TTL sweep ships as a scheduler TaskKind, not a goroutine).

CREATE TABLE aura.skill_audit (
    id                 uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at         timestamptz NOT NULL DEFAULT now(),
    -- Identity lives in audit rows ONLY (D-05): actor_id is the free-form actor
    -- label (e.g. 'model', 'cli', a swarm worker id); identity_id is the owning
    -- identity (defaults to the single-user 'local' scaffold).
    actor_id           text        NOT NULL,
    identity_id        text        NOT NULL DEFAULT 'local',
    skill_name         text        NOT NULL,
    -- The mutation that produced this row: create|update|delete|install|activate|
    -- archive plus the system sweep actions (auto_archive|cleanup_pending_stale).
    action             text        NOT NULL CHECK (action IN (
                                       'create', 'update', 'delete', 'install',
                                       'activate', 'archive',
                                       'auto_archive', 'cleanup_pending_stale')),
    content_hash       text        NOT NULL,                 -- D-23 recovery path, every row
    approval_source    text        CHECK (approval_source IS NULL OR approval_source IN ('ask_user', 'cli', 'auto')),
    paused_state_token uuid        REFERENCES aura.paused_states (token),
    gate_recommended   boolean     NOT NULL,
    gate_taken         boolean     NOT NULL,
    blocklist_override boolean     NOT NULL DEFAULT false,

    -- D-29 audit-coherence matrix: the (approval_source, paused_state_token,
    -- gate_recommended, gate_taken) tuple MUST be one of the five allowed events.
    -- Rows 2 (approve) and 3 (reject) share the same column shape, so this is a
    -- disjunction of four distinct tuples:
    --   pending  : (NULL,     NULL,     true,  false)  -- written to pending, gate not exercised
    --   ask_user : ('ask_user', NOT NULL, true,  true)  -- user approved OR rejected via resume
    --   cli      : ('cli',     NULL,     true,  true)  -- operator CLI approve / --allow-blocklisted
    --   system   : ('auto',    NULL,     false, true)  -- TTL auto_archive / cleanup_pending_stale
    CONSTRAINT skill_audit_d29_coherence CHECK (
        (approval_source IS NULL     AND paused_state_token IS NULL     AND gate_recommended AND NOT gate_taken)
     OR (approval_source = 'ask_user' AND paused_state_token IS NOT NULL AND gate_recommended AND gate_taken)
     OR (approval_source = 'cli'      AND paused_state_token IS NULL     AND gate_recommended AND gate_taken)
     OR (approval_source = 'auto'     AND paused_state_token IS NULL     AND NOT gate_recommended AND gate_taken)
    )
);

-- Plain (non-CONCURRENT) indexes on a fresh empty table inside the implicit
-- migration tx (Pitfall 6 — a CONCURRENT index build is forbidden in a tx block).
-- These back
-- the `aura skills audit [--skill] [--since]` CLI query.
CREATE INDEX skill_audit_name_idx       ON aura.skill_audit (skill_name);
CREATE INDEX skill_audit_created_at_idx ON aura.skill_audit (created_at DESC);

-- Append-only enforcement function (T-11-04-R1). Raises on any UPDATE/DELETE/
-- TRUNCATE so even a role mistakenly granted DML cannot mutate history.
CREATE FUNCTION aura.reject_skill_audit_mutation() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'aura.skill_audit is append-only: % is not permitted', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END;
$$;

-- Row trigger for UPDATE/DELETE (fires FOR EACH ROW).
CREATE TRIGGER skill_audit_no_update_delete
    BEFORE UPDATE OR DELETE ON aura.skill_audit
    FOR EACH ROW EXECUTE FUNCTION aura.reject_skill_audit_mutation();

-- SEPARATE statement trigger for TRUNCATE (Pitfall 1: a row trigger NEVER fires
-- for TRUNCATE — it must be a FOR EACH STATEMENT trigger on the TRUNCATE event).
CREATE TRIGGER skill_audit_no_truncate
    BEFORE TRUNCATE ON aura.skill_audit
    FOR EACH STATEMENT EXECUTE FUNCTION aura.reject_skill_audit_mutation();

-- Role separation (T-11-04-R1, mirrors 0009 agent_job_runs audit-forever posture):
-- aura_app gets SELECT + INSERT ONLY — never UPDATE/DELETE/TRUNCATE. aura_migrate
-- owns DDL.
GRANT SELECT, INSERT ON aura.skill_audit TO aura_app;
GRANT ALL              ON aura.skill_audit TO aura_migrate;

-- A2 landmine (D-16): widen the 0009 scheduler_tasks.kind CHECK to admit the
-- skill TTL-sweep TaskKind. The 0009 constraint is unnamed (inline column CHECK),
-- so Postgres auto-named it `scheduler_tasks_kind_check`; drop + re-add it.
ALTER TABLE aura.scheduler_tasks DROP CONSTRAINT scheduler_tasks_kind_check;
ALTER TABLE aura.scheduler_tasks ADD  CONSTRAINT scheduler_tasks_kind_check
    CHECK (kind IN ('reminder', 'agent_job', 'backup_postgres', 'backup_neo4j', 'skill_ttl_sweep'));

COMMENT ON TABLE aura.skill_audit IS
    'Append-only skill-mutation audit ledger (Slice 7c / Phase 11, D-29). aura_app has SELECT+INSERT only; UPDATE/DELETE raise via a row trigger and TRUNCATE via a statement trigger (Pitfall 1/6). The D-29 coherence CHECK constrains the approval tuple to five allowed events; content_hash is the D-23 recovery path on every row.';
