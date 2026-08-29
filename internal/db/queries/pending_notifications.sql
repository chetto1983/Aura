-- name: InsertPendingNotification :one
-- Exactly one of run_id / steer_queue_id owns the row (migration 0109). Route and
-- identity are mandatory snapshots; cron.Store mirrors all three invariants before the
-- round trip.
INSERT INTO aura.pending_notifications (
    id, run_id, notify_route, body, notify_after, attempts, last_error, status,
    identity_id, steer_queue_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id, run_id, notify_route, body, notify_after, attempts, last_error,
    status, created_at, updated_at, identity_id, steer_queue_id;

-- name: SweepDueNotifications :many
SELECT n.id, n.run_id, n.notify_route, n.body, n.notify_after, n.attempts, n.last_error,
    n.status, n.created_at, n.updated_at, n.identity_id, n.steer_queue_id,
    COALESCE(t.origin_conversation_id::text, s.conversation_id, '') AS origin_conversation_id
FROM aura.pending_notifications AS n
LEFT JOIN aura.agent_job_runs AS r ON r.id = n.run_id
LEFT JOIN aura.scheduler_tasks AS t ON t.id = r.task_id
LEFT JOIN aura.steer_queue AS s ON s.id = n.steer_queue_id
WHERE (n.status = 'pending' AND n.notify_after <= now())
   OR (n.status = 'failed' AND n.attempts < $1)
ORDER BY n.notify_after ASC, n.created_at ASC
LIMIT $2
FOR UPDATE OF n SKIP LOCKED;

-- name: MarkNotificationDelivered :exec
UPDATE aura.pending_notifications
SET status = 'delivered', updated_at = now()
WHERE id = $1;

-- name: MarkNotificationFailed :exec
UPDATE aura.pending_notifications
SET status = 'failed',
    attempts = attempts + 1,
    last_error = $2,
    updated_at = now()
WHERE id = $1;
