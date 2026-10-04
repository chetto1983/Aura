-- name: ListMCPProfiles :many
SELECT name, active, created_at
FROM aura.mcp_profile
ORDER BY name;

-- name: DeleteMCPProfilesExcept :exec
DELETE FROM aura.mcp_profile WHERE name <> ALL(@keep::text[]);

-- name: ClearActiveMCPProfile :exec
UPDATE aura.mcp_profile SET active = false WHERE active;

-- name: UpsertMCPProfile :exec
INSERT INTO aura.mcp_profile (name, active)
VALUES ($1, $2)
ON CONFLICT (name) DO UPDATE SET active = EXCLUDED.active;
