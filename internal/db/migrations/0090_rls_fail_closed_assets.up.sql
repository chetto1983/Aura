-- Bring aura.assets and aura.asset_events under fail-closed row-level security.
--
-- These are the two tables migration 0087 listed as "ONE RESHAPE AWAY": all ten
-- assets.Store methods already took an identityID, but assets.NewStore took a bare
-- sqlc.DBTX and so had no pool to open an identity-scoped transaction on. 0087 left them
-- out rather than ship a policy the store could not satisfy, and 0089 repeated the entry
-- verbatim in its own footer.
--
-- The Go half lands in the SAME change as this file, which is the rule PRD amendment #107
-- exists to state: a fail-closed migration must land WITH the identity plumbing of every
-- store it covers, never before it. 0087 covered ten tables ahead of their stores and the
-- live deployment took an outage — every idempotency Begin failed 42501 — which cost two
-- repair commits (7974f8a6a, f09fa5c99). assets.NewStore now type-asserts *pgxpool.Pool and
-- every method runs inside db.WithIdentityTx (internal/assets/store_identity.go).
--
-- UNLIKE 0087 and 0089, THIS MIGRATION ENABLES ROW SECURITY RATHER THAN REPLACING A
-- PREDICATE. Measured on the live deployment on 2026-08-03:
--
--     relname      | relrowsecurity | relforcerowsecurity | policies
--     assets       | false          | false               | 0
--     asset_events | false          | false               | 0
--
-- So there was no fail-OPEN escape hatch to remove here: there was no row security at all.
-- Every asset of every identity was readable and writable by any aura_app connection, and
-- the only thing standing between two operators' uploads was the WHERE identity_id clause
-- in each of the ten hand-written store methods. That is precisely the single layer D-07
-- says must have a backstop underneath it.
--
-- WHY ENABLE, NOT FORCE (unchanged from 0032/0041/0087/0089): aura_migrate OWNS both tables
-- (verified above) and a table owner bypasses RLS by default, which is what golang-migrate
-- needs to keep running DDL and backfills. aura_app is a non-owner, non-superuser,
-- non-BYPASSRLS role, so ENABLE already applies row security to the runtime pool.
--
-- The shape per table is 0087's, unchanged:
--
--   1. One permissive owner policy with NO `IS NULL OR` escape. (PostgreSQL manual, ALTER
--      TABLE / Row Security: "If no policy exists for the table, a default-deny policy is
--      used" — enabling RLS is itself most of the win.)
--   2. One `AS RESTRICTIVE FOR ALL TO aura_app` floor requiring the setting to be non-empty.
--      (CREATE POLICY: "All restrictive policies which are applicable to a given query will
--      be combined together using the Boolean AND operator".) Permissive policies OR with
--      each other but AND with every restrictive one, so no permissive policy added later
--      can widen past the identity requirement — the failure mode of a future mistake
--      becomes "too few rows", never "all rows". WITH CHECK is left unspecified and
--      therefore defaults to USING, which closes the write side.

-- ---------------------------------------------------------------------------
-- aura.assets — the direct owner policy on the NOT NULL identity_id column.
-- ---------------------------------------------------------------------------
ALTER TABLE aura.assets ENABLE ROW LEVEL SECURITY;

CREATE POLICY assets_owner_isolation ON aura.assets
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);

-- ---------------------------------------------------------------------------
-- aura.asset_events — name the parent's OWNER, not merely the parent.
-- ---------------------------------------------------------------------------
-- asset_events carries NO identity column of its own (migration 0020: asset_id, seq,
-- from_status, to_status, reason, detail, created_at). It can only be scoped through its
-- parent, and the shape of that join is the one thing in this migration that is easy to get
-- wrong: the tempting predicate is `EXISTS (SELECT 1 FROM aura.assets a WHERE a.id =
-- asset_id)`, which checks only that the parent row is VISIBLE and leans on
-- assets_owner_isolation to do the filtering.
--
-- That indirection is what 0032 did to conversation_turns and what 0089 had to undo: two
-- layers that cede together are one layer, and when the parent failed open the child failed
-- open with it. So the owner is named HERE, directly, and this policy stays correct even if
-- a later migration loosens the parent's. The join is on a uuid FK, so there is no cast
-- hazard.
--
-- The subquery is additionally filtered by assets_owner_isolation, because an RLS policy is
-- SECURITY INVOKER and its subqueries are subject to the referenced table's row security.
-- Both predicates demand the same identity, so that is belt and braces, not a conflict.
--
-- Note that INSERT into this table keeps working: the FK to aura.assets is checked by
-- PostgreSQL's referential-integrity triggers, and the manual (5.9 Row Security Policies)
-- states that "referential integrity checks, such as unique or primary key constraints and
-- foreign key references, always bypass row security to ensure that data integrity is
-- maintained". The parent lookup a write performs is therefore not the one this policy
-- filters; the row the write LANDS is.
ALTER TABLE aura.asset_events ENABLE ROW LEVEL SECURITY;

CREATE POLICY asset_events_owner_isolation ON aura.asset_events
    USING (EXISTS (
        SELECT 1 FROM aura.assets a
        WHERE a.id = asset_id
          AND a.identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid
    ));

-- ---------------------------------------------------------------------------
-- The restrictive identity-required floor.
-- ---------------------------------------------------------------------------
-- Generated from one array, exactly as 0087 and 0089 do: the predicate is byte-identical
-- for both tables and the array IS the auditable list of what this migration covers.
DO $$
DECLARE
    target text;
BEGIN
    FOREACH target IN ARRAY ARRAY[
        'assets',
        'asset_events'
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
            $doc$Fail-closed floor (migration 0090): AND-combined with every permissive policy, so no later permissive policy can restore visibility to a caller with no app.current_identity. Set it via internal/db.WithIdentityTx / WithIdentityTxRaw / WithCallerIdentityTx.$doc$);
    END LOOP;
END
$$;

COMMENT ON POLICY assets_owner_isolation ON aura.assets IS
    'Fail-closed owner isolation (migration 0090): no permissive-on-unset branch. Set the identity via internal/db.WithIdentityTx — assets.Store does it on every method.';
COMMENT ON POLICY asset_events_owner_isolation ON aura.asset_events IS
    'Fail-closed owner isolation (migration 0090) naming the parent asset OWNER directly, not merely the parent row: asset_events has no identity column of its own.';

-- ---------------------------------------------------------------------------
-- THE TWO CALLERS OUTSIDE internal/assets, AND WHY ONLY ONE OF THEM IS FINE.
-- ---------------------------------------------------------------------------
-- documents.catalogTx.softDeleteDocumentAssets (internal/documents/catalog_store.go) issues
-- a raw `UPDATE aura.assets ... WHERE identity_id = $1` from INSIDE the identity-scoped
-- transaction catalogTx already opens, and $1 is the same identity bound to
-- app.current_identity by PostgresCatalogStore.scoped. It needs no change and gets none:
-- the row it targets is the row assets_owner_isolation admits.
--
-- cmd/aura/serve_auth.go's retireLegacyLocalIdentityForAuthulaUser is NOT fine, and it is
-- NOT made worse by this migration — it is already broken, one table earlier. 0087's footer
-- named it the sharpest case in the codebase: it MOVES rows from the legacy `local` identity
-- to the enrolled one, so it is two-identity BY DEFINITION and no single set_config can
-- authorize it. Setting the identity to the source lets USING find the rows but makes
-- WITH CHECK reject the new owner; setting it to the target hides the rows entirely.
--
-- It runs on chat.pool, which is db.Open over AURA_DB_URL — the aura_app runtime role. Its
-- FIRST statement is `UPDATE aura.conversations SET identity_id=...`, and aura.conversations
-- has had a fail-closed floor since 0089. With no app.current_identity that UPDATE already
-- matches zero rows and silently migrates nothing; RLS filters an UPDATE rather than
-- erroring on it, which is why nothing surfaced. This migration adds aura.assets to the set
-- of statements in that function that no-op, taking it from partially silent to consistently
-- silent. Fixing it needs the migrate-role pool or a two-phase owner handoff — a change with
-- its own design and its own blast radius, sitting in the conversations plane rather than
-- this one. Reported, not half-fixed here.

-- ---------------------------------------------------------------------------
-- STILL UNCOVERED after this migration.
-- ---------------------------------------------------------------------------
-- 0087's list minus the two tables this file just claimed. aura.assets and
-- aura.asset_events come off it; everything else stays for the reasons 0087 measured — the
-- pre-identity authentication lookups, the append-only audit ledgers, the cross-identity
-- daemon tables (scheduler_tasks, pending_notifications, retention_operation_items,
-- storage_objects, provisioning_saga, telegram_setup_pending), aura.document_ingest_jobs
-- which has no identity column at all, and the global control plane.
--
-- aura.context_rot_events, which 0089 called out by name, is likewise unchanged: still no
-- policy, still only a conversation_id, still needing the same parent-owner join used above.
