-- Spike 100 probe: does aura.ingestion_jobs already survive the three scenarios
-- D-01 delegates to measurement? Run against the LIVE queue with a synthetic
-- job_type so nothing this writes can ever be claimed by a real processor
-- (ProcessorSet.For dispatches on modality and has no entry for 'spike100').
--
-- Every statement is scoped to job_type='spike100'. Nothing here reads, updates
-- or deletes a row belonging to any other job type.
\set ON_ERROR_STOP on
-- The identity is passed in: aura.ingestion_jobs.identity_id is a FK into
-- aura.identities, so a synthetic all-zero uuid is rejected. Run with:
--   -v ident="$(psql -t -c "SELECT id FROM aura.identities LIMIT 1")"

-- 1. A job whose owner died mid-flight: status='running' with an EXPIRED lease.
-- 2. A job that has burned its retry budget: attempt_count = max_attempts.
-- 3. A job still legitimately held: status='running' with a LIVE lease.
INSERT INTO aura.ingestion_jobs
  (identity_id, job_type, status, idempotency_key, stage, attempt_count, max_attempts,
   locked_by, locked_until, next_attempt_at, payload)
VALUES
  (:'ident', 'spike100', 'running', 'died-midflight', 'work', 1, 8,
   'worker-that-died', now() - interval '1 minute', now() - interval '1 minute', '{}'),
  (:'ident', 'spike100', 'queued',  'budget-burned',  'work', 8, 8,
   NULL, NULL, now() - interval '1 minute', '{}'),
  (:'ident', 'spike100', 'running', 'still-alive',    'work', 1, 8,
   'worker-still-running', now() + interval '10 minutes', now(), '{}')
ON CONFLICT (identity_id, job_type, idempotency_key) DO UPDATE SET updated_at = now();

-- The claim predicate, verbatim from internal/db/queries/ingestion_jobs.sql
-- (ClaimIngestionJobs), applied as a SELECT so it reports rather than mutates.
SELECT idempotency_key,
       status,
       attempt_count || '/' || max_attempts AS attempts,
       CASE WHEN locked_until IS NULL THEN 'none'
            WHEN locked_until < now()   THEN 'expired'
            ELSE 'live' END AS lease,
       (attempt_count < max_attempts
        AND ((status = 'queued' AND next_attempt_at <= now()
              AND (locked_until IS NULL OR locked_until < now()))
             OR (status = 'running' AND locked_until < now()))) AS would_be_reclaimed
FROM aura.ingestion_jobs
WHERE identity_id = :'ident' AND job_type = 'spike100'
ORDER BY idempotency_key;
