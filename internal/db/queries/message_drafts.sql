-- name: InsertMessageDraft :one
INSERT INTO aura.message_drafts (
  identity_id, conversation_id, tool_call_id, recipe, tool_name, registered_tool_name, action,
  original_args, original_fingerprint, expires_at, scheduled_task_id, send_at, sender_address
) VALUES (
  sqlc.arg(identity_id), sqlc.arg(conversation_id), sqlc.arg(tool_call_id),
  sqlc.arg(recipe), sqlc.arg(tool_name), sqlc.arg(registered_tool_name), sqlc.arg(action), sqlc.arg(original_args),
  sqlc.arg(original_fingerprint), sqlc.arg(expires_at), sqlc.narg(scheduled_task_id), sqlc.narg(send_at), sqlc.arg(sender_address)
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

-- name: FindMessageReviewBoundary :one
-- The resume transaction stamps both the pause and its tool answer with now().
-- Find the first committed review or user input on this branch. For a live send
-- the caller can continue past a verified legacy catalog-only user row.
WITH RECURSIVE accepted AS (
    SELECT answer.seq, d.status = 'approved' AND task.status = 'active' AS live
    FROM aura.message_drafts d
    JOIN aura.scheduler_tasks task ON task.id = d.scheduled_task_id AND task.identity_id = d.identity_id::text
    JOIN aura.paused_states p ON p.identity_id = d.identity_id AND p.conversation_id::text = d.conversation_id
        AND p.tool_call_id = d.tool_call_id AND p.resume_context->>'draft_id' = d.id::text
    JOIN aura.conversation_turns answer ON answer.conversation_id = p.conversation_id
        AND answer.role = 'tool' AND answer.tool_call_id = p.tool_call_id AND answer.created_at = p.resumed_at
    WHERE d.identity_id = sqlc.arg(identity_id) AND p.conversation_id = sqlc.arg(conversation_id)
        AND d.scheduled_task_id = sqlc.arg(task_id) AND d.send_at IS NOT NULL
        AND d.effective_fingerprint IS NOT NULL AND p.resume_context->>'type' = 'message_draft'
        AND p.resumed_answer->>'action' = 'accept' AND p.resumed_at IS NOT NULL
), path AS (
    SELECT t.seq, t.parent_seq, t.role
    FROM aura.conversation_turns t
    WHERE t.conversation_id = sqlc.arg(conversation_id)
        AND t.seq = sqlc.arg(leaf_seq)
        AND t.seq >= (SELECT MIN(seq) FROM accepted)
    UNION ALL
    SELECT t.seq, t.parent_seq, t.role
    FROM path JOIN aura.conversation_turns t
        ON t.conversation_id = sqlc.arg(conversation_id) AND t.seq = path.parent_seq
    WHERE path.role <> 'user' AND t.seq < path.seq AND t.seq >= (SELECT MIN(seq) FROM accepted)
)
SELECT path.seq, path.parent_seq, (a.seq IS NOT NULL)::boolean AS reviewed,
       COALESCE(t.content, '')::text AS content, (t.content_sidecar_path IS NOT NULL)::boolean AS spilled
FROM path
LEFT JOIN accepted a ON a.seq = path.seq
JOIN aura.conversation_turns t ON t.conversation_id = sqlc.arg(conversation_id) AND t.seq = path.seq
WHERE a.seq IS NOT NULL OR (path.role = 'user' AND COALESCE(cardinality(t.attachment_ids), 0) = 0
    AND EXISTS (SELECT FROM accepted WHERE live))
ORDER BY path.seq DESC LIMIT 1;
