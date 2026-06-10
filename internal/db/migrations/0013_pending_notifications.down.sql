-- Reverse 0013: pending_notifications depends on agent_job_runs and owns its
-- indexes, so dropping the table is sufficient and child-first relative to 0009.
DROP TABLE IF EXISTS aura.pending_notifications;
