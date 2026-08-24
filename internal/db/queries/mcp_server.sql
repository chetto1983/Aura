-- name: UpsertMCPServer :one
INSERT INTO aura.mcp_server (name, source, enabled, config, env_enc, profiles, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (name) DO UPDATE
SET source     = EXCLUDED.source,
    enabled    = EXCLUDED.enabled,
    config     = EXCLUDED.config,
    env_enc    = EXCLUDED.env_enc,
    profiles   = EXCLUDED.profiles,
    updated_at = now()
RETURNING name, source, enabled, config, env_enc, profiles, created_by, created_at, updated_at;

-- name: ListMCPServers :many
SELECT name, source, enabled, config, env_enc, profiles, created_by, created_at, updated_at
FROM aura.mcp_server
ORDER BY name;

-- name: GetMCPServer :one
SELECT name, source, enabled, config, env_enc, profiles, created_by, created_at, updated_at
FROM aura.mcp_server
WHERE name = $1;

-- name: DeleteMCPServer :execrows
DELETE FROM aura.mcp_server WHERE name = $1;

-- name: CountMCPServers :one
SELECT count(*) FROM aura.mcp_server;
