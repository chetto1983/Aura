-- name: InsertMediaJob :one
INSERT INTO aura.media_job (
    identity_id, conversation_id, tool_call_id, provider_job_id, model, request, status, cost_usd,
    surface, kind
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
)
RETURNING *;

-- name: InsertCompletedMediaJob :one
-- One synchronous image generation, recorded finished. The asset must be the owner's
-- accepted, undeleted agent image with no thread: the Studio's own result, never another
-- identity's or a chat delivery.
INSERT INTO aura.media_job (
    identity_id, conversation_id, tool_call_id, provider_job_id, model, request,
    status, cost_usd, surface, kind, asset_id, completed_at, delivered_at
)
SELECT sqlc.arg(identity_id), '', '', sqlc.arg(provider_job_id), sqlc.arg(model),
       sqlc.arg(request), 'completed', sqlc.narg(cost_usd)::numeric, 'studio', 'image',
       assets.id, now(), now()
FROM aura.assets
WHERE assets.id = sqlc.arg(asset_id)
  AND assets.identity_id = sqlc.arg(identity_id)
  AND assets.thread_id = ''
  AND assets.source_kind = 'agent'
  AND assets.modality = 'image'
  AND assets.status = 'accepted'
  AND assets.deleted_at IS NULL
RETURNING *;

-- name: GetMediaJobForIdentity :one
SELECT * FROM aura.media_job WHERE id = $1 AND identity_id = $2;

-- name: ListRecoverableMediaJobs :many
-- A completed, undelivered job is recoverable only while BindMediaJobAssetDelivery could still
-- bind its asset: once the clip is deleted no delivery can succeed, so waking the conversation
-- on every boot would only ever answer asset_not_found.
SELECT * FROM aura.media_job WHERE media_job.identity_id = $1
AND (media_job.status IN ('pending','in_progress') OR
     (media_job.status = 'completed' AND media_job.delivered_at IS NULL AND EXISTS (
         SELECT 1 FROM aura.assets
         WHERE assets.id = media_job.asset_id
           AND assets.identity_id = media_job.identity_id
           AND assets.thread_id = media_job.conversation_id
           AND assets.source_kind = 'agent'
           AND assets.status = 'accepted'
           AND assets.deleted_at IS NULL
     )))
ORDER BY created_at, id;

-- name: UpdateMediaJobProgress :one
-- A NULL cost keeps the recorded one; a zero cost is recorded.
UPDATE aura.media_job
SET status = sqlc.arg(status),
    cost_usd = COALESCE(sqlc.narg(cost_usd)::numeric, cost_usd),
    error = sqlc.narg(error),
    completed_at = CASE WHEN sqlc.arg(status) IN ('failed', 'expired', 'cancelled') THEN now() ELSE completed_at END,
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND identity_id = sqlc.arg(identity_id)
  AND status IN ('pending', 'in_progress')
RETURNING *;

-- name: CompleteMediaJob :one
-- The asset must be deliverable by BindMediaJobAssetDelivery when the job completes: owned,
-- in the job's conversation, an accepted agent video that is not deleted.
UPDATE aura.media_job
SET status = 'completed',
    asset_id = sqlc.arg(asset_id),
    cost_usd = COALESCE(sqlc.narg(cost_usd)::numeric, media_job.cost_usd),
    error = NULL,
    completed_at = now(),
    delivered_at = CASE WHEN media_job.surface = 'studio' THEN now() ELSE media_job.delivered_at END,
    updated_at = now()
WHERE media_job.id = sqlc.arg(id)
  AND media_job.identity_id = sqlc.arg(identity_id)
  AND media_job.status IN ('pending', 'in_progress')
  AND EXISTS (
      SELECT 1 FROM aura.assets
      WHERE assets.id = sqlc.arg(asset_id)
        AND assets.identity_id = media_job.identity_id
        AND assets.thread_id = media_job.conversation_id
        AND assets.source_kind = 'agent'
        AND assets.modality = media_job.kind
        AND assets.status = 'accepted'
        AND assets.deleted_at IS NULL
  )
RETURNING *;

-- name: ListStudioMediaJobs :many
-- One history page, newest first, optionally of one kind. before_id is the last row of the
-- previous page; an id the owner does not hold compares as NULL and yields an empty page.
SELECT * FROM aura.media_job
WHERE media_job.identity_id = sqlc.arg(identity_id)
  AND media_job.surface = 'studio'
  AND (sqlc.narg(kind)::text IS NULL OR media_job.kind = sqlc.narg(kind)::text)
  AND (sqlc.narg(before_id)::uuid IS NULL OR (media_job.created_at, media_job.id) < (
      SELECT b.created_at, b.id FROM aura.media_job b
      WHERE b.id = sqlc.narg(before_id)::uuid AND b.identity_id = sqlc.arg(identity_id)))
ORDER BY media_job.created_at DESC, media_job.id DESC
LIMIT sqlc.arg(row_limit);

-- name: ClaimMediaJobDelivery :one
UPDATE aura.media_job SET delivered_at = now(), updated_at = now()
WHERE id = $1 AND identity_id = $2 AND conversation_id = $3
  AND status = 'completed' AND asset_id IS NOT NULL AND delivered_at IS NULL
RETURNING *;

-- name: BindMediaJobAssetDelivery :execrows
UPDATE aura.assets SET tool_call_id = $3, updated_at = now()
WHERE id = $1 AND identity_id = $2 AND thread_id = $4
  AND source_kind = 'agent' AND status = 'accepted' AND deleted_at IS NULL;
