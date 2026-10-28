-- 0145: a scheduled task names the Telegram accounts its pushes reach, and a pending Telegram
-- push names the one account it is for.
-- docs/superpowers/specs/2026-10-28-telegram-push-recipients-design.md (R1, R4).
--
-- NULL recipients means the task named none: its pushes go to the account seen most recently
-- (R6). A list is never empty; "all" stores every account linked when the task was scheduled.
-- A pending row with a telegram_user_id is retried for that account only, so a push that
-- reached one phone is never sent to it again while the other is retried.
ALTER TABLE aura.scheduler_tasks
    ADD COLUMN telegram_recipients bigint[],
    ADD CONSTRAINT scheduler_tasks_telegram_recipients_not_empty
        CHECK (telegram_recipients IS NULL OR cardinality(telegram_recipients) > 0);

ALTER TABLE aura.pending_notifications ADD COLUMN telegram_user_id bigint;

COMMENT ON COLUMN aura.scheduler_tasks.telegram_recipients IS
    'Migration 0145: the Telegram user ids this task''s pushes (its result and its approval prompt) reach. NULL: the account seen most recently.';
COMMENT ON COLUMN aura.pending_notifications.telegram_user_id IS
    'Migration 0145: the one Telegram account a pending push is for; a task with recipients queues one row per account. NULL: the account seen most recently.';
