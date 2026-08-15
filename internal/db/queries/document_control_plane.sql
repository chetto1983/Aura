-- Get-or-create by source, refusing a document left mid-delete.
-- DO UPDATE rather than DO NOTHING because :one needs a row back, and it touches ONLY
-- updated_at: re-ingesting a file must not overwrite a title or tags the operator edited,
-- and aura.document_identity_immutable raises 23514 on any write to identity, source,
-- search id, or a lowered pipeline_generation. The status guard outlives the asynchronous
-- delete workflow it was written for: no statement here sets 'deleting' any more, but rows
-- the retired workflow stranded in that state are still on disk, and joining one would
-- hand the caller a document whose bytes are half gone. Zero rows is that case, and the
-- caller turns it into ErrDocumentDeleteInFlight.
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
-- The only storage-object statement Go still issues. The ledger's one writer travels with
-- the row it belongs to — ReservePipelineCandidateVersion inserts the object as part of
-- reserving the version that owns those bytes — so it has no standalone query to call.
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
