-- name: RequestWorkerCancellation :one
UPDATE aura.ingestion_jobs
SET payload = CASE
        WHEN COALESCE(NULLIF(payload->>'child_id', ''), id::text) = sqlc.arg(child_id)::text
        THEN payload || '{"operator_cancelled":true}'::jsonb
        ELSE payload || jsonb_build_object('cancelled_children',
            COALESCE(payload->'cancelled_children', '{}'::jsonb) ||
            jsonb_build_object(sqlc.arg(child_id)::text, true))
    END,
    updated_at = now()
WHERE id = sqlc.arg(id)::uuid
  AND identity_id = sqlc.arg(identity_id)::uuid
  AND job_type = 'swarm_delegation'
  AND status = 'running'
  AND locked_by = sqlc.arg(locked_by)
  AND lease_generation = sqlc.arg(lease_generation)
  AND locked_until > now()
RETURNING id;

-- name: RequestQueuedWorkerCancellation :one
UPDATE aura.ingestion_jobs
SET payload = payload || '{"operator_cancelled":true}'::jsonb,
    next_attempt_at = LEAST(next_attempt_at, now()),
    updated_at = now()
WHERE id = sqlc.arg(id)::uuid
  AND identity_id = sqlc.arg(identity_id)::uuid
  AND job_type = 'swarm_delegation'
  AND payload->>'conversation_id' = sqlc.arg(conversation_id)::text
  AND COALESCE(NULLIF(payload->>'child_id', ''), id::text) = sqlc.arg(child_id)::text
  AND status = 'queued'
  AND attempt_count = sqlc.arg(attempt_count)::integer
  AND attempt_count < max_attempts
  AND NOT payload ? 'pending_delivery'
  AND (locked_until IS NULL OR locked_until < now())
RETURNING id;
