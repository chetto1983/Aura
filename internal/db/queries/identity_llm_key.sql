-- name: GetIdentityLLMKey :one
SELECT identity_id, key_ciphertext, key_hash, key_label, limit_usd, limit_reset, created_at, updated_at
FROM aura.identity_llm_key
WHERE identity_id = $1;

-- name: UpsertIdentityLLMKey :exec
INSERT INTO aura.identity_llm_key (identity_id, key_ciphertext, key_hash, key_label, limit_usd, limit_reset)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (identity_id) DO UPDATE SET
    key_ciphertext = EXCLUDED.key_ciphertext,
    key_hash       = EXCLUDED.key_hash,
    key_label      = EXCLUDED.key_label,
    limit_usd      = EXCLUDED.limit_usd,
    limit_reset    = EXCLUDED.limit_reset,
    updated_at     = now();

-- name: InsertIdentityLLMKeyIfAbsent :execrows
-- Writes a key only when the identity has none. The reconciler and the provisioning saga can
-- mint for the same new identity at once; the one that loses sees 0 rows and revokes its own
-- key instead of overwriting the winner's.
INSERT INTO aura.identity_llm_key (identity_id, key_ciphertext, key_hash, key_label, limit_usd, limit_reset)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (identity_id) DO NOTHING;

-- name: ListIdentityLLMKeys :many
-- Deliberately selects NO ciphertext (mirrors internal/db/sqlc/identity_mcp_oauth.sql.go's
-- ListIdentityMCPOAuthServers): the admin roster needs the cap and the hash, never the key.
-- RLS-scoped like every other query here (0087 fail-closed floor); the identity_id
-- parameter is redundant with app.current_identity today (the table's PK means at most
-- one row per identity) but keeps this query's shape identical to its ListIdentityMCPOAuthServers
-- precedent for when a later plan adds an admin-bypass role.
SELECT identity_id, key_hash, key_label, limit_usd, limit_reset, updated_at
FROM aura.identity_llm_key
WHERE identity_id = $1
ORDER BY identity_id;
