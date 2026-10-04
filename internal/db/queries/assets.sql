-- name: CreateAsset :one
INSERT INTO aura.assets (
    identity_id, source_kind, source_ref, thread_id, scope, modality,
    status, file_name, mime_type, declared_size_bytes, object_bucket,
    object_key, metadata, tool_call_id
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11,
    $12, $13, $14
)
ON CONFLICT (identity_id, source_kind, source_ref)
    WHERE source_kind = 'agent' AND source_ref <> ''
DO UPDATE SET updated_at = aura.assets.updated_at
RETURNING *;

-- name: GetAsset :one
SELECT * FROM aura.assets
WHERE id = $1;

-- name: GetAssetByObjectKey :one
SELECT * FROM aura.assets
WHERE identity_id = $1
  AND object_key = $2
  AND deleted_at IS NULL;

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

-- name: ListRecentAssets :many
-- The assets of the asked kinds an identity can pick in a Studio — a frame or a reference in the
-- image Studio, a sound or a clip in the video Studio: usable (the statuses the cockpit's
-- isReadyAsset accepts) and not deleted, newest first, from any thread or none.
-- One page: before_id is the last row of the previous page; an id the owner does not hold
-- compares as NULL and yields an empty page.
SELECT * FROM aura.assets
WHERE assets.identity_id = sqlc.arg(identity_id)
  AND assets.modality = ANY(sqlc.arg(modalities)::text[])
  AND assets.status IN ('accepted', 'processing', 'complete')
  AND assets.deleted_at IS NULL
  AND (sqlc.narg(before_id)::uuid IS NULL OR (assets.created_at, assets.id) < (
      SELECT b.created_at, b.id FROM aura.assets b
      WHERE b.id = sqlc.narg(before_id)::uuid AND b.identity_id = sqlc.arg(identity_id)))
ORDER BY assets.created_at DESC, assets.id DESC
LIMIT sqlc.arg(row_limit);

-- name: UpdateAssetUploaded :one
UPDATE aura.assets
SET status = 'uploaded',
    size_bytes = $3,
    object_etag = $4,
    uploaded_at = now(),
    updated_at = now()
WHERE id = $1
  AND identity_id = $2
  AND deleted_at IS NULL
RETURNING *;

-- name: UpdateAssetAccepted :one
UPDATE aura.assets
SET status = 'accepted',
    size_bytes = $3,
    content_hash = $4,
    mime_type = $5,
    accepted_at = now(),
    updated_at = now()
WHERE id = $1
  AND identity_id = $2
  AND deleted_at IS NULL
RETURNING *;

-- name: UpdateAssetStatus :one
UPDATE aura.assets
SET status = $3,
    error_code = $4,
    error_message = $5,
    updated_at = now(),
    processed_at = CASE WHEN $3 IN ('complete', 'failed', 'refused') THEN now() ELSE processed_at END,
    completed_at = CASE WHEN $3 = 'complete' THEN now() ELSE completed_at END,
    deleted_at = CASE WHEN $3 = 'deleted' THEN now() ELSE deleted_at END
WHERE id = $1
  AND identity_id = $2
  AND deleted_at IS NULL
RETURNING *;

-- name: UpdateAssetResult :one
UPDATE aura.assets
SET status = $3,
    document_id = $4,
    summary = $5,
    metadata = $6,
    error_code = '',
    error_message = '',
    processed_at = now(),
    completed_at = CASE WHEN $3 = 'complete' THEN now() ELSE completed_at END,
    updated_at = now()
WHERE id = $1
  AND identity_id = $2
  AND deleted_at IS NULL
RETURNING *;

-- name: PromoteAssetToLibrary :one
UPDATE aura.assets
SET scope = 'library',
    updated_at = now()
WHERE id = $1
  AND identity_id = $2
  AND deleted_at IS NULL
RETURNING *;

-- name: RearmAssetForUpload :one
-- A library upload onto a name the library already holds replaces that file: the key is the
-- name's (libraryObjectID), so the row holding it goes back to presigned with the new upload's
-- name, type and declared size, and finalize checks the new bytes against the new declaration.
UPDATE aura.assets
SET status = 'presigned',
    file_name = sqlc.arg(file_name),
    mime_type = sqlc.arg(mime_type),
    modality = sqlc.arg(modality),
    declared_size_bytes = sqlc.arg(declared_size_bytes),
    size_bytes = 0,
    object_etag = '',
    content_hash = '',
    error_code = '',
    error_message = '',
    uploaded_at = NULL,
    accepted_at = NULL,
    processed_at = NULL,
    completed_at = NULL,
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND identity_id = sqlc.arg(identity_id)
  AND deleted_at IS NULL
RETURNING *;

-- name: AdoptAssetIntoThread :one
-- Claim an asset that was presigned before its conversation existed.
--
-- The web composer presigns as soon as the file is chosen, and on a BRAND NEW chat there
-- is no thread id yet, so the row is written with thread_id = ''. Nothing ever filled it
-- in, and ListAssetsForThread filters on thread_id -- so the first attachment of a new
-- conversation was invisible the moment the page reloaded (measured 2026-09-03: one such
-- row, an image the model had already answered about).
--
-- Only an UNCLAIMED asset is adopted. A row that already names a thread is left exactly as
-- it is, so this can never move someone's attachment between conversations.
UPDATE aura.assets
SET thread_id = $3,
    updated_at = now()
WHERE id = $1
  AND identity_id = $2
  AND thread_id = ''
  AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteAsset :one
-- The durable intent of a delete. Stamping deleted_at hides the row from every identity-scoped
-- read and every status write, so a job finishing on it later cannot bring it back;
-- FinalizeAsset or MarkAssetDeleted then ends it once its object is gone.
UPDATE aura.assets
SET status = 'deleting',
    deleted_at = now(),
    updated_at = now()
WHERE id = $1
  AND identity_id = $2
  AND deleted_at IS NULL
  AND status NOT IN ('deleting', 'deleted')
RETURNING *;

-- name: ListDeletingAssets :many
-- What a delete left unfinished, oldest first, whether or not deleted_at was stamped: rows
-- soft-deleted before SoftDeleteAsset stamped it are the backlog this drains.
SELECT * FROM aura.assets
WHERE identity_id = $1
  AND status = 'deleting'
ORDER BY created_at ASC, id ASC
LIMIT $2;

-- name: RetireIdleAssets :exec
-- Marks deleting, oldest first, the identity's rows in one of the given statuses that nothing
-- has written since the cutoff: an abandoned upload, or a refused or failed one kept past its
-- lifetime. The outer conditions repeat the inner ones because Postgres re-checks only the
-- outer ones on a row a concurrent write changed while this waited for its lock: a row a
-- finalize or a retry moved on meanwhile is left alone.
UPDATE aura.assets
SET status = 'deleting',
    deleted_at = now(),
    updated_at = now()
WHERE assets.id IN (
    SELECT idle.id FROM aura.assets AS idle
    WHERE idle.identity_id = sqlc.arg(identity_id)
      AND idle.status = ANY(sqlc.arg(statuses)::text[])
      AND idle.deleted_at IS NULL
      AND idle.updated_at < sqlc.arg(cutoff)
    ORDER BY idle.updated_at ASC, idle.id ASC
    LIMIT sqlc.arg(row_limit)
)
  AND assets.status = ANY(sqlc.arg(statuses)::text[])
  AND assets.deleted_at IS NULL
  AND assets.updated_at < sqlc.arg(cutoff);

-- name: FinalizeAsset :execrows
-- Removes a deleting row whose object is gone. A row a media_job points at is left for
-- MarkAssetDeleted instead: that key has no ON DELETE action (migration 0128) because a paid
-- clip keeps its pointer.
DELETE FROM aura.assets
WHERE assets.id = $1
  AND assets.identity_id = $2
  AND assets.status = 'deleting'
  AND NOT EXISTS (SELECT 1 FROM aura.media_job WHERE media_job.asset_id = assets.id);

-- name: ResetAssetForIngestionRetry :one
UPDATE aura.assets
SET status = 'accepted', error_code = '', error_message = '',
    processed_at = NULL, completed_at = NULL, updated_at = now()
WHERE id = sqlc.arg(id)
  AND identity_id = sqlc.arg(identity_id)
  AND status IN ('failed', 'refused', 'canceled')
  AND deleted_at IS NULL
RETURNING *;

-- name: MarkAssetDeleted :one
UPDATE aura.assets
SET status = 'deleted', deleted_at = COALESCE(deleted_at, now()), updated_at = now()
WHERE id = sqlc.arg(id)
  AND identity_id = sqlc.arg(identity_id)
  AND status = 'deleting'
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

-- name: AssetNamesByObjectKey :many
-- The file manager lists bucket KEYS, which deliberately carry no name (a chat attachment
-- is chat/<assetID>.<ext> so the name cannot leak through a presigned URL or an access log).
-- The name it needs is on the same row as the key, so the listing resolves it here rather
-- than deriving a search id and asking the document index — which failed whole-listing once
-- a page held more keys than that index accepts filters.
-- A key with no row simply has no entry, and the caller keeps the key tail it already shows.
-- The id rides along so a file opened from the manager can be edited AS its asset: without
-- it the editor took every file for a foreign object and uploaded a copy on each open.
SELECT id, object_key, file_name FROM aura.assets
WHERE identity_id = $1
  AND object_key = ANY(sqlc.arg(object_keys)::text[])
  AND file_name <> ''
  AND deleted_at IS NULL;

-- name: AssetKeysHeld :many
-- The keys among object_keys some row of the identity holds, in any status. A file-manager
-- write landing on one would replace an asset's bytes under its row, hand them to the sweep
-- removing a deleting row's object, or collide with the per-identity key index.
SELECT object_key FROM aura.assets
WHERE identity_id = $1
  AND object_key = ANY(sqlc.arg(object_keys)::text[]);

-- name: RelocateAsset :exec
-- Moves the identity's row in one bucket from from_key to to_key, taking new_name when the
-- move carries one. The file manager copies the object first and deletes the source after,
-- so the row never names a key without bytes.
UPDATE aura.assets
SET object_key = sqlc.arg(to_key),
    file_name = CASE WHEN sqlc.arg(new_name)::text <> '' THEN sqlc.arg(new_name)::text ELSE file_name END,
    updated_at = now()
WHERE identity_id = sqlc.arg(identity_id)
  AND object_bucket = sqlc.arg(object_bucket)
  AND object_key = sqlc.arg(from_key);
