-- name: InsertPendingNotification :one
-- run_id / steer_queue_id are both nullable (migration 0105); the DB-level
-- pending_notifications_owner_chk CHECK is the enforcement point, mirrored in Go by
-- cron.Store.InsertPendingNotification so a wiring bug fails loud before the round trip.
INSERT INTO aura.pending_notifications (
    id, run_id, notify_route, body, notify_after, attempts, last_error, status,
    identity_id, steer_queue_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id, run_id, notify_route, body, notify_after, attempts, last_error,
    status, created_at, updated_at, identity_id, steer_queue_id;

-- name: SweepDueNotifications :many
SELECT id, run_id, notify_route, body, notify_after, attempts, last_error,
    status, created_at, updated_at, identity_id, steer_queue_id
FROM aura.pending_notifications
WHERE (status = 'pending' AND notify_after <= now())
   OR (status = 'failed' AND attempts < $1)
ORDER BY notify_after ASC, created_at ASC
LIMIT $2
FOR UPDATE SKIP LOCKED;

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
