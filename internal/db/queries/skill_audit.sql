-- name: InsertSkillAudit :one
INSERT INTO aura.skill_audit (
    actor_id, identity_id, skill_name, action, content_hash,
    approval_source, paused_state_token, gate_recommended, gate_taken, blocklist_override
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id, created_at, actor_id, identity_id, skill_name, action, content_hash,
    approval_source, paused_state_token, gate_recommended, gate_taken, blocklist_override;

-- name: ListSkillAudit :many
SELECT id, created_at, actor_id, identity_id, skill_name, action, content_hash,
    approval_source, paused_state_token, gate_recommended, gate_taken, blocklist_override
FROM aura.skill_audit
WHERE (sqlc.narg('skill_name')::text IS NULL OR skill_name = sqlc.narg('skill_name')::text)
  AND (sqlc.narg('since')::timestamptz IS NULL OR created_at >= sqlc.narg('since')::timestamptz)
ORDER BY created_at DESC, id DESC
LIMIT $1;

-- name: ListSkillAuditByName :many
SELECT id, created_at, actor_id, identity_id, skill_name, action, content_hash,
    approval_source, paused_state_token, gate_recommended, gate_taken, blocklist_override
FROM aura.skill_audit
WHERE skill_name = $1
ORDER BY created_at DESC, id DESC;
