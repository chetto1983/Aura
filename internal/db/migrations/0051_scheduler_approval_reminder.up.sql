-- Source: fix-plan 1.7 / BUG-1B (PRD Amendment #92). A task in
-- status='pending_approval' that the model never relays is silently forgotten.
-- The per-tick reminder sweep re-surfaces such tasks to their origin channel and
-- throttles via this column: it stamps approval_reminded_at=now() after each
-- attempt so a pending approval re-nudges at most once per
-- AURA_SCHEDULER_APPROVAL_REMINDER_SEC. Nullable = never reminded yet. Leaving
-- pending_approval (approve/cancel) ends the reminders by construction, so no
-- backfill and no cleanup. aura_app already holds DML on scheduler_tasks; a new
-- column inherits those grants.
ALTER TABLE aura.scheduler_tasks ADD COLUMN approval_reminded_at timestamptz;
