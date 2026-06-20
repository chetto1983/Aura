-- name: InsertIdentityAudit :one
INSERT INTO aura.identity_audit (
    actor_identity_id, new_identity_id, new_identity_name,
    granted_capabilities, authula_user_id
)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, created_at, actor_identity_id, new_identity_id, new_identity_name,
    granted_capabilities, authula_user_id;

-- name: ListIdentityAudit :many
SELECT id, created_at, actor_identity_id, new_identity_id, new_identity_name,
    granted_capabilities, authula_user_id
FROM aura.identity_audit
WHERE (sqlc.narg('new_identity_id')::uuid IS NULL OR new_identity_id = sqlc.narg('new_identity_id')::uuid)
ORDER BY created_at DESC, id DESC
LIMIT $1;
