-- Source: PRD Phase 36 (MUSR-01) + 36-04-PLAN Task 1 + 36-RESEARCH Pattern 1 (D-07).
-- The migration floor before this is 0031 (audit_identity_indexes); owner-scoped RLS
-- lands at 0032, co-located with the internal/db.WithIdentityTx carrier that makes it
-- safe (RESEARCH Pitfall 1: RLS on a pooled non-tx read fails closed to 0 rows).
--
-- WHY ENABLE, NOT FORCE (RESEARCH A3 / Pattern 1 "table-owner bypass caveat"):
--   aura_migrate OWNS these tables (it runs every migration's DDL) and BY DEFAULT a
--   table owner bypasses RLS. aura_app is a NON-owner, NON-superuser, NON-BYPASSRLS
--   role (EnsureRoles creates it `WITH LOGIN PASSWORD` only — no BYPASSRLS/SUPERUSER),
--   so ENABLE (not FORCE) suffices: RLS applies to the runtime aura_app pool while
--   aura_migrate (the D-12 backfill role) still bypasses it. The aura_app privilege
--   assumption is asserted live by TestAuraAppLacksRLSBypass (rls_integration_test.go).
--
-- POLICY SEMANTICS — fail-closed-on-MISMATCH, permissive-on-UNSET (deliberate, D-06/D-07):
--   USING ( NULLIF(current_setting('app.current_identity', true), '') IS NULL
--           OR identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid )
--   * When WithIdentityTx has set the var to an identity X (every owner-scoped web
--     read/mutate), the first branch is false and the row is visible IFF identity_id = X
--     — foreign rows are filtered even when the app forgot its *ForIdentity WHERE clause
--     (the D-07 backstop; proven by TestRLSBackstop) and a scoped INSERT/UPDATE can only
--     write X's own rows (WITH CHECK defaults to USING).
--   * When the var is UNSET/'' (the legacy pool + db.WithTx paths: the runner turn-loop
--     AppendTurn, CLI `aura chat`, Telegram, the 34-06 ResumeCommitter — none of which
--     carry a principal), the first branch is TRUE and the policy is permissive, so those
--     pre-isolation paths keep working exactly as before RLS. This is REQUIRED, not a
--     weakening: (a) a static always-on migration cannot thread the identity through the
--     runner/CLI/Telegram write paths (that is later-plan + plan-05 write-path work), and
--     (b) D-06's "403 on a known-foreign mutate" is only implementable if the app can
--     still observe that a foreign row EXISTS — an unscoped existence probe on the pool
--     (var unset → permissive) sees it and returns 403 vs 404; a strict fail-closed-on-
--     unset policy would hide it and force a 404, contradicting D-06. Tightening to
--     fail-closed-on-unset is deferred until every write/read path sets the var
--     (consistent with the D-13 flag-gated reversible rollout).

-- conversations: direct owner policy on identity_id (NOT NULL since 0005).
ALTER TABLE aura.conversations ENABLE ROW LEVEL SECURITY;
CREATE POLICY conversations_owner_isolation ON aura.conversations
    USING (
        NULLIF(current_setting('app.current_identity', true), '') IS NULL
        OR identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid
    );

-- paused_states (approvals): direct owner policy on the 0027 identity_id column.
ALTER TABLE aura.paused_states ENABLE ROW LEVEL SECURITY;
CREATE POLICY paused_states_owner_isolation ON aura.paused_states
    USING (
        NULLIF(current_setting('app.current_identity', true), '') IS NULL
        OR identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid
    );

-- conversation_turns has no identity_id — a turn is reachable ONLY through its parent
-- conversation, so the policy defers to the parent's visibility. The subquery is itself
-- filtered by conversations_owner_isolation (RLS composes), so under a set var a turn is
-- visible/insertable iff its parent conversation is the caller's, and under an unset var
-- (pool/WithTx) the parent is visible → turns behave as before (LoadHistory + AppendTurn
-- keep working). WITH CHECK defaults to USING, so an INSERT is allowed only when the
-- parent conversation is visible in the current RLS context.
ALTER TABLE aura.conversation_turns ENABLE ROW LEVEL SECURITY;
CREATE POLICY conversation_turns_owner_isolation ON aura.conversation_turns
    USING (EXISTS (SELECT 1 FROM aura.conversations c WHERE c.id = conversation_id));

-- paused_states.identity_id write-path population (0027 left it NULLABLE; the runner
-- write-path is plan 05). The owner *ForIdentity / RLS filter on paused_states is only
-- functional when the column is populated, so this migration (a) backfills any row still
-- NULL from its parent conversation and (b) installs a BEFORE INSERT trigger that keeps
-- it populated going forward — COALESCE-style (only fills a NULL), so a future explicit
-- write-path (plan 05) that sets identity_id itself takes precedence. Without this, a
-- freshly-created pause would carry NULL and vanish from the operator's owner-scoped
-- approval center (a self-lockout regression).
UPDATE aura.paused_states AS p
    SET identity_id = c.identity_id
    FROM aura.conversations AS c
    WHERE p.conversation_id = c.id AND p.identity_id IS NULL;

CREATE FUNCTION aura.paused_states_set_identity() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.identity_id IS NULL THEN
        SELECT c.identity_id INTO NEW.identity_id
        FROM aura.conversations c
        WHERE c.id = NEW.conversation_id;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER paused_states_set_identity_trg
    BEFORE INSERT ON aura.paused_states
    FOR EACH ROW EXECUTE FUNCTION aura.paused_states_set_identity();

-- The trigger fires under the inserting role (aura_app); grant it EXECUTE so the pause
-- write-path (askuser.Store.Insert) is never denied on the auto-population.
GRANT EXECUTE ON FUNCTION aura.paused_states_set_identity() TO aura_app;

COMMENT ON POLICY conversations_owner_isolation ON aura.conversations IS
    'Phase 36 D-07 owner isolation: fail-closed-on-mismatch, permissive-on-unset. Set via internal/db.WithIdentityTx SET LOCAL app.current_identity.';
COMMENT ON POLICY paused_states_owner_isolation ON aura.paused_states IS
    'Phase 36 D-07 owner isolation on the 0027 identity_id column (auto-populated from the parent conversation by paused_states_set_identity_trg).';
COMMENT ON POLICY conversation_turns_owner_isolation ON aura.conversation_turns IS
    'Phase 36 D-07 owner isolation via the parent conversation (turns carry no identity_id).';
