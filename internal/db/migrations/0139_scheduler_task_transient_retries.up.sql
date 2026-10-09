-- Transient retry of a scheduled job (prd.md §15, decision of 2026-10-09). Slot measured with
-- `ls internal/db/migrations/ | tail -1`: 0138 was the head.
--
-- transient_retries counts the fires the scheduler re-armed after a job failed on a transient
-- model error before any tool ran. Any reported run outcome and a resume reset it. 0009
-- granted aura_app DML on scheduler_tasks, so no grant change is needed.

ALTER TABLE aura.scheduler_tasks
    ADD COLUMN transient_retries integer NOT NULL DEFAULT 0
        CONSTRAINT scheduler_tasks_transient_retries_chk CHECK (transient_retries >= 0);
