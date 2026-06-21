-- name: InsertMcpAudit :one
INSERT INTO aura.mcp_audit (
    actor_identity_id, action, server_name, reason
)
VALUES ($1, $2, $3, $4)
RETURNING id, created_at, actor_identity_id, action, server_name, reason;

-- name: ListMcpAudit :many
SELECT id, created_at, actor_identity_id, action, server_name, reason
FROM aura.mcp_audit
WHERE (sqlc.narg('server_name')::text IS NULL OR server_name = sqlc.narg('server_name')::text)
  AND (sqlc.narg('since')::timestamptz IS NULL OR created_at >= sqlc.narg('since')::timestamptz)
ORDER BY created_at DESC, id DESC
LIMIT $1;
