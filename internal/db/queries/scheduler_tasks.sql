-- name: CreateTask :one
INSERT INTO aura.scheduler_tasks (
    id, kind, schedule_kind, cron_expr, every_minutes, run_at, tz, payload,
    step_budget, status, next_run_at, notify_route, identity_id, origin_conversation_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING id, kind, schedule_kind, cron_expr, every_minutes, run_at, tz, payload,
    step_budget, status, next_run_at, notify_route, identity_id, origin_conversation_id,
    created_at, updated_at, approval_reminded_at, consecutive_failures, paused_reason, transient_retries;

-- name: GetTask :one
SELECT id, kind, schedule_kind, cron_expr, every_minutes, run_at, tz, payload,
    step_budget, status, next_run_at, notify_route, identity_id, origin_conversation_id,
    created_at, updated_at, approval_reminded_at, consecutive_failures, paused_reason, transient_retries
FROM aura.scheduler_tasks
WHERE id = $1;

-- name: ListActiveTasks :many
SELECT id, kind, schedule_kind, cron_expr, every_minutes, run_at, tz, payload,
    step_budget, status, next_run_at, notify_route, identity_id, origin_conversation_id,
    created_at, updated_at, approval_reminded_at, consecutive_failures, paused_reason, transient_retries
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
    created_at, updated_at, approval_reminded_at, consecutive_failures, paused_reason, transient_retries
FROM aura.scheduler_tasks
WHERE status = 'active' AND next_run_at <= now()
ORDER BY next_run_at ASC
LIMIT $1;

-- name: CancelTask :exec
-- A paused task cancels too; its pause reason goes with the pause (migration 0138 ties a
-- reason to the paused status).
UPDATE aura.scheduler_tasks
SET status = 'cancelled', paused_reason = NULL, updated_at = now()
WHERE id = $1;

-- name: UpdateNextRunAt :exec
UPDATE aura.scheduler_tasks
SET next_run_at = $2, updated_at = now()
WHERE id = $1;

-- name: DeleteSettledOneShots :execrows
-- A one-shot task is finished once it fired (it keeps status 'active' with its next fire
-- cleared) or was cancelled. Once nothing is left for it to do -- no run still running, no
-- notification still owed a retry (one is owed while pending, or failed under the attempt
-- bound $1) -- it is deleted, and ON DELETE CASCADE takes its runs and notification rows with
-- it (0009, 0013): the operator's decision of 2026-10-05. An active one-shot that never ran
-- keeps its row; the board flags it unschedulable.
DELETE FROM aura.scheduler_tasks AS t
WHERE t.schedule_kind = 'at'
    AND (t.status = 'cancelled'
        OR (t.status = 'active'
            AND t.next_run_at IS NULL
            AND EXISTS (SELECT 1 FROM aura.agent_job_runs AS r WHERE r.task_id = t.id)))
    AND NOT EXISTS (
        SELECT 1 FROM aura.agent_job_runs AS r
        WHERE r.task_id = t.id AND r.status = 'running')
    AND NOT EXISTS (
        SELECT 1 FROM aura.pending_notifications AS n
        JOIN aura.agent_job_runs AS r ON r.id = n.run_id
        WHERE r.task_id = t.id
            AND (n.status = 'pending' OR (n.status = 'failed' AND n.attempts < $1)));

-- name: ListManageableTasks :many
-- The cockpit scheduler board (GOV-03 write): active, pending_approval and paused tasks, so
-- an operator can approve a gated task or resume a paused one on-screen. Ordered by next fire
-- (pending rows have a non-null next_run_at too — it is the first fire computed at schedule
-- time).
SELECT id, kind, schedule_kind, cron_expr, every_minutes, run_at, tz, payload,
    step_budget, status, next_run_at, notify_route, identity_id, origin_conversation_id,
    created_at, updated_at, approval_reminded_at, consecutive_failures, paused_reason, transient_retries
FROM aura.scheduler_tasks
WHERE status IN ('active', 'pending_approval', 'paused')
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
    created_at, updated_at, approval_reminded_at, consecutive_failures, paused_reason, transient_retries
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
-- payload, notify route, and the recomputed first fire. Guarded to active/pending/paused rows
-- so a cancelled/completed task is not silently revived; a paused task stays paused. Returns
-- rows affected.
UPDATE aura.scheduler_tasks
SET schedule_kind = $2, cron_expr = $3, every_minutes = $4, run_at = $5, tz = $6,
    payload = $7, notify_route = $8, next_run_at = $9, updated_at = now()
WHERE id = $1 AND status IN ('active', 'pending_approval', 'paused');

-- name: PauseTaskRow :execrows
-- The operator pause: an active task stops firing until resumed. Returns rows affected so a
-- task that is not active (pending, already paused, cancelled, absent) maps to a miss.
UPDATE aura.scheduler_tasks
SET status = 'paused', paused_reason = 'operator', updated_at = now()
WHERE id = $1 AND status = 'active';

-- name: ResumeTaskRow :execrows
-- Reactivate a paused task at the next fire the caller computed from now, clearing the
-- failure count that may have paused it and any retry it was waiting for. Returns rows
-- affected (a non-paused task misses).
UPDATE aura.scheduler_tasks
SET status = 'active', paused_reason = NULL, consecutive_failures = 0, transient_retries = 0,
    next_run_at = $2, updated_at = now()
WHERE id = $1 AND status = 'paused';

-- name: RecordTaskRunOutcome :one
-- One finished run's effect on its task, in one statement: a success resets the failure count;
-- a failure increments it and, when the caller's pause_after is positive and the new count
-- reaches it, pauses an active task with reason 'failures'. Every SET expression reads the
-- row as it was before the update, so consecutive_failures + 1 is the new count. A reported
-- outcome ends any chain of transient retries, so their count starts over.
UPDATE aura.scheduler_tasks
SET consecutive_failures = CASE WHEN sqlc.arg(succeeded)::boolean THEN 0 ELSE consecutive_failures + 1 END,
    transient_retries = 0,
    status = CASE
        WHEN NOT sqlc.arg(succeeded)::boolean AND sqlc.arg(pause_after)::integer > 0
            AND consecutive_failures + 1 >= sqlc.arg(pause_after)::integer AND status = 'active'
        THEN 'paused' ELSE status END,
    paused_reason = CASE
        WHEN NOT sqlc.arg(succeeded)::boolean AND sqlc.arg(pause_after)::integer > 0
            AND consecutive_failures + 1 >= sqlc.arg(pause_after)::integer AND status = 'active'
        THEN 'failures' ELSE paused_reason END,
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING status, consecutive_failures, paused_reason;

-- name: ScheduleTransientRetry :one
-- Re-arm a task whose run failed on a transient model error before any tool ran (prd.md §15).
-- The next fire becomes the earlier of its regular one and now plus the delay for this retry,
-- taken from the caller's schedule. LEAST ignores a NULL, so a fired one-shot takes the retry.
-- A task past its last delay, or no longer active, is left alone and returns no row.
UPDATE aura.scheduler_tasks
SET transient_retries = transient_retries + 1,
    next_run_at = LEAST(next_run_at,
        now() + make_interval(secs => (sqlc.arg(delay_seconds)::integer[])[transient_retries + 1])),
    updated_at = now()
WHERE id = sqlc.arg(id) AND status = 'active'
    AND transient_retries < cardinality(sqlc.arg(delay_seconds)::integer[])
RETURNING next_run_at, transient_retries;
