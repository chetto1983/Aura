-- Widen aura.pending_notifications (0013/0014) to accept a second kind of owner: an
-- aura.steer_queue row (migration 0103) whose absent-operator channel push failed
-- owns-but-failed. Plan 51-10 (SWARM-03/09) needs a retry-outbox row for that leg of
-- the delegation nudge sweep, and MUST NOT build a second messaging/outbox table to do
-- it (D-02, spike 101) -- but the shipped table's `run_id uuid NOT NULL REFERENCES
-- aura.agent_job_runs` makes every insert from the nudge sweep fail its FK: the row
-- being retried is a steer_queue row, which has no agent_job_runs counterpart. The
-- row's OWNING aura.ingestion_jobs job (the delegation claim) is also the wrong key --
-- by the time a nudge fires (a grace window after the worker already finished and
-- Task 1's record succeeded, transitioning that job to `succeeded`), the retry is for
-- DELIVERING THE STEER ROW to a channel, not for the already-finished job.
--
-- run_id becomes NULLable rather than being replaced -- every existing cron caller
-- (internal/cron/store_runs.go's InsertPendingNotification, internal/cron/deliver.go's
-- owns-but-failed leg) keeps writing run_id exactly as before, byte-for-byte, and the
-- ON DELETE CASCADE from agent_job_runs is unchanged for those rows. steer_queue_id is
-- the new sibling reference, ON DELETE CASCADE from aura.steer_queue so a retry-outbox
-- row can never outlive the steer row it retries delivery for. The CHECK constraint
-- keeps "audit-forever, cleaned up by its owner's cascade" true for every row: exactly
-- the reason run_id was NOT NULL in the first place (0013's own comment, "Rows are
-- cleaned up by agent_job_runs cascade") now generalized to "cleaned up by WHICHEVER
-- owner it has".
ALTER TABLE aura.pending_notifications ALTER COLUMN run_id DROP NOT NULL;

ALTER TABLE aura.pending_notifications
    ADD COLUMN steer_queue_id uuid REFERENCES aura.steer_queue(id) ON DELETE CASCADE;

ALTER TABLE aura.pending_notifications
    ADD CONSTRAINT pending_notifications_owner_chk
    CHECK (run_id IS NOT NULL OR steer_queue_id IS NOT NULL);

COMMENT ON COLUMN aura.pending_notifications.steer_queue_id IS
    'Plan 51-10: the owns-but-failed leg of the delegation nudge sweep (SWARM-03/09) '
    'inserts a retry row keyed on the aura.steer_queue row it is retrying delivery for '
    '-- NOT the (by then already-succeeded) aura.ingestion_jobs delegation row, since '
    'the nudge fires only after that job is long finished. NULL for every scheduler- '
    'originated row (unchanged). ON DELETE CASCADE mirrors run_id''s own cleanup '
    'contract so a retry row can never outlive its owner.';

COMMENT ON CONSTRAINT pending_notifications_owner_chk ON aura.pending_notifications IS
    'Every row must still be owned by exactly one cascade-deleting parent (run_id OR '
    'steer_queue_id) -- generalizes 0013''s "audit-forever, cleaned up by its owner''s '
    'cascade" invariant to the second owner kind this migration adds.';

-- No new GRANT: the existing aura_app DML grant (0013) already covers the new column.
