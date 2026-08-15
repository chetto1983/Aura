-- Get-or-create by source, refusing a document that is already being deleted.
-- DO UPDATE rather than DO NOTHING because :one needs a row back, and it touches ONLY
-- updated_at: re-ingesting a file must not overwrite a title or tags the operator edited,
-- and aura.document_identity_immutable raises 23514 on any write to identity, source,
-- search id, or a lowered pipeline_generation. The status guard matters because deletion
-- is asynchronous: deleted_at is set by FinalizeDocumentDelete, not by the soft delete, so
-- without it a re-upload mid-delete would silently join a document the finalize erases.
-- Zero rows is that case, and the caller turns it into ErrDocumentDeleteInFlight.
-- name: CreateDocument :one
INSERT INTO aura.documents (
    identity_id, scope, title, tags, metadata, status,
    source_kind, source_key, search_document_id, pipeline_generation
) VALUES (
    sqlc.arg(identity_id), sqlc.arg(scope), sqlc.arg(title), sqlc.arg(tags),
    sqlc.arg(metadata), sqlc.arg(status), sqlc.arg(source_kind),
    sqlc.arg(source_key), sqlc.arg(search_document_id), sqlc.arg(pipeline_generation)
)
ON CONFLICT (identity_id, source_kind, source_key) WHERE deleted_at IS NULL
DO UPDATE SET updated_at = now()
    WHERE documents.status <> 'deleting'
RETURNING *;
-- name: ListDocuments :many
SELECT * FROM aura.documents
WHERE identity_id = sqlc.arg(identity_id)
  AND deleted_at IS NULL
  AND (sqlc.arg(scope_filter)::text = '' OR scope = sqlc.arg(scope_filter))
  AND (
    sqlc.arg(query)::text = ''
    OR title ILIKE '%' || sqlc.arg(query) || '%'
    OR sqlc.arg(query) = ANY(tags)
  )
  AND (sqlc.arg(tag_filter)::text = '' OR tags @> ARRAY[sqlc.arg(tag_filter)]::text[])
ORDER BY updated_at DESC, created_at DESC
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);
-- name: GetDocument :one
SELECT * FROM aura.documents
WHERE id = sqlc.arg(id)
  AND identity_id = sqlc.arg(identity_id)
  AND deleted_at IS NULL;
-- name: GetDocumentBySearchID :one
SELECT * FROM aura.documents
WHERE identity_id = sqlc.arg(identity_id)
  AND search_document_id = sqlc.arg(search_document_id)
  AND deleted_at IS NULL;
-- name: UpdateDocument :one
UPDATE aura.documents
SET scope = sqlc.arg(scope),
    title = sqlc.arg(title),
    tags = sqlc.arg(tags),
    metadata = sqlc.arg(metadata),
    active_version_id = sqlc.narg(active_version_id),
    status = sqlc.arg(status),
    pipeline_generation = GREATEST(pipeline_generation, sqlc.arg(pipeline_generation)),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND identity_id = sqlc.arg(identity_id)
  AND deleted_at IS NULL
RETURNING *;
-- name: UpdateDocumentTags :one
UPDATE aura.documents
SET tags = sqlc.arg(tags), updated_at = now()
WHERE id = sqlc.arg(id)
  AND identity_id = sqlc.arg(identity_id)
  AND deleted_at IS NULL
RETURNING *;
-- name: SoftDeleteDocument :one
WITH target AS (
    SELECT document.* FROM aura.documents document
    WHERE document.id = sqlc.arg(id) AND document.identity_id = sqlc.arg(identity_id)
      AND document.deleted_at IS NULL AND document.status <> 'deleted'
    FOR UPDATE
), snapshot AS (
    SELECT target.id, target.identity_id, target.pipeline_generation,
           jsonb_build_object(
               'document_id', target.id,
               'pipeline_generation', target.pipeline_generation,
               'objects', COALESCE((SELECT jsonb_agg(jsonb_build_object(
                   'id', object.id, 'bucket', object.bucket, 'object_key', object.object_key,
                   'deletion_generation', object.deletion_generation + 1) ORDER BY object.id)
                   FROM aura.storage_objects object
                   WHERE object.identity_id = target.identity_id AND object.document_id = target.id
                     AND object.status <> 'object_deleted'), '[]'::jsonb),
               'projection_generations', COALESCE((SELECT jsonb_agg(DISTINCT version.pipeline_generation)
                   FROM aura.document_versions version
                   WHERE version.identity_id = target.identity_id
                     AND version.document_id = target.id
                     AND version.deleted_at IS NULL), '[]'::jsonb)
           ) AS steps
    FROM target
), changed AS (
    UPDATE aura.documents document SET status = 'deleting', updated_at = now()
    FROM target WHERE document.id = target.id RETURNING document.*
), pending_objects AS (
    UPDATE aura.storage_objects object
    SET status = 'delete_pending', deletion_generation = deletion_generation + 1
    FROM target WHERE object.identity_id = target.identity_id AND object.document_id = target.id
      AND object.status <> 'object_deleted' RETURNING object.id
), job AS (
    INSERT INTO aura.delete_jobs (
        identity_id, document_id, scope, status, steps, max_attempts,
        next_attempt_at, idempotency_key, delete_generation
    ) SELECT identity_id, id, 'hard', 'queued', steps, 10, now(),
             'document:' || id::text, GREATEST(pipeline_generation, 1) FROM snapshot
    ON CONFLICT (identity_id, idempotency_key) DO UPDATE SET updated_at = now()
    RETURNING document_id
)
SELECT changed.* FROM changed JOIN job ON job.document_id = changed.id;
-- name: DeleteDocumentTags :exec
DELETE FROM aura.document_tags WHERE document_id = sqlc.arg(document_id);
-- name: UpsertDocumentTag :exec
INSERT INTO aura.document_tags (document_id, tag, created_by)
VALUES (sqlc.arg(document_id), sqlc.arg(tag), sqlc.narg(created_by))
ON CONFLICT (document_id, tag) DO NOTHING;
-- name: ListDocumentTags :many
SELECT tag FROM aura.document_tags
WHERE document_id = sqlc.arg(document_id)
ORDER BY tag;
-- The only storage-object statement Go still issues. The ledger's writes travel with the
-- rows they belong to: ReservePipelineCandidateVersion inserts the object, and
-- SoftDeleteDocument marks the document's objects delete_pending in the same statement
-- that soft-deletes the document, so neither has a standalone query to call.
-- name: ListStorageObjects :many
SELECT * FROM aura.storage_objects
WHERE identity_id = sqlc.arg(identity_id)
  AND bucket = sqlc.arg(bucket)
  AND status <> 'object_deleted'
  AND (sqlc.arg(prefix)::text = '' OR object_key LIKE sqlc.arg(prefix) || '%')
ORDER BY object_key;
-- name: CreateDocumentVersion :one
WITH locked_document AS (
    SELECT document.id, document.identity_id, document.search_document_id, document.pipeline_generation
    FROM aura.documents document
    WHERE document.id = sqlc.arg(document_id)
      AND document.identity_id = sqlc.arg(identity_id)
      AND document.deleted_at IS NULL
      AND document.status NOT IN ('deleting', 'deleted')
    FOR UPDATE
), next_version AS (
    SELECT d.id AS document_id, d.identity_id, d.search_document_id,
           COALESCE(max(v.version_number), 0)::integer + 1 AS version_number,
           GREATEST(d.pipeline_generation, COALESCE(max(v.pipeline_generation), 0),
                    COALESCE(max(v.version_number), 0), sqlc.arg(pipeline_generation)::bigint - 1) + 1 AS pipeline_generation
    FROM locked_document d
    LEFT JOIN aura.document_versions v ON v.document_id = d.id
    GROUP BY d.id, d.identity_id, d.search_document_id, d.pipeline_generation
)
INSERT INTO aura.document_versions (
    id, identity_id, document_id, asset_id, version_number, status, sha1, sha256,
    content_type, size_bytes, storage_object_id, chunking_config_hash,
    pipeline_config_hash, search_document_id, pipeline_generation
)
SELECT sqlc.arg(id), n.identity_id, n.document_id, sqlc.narg(asset_id),
       n.version_number, sqlc.arg(status), sqlc.arg(sha1), sqlc.arg(sha256),
       sqlc.arg(content_type), sqlc.arg(size_bytes), sqlc.arg(storage_object_id),
       sqlc.arg(chunking_config_hash), sqlc.arg(pipeline_config_hash),
       n.search_document_id, n.pipeline_generation
FROM next_version n
ON CONFLICT (document_id, sha256) WHERE deleted_at IS NULL
DO UPDATE SET updated_at = aura.document_versions.updated_at
RETURNING *;
-- name: ListDocumentVersions :many
SELECT * FROM aura.document_versions
WHERE identity_id = sqlc.arg(identity_id)
  AND document_id = sqlc.arg(document_id)
  AND deleted_at IS NULL
ORDER BY version_number DESC;
-- name: CreateIngestionJob :one
INSERT INTO aura.ingestion_jobs (
    identity_id, job_type, asset_id, document_id, version_id, status,
    idempotency_key, stage, max_attempts, next_attempt_at, payload,
    pipeline_generation
) VALUES (
    sqlc.arg(identity_id), sqlc.arg(job_type), sqlc.narg(asset_id),
    sqlc.narg(document_id), sqlc.narg(version_id), sqlc.arg(status),
    sqlc.arg(idempotency_key), sqlc.arg(stage), sqlc.arg(max_attempts),
    sqlc.arg(next_attempt_at), sqlc.arg(payload), sqlc.arg(pipeline_generation)
)
ON CONFLICT (identity_id, job_type, idempotency_key) DO UPDATE
SET updated_at = now()
RETURNING *;
-- name: ClaimIngestionJobs :many
WITH candidates AS (
    SELECT queued_job.id, queued_job.status AS prior_status
    FROM aura.ingestion_jobs queued_job
    WHERE queued_job.identity_id = sqlc.arg(identity_id)
      AND queued_job.attempt_count < queued_job.max_attempts
      AND (
        (queued_job.status = 'queued' AND queued_job.next_attempt_at <= now()
         AND (queued_job.locked_until IS NULL OR queued_job.locked_until < now()))
        OR (queued_job.status = 'running' AND queued_job.locked_until < now())
      )
    ORDER BY queued_job.next_attempt_at, queued_job.created_at
    LIMIT sqlc.arg(batch_size)
    FOR UPDATE SKIP LOCKED
), claimed AS (
    UPDATE aura.ingestion_jobs job
    SET status = 'running',
        locked_by = sqlc.arg(locked_by),
        locked_until = now() + sqlc.arg(lease_duration)::interval,
        attempt_count = attempt_count + 1,
        lease_generation = lease_generation + 1,
        completed_at = NULL,
        updated_at = now()
    FROM candidates candidate
    WHERE job.id = candidate.id
    RETURNING job.*, candidate.prior_status
), events AS (
    INSERT INTO aura.ingestion_events (
        identity_id, entity_type, entity_id, job_id, from_status, to_status,
        event_type, detail, pipeline_generation, attempt_generation, lease_generation
    )
    SELECT claimed.identity_id, 'ingestion_job', claimed.id, claimed.id,
           claimed.prior_status, 'running',
           CASE WHEN claimed.prior_status = 'running' THEN 'job_lease_reclaimed'
                ELSE 'job_claimed' END,
           '{}'::jsonb, claimed.pipeline_generation, claimed.attempt_generation,
           claimed.lease_generation
    FROM claimed
    RETURNING job_id
)
SELECT claimed.id, claimed.job_type, claimed.document_id, claimed.version_id, claimed.status, claimed.idempotency_key, claimed.stage, claimed.attempt_count, claimed.max_attempts, claimed.locked_by, claimed.locked_until, claimed.next_attempt_at, claimed.payload, claimed.error_code, claimed.error_message, claimed.created_at, claimed.updated_at, claimed.completed_at, claimed.identity_id, claimed.asset_id, claimed.pipeline_generation, claimed.attempt_generation, claimed.lease_generation FROM claimed
JOIN events ON events.job_id = claimed.id
ORDER BY claimed.next_attempt_at, claimed.created_at;
-- name: HeartbeatIngestionJob :one
UPDATE aura.ingestion_jobs
SET locked_until = now() + sqlc.arg(lease_duration)::interval,
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND identity_id = sqlc.arg(identity_id)
  AND status = 'running'
  AND locked_by = sqlc.arg(locked_by)
  AND lease_generation = sqlc.arg(lease_generation)
  AND locked_until > now()
RETURNING *;
-- name: UpdateIngestionJobStatus :one
WITH target AS (
    SELECT target_job.id, target_job.status AS prior_status
    FROM aura.ingestion_jobs target_job
    WHERE target_job.id = sqlc.arg(id)
      AND target_job.identity_id = sqlc.arg(identity_id)
      AND target_job.status = 'running'
      AND target_job.locked_by = sqlc.arg(locked_by)
      AND target_job.lease_generation = sqlc.arg(lease_generation)
    FOR UPDATE
), transitioned AS (
    UPDATE aura.ingestion_jobs job
    SET status = sqlc.arg(status), stage = sqlc.arg(stage),
        error_code = sqlc.arg(error_code), error_message = sqlc.arg(error_message),
        locked_by = NULL, locked_until = NULL,
        completed_at = CASE WHEN sqlc.arg(status)::text IN (
            'succeeded', 'failed', 'dead_letter', 'canceled'
        ) THEN now() ELSE NULL END,
        updated_at = now()
    FROM target WHERE job.id = target.id
    RETURNING job.*, target.prior_status
), events AS (
    INSERT INTO aura.ingestion_events (
        identity_id, entity_type, entity_id, job_id, from_status, to_status,
        event_type, message, detail, trace_id,
        pipeline_generation, attempt_generation, lease_generation
    )
    SELECT transitioned.identity_id, 'ingestion_job', transitioned.id, transitioned.id,
           transitioned.prior_status, transitioned.status, sqlc.arg(event_type),
           sqlc.arg(event_message), sqlc.arg(event_detail), sqlc.arg(trace_id),
           transitioned.pipeline_generation, transitioned.attempt_generation, transitioned.lease_generation
    FROM transitioned
    RETURNING job_id
)
SELECT transitioned.id, transitioned.job_type, transitioned.document_id, transitioned.version_id, transitioned.status, transitioned.idempotency_key, transitioned.stage, transitioned.attempt_count, transitioned.max_attempts, transitioned.locked_by, transitioned.locked_until, transitioned.next_attempt_at, transitioned.payload, transitioned.error_code, transitioned.error_message, transitioned.created_at, transitioned.updated_at, transitioned.completed_at, transitioned.identity_id, transitioned.asset_id, transitioned.pipeline_generation, transitioned.attempt_generation, transitioned.lease_generation FROM transitioned JOIN events ON events.job_id = transitioned.id;
-- name: RetryIngestionJob :one
WITH target AS (
    SELECT target_job.id, target_job.status AS prior_status FROM aura.ingestion_jobs target_job
    WHERE target_job.id = sqlc.arg(id) AND target_job.identity_id = sqlc.arg(identity_id)
      AND target_job.status = 'running' AND target_job.locked_by = sqlc.arg(locked_by)
      AND target_job.lease_generation = sqlc.arg(lease_generation)
    FOR UPDATE
), retried AS (
    UPDATE aura.ingestion_jobs job
    SET status = 'queued', stage = sqlc.arg(stage),
        error_code = sqlc.arg(error_code), error_message = sqlc.arg(error_message),
        locked_by = NULL, locked_until = NULL,
        next_attempt_at = sqlc.arg(next_attempt_at), updated_at = now()
    FROM target WHERE job.id = target.id
    RETURNING job.*, target.prior_status
), events AS (
    INSERT INTO aura.ingestion_events (
        identity_id, entity_type, entity_id, job_id, from_status, to_status,
        event_type, message, pipeline_generation, attempt_generation, lease_generation
    )
    SELECT retried.identity_id, 'ingestion_job', retried.id, retried.id,
           retried.prior_status, 'queued', 'job_retry_scheduled',
           sqlc.arg(event_message), retried.pipeline_generation,
           retried.attempt_generation, retried.lease_generation
    FROM retried
    RETURNING job_id
)
SELECT retried.id, retried.job_type, retried.document_id, retried.version_id, retried.status, retried.idempotency_key, retried.stage, retried.attempt_count, retried.max_attempts, retried.locked_by, retried.locked_until, retried.next_attempt_at, retried.payload, retried.error_code, retried.error_message, retried.created_at, retried.updated_at, retried.completed_at, retried.identity_id, retried.asset_id, retried.pipeline_generation, retried.attempt_generation, retried.lease_generation FROM retried JOIN events ON events.job_id = retried.id;
-- name: ManualRetryIngestionJob :one
WITH target AS (
    SELECT target_job.id, target_job.status AS prior_status FROM aura.ingestion_jobs target_job
    WHERE target_job.id = sqlc.arg(id) AND target_job.identity_id = sqlc.arg(identity_id)
      AND target_job.status IN ('failed', 'dead_letter', 'canceled')
    FOR UPDATE
), retried AS (
    UPDATE aura.ingestion_jobs job
    SET status = 'queued', stage = sqlc.arg(stage), attempt_count = 0,
        attempt_generation = attempt_generation + 1,
        lease_generation = lease_generation + 1,
        error_code = '', error_message = '', locked_by = NULL, locked_until = NULL,
        next_attempt_at = sqlc.arg(next_attempt_at), completed_at = NULL, updated_at = now()
    FROM target WHERE job.id = target.id
    RETURNING job.*, target.prior_status
), events AS (
    INSERT INTO aura.ingestion_events (
        identity_id, entity_type, entity_id, job_id, from_status, to_status,
        event_type, pipeline_generation, attempt_generation, lease_generation
    )
    SELECT retried.identity_id, 'ingestion_job', retried.id, retried.id,
           retried.prior_status, 'queued', 'job_manual_retry',
           retried.pipeline_generation, retried.attempt_generation, retried.lease_generation
    FROM retried
    RETURNING job_id
)
SELECT retried.id, retried.job_type, retried.document_id, retried.version_id, retried.status, retried.idempotency_key, retried.stage, retried.attempt_count, retried.max_attempts, retried.locked_by, retried.locked_until, retried.next_attempt_at, retried.payload, retried.error_code, retried.error_message, retried.created_at, retried.updated_at, retried.completed_at, retried.identity_id, retried.asset_id, retried.pipeline_generation, retried.attempt_generation, retried.lease_generation FROM retried JOIN events ON events.job_id = retried.id;
-- name: CountIngestionJobsByStatus :one
SELECT count(*) FROM aura.ingestion_jobs
WHERE identity_id = sqlc.arg(identity_id) AND status = sqlc.arg(status);
-- aura.ingestion_events has no standalone statement of its own: every row is written by
-- the `events` CTE inside the job statement that caused it, so the timeline can never
-- disagree with the transition it records.
-- name: CreateDeleteJob :one
INSERT INTO aura.delete_jobs (
    identity_id, document_id, version_id, scope, status, steps,
    max_attempts, next_attempt_at, idempotency_key, delete_generation
) VALUES (
    sqlc.arg(identity_id), sqlc.narg(document_id), sqlc.narg(version_id),
    sqlc.arg(scope), sqlc.arg(status), sqlc.arg(steps), sqlc.arg(max_attempts),
    sqlc.arg(next_attempt_at), sqlc.arg(idempotency_key), sqlc.arg(delete_generation)
)
ON CONFLICT (identity_id, idempotency_key) DO UPDATE SET updated_at = now()
RETURNING *;
-- name: ClaimDeleteJobs :many
WITH candidates AS (
    SELECT queued_job.id, queued_job.status AS prior_status FROM aura.delete_jobs queued_job
    WHERE queued_job.identity_id = sqlc.arg(identity_id)
      AND queued_job.attempt_count < queued_job.max_attempts
      AND ((queued_job.status = 'queued' AND queued_job.next_attempt_at <= now())
           OR (queued_job.status = 'running' AND queued_job.locked_until < now()))
    ORDER BY queued_job.next_attempt_at, queued_job.created_at
    LIMIT sqlc.arg(batch_size) FOR UPDATE SKIP LOCKED
), claimed AS (
    UPDATE aura.delete_jobs job
    SET status = 'running', locked_by = sqlc.arg(locked_by),
        locked_until = now() + sqlc.arg(lease_duration)::interval,
        attempt_count = attempt_count + 1, lease_generation = lease_generation + 1,
        completed_at = NULL, updated_at = now()
    FROM candidates candidate WHERE job.id = candidate.id
    RETURNING job.*, candidate.prior_status
), events AS (
    INSERT INTO aura.ingestion_events (
        identity_id, entity_type, entity_id, from_status, to_status, event_type,
        attempt_generation, lease_generation
    )
    SELECT claimed.identity_id, 'delete_job', claimed.id, claimed.prior_status, 'running',
           CASE WHEN claimed.prior_status = 'running' THEN 'delete_lease_reclaimed'
                ELSE 'delete_claimed' END,
           claimed.delete_generation, claimed.lease_generation
    FROM claimed
    RETURNING entity_id
)
SELECT claimed.id, claimed.document_id, claimed.version_id, claimed.scope, claimed.status, claimed.steps, claimed.attempt_count, claimed.max_attempts, claimed.locked_by, claimed.locked_until, claimed.next_attempt_at, claimed.created_at, claimed.updated_at, claimed.completed_at, claimed.identity_id, claimed.idempotency_key, claimed.delete_generation, claimed.lease_generation, claimed.error_code, claimed.error_message FROM claimed JOIN events ON events.entity_id = claimed.id
ORDER BY claimed.next_attempt_at, claimed.created_at;
-- name: HeartbeatDeleteJob :one
UPDATE aura.delete_jobs
SET locked_until = now() + sqlc.arg(lease_duration)::interval, updated_at = now()
WHERE id = sqlc.arg(id) AND identity_id = sqlc.arg(identity_id)
  AND status = 'running' AND locked_by = sqlc.arg(locked_by)
  AND lease_generation = sqlc.arg(lease_generation) AND locked_until > now()
RETURNING *;
-- name: MarkDeleteJobStorageObjectDeleted :one
WITH active_job AS (
    SELECT job.id, job.identity_id, job.document_id
    FROM aura.delete_jobs job
    WHERE job.id = sqlc.arg(job_id)
      AND job.identity_id = sqlc.arg(identity_id)
      AND job.status = 'running'
      AND job.locked_by = sqlc.arg(locked_by)
      AND job.lease_generation = sqlc.arg(lease_generation)
      AND job.locked_until > now()
), verified AS (
    UPDATE aura.storage_objects object
    SET status = 'object_deleted',
        deleted_at = COALESCE(object.deleted_at, now()),
        deletion_verified_at = now()
    FROM active_job job
    WHERE object.id = sqlc.arg(storage_object_id)
      AND object.identity_id = job.identity_id
      AND object.document_id = job.document_id
      AND object.deletion_generation = sqlc.arg(deletion_generation)
      AND (
          object.status = 'delete_pending'
          OR (object.status = 'object_deleted' AND object.deletion_verified_at IS NOT NULL)
      )
    RETURNING object.*
)
SELECT * FROM verified;
-- name: RetryDeleteJob :one
WITH target AS (
    SELECT job.id, job.status AS prior_status,
           CASE WHEN job.attempt_count >= job.max_attempts
                THEN 'dead_letter' ELSE 'queued' END AS next_status
    FROM aura.delete_jobs job
    WHERE job.id = sqlc.arg(id)
      AND job.identity_id = sqlc.arg(identity_id)
      AND job.status = 'running'
      AND job.locked_by = sqlc.arg(locked_by)
      AND job.lease_generation = sqlc.arg(lease_generation)
      AND job.locked_until > now()
    FOR UPDATE
), transitioned AS (
    UPDATE aura.delete_jobs job
    SET status = target.next_status,
        error_code = sqlc.arg(error_code),
        error_message = sqlc.arg(error_message),
        locked_by = NULL,
        locked_until = NULL,
        next_attempt_at = CASE WHEN target.next_status = 'queued'
                               THEN sqlc.arg(next_attempt_at) ELSE now() END,
        completed_at = CASE WHEN target.next_status = 'dead_letter'
                            THEN now() ELSE NULL END,
        updated_at = now()
    FROM target
    WHERE job.id = target.id
    RETURNING job.*, target.prior_status
), event AS (
    INSERT INTO aura.ingestion_events (
        identity_id, entity_type, entity_id, from_status, to_status,
        event_type, message, attempt_generation, lease_generation
    )
    SELECT transitioned.identity_id, 'delete_job', transitioned.id,
           transitioned.prior_status, transitioned.status,
           CASE WHEN transitioned.status = 'dead_letter'
                THEN 'delete_dead_letter' ELSE 'delete_retry_scheduled' END,
           sqlc.arg(event_message), transitioned.delete_generation,
           transitioned.lease_generation
    FROM transitioned
    RETURNING entity_id
)
SELECT transitioned.id, transitioned.document_id, transitioned.version_id,
       transitioned.scope, transitioned.status, transitioned.steps,
       transitioned.attempt_count, transitioned.max_attempts,
       transitioned.locked_by, transitioned.locked_until,
       transitioned.next_attempt_at, transitioned.created_at,
       transitioned.updated_at, transitioned.completed_at,
       transitioned.identity_id, transitioned.idempotency_key,
       transitioned.delete_generation, transitioned.lease_generation,
       transitioned.error_code, transitioned.error_message
FROM transitioned JOIN event ON event.entity_id = transitioned.id;
-- name: UpdateDeleteJobStatus :one
WITH target AS (
    SELECT target_job.id, target_job.status AS prior_status FROM aura.delete_jobs target_job
    WHERE target_job.id = sqlc.arg(id) AND target_job.identity_id = sqlc.arg(identity_id)
      AND target_job.status = 'running' AND target_job.locked_by = sqlc.arg(locked_by)
      AND target_job.lease_generation = sqlc.arg(lease_generation)
      AND target_job.locked_until > now()
    FOR UPDATE
), transitioned AS (
    UPDATE aura.delete_jobs job
    SET status = sqlc.arg(status), error_code = sqlc.arg(error_code),
        error_message = sqlc.arg(error_message), locked_by = NULL, locked_until = NULL,
        completed_at = CASE WHEN sqlc.arg(status)::text IN (
            'succeeded', 'failed', 'dead_letter', 'canceled'
        ) THEN now() ELSE NULL END,
        updated_at = now()
    FROM target WHERE job.id = target.id
    RETURNING job.*, target.prior_status
), events AS (
    INSERT INTO aura.ingestion_events (
        identity_id, entity_type, entity_id, from_status, to_status,
        event_type, message, attempt_generation, lease_generation
    )
    SELECT transitioned.identity_id, 'delete_job', transitioned.id, transitioned.prior_status,
           transitioned.status, sqlc.arg(event_type), sqlc.arg(event_message),
           transitioned.delete_generation, transitioned.lease_generation
    FROM transitioned
    RETURNING entity_id
)
SELECT transitioned.id, transitioned.document_id, transitioned.version_id, transitioned.scope, transitioned.status, transitioned.steps, transitioned.attempt_count, transitioned.max_attempts, transitioned.locked_by, transitioned.locked_until, transitioned.next_attempt_at, transitioned.created_at, transitioned.updated_at, transitioned.completed_at, transitioned.identity_id, transitioned.idempotency_key, transitioned.delete_generation, transitioned.lease_generation, transitioned.error_code, transitioned.error_message FROM transitioned JOIN events ON events.entity_id = transitioned.id;
-- Physically erases the document body. Amendment #117: for these two tables
-- "marked deleted" means DELETE, because document_chunks.text is the document text
-- itself and a deleted_at tombstone is retained PII. The fence row is READ, never
-- locked, matching MarkDeleteJobStorageObjectDeleted. Chunks go first so the
-- document_embeddings FK cascade fires in the same order the writer inserted.
-- name: PurgeDocumentPassages :one
WITH active_job AS (
    SELECT job.id, job.identity_id, job.document_id
    FROM aura.delete_jobs job
    WHERE job.id = sqlc.arg(job_id)
      AND job.identity_id = sqlc.arg(identity_id)
      AND job.status = 'running'
      AND job.locked_by = sqlc.arg(locked_by)
      AND job.lease_generation = sqlc.arg(lease_generation)
      AND job.locked_until > now()
), purged_chunks AS (
    DELETE FROM aura.document_chunks chunk
    USING active_job job
    WHERE chunk.identity_id = job.identity_id AND chunk.document_id = job.document_id
    RETURNING chunk.id
)
SELECT count(*)::bigint FROM purged_chunks;

-- Fail-closed evidence for the finalize gate: a delete that cannot prove erasure
-- must not report success.
-- name: CountLiveDocumentPassages :one
SELECT (
    (SELECT count(*) FROM aura.document_chunks chunk
      WHERE chunk.identity_id = sqlc.arg(identity_id)
        AND chunk.document_id = sqlc.arg(document_id))
  + (SELECT count(*) FROM aura.document_embeddings embedding
      WHERE embedding.identity_id = sqlc.arg(identity_id)
        AND embedding.document_id = sqlc.arg(document_id))
)::bigint;

-- name: FinalizeDocumentDelete :one
WITH target AS (
    SELECT job.id AS job_id, job.identity_id, job.document_id, job.delete_generation,
           job.lease_generation, job.status AS prior_status
    FROM aura.delete_jobs job
    JOIN aura.documents document ON document.id = job.document_id
      AND document.identity_id = job.identity_id
    WHERE job.id = sqlc.arg(id) AND job.identity_id = sqlc.arg(identity_id)
      AND job.status = 'running' AND job.locked_by = sqlc.arg(locked_by)
      AND job.lease_generation = sqlc.arg(lease_generation)
      AND job.locked_until > now()
      AND document.status = 'deleting' AND sqlc.arg(projection_verified)::boolean
      AND NOT EXISTS (
          SELECT 1 FROM aura.storage_objects object
          WHERE object.identity_id = job.identity_id AND object.document_id = job.document_id
            AND (object.status <> 'object_deleted' OR object.deletion_verified_at IS NULL)
      )
      AND NOT EXISTS (
          SELECT 1 FROM aura.document_chunks chunk
          WHERE chunk.identity_id = job.identity_id AND chunk.document_id = job.document_id
      )
      AND NOT EXISTS (
          SELECT 1 FROM aura.document_embeddings embedding
          WHERE embedding.identity_id = job.identity_id AND embedding.document_id = job.document_id
      )
    FOR UPDATE OF job, document
), deleted_assets AS (
    UPDATE aura.assets asset SET status = 'deleted', deleted_at = COALESCE(asset.deleted_at, now()), updated_at = now()
    FROM target WHERE asset.identity_id = target.identity_id AND asset.id IN (
        SELECT version.asset_id FROM aura.document_versions version
        WHERE version.identity_id = target.identity_id AND version.document_id = target.document_id
          AND version.asset_id IS NOT NULL
    ) RETURNING asset.id
), deleted_versions AS (
    UPDATE aura.document_versions version
    SET status = 'deleted', deleted_at = COALESCE(version.deleted_at, now()), updated_at = now()
    FROM target WHERE version.identity_id = target.identity_id
      AND version.document_id = target.document_id RETURNING version.id
), deleted_document AS (
    UPDATE aura.documents document
    SET status = 'deleted', active_version_id = NULL,
        deleted_at = COALESCE(document.deleted_at, now()), updated_at = now()
    FROM target WHERE document.id = target.document_id
      AND document.identity_id = target.identity_id RETURNING document.*
), completed_job AS (
    UPDATE aura.delete_jobs job
    SET status = 'succeeded', locked_by = NULL, locked_until = NULL,
        error_code = '', error_message = '', completed_at = now(), updated_at = now()
    FROM target WHERE job.id = target.job_id RETURNING job.*
), event AS (
    INSERT INTO aura.ingestion_events (
        identity_id, entity_type, entity_id, from_status, to_status, event_type,
        message, attempt_generation, lease_generation
    ) SELECT completed_job.identity_id, 'delete_job', completed_job.id,
             target.prior_status, 'succeeded', 'delete_succeeded',
             sqlc.arg(event_message), completed_job.delete_generation,
             completed_job.lease_generation
      FROM completed_job JOIN target ON target.job_id = completed_job.id
    RETURNING entity_id
)
SELECT deleted_document.* FROM deleted_document
JOIN target ON target.document_id = deleted_document.id
JOIN event ON event.entity_id = target.job_id;
