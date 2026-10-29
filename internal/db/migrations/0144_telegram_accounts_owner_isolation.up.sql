-- 0144: row security on aura.telegram_accounts, and the function the legacy `local`
-- retirement moves its rows through.
-- docs/superpowers/specs/2026-10-27-telegram-account-links-design.md (D1, D3).
--
-- 0087 left the links without a policy because GetAccountByTelegramID is the inbound lookup
-- that PRODUCES a Telegram turn's identity, so a policy that needs one would deny it. That
-- holds for reads with no identity, and the widening below keeps exactly those. It does not
-- hold for the owner's list and unlink: measured 2026-10-27, as aura_app bound to identity A,
-- a SELECT returned identity B's link and a DELETE by its key removed it.
--
-- The shape is 0089's aura.shared_links: owner isolation for every command, a SELECT-only
-- widening for a session with no app.current_identity, and a restrictive floor that holds
-- whatever permissive policy a later migration adds. The widening is a separate FOR SELECT
-- policy so a session with no identity can read every link but change none: an UPDATE or
-- DELETE has only owner isolation to pass, and with no identity it matches no row.

ALTER TABLE aura.telegram_accounts ENABLE ROW LEVEL SECURITY;

CREATE POLICY telegram_accounts_owner_isolation ON aura.telegram_accounts
    FOR ALL
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid)
    WITH CHECK (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);

CREATE POLICY telegram_accounts_pre_identity_read ON aura.telegram_accounts
    FOR SELECT
    USING (NULLIF(current_setting('app.current_identity', true), '') IS NULL);

CREATE POLICY telegram_accounts_owner_or_pre_identity ON aura.telegram_accounts
    AS RESTRICTIVE FOR ALL TO aura_app
    USING (
        identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid
        OR NULLIF(current_setting('app.current_identity', true), '') IS NULL
    )
    WITH CHECK (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);

COMMENT ON POLICY telegram_accounts_owner_isolation ON aura.telegram_accounts IS
    'Owner isolation (migration 0144) for every command: a link is read, inserted, updated or deleted only under the identity that owns it.';
COMMENT ON POLICY telegram_accounts_pre_identity_read ON aura.telegram_accounts IS
    'SELECT-only widening (migration 0144) for a session with NO app.current_identity: the inbound lookup, scheduler delivery, the setup count, the password reset and the recovery resolve a link before any identity is known. Do NOT extend this to a write command.';
COMMENT ON POLICY telegram_accounts_owner_or_pre_identity ON aura.telegram_accounts IS
    'Floor (migration 0144): a read reaches a row the caller owns, or any row for a caller with no identity; a write reaches only a row the caller owns. Holds whatever permissive policy a later migration adds.';

-- Moving a row to another owner is two identities by definition: USING needs the old owner
-- and WITH CHECK the new one, so no single app.current_identity authorizes it (0087, 0090).
-- With no identity bound, the retirement's UPDATEs on conversations and assets matched no row,
-- and deleting `local` then cascaded the rows they left behind, past row security. This
-- function runs as its owner, aura_migrate, who owns these tables and so bypasses their row
-- security ("Table owners normally bypass row security as well", PostgreSQL manual, Row
-- Security Policies). search_path is pinned because a SECURITY DEFINER function must not
-- resolve a name through a caller-controlled path (CREATE FUNCTION, "Writing SECURITY DEFINER
-- Functions Safely").
--
-- It is not a primitive that moves any tenant's rows: it finds the source itself, the
-- identity named `local` of kind `system`, and refuses a target that is not of kind `user`.
--
-- The assets, their ingestion jobs and the jobs' events move in ONE statement. ingestion_jobs
-- references assets (id, identity_id), and ingestion_events references ingestion_jobs
-- (id, identity_id), with no ON UPDATE action: measured 2026-10-28, a lone UPDATE of the
-- asset's owner fails on ingestion_jobs_asset_identity_fkey, while one statement whose WITH
-- clauses move all three passes, because the foreign key checks run when the statement ends.
-- A WITH clause that modifies data runs to completion whether or not its output is read
-- (PostgreSQL manual, WITH Queries, "Data-Modifying Statements in WITH").
CREATE FUNCTION aura.retire_legacy_local_rows(target uuid) RETURNS void
    LANGUAGE plpgsql
    SECURITY DEFINER
    SET search_path = pg_catalog
    AS $$
DECLARE
    source uuid;
BEGIN
    SELECT id INTO source FROM aura.identities WHERE name = 'local' AND kind = 'system';
    IF source IS NULL THEN
        RETURN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM aura.identities WHERE id = target AND kind = 'user') THEN
        RAISE EXCEPTION 'retire_legacy_local_rows: % is not a user identity', target
            USING ERRCODE = 'invalid_parameter_value';
    END IF;

    UPDATE aura.conversations SET identity_id = target WHERE identity_id = source;

    WITH moved_assets AS (
        UPDATE aura.assets SET identity_id = target WHERE identity_id = source
    ), moved_jobs AS (
        UPDATE aura.ingestion_jobs SET identity_id = target WHERE identity_id = source
    )
    UPDATE aura.ingestion_events SET identity_id = target WHERE identity_id = source;

    UPDATE aura.assets
       SET object_key = replace(object_key, source::text, target::text)
     WHERE identity_id = target
       AND object_key LIKE '%' || source::text || '%'
       AND status IN ('deleted', 'canceled', 'failed', 'refused');

    UPDATE aura.telegram_accounts SET identity_id = target WHERE identity_id = source;
END;
$$;

REVOKE ALL ON FUNCTION aura.retire_legacy_local_rows(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION aura.retire_legacy_local_rows(uuid) TO aura_app;

COMMENT ON FUNCTION aura.retire_legacy_local_rows(uuid) IS
    'Migration 0144: moves the legacy local identity''s conversations, assets (with their ingestion jobs and events, and the deleted-key scrub) and Telegram links to a user identity, past the row security a single app.current_identity cannot satisfy. Called by cmd/aura retireLegacyLocalIdentity before it deletes local.';
