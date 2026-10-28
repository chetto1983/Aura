ALTER TABLE aura.pending_notifications DROP COLUMN IF EXISTS telegram_user_id;
ALTER TABLE aura.scheduler_tasks
    DROP CONSTRAINT IF EXISTS scheduler_tasks_telegram_recipients_not_empty,
    DROP COLUMN IF EXISTS telegram_recipients;
