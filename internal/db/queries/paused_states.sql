-- name: InsertPausedState :exec
INSERT INTO aura.paused_states (
    token, conversation_id, kind, question, options, priority,
    resume_context, tool_call_id, proxied_from_child_id, proxied_tool_call_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: GetPausedStateByToken :one
SELECT token, conversation_id, kind, question, options, priority,
       resume_context, tool_call_id, proxied_from_child_id, proxied_tool_call_id,
       created_at, resumed_at, resumed_answer
FROM aura.paused_states
WHERE token = $1;

-- name: ListPendingPausedStates :many
SELECT token, conversation_id, kind, question, options, priority,
       resume_context, tool_call_id, proxied_from_child_id, proxied_tool_call_id,
       created_at, resumed_at, resumed_answer
FROM aura.paused_states
WHERE conversation_id = $1
  AND resumed_at IS NULL
ORDER BY priority DESC, created_at ASC, token ASC;

-- name: ListAllPendingPausedStates :many
-- Cross-thread pending list (APRV-01 / D-04): the same SELECT as
-- ListPendingPausedStates with NO conversation_id filter, so the approval center
-- can aggregate every still-pending pause across ALL conversations. KEEPS the
-- mandatory total-order tiebreaker (priority DESC, created_at ASC, token ASC) —
-- `token ASC` is non-negotiable because tx-batched rows share created_at = now()
-- (RESEARCH Pitfall 4 / askuser/store.go:158-160), so without it the order would
-- be non-deterministic. LIMIT $1 caps the scan (a single operator's pending set is
-- tiny; no new index is warranted — RESEARCH A4).
SELECT token, conversation_id, kind, question, options, priority,
       resume_context, tool_call_id, proxied_from_child_id, proxied_tool_call_id,
       created_at, resumed_at, resumed_answer
FROM aura.paused_states
WHERE resumed_at IS NULL
ORDER BY priority DESC, created_at ASC, token ASC
LIMIT $1;

-- name: ListRecentPausedStates :many
SELECT token, conversation_id, kind, question, options, priority,
       resume_context, tool_call_id, proxied_from_child_id, proxied_tool_call_id,
       created_at, resumed_at, resumed_answer
FROM aura.paused_states
ORDER BY created_at DESC, token ASC
LIMIT $1;

-- name: MarkPausedStateResumed :execrows
UPDATE aura.paused_states
SET resumed_at = now(),
    resumed_answer = $2
WHERE token = $1
  AND resumed_at IS NULL;

-- name: AutoResolvePendingForConversation :exec
UPDATE aura.paused_states
SET resumed_at = now(),
    resumed_answer = $2
WHERE conversation_id = $1
  AND resumed_at IS NULL;

-- name: CleanupResumedOlderThan :exec
DELETE FROM aura.paused_states
WHERE resumed_at IS NOT NULL
  AND resumed_at < $1;
