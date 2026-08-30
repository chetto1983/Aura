-- name: StageDelegationDelivery :one
UPDATE aura.ingestion_jobs
SET payload = sqlc.arg(payload),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND identity_id = sqlc.arg(identity_id)
  AND job_type = 'swarm_delegation'
  AND status = 'running'
  AND locked_by = sqlc.arg(locked_by)
  AND lease_generation = sqlc.arg(lease_generation)
  AND locked_until > now()
RETURNING *;
