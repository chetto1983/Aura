-- name: CreateTask :one
INSERT INTO aura.scheduler_tasks (
    id, kind, schedule_kind, cron_expr, every_minutes, run_at, tz, payload,
    step_budget, status, next_run_at, notify_route, identity_id, origin_conversation_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING id, kind, schedule_kind, cron_expr, every_minutes, run_at, tz, payload,
    step_budget, status, next_run_at, notify_route, identity_id, origin_conversation_id,
    created_at, updated_at;

-- name: GetTask :one
SELECT id, kind, schedule_kind, cron_expr, every_minutes, run_at, tz, payload,
    step_budget, status, next_run_at, notify_route, identity_id, origin_conversation_id,
    created_at, updated_at
FROM aura.scheduler_tasks
WHERE id = $1;

-- name: ListActiveTasks :many
SELECT id, kind, schedule_kind, cron_expr, every_minutes, run_at, tz, payload,
    step_budget, status, next_run_at, notify_route, identity_id, origin_conversation_id,
    created_at, updated_at
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
    created_at, updated_at
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
