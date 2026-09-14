-- name: InsertMediaJob :one
INSERT INTO aura.media_job (
    identity_id, conversation_id, tool_call_id, provider_job_id, model, request, status, cost_usd
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
RETURNING *;

-- name: GetMediaJobForIdentity :one
SELECT * FROM aura.media_job WHERE id = $1 AND identity_id = $2;

-- name: ListRecoverableMediaJobs :many
SELECT * FROM aura.media_job WHERE identity_id = $1
AND (status IN ('pending','in_progress') OR
     (status = 'completed' AND delivered_at IS NULL))
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
        AND assets.modality = 'video'
        AND assets.status = 'accepted'
        AND assets.deleted_at IS NULL
  )
RETURNING *;

-- name: ClaimMediaJobDelivery :one
UPDATE aura.media_job SET delivered_at = now(), updated_at = now()
WHERE id = $1 AND identity_id = $2 AND conversation_id = $3
  AND status = 'completed' AND asset_id IS NOT NULL AND delivered_at IS NULL
RETURNING *;

-- name: BindMediaJobAssetDelivery :execrows
UPDATE aura.assets SET tool_call_id = $3, updated_at = now()
WHERE id = $1 AND identity_id = $2 AND thread_id = $4
  AND source_kind = 'agent' AND status = 'accepted' AND deleted_at IS NULL;
