-- Source: 02-04-PLAN Task 1 (RBAC-09/RBAC-10). Migration floor before this is 0122
-- (identity_llm_key); the capability-denial ledger lands at 0123. Number MEASURED via
-- `ls internal/db/migrations/ | tail -1` at landing time (CLAUDE.md C-04), not deduced.
--
-- aura.capability_denials answers RBAC-10's closing question: who was refused, which
-- capability, at which route, when. All three of RequireCapability's refusal branches
-- (internal/agui/auth.go) write a row here BEFORE the 403 goes out, including the
-- store-error branch RBAC-09 cares about most -- the one that would otherwise be
-- invisible.
--
-- identity_id is TEXT, not uuid (unlike aura.identity_mcp_oauth's FK'd uuid column,
-- 0100): the no-principal refusal has no identity uuid to record, and dropping that row
-- would lose exactly the denial this table exists to keep. A documented sentinel
-- ('(no-principal)', internal/agui/capability_denial_store.go's NoPrincipalIdentityID)
-- fills the column for that branch instead -- the same shape aura.identity_audit's
-- actor_identity_id already uses (plain text, no FK, 0021). cause is a closed
-- three-value set matching the same file's DenialCause* constants -- a CHECK constraint
-- enforces the vocabulary at the database layer too, so a bug that produces a fourth
-- value fails loudly rather than becoming free text.
--
-- No request body, no header, no cookie, no token, no user agent, no IP: the four facts
-- RBAC-10 names plus the cause, nothing else. This table is readable by an admin
-- (through the audit feed's fifth UNION leg, 02-04 Task 2), and a denial audit that
-- records what the caller sent is a place credentials accumulate.
--
-- RLS shape copied from 0100_identity_mcp_oauth.up.sql: a permissive owner-isolation
-- policy plus the RESTRICTIVE fail-closed floor from 0087, so a caller with no
-- app.current_identity sees nothing rather than everything. The write path
-- (PgCapabilityDenialStore.RecordDenial, via db.WithIdentityTx) sets
-- app.current_identity to the SAME identity_id being recorded -- including the
-- no-principal sentinel -- so the permissive policy admits the insert.

CREATE TABLE aura.capability_denials (
    id          bigserial   PRIMARY KEY,
    identity_id text        NOT NULL,
    capability  text        NOT NULL,
    route       text        NOT NULL,
    cause       text        NOT NULL CHECK (cause IN ('no_principal', 'store_error', 'not_held')),
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- Index convention from 0031_audit_identity_indexes.up.sql: (identity_id, created_at DESC)
-- backs the per-identity newest-first read the audit feed's UNION leg performs.
CREATE INDEX capability_denials_identity_created_idx
    ON aura.capability_denials (identity_id, created_at DESC);

-- aura_app writes denials and reads them back through the audit feed; it never updates or
-- deletes one. No UPDATE/DELETE grant, mirroring the append-only intent of
-- 0021_identity_audit without the trigger machinery -- no code path in this schema issues
-- either verb, so the absence of the grant is the enforcement.
GRANT SELECT, INSERT ON aura.capability_denials TO aura_app;
GRANT ALL             ON aura.capability_denials TO aura_migrate;

ALTER TABLE aura.capability_denials ENABLE ROW LEVEL SECURITY;

CREATE POLICY capability_denials_owner_isolation ON aura.capability_denials
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), ''));

CREATE POLICY capability_denials_requires_identity ON aura.capability_denials
    AS RESTRICTIVE FOR ALL TO aura_app
    USING (current_setting('app.current_identity', true) IS NOT NULL
           AND current_setting('app.current_identity', true) <> '');

COMMENT ON POLICY capability_denials_requires_identity ON aura.capability_denials IS
    'Fail-closed floor (migration 0087 pattern): AND-combined with every permissive policy, so no later permissive policy can restore visibility to a caller with no app.current_identity. Set it via internal/db.WithIdentityTx / WithIdentityTxRaw.';

COMMENT ON TABLE aura.capability_denials IS
    'Append-only capability-refusal ledger (migration 0123, RBAC-09/RBAC-10). One row per RequireCapability refusal branch (internal/agui/auth.go): who (identity_id, text -- a documented sentinel for the no-principal case), which capability, which matched route pattern, when, and a closed-set cause. No request body, header, cookie, token, IP or user agent. Read back through GET /api/admin/audit''s fifth UNION leg (internal/agui/audit_store.go).';
