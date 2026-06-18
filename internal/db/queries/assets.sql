-- name: CreateAsset :one
INSERT INTO aura.assets (
    identity_id, source_kind, source_ref, thread_id, scope, modality,
    status, file_name, mime_type, declared_size_bytes, object_bucket,
    object_key, metadata
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11,
    $12, $13
)
RETURNING *;

-- name: GetAsset :one
SELECT * FROM aura.assets
WHERE id = $1;

-- name: GetAssetForIdentity :one
SELECT * FROM aura.assets
WHERE id = $1
  AND identity_id = $2
  AND deleted_at IS NULL;

-- name: ListAssetsForThread :many
SELECT * FROM aura.assets
WHERE identity_id = $1
  AND thread_id = $2
  AND deleted_at IS NULL
ORDER BY created_at ASC;

-- name: ListAssetsForLibrary :many
SELECT * FROM aura.assets
WHERE identity_id = $1
  AND scope = 'library'
  AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT $2;

-- name: UpdateAssetUploaded :one
UPDATE aura.assets
SET status = 'uploaded',
    size_bytes = $2,
    object_etag = $3,
    uploaded_at = now(),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateAssetAccepted :one
UPDATE aura.assets
SET status = 'accepted',
    size_bytes = $2,
    content_hash = $3,
    mime_type = $4,
    accepted_at = now(),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateAssetStatus :one
UPDATE aura.assets
SET status = $2,
    error_code = $3,
    error_message = $4,
    updated_at = now(),
    processed_at = CASE WHEN $2 IN ('searchable', 'complete', 'failed', 'refused') THEN now() ELSE processed_at END,
    searchable_at = CASE WHEN $2 = 'searchable' THEN now() ELSE searchable_at END,
    completed_at = CASE WHEN $2 = 'complete' THEN now() ELSE completed_at END,
    deleted_at = CASE WHEN $2 = 'deleted' THEN now() ELSE deleted_at END
WHERE id = $1
RETURNING *;

-- name: UpdateAssetResult :one
UPDATE aura.assets
SET status = $2,
    document_id = $3,
    summary = $4,
    metadata = $5,
    error_code = '',
    error_message = '',
    processed_at = now(),
    searchable_at = CASE WHEN $2 = 'searchable' THEN now() ELSE searchable_at END,
    completed_at = CASE WHEN $2 = 'complete' THEN now() ELSE completed_at END,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: PromoteAssetToLibrary :one
UPDATE aura.assets
SET scope = 'library',
    updated_at = now()
WHERE id = $1
  AND identity_id = $2
  AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteAsset :one
UPDATE aura.assets
SET status = 'deleted',
    deleted_at = now(),
    updated_at = now()
WHERE id = $1
  AND identity_id = $2
  AND deleted_at IS NULL
RETURNING *;

-- name: NextAssetEventSeq :one
SELECT COALESCE(MAX(seq), 0) + 1::integer
FROM aura.asset_events
WHERE asset_id = $1;

-- name: InsertAssetEvent :exec
INSERT INTO aura.asset_events (
    asset_id, seq, from_status, to_status, reason, detail
) VALUES (
    $1, $2, $3, $4, $5, $6
);
