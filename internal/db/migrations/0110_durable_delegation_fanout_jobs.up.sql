-- Migration 0109 was already deployed before the ingestion-job audit found older
-- swarm_delegation payloads without fanout_key. Keep that applied migration
-- immutable and close the remaining durable-queue shape here.

UPDATE aura.ingestion_jobs
SET payload = jsonb_set(payload, '{fanout_key}', to_jsonb('f-' || substr(md5(id::text), 1, 16)), true)
WHERE job_type = 'swarm_delegation'
  AND COALESCE(btrim(payload->>'fanout_key'), '') = '';

ALTER TABLE aura.ingestion_jobs
    ADD CONSTRAINT ingestion_jobs_swarm_fanout_key_chk CHECK (
        job_type <> 'swarm_delegation'
        OR COALESCE(btrim(payload->>'fanout_key'), '') <> ''
    );
