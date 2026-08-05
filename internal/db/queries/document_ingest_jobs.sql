-- Legacy queue kept owner-scoped during retirement. New ingress uses aura.ingestion_jobs.

-- name: CreateDocumentIngestJob :one
INSERT INTO aura.document_ingest_jobs (
    identity_id, source_id, source_kind, document_id, content_hash, original_path,
    file_name, mime_type, size_bytes, status, asset_id, catalog_document_id,
    version_id, pipeline_generation
) VALUES (
    sqlc.arg(identity_id), sqlc.arg(source_id), sqlc.arg(source_kind),
    sqlc.arg(document_id), sqlc.arg(content_hash), sqlc.arg(original_path),
    sqlc.arg(file_name), sqlc.arg(mime_type), sqlc.arg(size_bytes), sqlc.arg(status),
    sqlc.narg(asset_id), sqlc.narg(catalog_document_id), sqlc.narg(version_id),
    sqlc.arg(pipeline_generation)
)
ON CONFLICT (identity_id, source_id, document_id, content_hash)
DO UPDATE SET original_path = EXCLUDED.original_path,
              file_name = EXCLUDED.file_name,
              mime_type = EXCLUDED.mime_type,
              size_bytes = EXCLUDED.size_bytes,
              updated_at = now()
RETURNING *;

-- name: GetDocumentIngestJob :one
SELECT * FROM aura.document_ingest_jobs
WHERE id = sqlc.arg(id) AND identity_id = sqlc.arg(identity_id);

-- name: GetDocumentIngestJobByDocumentID :one
SELECT * FROM aura.document_ingest_jobs
WHERE identity_id = sqlc.arg(identity_id)
  AND document_id = sqlc.arg(document_id)
ORDER BY created_at DESC
LIMIT 1;

-- name: UpdateDocumentIngestJobStatus :one
UPDATE aura.document_ingest_jobs
SET status = sqlc.arg(status), error = sqlc.narg(error), updated_at = now(),
    searchable_at = CASE WHEN sqlc.arg(status)::text = 'searchable' THEN now()
                         ELSE searchable_at END,
    completed_at = CASE WHEN sqlc.arg(status)::text = 'complete' THEN now()
                        ELSE completed_at END
WHERE id = sqlc.arg(id) AND identity_id = sqlc.arg(identity_id)
RETURNING *;

-- name: UpdateDocumentIngestJobProgress :one
UPDATE aura.document_ingest_jobs
SET status = sqlc.arg(status), sparse_chunks = sqlc.arg(sparse_chunks),
    embedded_chunks = sqlc.arg(embedded_chunks), error = NULL, updated_at = now(),
    searchable_at = CASE
        WHEN sqlc.arg(status)::text = 'searchable' AND searchable_at IS NULL THEN now()
        ELSE searchable_at END,
    completed_at = CASE
        WHEN sqlc.arg(status)::text = 'complete' AND completed_at IS NULL THEN now()
        ELSE completed_at END
WHERE id = sqlc.arg(id) AND identity_id = sqlc.arg(identity_id)
RETURNING *;

-- name: ListRecentDocumentIngestJobs :many
SELECT * FROM aura.document_ingest_jobs
WHERE identity_id = sqlc.arg(identity_id)
ORDER BY created_at DESC
LIMIT sqlc.arg(row_limit);
