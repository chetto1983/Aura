ALTER TABLE aura.steer_queue
    DROP CONSTRAINT steer_queue_fanout_key_chk;

ALTER TABLE aura.pending_notifications
    DROP CONSTRAINT pending_notifications_owner_chk,
    DROP CONSTRAINT pending_notifications_notify_route_chk,
    ALTER COLUMN notify_route DROP NOT NULL,
    ALTER COLUMN identity_id DROP NOT NULL,
    ADD CONSTRAINT pending_notifications_owner_chk
        CHECK (run_id IS NOT NULL OR steer_queue_id IS NOT NULL);

COMMENT ON COLUMN aura.pending_notifications.notify_route IS NULL;
COMMENT ON COLUMN aura.pending_notifications.identity_id IS
    'Stable owning identity snapshot used by deferred delivery; NULL rows fall back to notify_route.';
COMMENT ON CONSTRAINT pending_notifications_owner_chk ON aura.pending_notifications IS
    'A row is owned by a scheduler run or a steer queue row.';

ALTER TABLE aura.scheduler_tasks
    DROP CONSTRAINT scheduler_tasks_notify_route_chk,
    ALTER COLUMN notify_route DROP NOT NULL;
