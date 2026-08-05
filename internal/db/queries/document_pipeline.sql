-- name: ReservePipelineCandidateVersion :one
WITH locked_document AS MATERIALIZED (
    SELECT document.id, document.identity_id, document.search_document_id,
           document.pipeline_generation
    FROM aura.documents document
    WHERE document.id = sqlc.arg(document_id)
      AND document.identity_id = sqlc.arg(identity_id)
      AND document.deleted_at IS NULL
      AND document.status NOT IN ('deleting', 'deleted')
    FOR UPDATE
), existing AS MATERIALIZED (
    SELECT version.*
    FROM aura.document_versions version
    JOIN locked_document document ON document.id = version.document_id
    JOIN aura.assets asset
      ON asset.id = version.asset_id
     AND asset.identity_id = version.identity_id
     AND asset.deleted_at IS NULL
     AND asset.status NOT IN ('refused', 'deleting', 'deleted', 'canceled')
    JOIN aura.storage_objects raw_object
      ON raw_object.id = version.storage_object_id
     AND raw_object.identity_id = version.identity_id
     AND raw_object.status = 'live'
     AND raw_object.deleted_at IS NULL
     AND raw_object.kind = 'raw'
    WHERE version.sha256 = sqlc.arg(sha256)
      AND version.deleted_at IS NULL
    LIMIT 1
), next_slot AS MATERIALIZED (
    SELECT document.id AS document_id, document.identity_id,
           document.search_document_id,
           COALESCE(max(version.version_number), 0)::integer + 1 AS version_number,
           GREATEST(
               document.pipeline_generation,
               COALESCE(max(version.pipeline_generation), 0),
               COALESCE(max(version.version_number), 0)
           ) + 1 AS pipeline_generation
    FROM locked_document document
    LEFT JOIN aura.document_versions version ON version.document_id = document.id
    JOIN aura.assets asset
      ON asset.id = sqlc.arg(asset_id)
     AND asset.identity_id = document.identity_id
     AND asset.deleted_at IS NULL
     AND asset.status NOT IN ('refused', 'deleting', 'deleted', 'canceled')
    JOIN aura.storage_objects raw_object
      ON raw_object.id = sqlc.arg(storage_object_id)
     AND raw_object.identity_id = document.identity_id
     AND raw_object.document_id = document.id
     AND raw_object.asset_id = asset.id
     AND raw_object.version_id IS NULL
     AND raw_object.status = 'live'
     AND raw_object.deleted_at IS NULL
     AND raw_object.kind = 'raw'
     AND raw_object.sha256 = sqlc.arg(sha256)
    WHERE NOT EXISTS (SELECT 1 FROM existing)
    GROUP BY document.id, document.identity_id, document.search_document_id,
             document.pipeline_generation
), inserted AS (
    INSERT INTO aura.document_versions (
        id, identity_id, document_id, asset_id, version_number, status,
        sha1, sha256, content_type, size_bytes, storage_object_id,
        chunking_config_hash, pipeline_config_hash, search_document_id,
        pipeline_generation
    )
    SELECT sqlc.arg(id), slot.identity_id, slot.document_id, sqlc.arg(asset_id),
           slot.version_number, sqlc.arg(status), sqlc.arg(sha1), sqlc.arg(sha256),
           sqlc.arg(content_type), sqlc.arg(size_bytes), sqlc.arg(storage_object_id),
           sqlc.arg(chunking_config_hash), sqlc.arg(pipeline_config_hash),
           slot.search_document_id, slot.pipeline_generation
    FROM next_slot slot
    RETURNING *
), selected AS MATERIALIZED (
    SELECT existing.*, true AS replayed FROM existing
    UNION ALL
    SELECT inserted.*, false AS replayed FROM inserted
), linked_object AS (
    UPDATE aura.storage_objects raw_object
    SET version_id = selected.id,
        pipeline_generation = selected.pipeline_generation
    FROM selected
    WHERE raw_object.id = selected.storage_object_id
      AND raw_object.identity_id = selected.identity_id
    RETURNING raw_object.id
), linked_asset AS (
    UPDATE aura.assets asset
    SET catalog_document_id = selected.document_id,
        document_version_id = selected.id,
        pipeline_generation = selected.pipeline_generation,
        updated_at = now()
    FROM selected
    WHERE asset.id = selected.asset_id
      AND asset.identity_id = selected.identity_id
    RETURNING asset.id
)
SELECT selected.id, selected.document_id, selected.asset_id,
       selected.version_number, selected.status, selected.sha1, selected.sha256,
       selected.content_type, selected.size_bytes, selected.storage_object_id,
       selected.chunking_config_hash, selected.pipeline_config_hash,
       selected.ready_at, selected.activated_at, selected.created_at,
       selected.updated_at, selected.deleted_at, selected.error_code,
       selected.error_message, selected.identity_id, selected.search_document_id,
       selected.pipeline_generation, selected.replayed
FROM selected
JOIN linked_object ON linked_object.id = selected.storage_object_id
JOIN linked_asset ON linked_asset.id = selected.asset_id;

-- name: GetPipelineCandidateVersion :one
SELECT version.version_number, version.sha256, version.search_document_id,
       version.pipeline_generation
FROM aura.document_versions version
JOIN aura.documents document
  ON document.id = version.document_id
 AND document.identity_id = version.identity_id
WHERE version.id = sqlc.arg(version_id)
  AND version.identity_id = sqlc.arg(identity_id)
  AND version.document_id = sqlc.arg(document_id)
  AND version.deleted_at IS NULL
  AND document.deleted_at IS NULL
  AND document.status NOT IN ('deleting', 'deleted')
FOR UPDATE OF version, document;

-- name: RecordPipelineDerivedArtifact :one
WITH eligible AS MATERIALIZED (
    SELECT version.id
    FROM aura.document_versions version
    JOIN aura.documents document
      ON document.id = version.document_id
     AND document.identity_id = version.identity_id
    WHERE version.id = sqlc.arg(version_id)
      AND version.identity_id = sqlc.arg(identity_id)
      AND version.document_id = sqlc.arg(document_id)
      AND version.pipeline_generation = sqlc.arg(pipeline_generation)
      AND version.deleted_at IS NULL
      AND document.deleted_at IS NULL
      AND document.status NOT IN ('deleting', 'deleted')
    FOR UPDATE OF version, document
)
INSERT INTO aura.storage_objects (
    identity_id, document_id, version_id, bucket, object_key, kind,
    sha256, size_bytes, content_type, retention_class, status,
    pipeline_generation
)
SELECT sqlc.arg(identity_id), sqlc.arg(document_id), sqlc.arg(version_id),
       sqlc.arg(bucket), sqlc.arg(object_key), sqlc.arg(kind), sqlc.arg(sha256),
       sqlc.arg(size_bytes), sqlc.arg(content_type), sqlc.arg(retention_class),
       'live', sqlc.arg(pipeline_generation)
FROM eligible
ON CONFLICT (bucket, object_key) DO UPDATE SET
    object_key = aura.storage_objects.object_key
WHERE aura.storage_objects.identity_id = EXCLUDED.identity_id
  AND aura.storage_objects.document_id = EXCLUDED.document_id
  AND aura.storage_objects.version_id = EXCLUDED.version_id
  AND aura.storage_objects.kind = EXCLUDED.kind
  AND aura.storage_objects.sha256 = EXCLUDED.sha256
  AND aura.storage_objects.size_bytes = EXCLUDED.size_bytes
  AND aura.storage_objects.content_type = EXCLUDED.content_type
  AND aura.storage_objects.retention_class = EXCLUDED.retention_class
  AND aura.storage_objects.status = 'live'
  AND aura.storage_objects.pipeline_generation = EXCLUDED.pipeline_generation
  AND aura.storage_objects.deleted_at IS NULL
RETURNING id;

-- name: UpsertPipelineCandidateChunk :one
INSERT INTO aura.document_chunks (
    id, identity_id, document_id, version_id, chunk_index, chunk_hash, text,
    locator, active, search_document_id, version_number, original_sha256,
    pipeline_generation, ordinal, normalized_text_sha256, self_ref,
    heading_path, captions, page_number, bounding_box, char_start, char_end,
    sheet_name, table_name, row_number, column_number, cell_ref
) VALUES (
    sqlc.arg(id), sqlc.arg(identity_id), sqlc.arg(document_id), sqlc.arg(version_id),
    sqlc.arg(ordinal), sqlc.arg(normalized_text_sha256), sqlc.arg(text),
    sqlc.arg(locator), false, sqlc.arg(search_document_id), sqlc.arg(version_number),
    sqlc.arg(original_sha256), sqlc.arg(pipeline_generation), sqlc.arg(ordinal),
    sqlc.arg(normalized_text_sha256), sqlc.arg(self_ref), sqlc.arg(heading_path),
    sqlc.arg(captions), sqlc.narg(page_number), sqlc.narg(bounding_box),
    sqlc.narg(char_start), sqlc.narg(char_end), sqlc.narg(sheet_name),
    sqlc.narg(table_name), sqlc.narg(row_number), sqlc.narg(column_number),
    sqlc.narg(cell_ref)
)
ON CONFLICT (id) DO UPDATE SET
    text = EXCLUDED.text,
    locator = EXCLUDED.locator,
    normalized_text_sha256 = EXCLUDED.normalized_text_sha256,
    self_ref = EXCLUDED.self_ref,
    heading_path = EXCLUDED.heading_path,
    captions = EXCLUDED.captions,
    page_number = EXCLUDED.page_number,
    bounding_box = EXCLUDED.bounding_box,
    char_start = EXCLUDED.char_start,
    char_end = EXCLUDED.char_end,
    sheet_name = EXCLUDED.sheet_name,
    table_name = EXCLUDED.table_name,
    row_number = EXCLUDED.row_number,
    column_number = EXCLUDED.column_number,
    cell_ref = EXCLUDED.cell_ref
WHERE aura.document_chunks.identity_id = EXCLUDED.identity_id
  AND aura.document_chunks.document_id = EXCLUDED.document_id
  AND aura.document_chunks.version_id = EXCLUDED.version_id
  AND aura.document_chunks.search_document_id = EXCLUDED.search_document_id
  AND aura.document_chunks.version_number = EXCLUDED.version_number
  AND aura.document_chunks.original_sha256 = EXCLUDED.original_sha256
  AND aura.document_chunks.pipeline_generation = EXCLUDED.pipeline_generation
  AND aura.document_chunks.ordinal = EXCLUDED.ordinal
  AND aura.document_chunks.deleted_at IS NULL
RETURNING *;

-- name: ListPipelineCandidateChunks :many
SELECT *
FROM aura.document_chunks
WHERE identity_id = sqlc.arg(identity_id)
  AND version_id = sqlc.arg(version_id)
  AND pipeline_generation = sqlc.arg(pipeline_generation)
  AND deleted_at IS NULL
ORDER BY ordinal, id;

-- name: UpsertPipelineCandidateEmbedding :one
INSERT INTO aura.document_embeddings (
    id, identity_id, document_id, version_id, chunk_id, embedding_model,
    embedding_version, embedding_dim, vector_namespace, vector_id, active,
    pipeline_generation, embedding_fingerprint, projection_status, projected_at
) VALUES (
    sqlc.arg(id), sqlc.arg(identity_id), sqlc.arg(document_id), sqlc.arg(version_id),
    sqlc.arg(chunk_id), sqlc.arg(embedding_model), sqlc.arg(embedding_version),
    sqlc.arg(embedding_dim), sqlc.arg(vector_namespace), sqlc.arg(vector_id), false,
    sqlc.arg(pipeline_generation), sqlc.arg(embedding_fingerprint),
    sqlc.arg(projection_status),
    CASE WHEN sqlc.arg(projection_status)::text = 'active' THEN now() ELSE NULL END
)
ON CONFLICT (id) DO UPDATE SET
    projection_status = CASE
        WHEN EXCLUDED.projection_status = 'active' THEN 'active'
        ELSE aura.document_embeddings.projection_status
    END,
    projected_at = CASE
        WHEN EXCLUDED.projection_status = 'active'
            THEN COALESCE(aura.document_embeddings.projected_at, now())
        ELSE aura.document_embeddings.projected_at
    END
WHERE aura.document_embeddings.identity_id = EXCLUDED.identity_id
  AND aura.document_embeddings.document_id = EXCLUDED.document_id
  AND aura.document_embeddings.version_id = EXCLUDED.version_id
  AND aura.document_embeddings.chunk_id = EXCLUDED.chunk_id
  AND aura.document_embeddings.embedding_model = EXCLUDED.embedding_model
  AND aura.document_embeddings.embedding_version = EXCLUDED.embedding_version
  AND aura.document_embeddings.embedding_dim = EXCLUDED.embedding_dim
  AND aura.document_embeddings.vector_namespace = EXCLUDED.vector_namespace
  AND aura.document_embeddings.vector_id = EXCLUDED.vector_id
  AND aura.document_embeddings.pipeline_generation = EXCLUDED.pipeline_generation
  AND aura.document_embeddings.embedding_fingerprint = EXCLUDED.embedding_fingerprint
  AND aura.document_embeddings.deleted_at IS NULL
RETURNING *;

-- name: ListPipelineCandidateEmbeddings :many
SELECT embedding.*
FROM aura.document_embeddings embedding
JOIN aura.document_chunks chunk
  ON chunk.id = embedding.chunk_id
 AND chunk.identity_id = embedding.identity_id
WHERE embedding.identity_id = sqlc.arg(identity_id)
  AND embedding.version_id = sqlc.arg(version_id)
  AND embedding.pipeline_generation = sqlc.arg(pipeline_generation)
  AND embedding.deleted_at IS NULL
  AND chunk.version_id = embedding.version_id
  AND chunk.pipeline_generation = embedding.pipeline_generation
  AND chunk.deleted_at IS NULL
ORDER BY chunk.ordinal, embedding.id;

-- name: UpsertPipelineStageLedger :one
INSERT INTO aura.document_pipeline_stages (
    identity_id, document_id, version_id, stage, input_fingerprint,
    producer_version, pipeline_generation, status, max_attempts,
    next_attempt_at, diagnostic_state
)
SELECT sqlc.arg(identity_id), sqlc.arg(document_id), sqlc.arg(version_id),
       sqlc.arg(stage), sqlc.arg(input_fingerprint), sqlc.arg(producer_version),
       sqlc.arg(pipeline_generation), 'pending', sqlc.arg(max_attempts),
       sqlc.arg(next_attempt_at), sqlc.arg(diagnostic_state)
FROM aura.document_versions version
JOIN aura.documents document
  ON document.id = version.document_id
 AND document.identity_id = version.identity_id
WHERE version.id = sqlc.arg(version_id)
  AND version.identity_id = sqlc.arg(identity_id)
  AND version.document_id = sqlc.arg(document_id)
  AND version.pipeline_generation = sqlc.arg(pipeline_generation)
  AND version.deleted_at IS NULL
  AND document.deleted_at IS NULL
  AND document.status NOT IN ('deleting', 'deleted')
ON CONFLICT (identity_id, version_id, stage, input_fingerprint) DO UPDATE SET
    updated_at = aura.document_pipeline_stages.updated_at
WHERE aura.document_pipeline_stages.document_id = EXCLUDED.document_id
  AND aura.document_pipeline_stages.producer_version = EXCLUDED.producer_version
  AND aura.document_pipeline_stages.pipeline_generation = EXCLUDED.pipeline_generation
RETURNING *;

-- name: ClaimPipelineStageLedger :one
WITH candidate AS (
    SELECT ledger.id
    FROM aura.document_pipeline_stages ledger
    WHERE ledger.id = sqlc.arg(id)
      AND ledger.identity_id = sqlc.arg(identity_id)
      AND ledger.stage = sqlc.arg(stage)
      AND ledger.attempt_count < ledger.max_attempts
      AND (
          (ledger.status = 'pending' AND ledger.next_attempt_at <= now())
          OR (ledger.status = 'running' AND ledger.locked_until < now())
      )
    FOR UPDATE SKIP LOCKED
)
UPDATE aura.document_pipeline_stages ledger
SET status = 'running',
    attempt_count = ledger.attempt_count + 1,
    lease_generation = ledger.lease_generation + 1,
    locked_by = sqlc.arg(locked_by),
    locked_until = now() + sqlc.arg(lease_duration)::interval,
    error_class = '', error_code = '', error_message = '',
    completed_at = NULL, updated_at = now()
FROM candidate
WHERE ledger.id = candidate.id
RETURNING ledger.*;

-- name: HeartbeatPipelineStageLedger :one
UPDATE aura.document_pipeline_stages
SET locked_until = now() + sqlc.arg(lease_duration)::interval,
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND identity_id = sqlc.arg(identity_id)
  AND status = 'running'
  AND locked_by = sqlc.arg(locked_by)
  AND lease_generation = sqlc.arg(lease_generation)
  AND locked_until > now()
RETURNING id;

-- name: CompletePipelineStageLedger :one
UPDATE aura.document_pipeline_stages
SET status = 'succeeded',
    artifact_storage_object_id = sqlc.narg(artifact_storage_object_id),
    artifact_object_key = sqlc.arg(artifact_object_key),
    artifact_sha256 = sqlc.arg(artifact_sha256),
    artifact_size_bytes = sqlc.arg(artifact_size_bytes),
    diagnostic_state = sqlc.arg(diagnostic_state),
    error_class = '', error_code = '', error_message = '',
    locked_by = NULL, locked_until = NULL,
    completed_at = now(), updated_at = now()
WHERE id = sqlc.arg(id)
  AND identity_id = sqlc.arg(identity_id)
  AND status = 'running'
  AND locked_by = sqlc.arg(locked_by)
  AND lease_generation = sqlc.arg(lease_generation)
  AND locked_until > now()
RETURNING id;

-- name: FailPipelineStageLedger :one
UPDATE aura.document_pipeline_stages
SET status = CASE
        WHEN sqlc.arg(retryable)::boolean AND attempt_count < max_attempts THEN 'pending'
        WHEN sqlc.arg(retryable)::boolean THEN 'dead_letter'
        ELSE 'failed'
    END,
    error_class = sqlc.arg(error_class),
    error_code = sqlc.arg(error_code),
    error_message = sqlc.arg(error_message),
    next_attempt_at = sqlc.arg(next_attempt_at),
    locked_by = NULL, locked_until = NULL,
    completed_at = CASE
        WHEN sqlc.arg(retryable)::boolean AND attempt_count < max_attempts THEN NULL
        ELSE now()
    END,
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND identity_id = sqlc.arg(identity_id)
  AND status = 'running'
  AND locked_by = sqlc.arg(locked_by)
  AND lease_generation = sqlc.arg(lease_generation)
  AND locked_until > now()
RETURNING id;

-- name: ActivatePipelineCandidate :one
WITH candidate AS MATERIALIZED (
    SELECT document.id AS document_id,
           document.identity_id,
           document.status AS document_status,
           document.pipeline_generation AS prior_generation,
           version.id AS version_id,
           version.version_number,
           version.sha256 AS raw_sha256,
           asset.id AS asset_id,
           job.id AS job_id,
           job.attempt_generation,
           job.lease_generation
    FROM aura.documents document
    JOIN aura.document_versions version
      ON version.id = sqlc.arg(version_id)
     AND version.identity_id = document.identity_id
     AND version.document_id = document.id
    JOIN aura.assets asset
      ON asset.id = sqlc.arg(asset_id)
     AND asset.identity_id = document.identity_id
    JOIN aura.storage_objects raw_object
      ON raw_object.id = version.storage_object_id
     AND raw_object.identity_id = document.identity_id
     AND raw_object.document_id = document.id
     AND raw_object.version_id = version.id
    JOIN aura.ingestion_jobs job
      ON job.id = sqlc.arg(job_id)
     AND job.identity_id = document.identity_id
     AND job.document_id = document.id
     AND job.version_id = version.id
     AND job.asset_id = asset.id
    WHERE document.id = sqlc.arg(document_id)
      AND document.identity_id = sqlc.arg(identity_id)
      AND document.deleted_at IS NULL
      AND document.status NOT IN ('deleting', 'deleted')
      AND document.pipeline_generation <= sqlc.arg(pipeline_generation)
      AND (
          document.active_version_id IS NULL
          OR document.active_version_id = version.id
          OR document.pipeline_generation < sqlc.arg(pipeline_generation)
      )
      AND version.deleted_at IS NULL
      AND version.status NOT IN ('failed', 'deleting', 'deleted', 'archived')
      AND version.pipeline_generation = sqlc.arg(pipeline_generation)
      AND version.search_document_id = document.search_document_id
      AND NOT EXISTS (
          SELECT 1 FROM aura.document_versions newer
          WHERE newer.identity_id = document.identity_id
            AND newer.document_id = document.id
            AND newer.deleted_at IS NULL
            AND newer.pipeline_generation > sqlc.arg(pipeline_generation)
      )
      AND asset.deleted_at IS NULL
      AND asset.status IN ('accepted', 'processing', 'embedding', 'complete', 'searchable')
      AND asset.catalog_document_id = document.id
      AND asset.document_version_id = version.id
      AND raw_object.deleted_at IS NULL
      AND raw_object.status = 'live'
      AND raw_object.kind = 'raw'
      AND raw_object.sha256 = version.sha256
      AND job.status = 'running'
      AND job.locked_by = sqlc.arg(locked_by)
      AND job.locked_until > now()
      AND job.lease_generation = sqlc.arg(lease_generation)
      AND job.pipeline_generation = sqlc.arg(pipeline_generation)
    FOR UPDATE OF document, version, asset, raw_object, job
), eligible AS MATERIALIZED (
    SELECT candidate.*
    FROM candidate
    WHERE (SELECT count(*) FROM aura.document_chunks chunk
           WHERE chunk.identity_id = sqlc.arg(identity_id)
             AND chunk.version_id = sqlc.arg(version_id)
             AND chunk.pipeline_generation = sqlc.arg(pipeline_generation)
             AND chunk.deleted_at IS NULL) = sqlc.arg(expected_passage_count)::bigint
      AND (SELECT count(*) FROM aura.document_embeddings embedding
           WHERE embedding.identity_id = sqlc.arg(identity_id)
             AND embedding.version_id = sqlc.arg(version_id)
             AND embedding.pipeline_generation = sqlc.arg(pipeline_generation)
             AND embedding.deleted_at IS NULL) = sqlc.arg(expected_embedding_count)::bigint
      AND (SELECT count(*)
           FROM aura.document_embeddings embedding
           JOIN aura.document_chunks chunk
             ON chunk.id = embedding.chunk_id
            AND chunk.identity_id = embedding.identity_id
            AND chunk.version_id = embedding.version_id
            AND chunk.pipeline_generation = embedding.pipeline_generation
           WHERE embedding.identity_id = sqlc.arg(identity_id)
             AND embedding.version_id = sqlc.arg(version_id)
             AND embedding.pipeline_generation = sqlc.arg(pipeline_generation)
             AND embedding.deleted_at IS NULL
             AND chunk.deleted_at IS NULL) = sqlc.arg(expected_embedding_count)::bigint
      AND (SELECT count(*)
           FROM aura.document_embeddings embedding
           JOIN aura.document_chunks chunk
             ON chunk.id = embedding.chunk_id
            AND chunk.identity_id = embedding.identity_id
            AND chunk.version_id = embedding.version_id
            AND chunk.pipeline_generation = embedding.pipeline_generation
           WHERE embedding.identity_id = sqlc.arg(identity_id)
             AND embedding.version_id = sqlc.arg(version_id)
             AND embedding.pipeline_generation = sqlc.arg(pipeline_generation)
             AND embedding.deleted_at IS NULL
             AND chunk.deleted_at IS NULL
             AND embedding.projection_status = 'active'
             AND embedding.projected_at IS NOT NULL) = sqlc.arg(expected_projection_count)::bigint
      AND sqlc.arg(expected_passage_count)::bigint > 0
      AND sqlc.arg(expected_passage_count)::bigint = sqlc.arg(expected_embedding_count)::bigint
      AND sqlc.arg(expected_passage_count)::bigint = sqlc.arg(expected_projection_count)::bigint
), deactivated_chunks AS (
    UPDATE aura.document_chunks chunk
    SET active = false
    FROM eligible
    WHERE chunk.identity_id = eligible.identity_id
      AND chunk.document_id = eligible.document_id
      AND chunk.active
      AND (
          chunk.version_id <> eligible.version_id
          OR chunk.pipeline_generation <> sqlc.arg(pipeline_generation)
      )
    RETURNING chunk.id
), activated_chunks AS (
    UPDATE aura.document_chunks chunk
    SET active = true
    FROM eligible
    WHERE chunk.identity_id = eligible.identity_id
      AND chunk.version_id = eligible.version_id
      AND chunk.pipeline_generation = sqlc.arg(pipeline_generation)
      AND chunk.deleted_at IS NULL
    RETURNING chunk.id
), deactivated_embeddings AS (
    UPDATE aura.document_embeddings embedding
    SET active = false
    FROM eligible
    WHERE embedding.identity_id = eligible.identity_id
      AND embedding.document_id = eligible.document_id
      AND embedding.active
      AND (
          embedding.version_id <> eligible.version_id
          OR embedding.pipeline_generation <> sqlc.arg(pipeline_generation)
      )
    RETURNING embedding.id
), activated_embeddings AS (
    UPDATE aura.document_embeddings embedding
    SET active = true
    FROM eligible
    WHERE embedding.identity_id = eligible.identity_id
      AND embedding.version_id = eligible.version_id
      AND embedding.pipeline_generation = sqlc.arg(pipeline_generation)
      AND embedding.projection_status = 'active'
      AND embedding.projected_at IS NOT NULL
      AND embedding.deleted_at IS NULL
    RETURNING embedding.id
), ready_version AS (
    UPDATE aura.document_versions version
    SET status = 'ready', ready_at = COALESCE(version.ready_at, now()),
        activated_at = now(), error_code = '', error_message = '', updated_at = now()
    FROM eligible
    WHERE version.id = eligible.version_id
      AND version.identity_id = eligible.identity_id
    RETURNING version.id
), ready_document AS (
    UPDATE aura.documents document
    SET active_version_id = eligible.version_id,
        pipeline_generation = sqlc.arg(pipeline_generation),
        status = 'ready', error_code = '', error_message = '',
        deleted_at = NULL, updated_at = now()
    FROM eligible
    WHERE document.id = eligible.document_id
      AND document.identity_id = eligible.identity_id
    RETURNING document.id
), searchable_asset AS (
    UPDATE aura.assets asset
    SET status = 'searchable',
        pipeline_generation = sqlc.arg(pipeline_generation),
        updated_at = now()
    FROM eligible
    WHERE asset.id = eligible.asset_id
      AND asset.identity_id = eligible.identity_id
    RETURNING asset.id
), completed_job AS (
    UPDATE aura.ingestion_jobs job
    SET status = 'succeeded', stage = 'activate',
        locked_by = NULL, locked_until = NULL,
        error_code = '', error_message = '',
        completed_at = now(), updated_at = now()
    FROM eligible
    WHERE job.id = eligible.job_id
      AND job.identity_id = eligible.identity_id
    RETURNING job.id, job.identity_id, job.attempt_generation,
              job.lease_generation, eligible.document_status
), event AS (
    INSERT INTO aura.ingestion_events (
        identity_id, entity_type, entity_id, job_id, from_status, to_status,
        event_type, message, detail, pipeline_generation,
        attempt_generation, lease_generation
    )
    SELECT completed_job.identity_id, 'document', ready_document.id,
           completed_job.id, completed_job.document_status, 'ready',
           'candidate_activated', sqlc.arg(event_message),
           jsonb_build_object(
               'version_id', sqlc.arg(version_id),
               'passage_count', sqlc.arg(expected_passage_count)::bigint,
               'embedding_count', sqlc.arg(expected_embedding_count)::bigint,
               'projection_count', sqlc.arg(expected_projection_count)::bigint
           ),
           sqlc.arg(pipeline_generation), completed_job.attempt_generation,
           completed_job.lease_generation
    FROM completed_job
    JOIN ready_document ON true
    JOIN ready_version ON true
    JOIN searchable_asset ON true
    WHERE (SELECT count(*) FROM activated_chunks) = sqlc.arg(expected_passage_count)::bigint
      AND (SELECT count(*) FROM activated_embeddings) = sqlc.arg(expected_embedding_count)::bigint
    RETURNING id
)
SELECT event.id FROM event;

-- Proves whether any live ledger row still owns a derived-artifact key, so cleanup
-- after an ambiguous write deletes only objects PostgreSQL says nobody references.
-- Artifact keys are content/config addressed and therefore shareable, and a returned
-- commit error does not prove the insert failed. Identity-scoped by RLS while the
-- uniqueness constraint is global, so a zero count disproves ownership only within
-- this identity -- which is exactly the scope permitted to delete the object.
-- name: CountPipelineArtifactLedgerOwners :one
SELECT count(*)::bigint
FROM aura.storage_objects
WHERE identity_id = sqlc.arg(identity_id)
  AND bucket = sqlc.arg(bucket)
  AND object_key = sqlc.arg(object_key)
  AND status <> 'object_deleted'
  AND deleted_at IS NULL;
