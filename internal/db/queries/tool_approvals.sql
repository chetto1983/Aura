-- name: InsertToolApproval :exec
INSERT INTO aura.tool_approvals (
    identity_id, conversation_id, pause_token, assistant_seq, call_index, tool_call_id, tool_name,
    source, execute_args, execute_fingerprint, review_binding, ran_note, brief, grant_tool, grant_action
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15);

-- name: DecideToolApproval :one
-- The one-shot decision of a parked call: only a pending record moves, so a second resolve
-- matches no row. Run in the identity transaction that also resumes the pause (migration 0160's
-- trigger needs the record decided first).
UPDATE aura.tool_approvals
SET status = @to_status::text,
    approval_scope = sqlc.narg(approval_scope),
    operator_id = sqlc.narg(operator_id),
    decided_at = now(),
    finished_at = CASE WHEN @to_status::text = 'accepted' THEN NULL ELSE now() END
WHERE pause_token = @pause_token
  AND status = 'pending'
RETURNING *;

-- name: TransitionToolApproval :execrows
-- A compare-and-set move; the Go side admits only the pairs of the state table.
UPDATE aura.tool_approvals
SET status = @to_status::text,
    decided_at = COALESCE(decided_at, now()),
    started_at = CASE WHEN @to_status::text = 'running' THEN now() ELSE started_at END,
    finished_at = CASE WHEN @to_status::text IN ('accepted', 'running') THEN finished_at ELSE now() END
WHERE id = @id
  AND status = @from_status::text;

-- name: ListOpenToolApprovals :many
SELECT * FROM aura.tool_approvals
WHERE identity_id = $1
  AND conversation_id = $2
  AND status IN ('pending', 'accepted', 'running')
ORDER BY assistant_seq, call_index;

-- name: ListOpenToolApprovalCalls :many
SELECT assistant_seq, tool_call_id FROM aura.tool_approvals
WHERE identity_id = $1
  AND conversation_id = $2
  AND status IN ('accepted', 'running')
ORDER BY assistant_seq, call_index;

-- name: ListToolApprovalsOfTurn :many
SELECT * FROM aura.tool_approvals
WHERE identity_id = $1
  AND conversation_id = $2
  AND assistant_seq = $3
ORDER BY call_index;

-- name: ListStaleAcceptedToolApprovals :many
SELECT * FROM aura.tool_approvals
WHERE identity_id = $1
  AND status = 'accepted'
  AND decided_at < $2
ORDER BY decided_at, id;

-- name: HasToolResultAfterSeq :one
SELECT EXISTS (
    SELECT 1 FROM aura.conversation_turns
    WHERE conversation_id = $1
      AND role = 'tool'
      AND tool_call_id = sqlc.arg(tool_call_id)::text
      AND seq > sqlc.arg(after_seq)::integer
)::boolean AS committed;
