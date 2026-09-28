-- An ingestion job's events leave with it.
--
-- Every event is one transition of one ingestion job: each writer names the job as both
-- entity_id and job_id. 0025 and 0093 gave both keys on job_id ON DELETE SET NULL, so when a
-- job went -- and a job goes with its asset, whose delete cascades to it -- its events stayed
-- behind naming a job that no longer exists, read by no query. Measured on the lab VM on
-- 2026-09-28: 384 of 406 events had job_id NULL, all written since 2026-09-27 17:48 and left
-- by the asset deletes the retention sweep finished. aura.audit_logs is the table kept
-- forever; this one grants aura_app DELETE.
DELETE FROM aura.ingestion_events WHERE job_id IS NULL;

ALTER TABLE aura.ingestion_events
    DROP CONSTRAINT ingestion_events_job_id_fkey,
    DROP CONSTRAINT ingestion_events_job_identity_fkey,
    ADD CONSTRAINT ingestion_events_job_id_fkey
        FOREIGN KEY (job_id) REFERENCES aura.ingestion_jobs(id) ON DELETE CASCADE,
    ADD CONSTRAINT ingestion_events_job_identity_fkey
        FOREIGN KEY (job_id, identity_id)
        REFERENCES aura.ingestion_jobs(id, identity_id) ON DELETE CASCADE;
