-- Settles the assets the document processor left in 'processing'.
--
-- The processor names an uploaded document (and the document leg of an image) the way the
-- ingest sidecar will index it, and used to report 'processing'. Nothing ever moved an asset
-- on from there: the sidecar indexes from the bucket and writes no Postgres row. Measured on
-- the lab VM on 2026-09-28: every 'processing' row, the oldest a week old, sat over an
-- asset_process job that had succeeded. The processor now reports 'complete'.
--
-- Only a row whose asset_process job succeeded is settled. A row whose job is still queued or
-- running is that job's to finish, and a row with no job says nothing about being done.
UPDATE aura.assets AS asset
SET status = 'complete',
    completed_at = COALESCE(asset.completed_at, asset.processed_at, asset.updated_at),
    updated_at = now()
WHERE asset.status = 'processing'
  AND asset.deleted_at IS NULL
  AND EXISTS (
      SELECT 1 FROM aura.ingestion_jobs AS job
      WHERE job.asset_id = asset.id
        AND job.job_type = 'asset_process'
        AND job.status = 'succeeded'
  );
