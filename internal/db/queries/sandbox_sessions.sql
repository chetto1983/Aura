-- name: InsertSession :one
INSERT INTO aura.sandbox_sessions (conversation_id, container_id, image_digest)
VALUES ($1, $2, $3)
RETURNING id, conversation_id, container_id, image_digest, started_at, last_used_at, status;

-- name: TouchLastUsed :exec
UPDATE aura.sandbox_sessions
SET last_used_at = now()
WHERE id = $1;

-- name: MarkTerminated :exec
UPDATE aura.sandbox_sessions
SET status = 'terminated'
WHERE id = $1;

-- name: ListActive :many
SELECT id, conversation_id, container_id, image_digest, started_at, last_used_at, status
FROM aura.sandbox_sessions
WHERE status = 'active'
ORDER BY last_used_at;
