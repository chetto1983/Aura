-- Source: Phase 37F gap closure (plan 37F-20), landing after migration 0040 shipped
-- aura.shared_links with NO row-level-security policy. 37F-07 flagged the gap: every
-- other identity-scoped table (aura.conversations, aura.paused_states,
-- aura.conversation_turns) has carried an owner-isolation RLS backstop since migration
-- 0032_owner_rls, but aura.shared_links did not — leaving ADR-0039's own Context
-- sentence ("Postgres RLS (migration 0032_owner_rls) backstop[ping] a forgotten WHERE
-- clause as a kernel-enforced defense-in-depth layer beneath the app-level scoping")
-- false for this one table. internal/share/store.go's owner-scoped methods (Insert,
-- ListForIdentity, ListForConversation, UpdateSnapshot, RevokeForIdentity,
-- RevokeForConversation) already route through db.WithIdentityTxRaw, so today's gap is
-- schema-only: the app-level `owner_identity_id = $N` predicate in every query is the
-- ONLY enforcement layer, with no kernel backstop beneath it. This migration closes
-- that gap with zero Go-side changes required.
--
-- WHY ENABLE, NOT FORCE: identical reasoning to 0032. aura_migrate OWNS
-- aura.shared_links (it ran migration 0040's CREATE TABLE) and a table owner bypasses
-- RLS by default; aura_app is a non-owner, non-superuser, non-BYPASSRLS role, so ENABLE
-- suffices — RLS applies to the runtime aura_app pool while aura_migrate still bypasses
-- it for backfills/maintenance.
--
-- POLICY SEMANTICS — mirrors 0032's conversations_owner_isolation EXACTLY, keyed on
-- shared_links' owner_identity_id column instead of identity_id (fail-closed-on-
-- MISMATCH, permissive-on-UNSET; do NOT invent a bypass role or a token carve-out, and
-- do NOT tighten to fail-closed-on-unset):
--
--   USING ( NULLIF(current_setting('app.current_identity', true), '') IS NULL
--           OR owner_identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid )
--
--   * Under a SET var (every owner-scoped Store method above, via WithIdentityTxRaw),
--     the first branch is false and a row is visible IFF owner_identity_id matches —
--     foreign rows are filtered even when a future query forgets its own
--     `owner_identity_id = $N` predicate, and a scoped INSERT/UPDATE can only write the
--     caller's own rows (WITH CHECK defaults to USING, unspecified here exactly as in
--     0032).
--   * Under an UNSET var, the first branch is true and the policy is permissive. This is
--     REQUIRED, not a weakening: internal/share/store.go's two token/id resolvers,
--     ResolveByToken (the public /s/{token} tier) and ResolveLiveByID (the internal
--     /shared/{id} bearer-within-auth tier, D-10), both deliberately read on the PLAIN
--     pool with no principal to set — the unset-identity permissive branch is precisely
--     the mechanism that keeps both resolvers functional after this migration. Neither
--     resolver's own id/token/tier/liveness predicate changes; this policy sits beneath
--     them and stays out of their way.
--
-- aura.share_audit deliberately carries NO RLS in this migration, consistent with the
-- append-only audit-table precedent (aura.skill_audit 0010, aura.mcp_audit): a ledger's
-- append-only property is enforced by grants (aura_app has SELECT+INSERT only, per
-- migration 0040), not RLS, and an audit trail that filtered itself by owner would stop
-- being usable for cross-identity forensics — the opposite of what an audit table is for.
ALTER TABLE aura.shared_links ENABLE ROW LEVEL SECURITY;

CREATE POLICY shared_links_owner_isolation ON aura.shared_links
    USING (
        NULLIF(current_setting('app.current_identity', true), '') IS NULL
        OR owner_identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid
    );

COMMENT ON POLICY shared_links_owner_isolation ON aura.shared_links IS
    'Phase 37F-20 gap closure: owner isolation mirroring 0032_owner_rls exactly (fail-closed-on-mismatch, permissive-on-unset). Set via internal/db.WithIdentityTxRaw SET LOCAL app.current_identity. ResolveByToken/ResolveLiveByID deliberately read on the unset-var plain pool by design — do not tighten to fail-closed-on-unset.';
