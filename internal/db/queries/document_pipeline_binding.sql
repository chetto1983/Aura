-- name: BindPipelineJobCandidate :one
WITH lease AS MATERIALIZED (
    SELECT job.id, job.identity_id
    FROM aura.ingestion_jobs job
    WHERE job.id = sqlc.arg(job_id)
      AND job.identity_id = sqlc.arg(identity_id)
      AND job.status = 'running'
      AND job.locked_by = sqlc.arg(locked_by)
      AND job.lease_generation = sqlc.arg(lease_generation)
      AND job.locked_until > now()
    FOR UPDATE
), candidate AS MATERIALIZED (
    SELECT lease.id AS job_id, lease.identity_id
    FROM lease
    JOIN aura.ingestion_jobs job
      ON job.id = lease.id AND job.identity_id = lease.identity_id
    JOIN aura.assets asset
      ON asset.id = sqlc.arg(asset_id)
     AND asset.identity_id = lease.identity_id
     AND asset.id = job.asset_id
     AND asset.deleted_at IS NULL
     AND asset.status NOT IN ('failed', 'refused', 'deleting', 'deleted', 'canceled')
    JOIN aura.documents document
      ON document.id = sqlc.arg(document_id)
     AND document.identity_id = lease.identity_id
     AND document.deleted_at IS NULL
     AND document.status NOT IN ('deleting', 'deleted')
    JOIN aura.document_versions version
      ON version.id = sqlc.arg(version_id)
     AND version.identity_id = lease.identity_id
     AND version.document_id = document.id
     AND version.pipeline_generation = sqlc.arg(pipeline_generation)
     AND version.deleted_at IS NULL
     AND version.status NOT IN ('failed', 'deleting', 'deleted', 'archived')
    WHERE asset.catalog_document_id = document.id
      AND asset.document_version_id = version.id
      AND asset.pipeline_generation = version.pipeline_generation
      AND (job.document_id IS NULL OR job.document_id = document.id)
      AND (job.version_id IS NULL OR job.version_id = version.id)
      AND (job.pipeline_generation = 0 OR job.pipeline_generation = version.pipeline_generation)
), bound AS (
    UPDATE aura.ingestion_jobs job
    SET document_id = sqlc.arg(document_id),
        version_id = sqlc.arg(version_id),
        pipeline_generation = sqlc.arg(pipeline_generation),
        stage = sqlc.arg(stage), updated_at = now()
    FROM candidate
    WHERE job.id = candidate.job_id
      AND job.identity_id = candidate.identity_id
    RETURNING job.id
)
SELECT EXISTS (SELECT 1 FROM bound) AS bound,
       EXISTS (SELECT 1 FROM lease) AS lease_valid;
