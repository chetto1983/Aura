-- Roll back 0107: refuse if any row is CURRENTLY parked awaiting a human answer.
-- Silently narrowing the CHECK back would either fail the ALTER (a live 'awaiting_input'
-- row already violates the narrower vocabulary) or, if Postgres somehow let it through,
-- leave a row whose status the narrower CHECK cannot describe -- loud failure is the
-- honest outcome, mirroring 0093's own "data that cannot fit the old schema blocks
-- rollback" precedent.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM aura.ingestion_jobs WHERE status = 'awaiting_input') THEN
        RAISE EXCEPTION '0107 rollback blocked: a row is parked awaiting_input -- answer or expire it before rolling back';
    END IF;
END $$;

ALTER TABLE aura.ingestion_jobs
    DROP CONSTRAINT IF EXISTS ingestion_jobs_status_check,
    ADD CONSTRAINT ingestion_jobs_status_check CHECK (status IN (
        'queued', 'running', 'succeeded', 'failed', 'dead_letter', 'canceled'
    ));
