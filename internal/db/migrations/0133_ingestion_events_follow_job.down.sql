-- Puts back the keys that leave a deleted job's events behind with job_id NULL. The events the
-- up migration removed named jobs that no longer existed and cannot be restored.
ALTER TABLE aura.ingestion_events
    DROP CONSTRAINT ingestion_events_job_id_fkey,
    DROP CONSTRAINT ingestion_events_job_identity_fkey,
    ADD CONSTRAINT ingestion_events_job_id_fkey
        FOREIGN KEY (job_id) REFERENCES aura.ingestion_jobs(id) ON DELETE SET NULL,
    ADD CONSTRAINT ingestion_events_job_identity_fkey
        FOREIGN KEY (job_id, identity_id)
        REFERENCES aura.ingestion_jobs(id, identity_id) ON DELETE SET NULL (job_id);
