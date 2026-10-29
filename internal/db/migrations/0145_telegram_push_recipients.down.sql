-- An undeliverable row is terminal: nothing will ever send it, so it is dropped rather than
-- turned back into a retry the older schema would send to the identity's default account.
DELETE FROM aura.pending_notifications WHERE status = 'undeliverable';
ALTER TABLE aura.pending_notifications
    DROP CONSTRAINT pending_notifications_status_check,
    ADD CONSTRAINT pending_notifications_status_check
        CHECK (status IN ('pending', 'failed', 'delivered')),
    DROP COLUMN IF EXISTS telegram_user_id;
ALTER TABLE aura.scheduler_tasks
    DROP CONSTRAINT IF EXISTS scheduler_tasks_telegram_recipients_not_empty,
    DROP COLUMN IF EXISTS telegram_recipients;
