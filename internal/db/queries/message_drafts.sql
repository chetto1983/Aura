-- name: InsertMessageDraft :one
INSERT INTO aura.message_drafts (
  identity_id, conversation_id, tool_call_id, recipe, tool_name, registered_tool_name, action,
  original_args, original_fingerprint, expires_at, scheduled_task_id, send_at
) VALUES (
  sqlc.arg(identity_id), sqlc.arg(conversation_id), sqlc.arg(tool_call_id),
  sqlc.arg(recipe), sqlc.arg(tool_name), sqlc.arg(registered_tool_name), sqlc.arg(action), sqlc.arg(original_args),
  sqlc.arg(original_fingerprint), sqlc.arg(expires_at), sqlc.narg(scheduled_task_id), sqlc.narg(send_at)
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
  AND status = 'pending' AND expires_at > now() AND send_at IS NULL
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

-- name: ExpireMessageDraft :one
UPDATE aura.message_drafts
SET status = 'expired', resolved_at = now(), updated_at = now()
WHERE id = sqlc.arg(id) AND identity_id = sqlc.arg(identity_id)
  AND status = 'pending' AND expires_at <= now()
RETURNING *;

-- name: MarkInterruptedMessageDraftUncertain :one
UPDATE aura.message_drafts
SET status = 'uncertain', outcome_code = 'interrupted',
    resolved_at = now(), updated_at = now()
WHERE id = sqlc.arg(id) AND identity_id = sqlc.arg(identity_id)
  AND status = 'dispatching'
  AND dispatch_started_at <= now() - interval '30 minutes'
RETURNING *;

-- A scheduled draft's review fixes what will be sent, before its time; the task sends it then
-- (migration 0155). An immediate draft is never approved, only claimed.
-- name: ApproveScheduledMessageDraft :one
UPDATE aura.message_drafts
SET status = 'approved', effective_args = sqlc.arg(effective_args),
    effective_fingerprint = sqlc.arg(effective_fingerprint), updated_at = now()
WHERE id = sqlc.arg(id) AND identity_id = sqlc.arg(identity_id)
  AND status = 'pending' AND send_at IS NOT NULL AND send_at > now() AND expires_at > now()
RETURNING *;

-- Cancelling a message task declines its draft first (decision 15), so a handler already
-- holding the task cannot claim it. A claimed draft is left as it is: it was sent, or is being.
-- name: DeclineScheduledMessageDraftsForTask :exec
UPDATE aura.message_drafts
SET status = 'declined', resolved_at = now(), updated_at = now()
WHERE identity_id = sqlc.arg(identity_id) AND scheduled_task_id = sqlc.arg(scheduled_task_id)
  AND status IN ('pending', 'approved');

-- Deactivating an identity declines every scheduled draft it has not sent (decision 37).
-- name: DeclineScheduledMessageDraftsForOwner :exec
UPDATE aura.message_drafts
SET status = 'declined', resolved_at = now(), updated_at = now()
WHERE identity_id = sqlc.arg(identity_id) AND send_at IS NOT NULL AND status IN ('pending', 'approved');

-- name: GetMessageDraftByTask :one
SELECT * FROM aura.message_drafts
WHERE identity_id = sqlc.arg(identity_id) AND scheduled_task_id = sqlc.arg(scheduled_task_id);

-- The fire-time claim: an approved draft whose time has come and whose review still stands.
-- It is the one-send boundary of a scheduled message (approved -> dispatching).
-- name: ClaimScheduledMessageDraft :one
UPDATE aura.message_drafts
SET status = 'dispatching', dispatch_started_at = now(), updated_at = now()
WHERE id = sqlc.arg(id) AND identity_id = sqlc.arg(identity_id)
  AND status = 'approved' AND send_at <= now() AND expires_at > now()
RETURNING *;

-- A scheduled draft whose time came unreviewed, or whose approval outlived its review.
-- name: ExpireScheduledMessageDraft :one
UPDATE aura.message_drafts
SET status = 'expired', resolved_at = now(), updated_at = now()
WHERE id = sqlc.arg(id) AND identity_id = sqlc.arg(identity_id)
  AND send_at IS NOT NULL AND send_at <= now()
  AND (status = 'pending' OR (status = 'approved' AND expires_at <= now()))
RETURNING *;
