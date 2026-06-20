-- Source: 28-RESEARCH §Hard Problem 1 (immutable identity-create audit row),
-- 28-PATTERNS §0021_identity_audit (modeled VERBATIM on 0010_skill_audit). The
-- migration floor before this is 0020 (assets); identity_audit lands at 0021.
--
-- aura.identity_audit is APPEND-ONLY (T-28-01-01, Tampering/Repudiation defense,
-- belt-and-suspenders): aura_app holds SELECT + INSERT ONLY (no UPDATE/DELETE/
-- TRUNCATE grant), AND a BEFORE UPDATE OR DELETE row trigger raises, AND a BEFORE
-- TRUNCATE statement trigger raises (a row trigger never fires for TRUNCATE —
-- Pitfall 1). One immutable row is written per successful onboarding provision
-- (ONBD-01a), inside a db.WithTx so it commits atomically with the provisioning.
--
-- Unlike 0010_skill_audit this table carries NO D-29 coherence CHECK (that matrix
-- is skill-specific); the identity-audit schema is flat.

CREATE TABLE aura.identity_audit (
    id                    uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at            timestamptz NOT NULL DEFAULT now(),
    -- actor_identity_id is the creator (the authenticated operator whose grant
    -- authorized the provision); new_identity_id/new_identity_name identify the
    -- provisioned identity; granted_capabilities is the exact set granted at
    -- creation; authula_user_id binds the minted Authula login.
    actor_identity_id     text        NOT NULL,
    new_identity_id       uuid        NOT NULL,
    new_identity_name     text        NOT NULL,
    granted_capabilities  text[]      NOT NULL,
    authula_user_id       text        NOT NULL
);

-- Plain (non-CONCURRENT) indexes on a fresh empty table inside the implicit
-- migration tx (Pitfall 6 — a CONCURRENT index build is forbidden in a tx block).
CREATE INDEX identity_audit_new_identity_idx ON aura.identity_audit (new_identity_id);
CREATE INDEX identity_audit_created_at_idx   ON aura.identity_audit (created_at DESC);

-- Append-only enforcement function (T-28-01-01). Raises on any UPDATE/DELETE/
-- TRUNCATE so even a role mistakenly granted DML cannot mutate provisioning history.
CREATE FUNCTION aura.reject_identity_audit_mutation() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'aura.identity_audit is append-only: % is not permitted', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END;
$$;

-- Row trigger for UPDATE/DELETE (fires FOR EACH ROW).
CREATE TRIGGER identity_audit_no_update_delete
    BEFORE UPDATE OR DELETE ON aura.identity_audit
    FOR EACH ROW EXECUTE FUNCTION aura.reject_identity_audit_mutation();

-- SEPARATE statement trigger for TRUNCATE (Pitfall 1: a row trigger NEVER fires
-- for TRUNCATE — it must be a FOR EACH STATEMENT trigger on the TRUNCATE event).
CREATE TRIGGER identity_audit_no_truncate
    BEFORE TRUNCATE ON aura.identity_audit
    FOR EACH STATEMENT EXECUTE FUNCTION aura.reject_identity_audit_mutation();

-- Role separation (T-28-01-01, mirrors 0010 skill_audit append-forever posture):
-- aura_app gets SELECT + INSERT ONLY — never UPDATE/DELETE/TRUNCATE. aura_migrate
-- owns DDL.
GRANT SELECT, INSERT ON aura.identity_audit TO aura_app;
GRANT ALL              ON aura.identity_audit TO aura_migrate;

COMMENT ON TABLE aura.identity_audit IS
    'Append-only identity-provisioning audit ledger (Phase 28, ONBD-01a). aura_app has SELECT+INSERT only; UPDATE/DELETE raise via a row trigger and TRUNCATE via a statement trigger (Pitfall 1/6). One immutable row per successful onboarding provision, written inside the provisioning db.WithTx.';
