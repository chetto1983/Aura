-- name: CreateDocumentIngestJob :one
INSERT INTO aura.document_ingest_jobs (
    source_id,
    source_kind,
    document_id,
    content_hash,
    original_path,
    file_name,
    mime_type,
    size_bytes,
    status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
ON CONFLICT (source_id, document_id, content_hash)
DO UPDATE SET
    original_path = EXCLUDED.original_path,
    file_name = EXCLUDED.file_name,
    mime_type = EXCLUDED.mime_type,
    size_bytes = EXCLUDED.size_bytes,
    updated_at = now()
RETURNING *;

-- name: GetDocumentIngestJob :one
SELECT *
FROM aura.document_ingest_jobs
WHERE id = $1;

-- name: GetDocumentIngestJobByDocumentID :one
SELECT *
FROM aura.document_ingest_jobs
WHERE document_id = $1
ORDER BY created_at DESC
LIMIT 1;

-- name: UpdateDocumentIngestJobStatus :one
UPDATE aura.document_ingest_jobs
SET
    status = $2,
    error = $3,
    updated_at = now(),
    searchable_at = CASE WHEN $2 = 'searchable' THEN now() ELSE searchable_at END,
    completed_at = CASE WHEN $2 = 'complete' THEN now() ELSE completed_at END
WHERE id = $1
RETURNING *;

-- name: UpdateDocumentIngestJobProgress :one
UPDATE aura.document_ingest_jobs
SET
    status = $2,
    sparse_chunks = $3,
    embedded_chunks = $4,
    updated_at = now(),
    searchable_at = CASE WHEN $2 = 'searchable' AND searchable_at IS NULL THEN now() ELSE searchable_at END,
    completed_at = CASE WHEN $2 = 'complete' AND completed_at IS NULL THEN now() ELSE completed_at END
WHERE id = $1
RETURNING *;

-- name: ListRecentDocumentIngestJobs :many
SELECT *
FROM aura.document_ingest_jobs
ORDER BY created_at DESC
LIMIT $1;
