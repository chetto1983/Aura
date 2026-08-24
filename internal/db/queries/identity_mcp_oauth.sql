-- Per-identity OAuth grants for remote MCP servers (migration 0100). Every statement is
-- scoped by identity_id in the WHERE clause AND by the two RLS policies underneath: the
-- predicate here is what makes the intent readable, the policy is what makes it true even
-- when a future caller forgets to write it.

-- name: GetIdentityMCPOAuth :one
SELECT identity_id, server_name, resource_url, access_token_enc, refresh_token_enc,
       client_info_enc, token_type, scopes, expires_at, created_at, updated_at
FROM aura.identity_mcp_oauth
WHERE identity_id = $1 AND server_name = $2;

-- name: UpsertIdentityMCPOAuth :exec
-- A refresh rewrites the same row, so this is an UPDATE-on-conflict rather than the
-- delete-then-insert aura.identity_object_store uses for key rotation. created_at is
-- deliberately left alone on conflict: it records when the identity first authorized this
-- server, which a refresh does not change.
INSERT INTO aura.identity_mcp_oauth (
    identity_id, server_name, resource_url, access_token_enc, refresh_token_enc,
    client_info_enc, token_type, scopes, expires_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
ON CONFLICT (identity_id, server_name) DO UPDATE SET
    resource_url      = EXCLUDED.resource_url,
    access_token_enc  = EXCLUDED.access_token_enc,
    refresh_token_enc = EXCLUDED.refresh_token_enc,
    client_info_enc   = EXCLUDED.client_info_enc,
    token_type        = EXCLUDED.token_type,
    scopes            = EXCLUDED.scopes,
    expires_at        = EXCLUDED.expires_at,
    updated_at        = now();

-- name: DeleteIdentityMCPOAuth :execrows
-- Revoking locally. execrows, not exec: a caller that reports "revoked" without knowing
-- whether a row existed cannot tell a real revocation from a no-op on someone else's row
-- that RLS filtered away.
DELETE FROM aura.identity_mcp_oauth
WHERE identity_id = $1 AND server_name = $2;

-- name: ListIdentityMCPOAuthServers :many
-- The names an identity has authorized, for `aura mcp login --status` and the cockpit's
-- connector list. Deliberately selects NO ciphertext: a listing must not pull three
-- credentials into memory to render a name and an expiry.
SELECT server_name, resource_url, expires_at, updated_at
FROM aura.identity_mcp_oauth
WHERE identity_id = $1
ORDER BY server_name;
