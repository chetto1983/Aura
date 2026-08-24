-- Source: measured 2026-08-24 against go-sdk v1.7.0 auth.AuthorizationCodeHandler and
-- against the two multi-user MCP hosts that already solved this (LibreChat
-- packages/api/src/mcp/oauth/tokens.ts, Hermes tools/mcp_oauth.py). The migration floor
-- before this is 0099 (gateway_approval_grants); the per-identity MCP OAuth token store
-- lands at 0100.
--
-- WHY A TABLE AND NOT A FILE. Hermes keeps these as JSON under HERMES_HOME/mcp-tokens/
-- because it is one agent on one laptop. LibreChat keeps them per user in its database
-- because it serves many people, and that is the shape Aura needs: a remote MCP server
-- (Slack, Notion, Linear, Atlassian) issues a token that identifies ONE person, so the
-- token is not deployment configuration and must never be readable by another identity.
--
-- THREE SECRETS, NOT ONE. LibreChat encrypts access token, refresh token and the
-- DCR-issued client information as three separate ciphertexts, and the third is the one
-- that is easy to miss: dynamic client registration returns a client_secret minted by the
-- authorization server, so the registration result is itself a credential. Columns are
-- nullable because each is genuinely optional -- a server may issue no refresh token, and
-- a server with a pre-registered client (Slack, GitHub) never produces a DCR result at
-- all.
--
-- WHAT IS *NOT* HERE. The operator's own pre-registered client_id/client_secret. Those are
-- deployment configuration with the lifetime of the deployment, not of an identity, and
-- both reference implementations keep them in the config file next to every other
-- credential (Hermes: the `oauth:` block; LibreChat: librechat.yaml). In Aura they ride
-- ManagedServer.Env, which httpAuthFromEnv already reads and internal/secret already
-- redacts. Putting them here would tie an operator-scoped secret to an identity row and
-- lose it on identity delete.
--
-- expires_at IS ABSOLUTE, and this is a measured trap rather than a preference. The OAuth
-- token shape carries a RELATIVE expires_in, which has no wall-clock reference once the
-- process restarts: Hermes documents (mcp_oauth.py, "Fix A") that reloading the relative
-- value leaves the SDK with no expiry and is_token_valid() then falsely reports true,
-- so a dead token is used until the first 401. Storing the absolute instant is what makes
-- a restart able to reconstruct the remaining TTL.

CREATE TABLE aura.identity_mcp_oauth (
    identity_id       uuid        NOT NULL REFERENCES aura.identities (id) ON DELETE CASCADE,
    server_name       text        NOT NULL,
    -- The endpoint the token was issued FOR. A token minted for one URL must never be
    -- replayed against another, so a server re-pointed at a different endpoint has to
    -- re-authorize rather than silently reuse the old grant.
    resource_url      text        NOT NULL,
    access_token_enc  bytea       NOT NULL,
    refresh_token_enc bytea,
    client_info_enc   bytea,
    token_type        text        NOT NULL DEFAULT 'Bearer',
    scopes            text[]      NOT NULL DEFAULT '{}',
    expires_at        timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (identity_id, server_name)
);

-- aura_app reads the token at mount, writes it after the authorization exchange, updates
-- it on refresh, and deletes it on revoke -- all four, unlike identity_object_store, whose
-- rotation is delete-then-insert. A refresh legitimately rewrites the same row.
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.identity_mcp_oauth TO aura_app;
GRANT ALL                           ON aura.identity_mcp_oauth TO aura_migrate;

-- Both RLS layers from 0087, in the same order and with the same predicates. The
-- permissive policy scopes rows to their owner; the RESTRICTIVE floor is what makes a
-- caller with no app.current_identity see NOTHING rather than everything. Adding only the
-- first would be the failure 0087 exists to prevent.
ALTER TABLE aura.identity_mcp_oauth ENABLE ROW LEVEL SECURITY;

CREATE POLICY identity_mcp_oauth_owner_isolation ON aura.identity_mcp_oauth
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);

CREATE POLICY identity_mcp_oauth_requires_identity ON aura.identity_mcp_oauth
    AS RESTRICTIVE FOR ALL TO aura_app
    USING (current_setting('app.current_identity', true) IS NOT NULL
           AND current_setting('app.current_identity', true) <> '');

COMMENT ON POLICY identity_mcp_oauth_requires_identity ON aura.identity_mcp_oauth IS
    'Fail-closed floor (migration 0087 pattern): AND-combined with every permissive policy, so no later permissive policy can restore visibility to a caller with no app.current_identity. Set it via internal/db.WithIdentityTx / WithIdentityTxRaw.';

COMMENT ON TABLE aura.identity_mcp_oauth IS
    'Per-identity OAuth grant for a remote MCP server (migration 0100). access_token_enc, refresh_token_enc and client_info_enc are AES-256-GCM ciphertext (KEK derived from AURA_AUTHULA_SECRET, the same trust boundary as .env and as aura.identity_object_store); client_info_enc holds the dynamic-client-registration result, which carries an AS-minted client_secret. The operator''s OWN pre-registered client credentials are NOT here: they are deployment config on ManagedServer.Env. expires_at is absolute, so a daemon restart can reconstruct the remaining TTL instead of trusting a relative expires_in with no wall-clock reference.';
