-- Source: 51-06b (SWARM-06, closing SC#4). Widens the GENERIC aura.ingestion_jobs status
-- vocabulary (migration 0025) with one new value: 'awaiting_input'. This is a
-- generalization of the queue itself (D-01 measured aura.ingestion_jobs as the right
-- substrate for durable background work), NOT a swarm-specific column -- any future
-- job_type that needs to park on a human answer reuses this same status.
--
-- Invariant this status enforces: an 'awaiting_input' row is NON-TERMINAL (it is not
-- succeeded/failed/dead_letter/canceled -- the work is not over) and NON-CLAIMABLE (it is
-- not 'queued' or 'running' -- ClaimIngestionJobs's own WHERE clause already excludes any
-- status outside those two, so widening the CHECK is the only schema change a parked row
-- needs to become invisible to every existing claim loop, both the delegation claim loop
-- and the document ingestion worker, with zero query changes).
--
-- The shipped status CHECK on aura.ingestion_jobs.status is an INLINE, UNNAMED column
-- constraint (migration 0025), so Postgres auto-named it by the usual <table>_<column>_check
-- rule. Verified against the live schema at landing time (2026-08-28):
--   SELECT conname FROM pg_constraint WHERE conrelid = 'aura.ingestion_jobs'::regclass
--     AND contype = 'c';
--   -> ingestion_jobs_status_check
-- DROP CONSTRAINT IF EXISTS makes a divergent auto-name (a different Postgres version,
-- a hand-edited schema) a loud no-op rather than a failed migration -- the ADD CONSTRAINT
-- right after still lands the widened vocabulary either way.
ALTER TABLE aura.ingestion_jobs
    DROP CONSTRAINT IF EXISTS ingestion_jobs_status_check,
    ADD CONSTRAINT ingestion_jobs_status_check CHECK (status IN (
        'queued', 'running', 'succeeded', 'failed', 'dead_letter', 'canceled', 'awaiting_input'
    ));

COMMENT ON CONSTRAINT ingestion_jobs_status_check ON aura.ingestion_jobs IS
    'D-12/D-13 (51-06b): awaiting_input is a background worker''s pause parking its queue '
    'row -- non-terminal (the work is not over) and non-claimable (it is waiting on a '
    'human, not on a worker). It is added, not removed or renamed, from the 0025 '
    'vocabulary (queued, running, succeeded, failed, dead_letter, canceled).';
