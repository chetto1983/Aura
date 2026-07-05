-- Source: PRD Phase 36 (D-28 admin per-user audit UI) + 36-02-PLAN Task 2. The
-- migration floor before this is 0030 (identity_object_store); the audit identity
-- indexes land at 0031.
--
-- The D-28 admin UI (plan 10) reads audit history filtered by a single identity, most
-- recent first. Two shipped audit ledgers carry an identity-principal column but lack an
-- index on it, forcing a seq scan for that read:
--   * aura.mcp_audit  — keyed on actor_identity_id, indexed only on server_name +
--     created_at (0022). Add (actor_identity_id, created_at DESC).
--   * aura.skill_audit — carries identity_id (default 'local'), indexed only on
--     skill_name + created_at (0010). Add (identity_id, created_at DESC).
-- Both match the assets_identity_created_idx composite shape (0020:48-49).
--
-- Intentionally NOT touched:
--   * aura.identity_audit already has identity_audit_new_identity_idx on new_identity_id
--     (0021) — it does not lack an identity index.
--   * aura.tool_invocations has NO identity column (it is conversation-keyed, 0011); its
--     per-user reads join through aura.conversations, so there is no identity column here
--     to index.
-- Plain (non-CONCURRENT) builds: these ledgers are effectively empty in this deployment
-- and a CONCURRENT build is forbidden inside the implicit migration tx (Pitfall 6).

CREATE INDEX mcp_audit_actor_created_idx
    ON aura.mcp_audit (actor_identity_id, created_at DESC);

CREATE INDEX skill_audit_identity_created_idx
    ON aura.skill_audit (identity_id, created_at DESC);
