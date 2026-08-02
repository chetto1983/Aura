-- Close the last four fail-OPEN row-level-security policies: aura.conversations,
-- aura.conversation_turns, aura.paused_states and aura.shared_links.
--
-- Migration 0087 brought ten tables under fail-closed RLS and deliberately left these four
-- alone, documenting exactly why in its header: the runner turn loop wrote conversations and
-- turns on EVERY turn with no principal, because conversations.Store and askuser.Store never
-- passed an identity to internal/db.WithIdentityTx. Tightening the policies before fixing
-- that would have failed in the hundreds across internal/agui, internal/askuser,
-- internal/breakglass and internal/conversations.
--
-- That write-path work is what this migration lands on top of. Both stores now derive the
-- caller's identity from the context (internal/db.WithCallerIdentityTx) at every read and
-- write, the daemon's three genuinely cross-tenant sweeps enumerate identities and run one
-- identity-scoped transaction each instead of one unscoped query
-- (conversations.ScanOrphans, conversations.Store.ListReservedDeletes and the
-- `aura paused-states` CLI), so nothing left in the product needs the escape hatch:
--
--     NULLIF(current_setting('app.current_identity', true), '') IS NULL OR identity_id = ...
--
-- An unset session variable made that first branch TRUE and the whole table visible. Measured
-- on the live deployment on 2026-08-02 as aura_app with no identity: all 32 conversations and
-- all 648 turns were readable.
--
-- Each of the first three tables gets the 0087 shape, unchanged:
--
--   1. One permissive owner policy with NO `IS NULL OR` escape. (PostgreSQL manual, ALTER
--      TABLE / Row Security: "If no policy exists for the table, a default-deny policy is
--      used" — enabling RLS is itself most of the win, and these tables already have it on.)
--   2. One `AS RESTRICTIVE FOR ALL TO aura_app` policy requiring an identity to be set.
--      (CREATE POLICY: "All restrictive policies which are applicable to a given query will
--      be combined together using the Boolean AND operator".) Permissive policies OR with
--      each other but AND with every restrictive one, so no permissive policy added later can
--      widen past the identity requirement — the failure mode of a future mistake becomes
--      "too few rows", never "all rows". WITH CHECK is left unspecified and therefore
--      defaults to USING, which closes the write side.
--
-- aura.shared_links is the exception, and it is the trap in this migration. See its section.
--
-- WHY ENABLE, NOT FORCE (unchanged from 0032/0041/0087): aura_migrate OWNS these tables and a
-- table owner bypasses RLS by default, which is what golang-migrate needs to keep running DDL
-- and backfills. aura_app is a non-owner, non-superuser, non-BYPASSRLS role, so ENABLE already
-- applies RLS to the runtime pool. Row security is already enabled on all four tables (0032
-- and 0041); this migration only replaces their predicates.

-- ---------------------------------------------------------------------------
-- aura.conversations — the direct owner policy, escape hatch removed.
-- ---------------------------------------------------------------------------
DROP POLICY conversations_owner_isolation ON aura.conversations;
CREATE POLICY conversations_owner_isolation ON aura.conversations
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);

-- ---------------------------------------------------------------------------
-- aura.conversation_turns — name the parent's OWNER, not merely the parent.
-- ---------------------------------------------------------------------------
-- 0032's policy checked only that the parent row EXISTS and leaned on
-- conversations_owner_isolation to filter the subquery. Two layers that cede together are one
-- layer: when the parent failed open the child failed open with it. 0087 already corrected
-- this shape for the document children; this brings turns in line. The join is on a uuid FK,
-- so there is no cast hazard.
DROP POLICY conversation_turns_owner_isolation ON aura.conversation_turns;
CREATE POLICY conversation_turns_owner_isolation ON aura.conversation_turns
    USING (EXISTS (
        SELECT 1 FROM aura.conversations c
        WHERE c.id = conversation_id
          AND c.identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid
    ));

-- ---------------------------------------------------------------------------
-- aura.paused_states — the direct owner policy, escape hatch removed.
-- ---------------------------------------------------------------------------
-- identity_id is still NULLABLE (0027) and populated by the 0032 BEFORE INSERT trigger from
-- the parent conversation. That trigger is SECURITY INVOKER, so its lookup is itself filtered
-- by conversations_owner_isolation: with the right identity set it finds the parent and fills
-- the column; with none set it finds nothing, identity_id stays NULL, and `NULL = NULL` is
-- NULL — the insert is refused rather than silently orphaned. Fail-closed on both halves.
DROP POLICY paused_states_owner_isolation ON aura.paused_states;
CREATE POLICY paused_states_owner_isolation ON aura.paused_states
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);

-- ---------------------------------------------------------------------------
-- aura.shared_links — "mine, or not yet revoked". NOT "mine".
-- ---------------------------------------------------------------------------
-- A share link is a capability handed to someone who is not the owner, so an owner-only
-- predicate here does not fail closed, it fails WRONG: the link resolves to nothing instead
-- of erroring, and sharing breaks silently. Three documented readers hold rows they do not
-- own, all on the plain pool with no identity to set (internal/share/store.go):
--
--   ResolveByToken    — the public /s/{token} tier. The recipient is ANONYMOUS by design;
--                       there is no identity that could ever be set.
--   ResolveLiveByID   — the internal tier (D-10): ANY authenticated identity holding the id
--                       may open someone else's already-redacted snapshot. The absence of an
--                       owner predicate there is the contract, not an oversight.
--   DueForExpiry      — the system-wide expiry sweep. This one is the trap inside the trap:
--                       it selects rows whose expires_at has ALREADY PASSED, so a predicate
--                       of "mine OR live" would hide exactly the rows it exists to find and
--                       the sweep would silently expire nothing, leaking object-store bytes
--                       forever. The union of all three is "mine OR revoked_at IS NULL".
--
-- So the read surface is widened for SELECT only, and only for a caller with NO identity —
-- which is precisely what those three readers are. An AUTHENTICATED caller still sees only
-- their own links: identity B running an owner-scoped query must not observe A's link, and
-- TestShareStoreRLSDeniesCrossIdentityRead pins that. D-10 is unaffected because
-- ResolveLiveByID deliberately reads on the plain pool — the bearer's identity is not the
-- scope of that read, which is the whole point of the decision.
--
-- Splitting the permissive policies by COMMAND is what makes the widening safe: a single FOR
-- ALL policy would also let an identity-less caller DELETE an unrevoked row someone else
-- owns, since DELETE has no WITH CHECK to catch it.
DROP POLICY shared_links_owner_isolation ON aura.shared_links;

CREATE POLICY shared_links_owner_isolation ON aura.shared_links
    FOR ALL
    USING (owner_identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid)
    WITH CHECK (owner_identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);

CREATE POLICY shared_links_unrevoked_resolve ON aura.shared_links
    FOR SELECT
    USING (
        NULLIF(current_setting('app.current_identity', true), '') IS NULL
        AND revoked_at IS NULL
    );

-- The restrictive floor for this table cannot be "an identity is required" — that is the one
-- thing a share link must not require. It is instead the tightest statement that stays true
-- of every legitimate access: you may READ a row you own or a row that is not yet revoked,
-- and you may only ever WRITE a row you own. Like the floors above it AND-combines with
-- every permissive policy, so no policy added later can widen past it.
CREATE POLICY shared_links_owner_or_unrevoked ON aura.shared_links
    AS RESTRICTIVE FOR ALL TO aura_app
    USING (
        owner_identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid
        OR (NULLIF(current_setting('app.current_identity', true), '') IS NULL
            AND revoked_at IS NULL)
    )
    WITH CHECK (owner_identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);

-- ---------------------------------------------------------------------------
-- The restrictive identity-required floor for the three owner-only tables.
-- ---------------------------------------------------------------------------
-- Generated from one array, exactly as 0087 does: the predicate is byte-identical for every
-- table and the array IS the auditable list of what this migration covers.
DO $$
DECLARE
    target text;
BEGIN
    FOREACH target IN ARRAY ARRAY[
        'conversations',
        'conversation_turns',
        'paused_states'
    ]
    LOOP
        EXECUTE format($fmt$
            CREATE POLICY %I ON aura.%I AS RESTRICTIVE FOR ALL TO aura_app
            USING (current_setting('app.current_identity', true) IS NOT NULL
                   AND current_setting('app.current_identity', true) <> '')
        $fmt$, target || '_requires_identity', target);
        EXECUTE format(
            'COMMENT ON POLICY %I ON aura.%I IS %L',
            target || '_requires_identity', target,
            $doc$Fail-closed floor (migration 0089): AND-combined with every permissive policy, so no later permissive policy can restore visibility to a caller with no app.current_identity. Set it via internal/db.WithIdentityTx / WithIdentityTxRaw / WithCallerIdentityTx.$doc$);
    END LOOP;
END
$$;

COMMENT ON POLICY conversations_owner_isolation ON aura.conversations IS
    'Fail-closed owner isolation (migration 0089): no permissive-on-unset branch. Set the identity via internal/db.WithCallerIdentityTx.';
COMMENT ON POLICY conversation_turns_owner_isolation ON aura.conversation_turns IS
    'Fail-closed owner isolation (migration 0089) naming the parent conversation OWNER directly, not merely the parent row.';
COMMENT ON POLICY paused_states_owner_isolation ON aura.paused_states IS
    'Fail-closed owner isolation (migration 0089) on the 0027 identity_id column, auto-populated from the parent conversation by paused_states_set_identity_trg.';
COMMENT ON POLICY shared_links_owner_isolation ON aura.shared_links IS
    'Fail-closed owner isolation (migration 0089) for every command: only the owner may read, insert, update or delete a link through this policy.';
COMMENT ON POLICY shared_links_unrevoked_resolve ON aura.shared_links IS
    'SELECT-only widening (migration 0089), and only for a caller with NO app.current_identity: ResolveByToken is anonymous by design, ResolveLiveByID serves any authenticated bearer on the plain pool (D-10), and DueForExpiry sweeps already-expired rows system-wide. An authenticated, owner-scoped read still sees only its own links. Do NOT extend this to a write command.';
COMMENT ON POLICY shared_links_owner_or_unrevoked ON aura.shared_links IS
    'Fail-closed floor (migration 0089): a read reaches a row only if the caller owns it or the caller has no identity at all and the row is unrevoked; a write only ever reaches a row the caller owns. Holds whatever permissive policy a later migration adds.';

-- ---------------------------------------------------------------------------
-- aura.tool_invocations' append-only guard asks a question RLS now answers WRONGLY.
-- ---------------------------------------------------------------------------
-- Migration 0016 taught the tamper-evidence trigger to tell a legitimate ON DELETE CASCADE
-- apart from surgical tampering by asking whether the parent conversation still exists:
-- during a cascade the parent row is already gone inside the same transaction, so a DELETE
-- whose conversation no longer exists IS the cascade and is allowed.
--
-- That probe is SECURITY INVOKER, so from this migration onward it is filtered by
-- conversations_owner_isolation. On a connection with no app.current_identity the parent is
-- INVISIBLE, not absent — and the trigger cannot tell the difference. It would conclude
-- "cascade", RETURN OLD, and let anyone holding an aura_app connection DELETE rows out of the
-- audit ledger the call-budget gate and every forensic query read. Making the whole
-- conversation plane fail closed would have quietly opened the one table that must never be
-- rewritable. Caught by TestLedgerAppendOnly, which is why that test exists.
--
-- SECURITY DEFINER fixes it at the source: the function runs as its owner, aura_migrate, who
-- OWNS aura.conversations and therefore bypasses row security, so the probe answers the
-- question it was written to ask. search_path is pinned because a SECURITY DEFINER function
-- must never resolve an identifier through a caller-controlled path (PostgreSQL manual,
-- CREATE FUNCTION / "Writing SECURITY DEFINER Functions Safely"). The body is otherwise
-- byte-identical to 0016.
CREATE OR REPLACE FUNCTION aura.reject_tool_invocations_mutation() RETURNS trigger
    LANGUAGE plpgsql
    SECURITY DEFINER
    SET search_path = pg_catalog
    AS $$
BEGIN
    IF TG_OP = 'DELETE'
        AND NOT EXISTS (SELECT 1 FROM aura.conversations c WHERE c.id = OLD.conversation_id) THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION 'aura.tool_invocations is append-only: % is not permitted', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END;
$$;

-- ---------------------------------------------------------------------------
-- STILL UNCOVERED after this migration — unchanged from 0087's list.
-- ---------------------------------------------------------------------------
-- Every table 0087 enumerated as deliberately uncovered stays uncovered for the same
-- measured reasons: the pre-identity authentication lookups, the append-only audit ledgers,
-- the cross-identity daemon tables (scheduler_tasks, pending_notifications,
-- retention_operation_items, storage_objects, provisioning_saga, telegram_setup_pending),
-- aura.assets/asset_events pending their constructor reshape, aura.document_ingest_jobs
-- which has no identity column at all, and the global control plane. Read 0087's footer for
-- the per-table reasoning; this migration adds nothing to that list and removes nothing
-- from it.
--
-- aura.context_rot_events is worth naming explicitly because this migration's Go half now
-- writes it inside an identity-scoped transaction: it still carries NO policy. It has no
-- identity column, only a conversation_id, and covering it needs the same parent-owner join
-- the turns policy uses — worth doing, but it is a separate table with its own callers and
-- was not measured in this pass.
