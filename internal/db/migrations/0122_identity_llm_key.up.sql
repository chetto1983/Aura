-- Source: PRD Phase 45+ Slice 3 (CRED-01, D-12) — per-identity OpenRouter key store.
-- Migration floor before this is 0121 (retire_capability_wildcard, this same plan); this
-- lands at 0122, measured from `ls internal/db/migrations/ | tail -1` at landing time.
--
-- Structured on internal/mcpoauth's aura.identity_mcp_oauth (0100): AES-256-GCM
-- ciphertext at rest, RLS-scoped through db.WithIdentityTx, ON DELETE CASCADE on the
-- owning identity. key_hash is deliberately PLAINTEXT (02-RESEARCH.md): OpenRouter's
-- hash addresses the key for PATCH/DELETE and appears in the provider's own error
-- messages, so a revoke must run without decrypting anything. key_label holds
-- OpenRouter's masked form (sk-or-v1-caa...61c), safe to display, and is what the
-- cockpit shows without ever touching the ciphertext.

CREATE TABLE aura.identity_llm_key (
    identity_id    uuid        PRIMARY KEY REFERENCES aura.identities (id) ON DELETE CASCADE,
    key_ciphertext bytea       NOT NULL,
    key_hash       text        NOT NULL,
    key_label      text        NOT NULL,
    limit_usd      numeric     NOT NULL DEFAULT 0,
    limit_reset    text        NOT NULL DEFAULT 'monthly',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

-- aura_app reads the key at turn-resolve time, writes it after the provisioning-API
-- mint (a later plan), updates it on rotation, and deletes it on revoke.
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.identity_llm_key TO aura_app;
GRANT ALL                           ON aura.identity_llm_key TO aura_migrate;

-- Both RLS layers from 0087, in the same order and with the same predicates as
-- 0100_identity_mcp_oauth.up.sql. The permissive policy scopes rows to their owner;
-- the RESTRICTIVE floor denies a caller with no app.current_identity set.
ALTER TABLE aura.identity_llm_key ENABLE ROW LEVEL SECURITY;

CREATE POLICY identity_llm_key_owner_isolation ON aura.identity_llm_key
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);

CREATE POLICY identity_llm_key_requires_identity ON aura.identity_llm_key
    AS RESTRICTIVE FOR ALL TO aura_app
    USING (current_setting('app.current_identity', true) IS NOT NULL
           AND current_setting('app.current_identity', true) <> '');

COMMENT ON POLICY identity_llm_key_requires_identity ON aura.identity_llm_key IS
    'Fail-closed floor (migration 0087 pattern): AND-combined with every permissive policy, so no later permissive policy can restore visibility to a caller with no app.current_identity. Set it via internal/db.WithIdentityTx / WithIdentityTxRaw.';

COMMENT ON TABLE aura.identity_llm_key IS
    'Per-identity OpenRouter API key (migration 0122, CRED-01). key_ciphertext is AES-256-GCM ciphertext (KEK derived from AURA_AUTHULA_SECRET with an info string domain-separated from internal/mcpoauth). key_hash is plaintext by design: OpenRouter addresses the key by hash for PATCH/DELETE and surfaces it in provider error messages, so a revoke never needs to decrypt.';
