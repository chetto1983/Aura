-- name: InsertPausedState :exec
INSERT INTO aura.paused_states (
    token, conversation_id, kind, question, options, priority,
    resume_context, tool_call_id, proxied_from_child_id, proxied_tool_call_id,
    pending_action_id, owning_worker_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12);

-- name: GetPausedStateByToken :one
SELECT token, conversation_id, kind, question, options, priority,
       resume_context, tool_call_id, proxied_from_child_id, proxied_tool_call_id,
       created_at, resumed_at, resumed_answer, identity_id,
       pending_action_id, owning_worker_id
FROM aura.paused_states
WHERE token = $1;

-- name: ListPendingPausedStates :many
SELECT token, conversation_id, kind, question, options, priority,
       resume_context, tool_call_id, proxied_from_child_id, proxied_tool_call_id,
       created_at, resumed_at, resumed_answer, identity_id,
       pending_action_id, owning_worker_id
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
       created_at, resumed_at, resumed_answer, identity_id,
       pending_action_id, owning_worker_id
FROM aura.paused_states
WHERE resumed_at IS NULL
ORDER BY priority DESC, created_at ASC, token ASC
LIMIT $1;

-- name: ListExpiredPendingApprovals :many
SELECT token, conversation_id, kind, question, options, priority,
       resume_context, tool_call_id, proxied_from_child_id, proxied_tool_call_id,
       created_at, resumed_at, resumed_answer, identity_id,
       pending_action_id, owning_worker_id
FROM aura.paused_states
WHERE kind = 'approval'
  AND resumed_at IS NULL
  AND created_at <= $1
ORDER BY created_at ASC, token ASC
LIMIT $2;

-- name: ListRecentPausedStates :many
SELECT token, conversation_id, kind, question, options, priority,
       resume_context, tool_call_id, proxied_from_child_id, proxied_tool_call_id,
       created_at, resumed_at, resumed_answer, identity_id,
       pending_action_id, owning_worker_id
FROM aura.paused_states
ORDER BY created_at DESC, token ASC
LIMIT $1;

-- name: MarkPausedStateResumed :execrows
UPDATE aura.paused_states
SET resumed_at = now(),
    resumed_answer = $2
WHERE token = $1
  AND resumed_at IS NULL;

-- name: MarkPausedStateResumedFenced :execrows
-- D-12 fencing (SWARM-06, 51-06a): the same conditional UPDATE as
-- MarkPausedStateResumed, with ONE more predicate. A NULL pending_action_id (every row
-- that predates migration 0106, and every ordinary operator pause) matches regardless of
-- what the caller supplies via expect_action_id, so the fence is additive — never a new
-- precondition on the shipped path. A NON-NULL pending_action_id matches only an EQUAL
-- expect_action_id: a stale OR ABSENT fence on a fenced row matches zero rows (`x = NULL`
-- is NULL, never true), which is the "a stale decision can't resume a pause that has
-- since paused for a different action" guarantee. resumed_at IS NULL stays the shipped
-- idempotency key (approval-resume-defects) — the fence is an ADDITIONAL guard, never a
-- replacement for it. Both MarkResumedFencedTx (single) and MarkResumedBatchTx's
-- per-token loop (internal/askuser/store.go) call this SAME query — there is one fenced
-- predicate to drift, not two independently-written ones.
UPDATE aura.paused_states
SET resumed_at = now(),
    resumed_answer = $2
WHERE token = $1
  AND resumed_at IS NULL
  AND (pending_action_id IS NULL OR pending_action_id = sqlc.narg(expect_action_id));

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

-- name: GetPausedStateByTokenForIdentity :one
-- Owner-scoped single-pause read (Phase 36 MUSR-01 / D-06): the same projection as
-- GetPausedStateByToken with an identity_id owner predicate. A miss (unknown token OR a
-- token owned by another identity) is the caller's 404/403 signal. Run through
-- db.WithIdentityTx so the RLS owner policy backstops a forgotten filter.
SELECT token, conversation_id, kind, question, options, priority,
       resume_context, tool_call_id, proxied_from_child_id, proxied_tool_call_id,
       created_at, resumed_at, resumed_answer, identity_id,
       pending_action_id, owning_worker_id
FROM aura.paused_states
WHERE token = $1
  AND identity_id = $2;

-- name: ListAllPendingPausedStatesForIdentity :many
-- Owner-scoped cross-thread pending list (Phase 36 MUSR-01 / APRV-01): ListAllPendingPausedStates
-- restricted to one identity's pauses. Same total order (priority DESC, created_at ASC, token ASC).
SELECT token, conversation_id, kind, question, options, priority,
       resume_context, tool_call_id, proxied_from_child_id, proxied_tool_call_id,
       created_at, resumed_at, resumed_answer, identity_id,
       pending_action_id, owning_worker_id
FROM aura.paused_states
WHERE resumed_at IS NULL
  AND identity_id = $1
ORDER BY priority DESC, created_at ASC, token ASC
LIMIT $2;
