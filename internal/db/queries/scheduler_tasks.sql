-- name: CreateTask :one
INSERT INTO aura.scheduler_tasks (
    id, kind, schedule_kind, cron_expr, every_minutes, run_at, tz, payload,
    step_budget, status, next_run_at, notify_route, identity_id, origin_conversation_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING id, kind, schedule_kind, cron_expr, every_minutes, run_at, tz, payload,
    step_budget, status, next_run_at, notify_route, identity_id, origin_conversation_id,
    created_at, updated_at, approval_reminded_at;

-- name: GetTask :one
SELECT id, kind, schedule_kind, cron_expr, every_minutes, run_at, tz, payload,
    step_budget, status, next_run_at, notify_route, identity_id, origin_conversation_id,
    created_at, updated_at, approval_reminded_at
FROM aura.scheduler_tasks
WHERE id = $1;

-- name: ListActiveTasks :many
SELECT id, kind, schedule_kind, cron_expr, every_minutes, run_at, tz, payload,
    step_budget, status, next_run_at, notify_route, identity_id, origin_conversation_id,
    created_at, updated_at, approval_reminded_at
FROM aura.scheduler_tasks
WHERE status = 'active'
ORDER BY next_run_at ASC NULLS LAST, id ASC;

-- name: DueTasks :many
-- Claim correctness is held by the per-task pg_try_advisory_lock (claim.go), NOT a
-- row lock here: this SELECT runs on the autocommit pool, so any FOR UPDATE SKIP
-- LOCKED would release the instant the SELECT returns (inert, L5). The advisory lock
-- is what makes each due task a singleton across concurrent workers.
SELECT id, kind, schedule_kind, cron_expr, every_minutes, run_at, tz, payload,
    step_budget, status, next_run_at, notify_route, identity_id, origin_conversation_id,
    created_at, updated_at, approval_reminded_at
FROM aura.scheduler_tasks
WHERE status = 'active' AND next_run_at <= now()
ORDER BY next_run_at ASC
LIMIT $1;

-- name: CancelTask :exec
UPDATE aura.scheduler_tasks
SET status = 'cancelled', updated_at = now()
WHERE id = $1;

-- name: UpdateNextRunAt :exec
UPDATE aura.scheduler_tasks
SET next_run_at = $2, updated_at = now()
WHERE id = $1;

-- name: ListManageableTasks :many
-- The cockpit scheduler board (GOV-03 write): active AND pending_approval tasks, so an
-- operator can approve a gated task on-screen. Ordered by next fire (pending rows have a
-- non-null next_run_at too — it is the first fire computed at schedule time).
SELECT id, kind, schedule_kind, cron_expr, every_minutes, run_at, tz, payload,
    step_budget, status, next_run_at, notify_route, identity_id, origin_conversation_id,
    created_at, updated_at, approval_reminded_at
FROM aura.scheduler_tasks
WHERE status IN ('active', 'pending_approval')
ORDER BY next_run_at ASC NULLS LAST, id ASC;

-- name: ListDuePendingApprovalReminders :many
-- fix-plan 1.7 / Amendment #92 (REVISED): the per-tick approval-reminder sweep. Every
-- pending_approval task with an ORIGIN CONVERSATION whose throttle stamp is due (never
-- reminded, or older than the cadence cutoff computed in Go: now() -
-- AURA_SCHEDULER_APPROVAL_REMINDER_SEC). The old identity_id NOT IN ('','local') filter is
-- DROPPED: WebUI/local-origin rows MUST be selected so the sweep can MINT their pause (they
-- surface via the pull /api/approvals; the Telegram push is simply a no-op for them). Rows
-- with a NULL origin_conversation_id (CLI-origin, no conversation to key a pause on) are
-- excluded and keep the `aura task approve` CLI path. No FOR UPDATE SKIP LOCKED: the sweep
-- runs on the autocommit pool (like DueTasks) where a row lock releases the instant the
-- SELECT returns (inert). The dedup is the approval_reminded_at throttle stamp, not a row
-- lock; a rare cross-instance double-nudge under HA is benign.
SELECT id, kind, schedule_kind, cron_expr, every_minutes, run_at, tz, payload,
    step_budget, status, next_run_at, notify_route, identity_id, origin_conversation_id,
    created_at, updated_at, approval_reminded_at
FROM aura.scheduler_tasks
WHERE status = 'pending_approval'
    AND origin_conversation_id IS NOT NULL
    AND (approval_reminded_at IS NULL OR approval_reminded_at <= $1)
ORDER BY created_at ASC
LIMIT $2;

-- name: MarkApprovalReminded :exec
-- Stamp the throttle after a reminder ATTEMPT (delivered or not) so a pending approval
-- re-nudges at most once per cadence and a failing channel cannot spam the tick.
UPDATE aura.scheduler_tasks
SET approval_reminded_at = now()
WHERE id = $1;

-- name: ApproveTaskRow :execrows
-- Flip a pending_approval task to active (the cockpit approval, parity with the CLI
-- `aura task approve`). Returns rows affected so the caller distinguishes a hit (1) from
-- a task that is not awaiting approval (0).
UPDATE aura.scheduler_tasks
SET status = 'active', updated_at = now()
WHERE id = $1 AND status = 'pending_approval';

-- name: RunTaskNowRow :execrows
-- Advance an active task's next fire to now so the next tick claims it. Returns rows
-- affected so the caller distinguishes a hit from a non-active (pending/cancelled) task.
UPDATE aura.scheduler_tasks
SET next_run_at = now(), updated_at = now()
WHERE id = $1 AND status = 'active';

-- name: UpdateTaskScheduleRow :execrows
-- Reschedule + re-payload a user task (the cockpit edit): rewrite the schedule grammar,
-- payload, notify route, and the recomputed first fire. Guarded to active/pending rows so
-- a cancelled/completed task is not silently revived. Returns rows affected.
UPDATE aura.scheduler_tasks
SET schedule_kind = $2, cron_expr = $3, every_minutes = $4, run_at = $5, tz = $6,
    payload = $7, notify_route = $8, next_run_at = $9, updated_at = now()
WHERE id = $1 AND status IN ('active', 'pending_approval');
