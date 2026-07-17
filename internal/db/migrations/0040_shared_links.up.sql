-- Source: Phase 37F (Conversation & Artifact Sharing / Export) / WEBSHARE-02 /
-- WEBSHARE-03 / D-11 / D-13 / D-14. Share create has nowhere to persist
-- tier/token/expiry/revocation, and an unaudited share is an SC3 violation — this
-- migration lands both tables together because a partial apply would let a share
-- be created without the audit trail that records it. `aura_migrate` owns DDL;
-- `aura_app` gets DML (asymmetric on share_audit — see below).

-- aura.shared_links persists one row per minted share (internal or public). The
-- shared_links_tier_shape CHECK makes D-04's mandatory-expiry and D-13's
-- hashed-token requirements DATABASE-enforced: a public link with no expiry, or
-- with no token hash, cannot exist even if a Go bug tries to write one — the CHECK
-- is the last line of defense, not the first.
CREATE TABLE aura.shared_links (
    id                uuid          PRIMARY KEY,
    owner_identity_id uuid          NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE,
    conversation_id   uuid          NOT NULL REFERENCES aura.conversations(id) ON DELETE CASCADE,
    tier              text          NOT NULL CHECK (tier IN ('internal', 'public')),
    -- token_hash is bytea, not text: SHA-256 is 32 raw bytes and bytea removes the
    -- hex-vs-base64 ambiguity (precedent: aura.identity_object_store.secret_key_enc
    -- bytea, 0030). This column holds SHA-256(token) ONLY — no plaintext token is
    -- stored anywhere (D-13).
    token_hash        bytea,
    snapshot_id       uuid          NOT NULL,
    snapshot_bucket   text          NOT NULL,
    format_options    jsonb         NOT NULL DEFAULT '{}'::jsonb,
    expires_at        timestamptz,
    revoked_at        timestamptz,
    created_at        timestamptz   NOT NULL DEFAULT now(),
    updated_at        timestamptz   NOT NULL DEFAULT now(),

    CONSTRAINT shared_links_tier_shape CHECK (
        (tier = 'public'   AND token_hash IS NOT NULL AND expires_at IS NOT NULL)
     OR (tier = 'internal' AND token_hash IS NULL)
    )
);

-- This unique partial index is BOTH the duplicate-mint guard AND the D-13
-- hash-indexed-equality lookup index (`WHERE token_hash = $1`), so a public-link
-- resolve is an index probe and never a table scan.
CREATE UNIQUE INDEX shared_links_token_hash_idx  ON aura.shared_links (token_hash) WHERE token_hash IS NOT NULL;
CREATE INDEX        shared_links_owner_idx       ON aura.shared_links (owner_identity_id, created_at DESC);
CREATE INDEX        shared_links_conversation_idx ON aura.shared_links (conversation_id);
-- The sweep's due-set index: only non-revoked links are ever candidates for expiry.
CREATE INDEX        shared_links_expiry_idx      ON aura.shared_links (expires_at) WHERE revoked_at IS NULL;

-- aura.share_audit is the append-only ledger every share create/update/revoke/
-- expire/open records (T-37F-10, Repudiation defense). Three deliberate
-- non-obvious choices:
-- (a) identity_id is text with NO FK — matching skill_audit (0010) / mcp_audit
--     (0022) verbatim. An FK with ON DELETE CASCADE would destroy the audit trail
--     the moment an identity is deleted, the exact opposite of a ledger's purpose.
--     text is also what lets this column hold the literal 'local' that
--     auditIdentityKeys (internal/agui/audit_store.go:99) appends for the seeded
--     operator identity.
-- (b) share_link_id and conversation_id carry NO FK — the audit outlives both the
--     link and the conversation it describes.
-- (c) the ledger is append-only (grants below omit UPDATE/DELETE for aura_app).
CREATE TABLE aura.share_audit (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id     text        NOT NULL,
    share_link_id   uuid,
    conversation_id uuid,
    action          text        NOT NULL CHECK (action IN ('create', 'update', 'revoke', 'expire', 'open')),
    tier            text,
    detail          text        NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX share_audit_identity_idx ON aura.share_audit (identity_id, created_at DESC);

GRANT SELECT, INSERT, UPDATE, DELETE ON aura.shared_links TO aura_app;
GRANT ALL                            ON aura.shared_links TO aura_migrate;
-- Asymmetric on purpose: aura_app has SELECT + INSERT ONLY on share_audit, never
-- UPDATE/DELETE. The asymmetry IS the audit-integrity statement, not an oversight
-- — a ledger a writer can edit is not a ledger.
GRANT SELECT, INSERT                 ON aura.share_audit  TO aura_app;
GRANT ALL                            ON aura.share_audit  TO aura_migrate;

-- Scheduler kind widen (mirrors 0034's drop+re-add verbatim, appending the one new
-- member). The 0009 constraint is an inline column CHECK, so Postgres auto-named
-- it `scheduler_tasks_kind_check`; the D-14 share_expiry_sweep task needs it
-- widened to be INSERTable into aura.scheduler_tasks.
ALTER TABLE aura.scheduler_tasks DROP CONSTRAINT scheduler_tasks_kind_check;
ALTER TABLE aura.scheduler_tasks ADD  CONSTRAINT scheduler_tasks_kind_check
    CHECK (kind IN ('reminder', 'agent_job', 'backup_postgres', 'backup_neo4j', 'skill_ttl_sweep', 'identity_purge', 'sandbox_reap', 'share_expiry_sweep'));

COMMENT ON TABLE aura.shared_links IS
    'Conversation/artifact share links (Phase 37F, WEBSHARE-02/03). shared_links_tier_shape makes D-04 mandatory-expiry + D-13 hashed-token database-enforced for public tier; shared_links_token_hash_idx is the unique lookup index (never a table scan).';
COMMENT ON TABLE aura.share_audit IS
    'Append-only share-lifecycle audit ledger (Phase 37F, T-37F-10). aura_app has SELECT+INSERT only; identity_id is text with NO FK so the ledger outlives the identity, link, and conversation it describes.';
