-- name: GetCompactionClaim :one
SELECT * FROM aura.compaction_claims WHERE operation_id = $1;

-- name: GetActiveCompactionPointer :one
SELECT * FROM aura.compaction_active_pointers WHERE conversation_id = $1 AND branch_id = $2;

-- name: GetCompactionCheckpoint :one
SELECT * FROM aura.compaction_checkpoints WHERE id = $1;

-- name: ListCompatibleCompactionCheckpoints :many
SELECT * FROM aura.compaction_checkpoints
WHERE conversation_id = $1 AND branch_id = $2 AND schema_version = $3 AND projection_version = $4
ORDER BY generation DESC LIMIT $5;
