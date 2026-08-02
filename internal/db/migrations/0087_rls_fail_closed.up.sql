-- Make row-level security fail CLOSED on the identity-scoped tables that can carry an
-- identity today, and stop the bleeding on the ones that cannot until they can.
--
-- MEASURED 2026-08-02 on the live deployment, connected as aura_app (the real
-- application role — not superuser, not BYPASSRLS, so RLS genuinely applies to it) with
-- NO app.current_identity set: all 32 conversations, all 648 turns and every document
-- were readable. The cause is the escape hatch every policy shipped with:
--
--     NULLIF(current_setting('app.current_identity', true), '') IS NULL OR identity_id = ...
--
-- An unset session variable makes the first branch TRUE and the whole table visible.
-- 0032 and 0041 chose that deliberately ("permissive-on-UNSET") because the write paths
-- did not yet carry a principal.
--
-- WHAT THIS MIGRATION DOES, and equally important, WHAT IT DOES NOT.
--
-- It brings under fail-closed RLS the identity-scoped tables whose Go call sites all have
-- an identity in hand: the document plane, capability grants, content parts, the
-- idempotency registry and the per-identity object-store binding. These had NO policy at
-- all before this migration, so they were readable and writable by any aura_app
-- connection regardless of principal.
--
-- Each covered table gets two policies:
--
--   1. One permissive owner policy with NO `IS NULL OR` escape. Enabling RLS is itself
--      most of the win: "If no policy exists for the table, a default-deny policy is
--      used" (PostgreSQL manual, ALTER TABLE / Row Security).
--   2. One `AS RESTRICTIVE FOR ALL TO aura_app` policy requiring an identity to be set.
--      "All restrictive policies which are applicable to a given query will be combined
--      together using the Boolean AND operator" (PostgreSQL manual, CREATE POLICY) —
--      permissive policies are OR'd with each other but AND'd with every restrictive
--      one, so no permissive policy added later can widen past the identity requirement.
--      That is the durable part: it makes the failure mode of a future mistake "too few
--      rows", never "all rows". WITH CHECK is left unspecified and therefore defaults to
--      USING, which is what closes the write side.
--
-- IT DELIBERATELY DOES NOT TIGHTEN aura.conversations, aura.conversation_turns,
-- aura.paused_states OR aura.shared_links, and that is the single biggest thing a reader
-- of this file needs to know. Their 0032/0041 permissive-on-unset policies are left
-- exactly as they were. The reason is measured, not assumed: the runner turn loop writes
-- both tables on EVERY turn with no principal set —
--     internal/runner/runner.go:453             AppendTurn on the raw store
--     internal/runner/runner_conversation.go:38 Create on the raw store
--     internal/runner/resume_committer.go:170   AppendTurn inside a plain db.WithTx
--     internal/agui/server.go:475               LoadHistory on the raw store
-- and running the db_integration tier against a database with those four policies
-- tightened fails in the hundreds, across internal/agui, internal/askuser,
-- internal/breakglass and internal/conversations, every failure a
-- "new row violates row-level security policy" on a write the product depends on.
--
-- The fix is known and is NOT a policy change: the identity is ALREADY on the context at
-- every one of those call sites (internal/runner/runner.go:448 derives it from the
-- conversation owner, internal/agui/auth.go:309 from the authenticated principal,
-- cmd/aura/chat_boot.go:106 for the CLI, internal/channels/telegram/bot_dispatch_turn.go:141
-- for Telegram). What is missing is that conversations.Store and askuser.Store never pass
-- it to db.WithIdentityTx. Threading it through those two stores — and through the test
-- seed helpers that currently insert conversations with no principal — is the follow-up
-- that lets a later migration delete the `IS NULL OR` from those four policies. Doing it
-- here would have meant shipping a migration whose blast radius is the entire agent
-- runtime.
--
-- WHY ENABLE, NOT FORCE (unchanged from 0032/0041): aura_migrate OWNS these tables and a
-- table owner bypasses RLS by default, which is exactly what golang-migrate needs to keep
-- running DDL and backfills. aura_app is a non-owner, non-superuser, non-BYPASSRLS role,
-- so ENABLE already applies RLS to the runtime pool. FORCE would add nothing and would
-- break migrations. That aura_app really lacks the bypass is asserted live by
-- TestAuraAppLacksRLSBypass, and from this phase the daemon itself refuses to boot on a
-- role with rolsuper/rolbypassrls (internal/db.VerifyRLSEnforced) — SQL cannot defend
-- against being run as superuser, so that check has to live in Go.
--
-- TEXT vs UUID: identity_id is `text` on scheduler_tasks, pending_notifications and
-- retention_operation_items and `uuid` everywhere else. None of the text-keyed tables is
-- covered here (see the uncovered list at the foot of this file), so every predicate
-- below compares uuid to uuid. Casting a free-text column to uuid would make the policy
-- ERROR on the first non-UUID row instead of filtering it — aura.skill_audit already
-- stores the literal 'local', so that is a live hazard, not a hypothetical.

-- ---------------------------------------------------------------------------
-- Permissive owner policies. Written out one table at a time rather than generated:
-- a reader auditing this file must see the exact predicate each table got, and the key
-- column is identity_id on most tables but owner_id on content_parts.
-- ---------------------------------------------------------------------------

ALTER TABLE aura.documents ENABLE ROW LEVEL SECURITY;
CREATE POLICY documents_owner_isolation ON aura.documents
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);

ALTER TABLE aura.capability_grants ENABLE ROW LEVEL SECURITY;
CREATE POLICY capability_grants_owner_isolation ON aura.capability_grants
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);

ALTER TABLE aura.content_parts ENABLE ROW LEVEL SECURITY;
CREATE POLICY content_parts_owner_isolation ON aura.content_parts
    USING (owner_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);

ALTER TABLE aura.idempotency_operations ENABLE ROW LEVEL SECURITY;
CREATE POLICY idempotency_operations_owner_isolation ON aura.idempotency_operations
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);

ALTER TABLE aura.identity_object_store ENABLE ROW LEVEL SECURITY;
CREATE POLICY identity_object_store_owner_isolation ON aura.identity_object_store
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);

-- Child tables that carry no identity column of their own. Each names its parent's owner
-- DIRECTLY rather than relying on the parent's policy to filter the subquery. That
-- indirection is the mistake aura.conversation_turns shipped with in 0032: its policy only
-- checked that the parent row EXISTS and leaned on conversations_owner_isolation to filter
-- it, so when the parent failed open the child failed open with it — two layers that ceded
-- together. All of these join on a uuid FK, so there is no cast hazard.

ALTER TABLE aura.document_versions ENABLE ROW LEVEL SECURITY;
CREATE POLICY document_versions_owner_isolation ON aura.document_versions
    USING (EXISTS (
        SELECT 1 FROM aura.documents d
        WHERE d.id = document_id
          AND d.identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid
    ));

ALTER TABLE aura.document_chunks ENABLE ROW LEVEL SECURITY;
CREATE POLICY document_chunks_owner_isolation ON aura.document_chunks
    USING (EXISTS (
        SELECT 1 FROM aura.documents d
        WHERE d.id = document_id
          AND d.identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid
    ));

ALTER TABLE aura.document_embeddings ENABLE ROW LEVEL SECURITY;
CREATE POLICY document_embeddings_owner_isolation ON aura.document_embeddings
    USING (EXISTS (
        SELECT 1 FROM aura.documents d
        WHERE d.id = document_id
          AND d.identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid
    ));

ALTER TABLE aura.document_tags ENABLE ROW LEVEL SECURITY;
CREATE POLICY document_tags_owner_isolation ON aura.document_tags
    USING (EXISTS (
        SELECT 1 FROM aura.documents d
        WHERE d.id = document_id
          AND d.identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid
    ));

ALTER TABLE aura.content_part_links ENABLE ROW LEVEL SECURITY;
CREATE POLICY content_part_links_owner_isolation ON aura.content_part_links
    USING (EXISTS (
        SELECT 1 FROM aura.content_parts p
        WHERE p.id = part_id
          AND p.owner_id = NULLIF(current_setting('app.current_identity', true), '')::uuid
    ));

-- ---------------------------------------------------------------------------
-- The restrictive identity-required floor, one policy per covered table.
-- ---------------------------------------------------------------------------
-- This is the only genuinely uniform part of the migration — the predicate is
-- byte-identical for every table — so it is generated from one array instead of being
-- copy-pasted twelve times. The array IS the auditable list of what is covered.

DO $$
DECLARE
    target text;
BEGIN
    FOREACH target IN ARRAY ARRAY[
        'documents',
        'document_versions',
        'document_chunks',
        'document_embeddings',
        'document_tags',
        'capability_grants',
        'content_parts',
        'content_part_links',
        'idempotency_operations',
        'identity_object_store'
    ]
    LOOP
        -- Dollar-quoted so the predicate reads exactly as it will be created; the
        -- doubled-quote spelling of the same string is unreadable and was wrong twice.
        EXECUTE format($fmt$
            CREATE POLICY %I ON aura.%I AS RESTRICTIVE FOR ALL TO aura_app
            USING (current_setting('app.current_identity', true) IS NOT NULL
                   AND current_setting('app.current_identity', true) <> '')
        $fmt$, target || '_requires_identity', target);
        EXECUTE format(
            'COMMENT ON POLICY %I ON aura.%I IS %L',
            target || '_requires_identity', target,
            $doc$Fail-closed floor (migration 0087): AND-combined with every permissive policy, so no later permissive policy can restore visibility to a caller with no app.current_identity. Set it via internal/db.WithIdentityTx / WithIdentityTxRaw.$doc$);
    END LOOP;
END
$$;

-- ---------------------------------------------------------------------------
-- DELIBERATELY LEFT UNCOVERED — each for a reason, not by omission.
-- ---------------------------------------------------------------------------
-- ALREADY COVERED BUT STILL PERMISSIVE-ON-UNSET, pending the write-path work described
-- in the header: aura.conversations, aura.conversation_turns, aura.paused_states,
-- aura.shared_links. shared_links additionally has two documented resolvers that read
-- rows the caller does not own — ResolveByToken (anonymous public tier, no identity
-- exists) and ResolveLiveByID (D-10 internal tier, ANY authenticated user may open
-- someone else's link) — so when it is tightened its predicate must be "mine OR live",
-- not "mine", or both routes break.
--
-- AUTHENTICATION, pre-identity. A login looks a token up BEFORE it knows who the user is;
-- a fail-closed policy here breaks authentication by construction — the lookup that would
-- establish the identity is the very lookup the policy would deny.
--   aura.password_reset_tokens, aura.password_reset_challenges,
--   aura.identity_recovery, aura.identity_auth_links
-- aura.telegram_accounts belongs to this family too: telegram.Store.GetAccountByTelegramID
-- is the inbound-message lookup that PRODUCES the identity for a Telegram turn.
--
-- APPEND-ONLY AUDIT LEDGERS. Their append-only property is enforced by grants, and an
-- audit trail that filtered itself by owner would stop being usable for cross-identity
-- forensics — the opposite of what an audit table is for. This continues the precedent
-- 0041 set explicitly for share_audit.
--   aura.share_audit, aura.skill_audit, aura.mcp_audit, aura.audit_logs,
--   aura.identity_audit, aura.identity_recovery_audit, aura.tool_invocations
-- (skill_audit and share_audit also store the literal 'local' in a text identity_id.)
--
-- CROSS-IDENTITY BY CONSTRUCTION — a daemon that must see every tenant's rows to do its
-- job. A per-request identity cannot express these; they need a service role with an
-- explicit carve-out, which is a design decision and not a policy:
--   aura.scheduler_tasks + aura.pending_notifications — the cron dispatcher's DueTasks /
--     SweepDueNotifications / ListActiveTasks / ListDueApprovalReminders scan all owners.
--   aura.retention_operation_items — retention.Store.SavePlan writes one plan spanning
--     many identities in a single transaction; Claim leases across owners.
--   aura.storage_objects — documents.ListStorageObjects is bucket/prefix scoped for
--     orphan GC.
--   aura.provisioning_saga — SagaJournal.CompletedSteps takes only a sagaID.
--   aura.telegram_setup_pending — ConsumeOnboarding only learns the identity from the
--     row it is consuming, inside the transaction that consumes it.
-- cmd/aura/serve_auth.go:189 (retireLegacyLocalIdentityForAuthulaUser) is the sharpest
-- case: it MOVES rows from the legacy `local` identity to the enrolled one, so it is
-- two-identity by definition and no single set_config can authorize it.
--
-- ONE RESHAPE AWAY. aura.assets and aura.asset_events are ready except for a constructor:
-- all ten assets.Store methods already take identityID, but assets.NewStore takes a bare
-- sqlc.DBTX and so has no pool to open an identity transaction on. Change it to
-- *pgxpool.Pool, wrap the ten methods in db.WithIdentityTx, and these two tables can join
-- the list above unchanged. Left out here only because that reshape could not be verified
-- green in the same pass.
--
-- NO IDENTITY TO FILTER ON. aura.document_ingest_jobs has no identity column, no foreign
-- key to aura.documents, and a TEXT document_id that is not a uuid — and its Go store
-- (documents.PostgresJobStore) carries no identity in ANY method signature. Covering it
-- needs an owner threaded through the ingestion pipeline first: a schema plus API change,
-- not a policy. Left uncovered and reported rather than half-covered behind a cast that
-- would error on the first non-uuid row.
--
-- GLOBAL / CONTROL PLANE, not user data: aura.identities (the login lookup itself),
-- aura.settings, aura.knowledge_migrations, aura.cache_metrics, aura.context_rot_events,
-- aura.retention_operations, aura.ingestion_jobs, aura.ingestion_events,
-- aura.agent_job_runs, aura.delete_jobs, aura.benchmark_settings_overrides and
-- aura.benchmark_settings_override_rows (the eight sqlc queries behind the last two have
-- no callers at all).
