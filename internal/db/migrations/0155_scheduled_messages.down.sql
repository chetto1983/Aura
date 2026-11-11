-- The scheduled-message rows cannot exist under the restored CHECKs, so they go first.
DELETE FROM aura.message_drafts WHERE send_at IS NOT NULL;
DELETE FROM aura.scheduler_tasks WHERE kind = 'message';

DROP INDEX IF EXISTS aura.scheduler_tasks_origin_call_idx;
ALTER TABLE aura.scheduler_tasks DROP COLUMN IF EXISTS origin_tool_call_id;

DROP INDEX IF EXISTS aura.message_drafts_task_idx;
ALTER TABLE aura.message_drafts DROP CONSTRAINT IF EXISTS message_drafts_approved_chk;
ALTER TABLE aura.message_drafts DROP CONSTRAINT IF EXISTS message_drafts_send_at_chk;
ALTER TABLE aura.message_drafts DROP CONSTRAINT IF EXISTS message_drafts_scheduled_chk;
ALTER TABLE aura.message_drafts DROP CONSTRAINT message_drafts_status_check;
ALTER TABLE aura.message_drafts ADD CONSTRAINT message_drafts_status_check CHECK (status IN
    ('pending', 'dispatching', 'sent', 'failed', 'declined', 'uncertain', 'expired'));
ALTER TABLE aura.message_drafts DROP COLUMN IF EXISTS send_at;
ALTER TABLE aura.message_drafts DROP COLUMN IF EXISTS scheduled_task_id;

ALTER TABLE aura.scheduler_tasks DROP CONSTRAINT scheduler_tasks_kind_check;
ALTER TABLE aura.scheduler_tasks ADD CONSTRAINT scheduler_tasks_kind_check
    CHECK (kind IN ('reminder', 'agent_job', 'backup_postgres',
        'skill_ttl_sweep', 'identity_purge', 'sandbox_reap', 'share_expiry_sweep',
        'retention_sweep', 'memory_embed_backfill', 'observability_check',
        'memory_mention_link', 'backup_objects', 'pim_watch', 'pim_digest'));
