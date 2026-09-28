-- name: InsertMessageDraft :one
INSERT INTO aura.message_drafts (
  identity_id, conversation_id, tool_call_id, recipe, tool_name, action,
  original_args, original_fingerprint, expires_at
) VALUES (
  sqlc.arg(identity_id), sqlc.arg(conversation_id), sqlc.arg(tool_call_id),
  sqlc.arg(recipe), sqlc.arg(tool_name), sqlc.arg(action), sqlc.arg(original_args),
  sqlc.arg(original_fingerprint), sqlc.arg(expires_at)
)
ON CONFLICT (identity_id, conversation_id, tool_call_id) DO NOTHING
RETURNING *;

-- name: GetMessageDraftByCall :one
SELECT * FROM aura.message_drafts
WHERE identity_id = $1 AND conversation_id = $2 AND tool_call_id = $3;

-- name: GetMessageDraftForIdentity :one
SELECT * FROM aura.message_drafts WHERE identity_id = $1 AND id = $2;

-- name: ListPendingMessageDrafts :many
SELECT * FROM aura.message_drafts
WHERE identity_id = $1 AND conversation_id = $2 AND status = 'pending' AND expires_at > now()
ORDER BY created_at, id;

-- name: ClaimMessageDraftSend :one
UPDATE aura.message_drafts
SET status = 'dispatching', effective_args = sqlc.arg(effective_args),
    effective_fingerprint = sqlc.arg(effective_fingerprint),
    dispatch_started_at = now(), updated_at = now()
WHERE id = sqlc.arg(id) AND identity_id = sqlc.arg(identity_id)
  AND status = 'pending' AND expires_at > now()
RETURNING *;

-- name: DeclineMessageDraft :one
UPDATE aura.message_drafts
SET status = 'declined', resolved_at = now(), updated_at = now()
WHERE id = sqlc.arg(id) AND identity_id = sqlc.arg(identity_id)
  AND status = 'pending' AND expires_at > now()
RETURNING *;

-- name: MarkMessageDraftOutcome :one
UPDATE aura.message_drafts
SET status = sqlc.arg(status), outcome_code = sqlc.arg(outcome_code),
    resolved_at = now(), updated_at = now()
WHERE id = sqlc.arg(id) AND identity_id = sqlc.arg(identity_id)
  AND status = 'dispatching'
  AND sqlc.arg(status) IN ('sent', 'failed', 'uncertain')
RETURNING *;
