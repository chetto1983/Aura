-- Source: 29-PATTERNS §1 (mcp_audit modeled VERBATIM on 0021_identity_audit),
-- 29-RESEARCH Pitfall 3 (a row trigger never fires for TRUNCATE). The migration
-- floor before this is 0021 (identity_audit); mcp_audit lands at 0022.
--
-- aura.mcp_audit is APPEND-ONLY (T-29-01-01, Tampering/Repudiation defense,
-- belt-and-suspenders): aura_app holds SELECT + INSERT ONLY (no UPDATE/DELETE/
-- TRUNCATE grant), AND a BEFORE UPDATE OR DELETE row trigger raises, AND a BEFORE
-- TRUNCATE statement trigger raises (a row trigger never fires for TRUNCATE —
-- Pitfall 3). One immutable row is written per successful MCP-config mutation,
-- inside a db.WithTx so it commits atomically with the mutation (D-04).
--
-- Like 0021_identity_audit this table carries NO D-29 coherence CHECK (that matrix
-- is skill-specific); the mcp-audit schema is flat.

CREATE TABLE aura.mcp_audit (
    id                 uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at         timestamptz NOT NULL DEFAULT now(),
    -- actor_identity_id is the capability-layer principal (principalFrom), NOT the
    -- raw Authula user id (D-03, same choice as 0021). action is one of
    -- install/edit/enable/disable/remove/trust; server_name names the affected MCP
    -- server; reason is the operator-authored note (NULL except on `trust`).
    actor_identity_id  text        NOT NULL,
    action             text        NOT NULL,
    server_name        text        NOT NULL,
    reason             text
);

-- Plain (non-CONCURRENT) indexes on a fresh empty table inside the implicit
-- migration tx (Pitfall 6 — a CONCURRENT index build is forbidden in a tx block).
CREATE INDEX mcp_audit_server_idx     ON aura.mcp_audit (server_name);
CREATE INDEX mcp_audit_created_at_idx ON aura.mcp_audit (created_at DESC);

-- Append-only enforcement function (T-29-01-01). Raises on any UPDATE/DELETE/
-- TRUNCATE so even a role mistakenly granted DML cannot mutate MCP-config history.
CREATE FUNCTION aura.reject_mcp_audit_mutation() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'aura.mcp_audit is append-only: % is not permitted', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END;
$$;

-- Row trigger for UPDATE/DELETE (fires FOR EACH ROW).
CREATE TRIGGER mcp_audit_no_update_delete
    BEFORE UPDATE OR DELETE ON aura.mcp_audit
    FOR EACH ROW EXECUTE FUNCTION aura.reject_mcp_audit_mutation();

-- SEPARATE statement trigger for TRUNCATE (Pitfall 3: a row trigger NEVER fires
-- for TRUNCATE — it must be a FOR EACH STATEMENT trigger on the TRUNCATE event).
CREATE TRIGGER mcp_audit_no_truncate
    BEFORE TRUNCATE ON aura.mcp_audit
    FOR EACH STATEMENT EXECUTE FUNCTION aura.reject_mcp_audit_mutation();

-- Role separation (T-29-01-01, mirrors 0021 identity_audit append-forever posture):
-- aura_app gets SELECT + INSERT ONLY — never UPDATE/DELETE/TRUNCATE. aura_migrate
-- owns DDL.
GRANT SELECT, INSERT ON aura.mcp_audit TO aura_app;
GRANT ALL              ON aura.mcp_audit TO aura_migrate;

COMMENT ON TABLE aura.mcp_audit IS
    'Append-only MCP-config-mutation audit ledger (Phase 29, MCPW-02). aura_app has SELECT+INSERT only; UPDATE/DELETE raise via a row trigger and TRUNCATE via a statement trigger (Pitfall 3). One immutable row per MCP config mutation, written inside the mutation db.WithTx.';
