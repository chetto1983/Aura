-- name: CreateDocument :one
INSERT INTO aura.documents (
    identity_id,
    scope,
    title,
    tags,
    metadata,
    status
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING *;

-- name: ListDocuments :many
SELECT *
FROM aura.documents
WHERE identity_id = $1
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
SELECT *
FROM aura.documents
WHERE id = $1
  AND identity_id = $2
  AND deleted_at IS NULL;

-- name: UpdateDocument :one
UPDATE aura.documents
SET
    scope = $3,
    title = $4,
    tags = $5,
    metadata = $6,
    active_version_id = $7,
    status = $8,
    updated_at = now()
WHERE id = $1
  AND identity_id = $2
  AND deleted_at IS NULL
RETURNING *;

-- Narrow status write for background workers, which know the document but not the
-- identity that owns it. UpdateDocument above needs identity_id and rewrites every column,
-- so a worker could not use it to correct a single field without inventing the rest.
-- name: SetDocumentStatus :one
UPDATE aura.documents
SET status = $2,
    metadata = jsonb_set(metadata, '{status_reason}', to_jsonb(sqlc.arg(reason)::text), true),
    updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL
RETURNING *;

-- name: UpdateDocumentTags :one
UPDATE aura.documents
SET
    tags = $3,
    updated_at = now()
WHERE id = $1
  AND identity_id = $2
  AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteDocument :one
UPDATE aura.documents
SET
    status = 'deleted',
    deleted_at = COALESCE(deleted_at, now()),
    updated_at = now()
WHERE id = $1
  AND identity_id = $2
  AND deleted_at IS NULL
RETURNING *;

-- name: DeleteDocumentTags :exec
DELETE FROM aura.document_tags
WHERE document_id = $1;

-- name: UpsertDocumentTag :exec
INSERT INTO aura.document_tags (
    document_id,
    tag,
    created_by
) VALUES (
    $1, $2, $3
)
ON CONFLICT (document_id, tag) DO NOTHING;

-- name: ListDocumentTags :many
SELECT tag
FROM aura.document_tags
WHERE document_id = $1
ORDER BY tag ASC;

-- name: CreateStorageObject :one
INSERT INTO aura.storage_objects (
    identity_id,
    document_id,
    version_id,
    asset_id,
    bucket,
    object_key,
    kind,
    sha1,
    sha256,
    etag,
    size_bytes,
    content_type,
    retention_class
) VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    $8, $9, $10, $11, $12, $13
)
ON CONFLICT (bucket, object_key)
DO UPDATE SET
    etag = EXCLUDED.etag,
    size_bytes = EXCLUDED.size_bytes,
    content_type = EXCLUDED.content_type
RETURNING *;

-- name: ListStorageObjects :many
SELECT *
FROM aura.storage_objects
WHERE bucket = $1
  AND deleted_at IS NULL
  AND (sqlc.arg(prefix)::text = '' OR object_key LIKE sqlc.arg(prefix) || '%')
ORDER BY object_key ASC;

-- name: UpdateStorageObjectVersion :one
UPDATE aura.storage_objects
SET
    version_id = $2
WHERE id = $1
RETURNING *;

-- name: CreateDocumentVersion :one
INSERT INTO aura.document_versions (
    document_id,
    asset_id,
    version_number,
    status,
    sha1,
    sha256,
    content_type,
    size_bytes,
    storage_object_id,
    chunking_config_hash,
    pipeline_config_hash
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11
)
ON CONFLICT (document_id, sha256, pipeline_config_hash)
DO UPDATE SET
    updated_at = aura.document_versions.updated_at
RETURNING *;

-- name: ListDocumentVersions :many
SELECT *
FROM aura.document_versions
WHERE document_id = $1
  AND deleted_at IS NULL
ORDER BY version_number DESC;

-- name: CreateIngestionJob :one
INSERT INTO aura.ingestion_jobs (
    job_type,
    document_id,
    version_id,
    status,
    idempotency_key,
    stage,
    max_attempts,
    next_attempt_at,
    payload
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
ON CONFLICT (job_type, idempotency_key)
DO UPDATE SET
    updated_at = now()
RETURNING *;

-- name: ClaimIngestionJobs :many
WITH claim AS (
    SELECT id
    FROM aura.ingestion_jobs
    WHERE status = 'queued'
      AND next_attempt_at <= now()
      AND (locked_until IS NULL OR locked_until < now())
    ORDER BY next_attempt_at ASC, created_at ASC
    LIMIT sqlc.arg(batch_size)
    FOR UPDATE SKIP LOCKED
)
UPDATE aura.ingestion_jobs AS job
SET
    status = 'running',
    locked_by = sqlc.arg(locked_by),
    locked_until = now() + sqlc.arg(lease_duration)::interval,
    attempt_count = attempt_count + 1,
    updated_at = now()
FROM claim
WHERE job.id = claim.id
RETURNING job.*;

-- name: UpdateIngestionJobStatus :one
UPDATE aura.ingestion_jobs
SET
    status = $2,
    stage = $3,
    error_code = $4,
    error_message = $5,
    locked_by = NULL,
    locked_until = NULL,
    completed_at = CASE
        WHEN $2 IN ('succeeded', 'failed', 'dead_letter', 'canceled') THEN now()
        ELSE completed_at
    END,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: RetryIngestionJob :one
UPDATE aura.ingestion_jobs
SET
    status = 'queued',
    stage = $2,
    error_code = $3,
    error_message = $4,
    locked_by = NULL,
    locked_until = NULL,
    next_attempt_at = $5,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CountIngestionJobsByStatus :one
SELECT count(*)
FROM aura.ingestion_jobs
WHERE status = sqlc.arg(status);

-- name: AppendIngestionEvent :one
INSERT INTO aura.ingestion_events (
    entity_type,
    entity_id,
    job_id,
    from_status,
    to_status,
    event_type,
    message,
    detail,
    trace_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
RETURNING *;

-- name: ListIngestionEventsByJob :many
SELECT *
FROM aura.ingestion_events
WHERE job_id = $1
ORDER BY created_at ASC, id ASC;

-- name: CreateDeleteJob :one
INSERT INTO aura.delete_jobs (
    document_id,
    version_id,
    scope,
    status,
    steps,
    max_attempts,
    next_attempt_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
RETURNING *;
