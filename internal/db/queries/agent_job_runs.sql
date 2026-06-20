-- name: InsertRun :one
INSERT INTO aura.agent_job_runs (id, task_id, status, step_budget)
VALUES ($1, $2, 'running', $3)
RETURNING id, task_id, status, step_budget, started_at, last_heartbeat_at,
    completed_with_hash, summary, last_error, missed_since, paused_state_token, completed_at;

-- name: GetRun :one
SELECT id, task_id, status, step_budget, started_at, last_heartbeat_at,
    completed_with_hash, summary, last_error, missed_since, paused_state_token, completed_at
FROM aura.agent_job_runs
WHERE id = $1;

-- name: ListRunsForTask :many
SELECT id, task_id, status, step_budget, started_at, last_heartbeat_at,
    completed_with_hash, summary, last_error, missed_since, paused_state_token, completed_at
FROM aura.agent_job_runs
WHERE task_id = $1
ORDER BY started_at DESC
LIMIT $2 OFFSET $3;

-- name: UpdateHeartbeat :exec
UPDATE aura.agent_job_runs
SET last_heartbeat_at = now()
WHERE id = $1;

-- name: CompleteRun :exec
UPDATE aura.agent_job_runs
SET status = $2, summary = $3, last_error = $4, completed_with_hash = $5, completed_at = now()
WHERE id = $1;

-- name: ScanStaleRuns :many
SELECT id, task_id
FROM aura.agent_job_runs
WHERE status = 'running'
  AND last_heartbeat_at < now() - make_interval(secs => $1);

-- name: MarkUnknownRecovery :exec
UPDATE aura.agent_job_runs
SET status = 'unknown_recovery', missed_since = now()
WHERE id = $1;
