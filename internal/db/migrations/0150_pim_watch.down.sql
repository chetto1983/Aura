-- Reverse 0150: drop the watch's tables and restore the 0142 scheduler_tasks.kind CHECK and the
-- 0138 paused_reason CHECK.
--
-- A watch task violates the narrowed list, so its rows go first or the down aborts mid-chain
-- (same shape as 0091, 0104, 0114 and 0142). Rolling back loses the identity's rules, labels and
-- corrections, which are its own words: a rollback past 0150 needs the pre-upgrade backup to keep
-- them (docs/BACKUP-RESTORE.md).
DROP INDEX IF EXISTS aura.scheduler_tasks_one_pim_digest;
DROP INDEX IF EXISTS aura.scheduler_tasks_one_pim_watch;

DELETE FROM aura.agent_job_runs
WHERE task_id IN (
    SELECT id FROM aura.scheduler_tasks WHERE kind IN ('pim_watch', 'pim_digest')
);

DELETE FROM aura.scheduler_tasks WHERE kind IN ('pim_watch', 'pim_digest');

ALTER TABLE aura.scheduler_tasks DROP CONSTRAINT scheduler_tasks_kind_check;
ALTER TABLE aura.scheduler_tasks ADD CONSTRAINT scheduler_tasks_kind_check
    CHECK (kind IN ('reminder', 'agent_job', 'backup_postgres',
        'skill_ttl_sweep', 'identity_purge', 'sandbox_reap', 'share_expiry_sweep',
        'retention_sweep', 'memory_embed_backfill', 'observability_check',
        'memory_mention_link', 'backup_objects'));

-- A task its deactivated owner left paused stays paused: 'operator' is the reason the narrowed
-- list has for a pause nothing lifts by itself.
UPDATE aura.scheduler_tasks SET paused_reason = 'operator' WHERE paused_reason = 'identity_deactivated';
ALTER TABLE aura.scheduler_tasks DROP CONSTRAINT scheduler_tasks_paused_reason_chk;
ALTER TABLE aura.scheduler_tasks ADD CONSTRAINT scheduler_tasks_paused_reason_chk
    CHECK (paused_reason IN ('operator', 'failures'));

DROP TABLE IF EXISTS aura.pim_watch_corrections;
DROP TABLE IF EXISTS aura.pim_watch_rules;
DROP TABLE IF EXISTS aura.pim_watch_verdicts;
DROP TABLE IF EXISTS aura.pim_watch_cursors;
DROP TABLE IF EXISTS aura.pim_watch_labels;
DROP TABLE IF EXISTS aura.pim_watch_settings;
