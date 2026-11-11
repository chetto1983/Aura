-- A message to someone else, reviewed when it is scheduled and sent once at its time
-- (docs/superpowers/specs/2026-11-05-scheduled-delivery-defaults-and-messages-design.md, Part 2).
ALTER TABLE aura.scheduler_tasks DROP CONSTRAINT scheduler_tasks_kind_check;
ALTER TABLE aura.scheduler_tasks ADD CONSTRAINT scheduler_tasks_kind_check
    CHECK (kind IN ('reminder', 'agent_job', 'backup_postgres',
        'skill_ttl_sweep', 'identity_purge', 'sandbox_reap', 'share_expiry_sweep',
        'retention_sweep', 'memory_embed_backfill', 'observability_check',
        'memory_mention_link', 'backup_objects', 'pim_watch', 'pim_digest', 'message'));

-- send_at marks a scheduled draft. The task may be deleted once settled; the draft stays as
-- the record of what was sent, so the link goes NULL rather than cascading.
ALTER TABLE aura.message_drafts
    ADD COLUMN scheduled_task_id uuid REFERENCES aura.scheduler_tasks(id) ON DELETE SET NULL,
    ADD COLUMN send_at timestamptz;
-- approved: reviewed, waiting for its time. Only a scheduled draft is ever approved.
ALTER TABLE aura.message_drafts DROP CONSTRAINT message_drafts_status_check;
ALTER TABLE aura.message_drafts ADD CONSTRAINT message_drafts_status_check CHECK (status IN
    ('pending', 'approved', 'dispatching', 'sent', 'failed', 'declined', 'uncertain', 'expired'));
ALTER TABLE aura.message_drafts ADD CONSTRAINT message_drafts_scheduled_chk
    CHECK (scheduled_task_id IS NULL OR send_at IS NOT NULL);
ALTER TABLE aura.message_drafts ADD CONSTRAINT message_drafts_send_at_chk
    CHECK (send_at IS NULL OR expires_at > send_at);
ALTER TABLE aura.message_drafts ADD CONSTRAINT message_drafts_approved_chk
    CHECK (status <> 'approved' OR (send_at IS NOT NULL AND effective_args IS NOT NULL
                                     AND effective_fingerprint IS NOT NULL));
CREATE UNIQUE INDEX message_drafts_task_idx ON aura.message_drafts (scheduled_task_id)
    WHERE scheduled_task_id IS NOT NULL;

-- The model call that parked the task: a retried turn parks the same call again and must
-- find this row, not create a second one (decision 30).
ALTER TABLE aura.scheduler_tasks ADD COLUMN origin_tool_call_id text;
CREATE UNIQUE INDEX scheduler_tasks_origin_call_idx
    ON aura.scheduler_tasks (identity_id, origin_conversation_id, origin_tool_call_id)
    WHERE origin_tool_call_id IS NOT NULL;
