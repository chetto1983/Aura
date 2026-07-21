-- name: CreateRetentionOperation :one
INSERT INTO aura.retention_operations (
    id, token, policy_version, status, candidate_count, planned_bytes, created_at, updated_at
) VALUES (
    sqlc.arg(id), sqlc.arg(token), sqlc.arg(policy_version), 'planned',
    sqlc.arg(candidate_count), sqlc.arg(planned_bytes), sqlc.arg(now), sqlc.arg(now)
)
ON CONFLICT (token) DO UPDATE SET token = EXCLUDED.token
RETURNING *;

-- name: GetRetentionOperationByToken :one
SELECT * FROM aura.retention_operations WHERE token = sqlc.arg(token);

-- name: AuthorizeRetentionOperation :execrows
-- First apply only: commit the freshly rebuilt plan authorization before any
-- item can be claimed. A crash after this transition resumes persisted items
-- without re-authorizing against a possibly changed global candidate set.
UPDATE aura.retention_operations
SET status = 'deleting',
    updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id)
  AND token = sqlc.arg(token)
  AND policy_version = sqlc.arg(policy_version)
  AND status = 'planned';

-- name: CreateRetentionItem :one
INSERT INTO aura.retention_operation_items (
    id, operation_id, item_key, identity_id, conversation_id, artifact_id,
    class, action, expected_version, expected_bytes, status, artifact_result,
    created_at, updated_at
) VALUES (
    sqlc.arg(id), sqlc.arg(operation_id), sqlc.arg(item_key), sqlc.arg(identity_id),
    sqlc.narg(conversation_id), sqlc.narg(artifact_id), sqlc.arg(class), sqlc.arg(action),
    sqlc.arg(expected_version), sqlc.arg(expected_bytes), 'planned', 'pending',
    sqlc.arg(now), sqlc.arg(now)
)
ON CONFLICT (operation_id, item_key) DO UPDATE SET item_key = EXCLUDED.item_key
RETURNING *;

-- name: ClaimRetentionItems :many
WITH claimable AS (
    SELECT candidate.id
    FROM aura.retention_operation_items AS candidate
    WHERE candidate.operation_id = sqlc.arg(operation_id)
      AND EXISTS (
        SELECT 1
        FROM aura.retention_operations AS operation
        WHERE operation.id = candidate.operation_id
          AND operation.status IN ('deleting', 'retryable')
      )
      AND (
        candidate.status IN ('planned', 'retryable')
        OR (candidate.status = 'deleting' AND candidate.lease_expires_at <= sqlc.arg(now))
      )
    ORDER BY candidate.item_key
    LIMIT sqlc.arg(batch_size)
    FOR UPDATE SKIP LOCKED
)
UPDATE aura.retention_operation_items AS item
SET status = 'deleting',
    claim_owner = sqlc.arg(claim_owner),
    lease_expires_at = sqlc.arg(lease_expires_at),
    attempt_count = item.attempt_count + 1,
    updated_at = sqlc.arg(now)
FROM claimable
WHERE item.id = claimable.id
RETURNING item.*;

-- name: RecordRetentionArtifactResult :execrows
UPDATE aura.retention_operation_items
SET artifact_result = sqlc.arg(artifact_result),
    removed_bytes = sqlc.arg(removed_bytes),
    failure_class = sqlc.narg(failure_class),
    updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id)
  AND status = 'deleting'
  AND claim_owner = sqlc.arg(claim_owner);

-- name: FinalizeRetentionItem :execrows
UPDATE aura.retention_operation_items
SET status = 'completed',
    claim_owner = NULL,
    lease_expires_at = NULL,
    failure_class = NULL,
    completed_at = sqlc.arg(now),
    updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id)
  AND status = 'deleting'
  AND claim_owner = sqlc.arg(claim_owner)
  AND artifact_result IN ('removed', 'absent');

-- name: RetryRetentionItem :execrows
UPDATE aura.retention_operation_items
SET status = 'retryable',
    artifact_result = CASE WHEN artifact_result = 'failed' THEN 'pending' ELSE artifact_result END,
    failure_class = sqlc.arg(failure_class),
    claim_owner = NULL,
    lease_expires_at = NULL,
    updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id)
  AND status = 'deleting'
  AND claim_owner = sqlc.arg(claim_owner);

-- name: FailRetentionItem :execrows
UPDATE aura.retention_operation_items
SET status = 'failed',
    artifact_result = CASE WHEN artifact_result = 'pending' THEN 'failed' ELSE artifact_result END,
    failure_class = sqlc.arg(failure_class),
    claim_owner = NULL,
    lease_expires_at = NULL,
    updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id)
  AND status = 'deleting'
  AND claim_owner = sqlc.arg(claim_owner);

-- name: GetRetentionItem :one
SELECT * FROM aura.retention_operation_items WHERE id = sqlc.arg(id);

-- name: FinalizeRetentionOperation :one
UPDATE aura.retention_operations AS operation
SET status = CASE
        WHEN EXISTS (
            SELECT 1 FROM aura.retention_operation_items item
            WHERE item.operation_id = operation.id AND item.status = 'failed'
        ) THEN 'failed'
        WHEN EXISTS (
            SELECT 1 FROM aura.retention_operation_items item
            WHERE item.operation_id = operation.id AND item.status <> 'completed'
        ) THEN 'retryable'
        ELSE 'completed'
    END,
    completed_count = (
        SELECT count(*) FROM aura.retention_operation_items item
        WHERE item.operation_id = operation.id AND item.status = 'completed'
    ),
    completed_bytes = COALESCE((
        SELECT sum(removed_bytes) FROM aura.retention_operation_items item
        WHERE item.operation_id = operation.id AND item.status = 'completed'
    ), 0),
    failure_count = (
        SELECT count(*) FROM aura.retention_operation_items item
        WHERE item.operation_id = operation.id AND item.status IN ('retryable', 'failed')
    ),
    completed_at = CASE WHEN NOT EXISTS (
        SELECT 1 FROM aura.retention_operation_items item
        WHERE item.operation_id = operation.id AND item.status <> 'completed'
    ) THEN sqlc.arg(now)::timestamptz ELSE NULL END,
    updated_at = sqlc.arg(now)
WHERE operation.id = sqlc.arg(id)
RETURNING operation.*;
