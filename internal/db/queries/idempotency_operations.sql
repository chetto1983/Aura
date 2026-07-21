-- name: TryStartOperation :execrows
INSERT INTO aura.idempotency_operations (
    identity_id,
    operation_scope,
    operation_key,
    payload_hash,
    state,
    lease_expires_at,
    retry_after,
    audit_conversation_id,
    audit_request_id,
    audit_tool_call_id,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(identity_id),
    sqlc.arg(operation_scope),
    sqlc.arg(operation_key),
    sqlc.arg(payload_hash),
    'in_progress',
    sqlc.arg(lease_expires_at),
    sqlc.arg(retry_after),
    sqlc.narg(audit_conversation_id),
    sqlc.narg(audit_request_id),
    sqlc.narg(audit_tool_call_id),
    sqlc.arg(now),
    sqlc.arg(now)
)
ON CONFLICT (identity_id, operation_scope, operation_key) DO NOTHING;

-- name: GetOperation :one
SELECT
    identity_id,
    operation_scope,
    operation_key,
    payload_hash,
    state,
    replay_body,
    replay_preview,
    replay_sidecar_ref,
    replay_bytes,
    replay_expires_at,
    lease_expires_at,
    retry_after,
    audit_conversation_id,
    audit_request_id,
    audit_tool_call_id,
    version,
    created_at,
    updated_at,
    completed_at,
    indeterminate_at
FROM aura.idempotency_operations
WHERE identity_id = sqlc.arg(identity_id)
  AND operation_scope = sqlc.arg(operation_scope)
  AND operation_key = sqlc.arg(operation_key);

-- name: CompleteOperation :execrows
UPDATE aura.idempotency_operations
SET state = 'completed',
    replay_body = sqlc.narg(replay_body),
    replay_preview = sqlc.narg(replay_preview),
    replay_sidecar_ref = sqlc.narg(replay_sidecar_ref),
    replay_bytes = sqlc.arg(replay_bytes),
    replay_expires_at = sqlc.arg(replay_expires_at),
    completed_at = sqlc.arg(now),
    updated_at = sqlc.arg(now),
    version = version + 1
WHERE identity_id = sqlc.arg(identity_id)
  AND operation_scope = sqlc.arg(operation_scope)
  AND operation_key = sqlc.arg(operation_key)
  AND state = 'in_progress' AND payload_hash = sqlc.arg(payload_hash);

-- name: MarkOperationIndeterminate :execrows
UPDATE aura.idempotency_operations
SET state = 'indeterminate',
    indeterminate_at = sqlc.arg(now),
    updated_at = sqlc.arg(now),
    version = version + 1
WHERE identity_id = sqlc.arg(identity_id)
  AND operation_scope = sqlc.arg(operation_scope)
  AND operation_key = sqlc.arg(operation_key)
  AND state = 'in_progress' AND payload_hash = sqlc.arg(payload_hash);

-- name: ListExpiredReplayBodies :many
SELECT
    identity_id,
    operation_scope,
    operation_key,
    replay_bytes,
    replay_expires_at
FROM aura.idempotency_operations
WHERE state = 'completed'
  AND replay_expires_at <= sqlc.arg(before)
  AND (replay_body IS NOT NULL OR replay_preview IS NOT NULL OR replay_sidecar_ref IS NOT NULL)
ORDER BY replay_expires_at ASC, identity_id, operation_scope, operation_key
LIMIT sqlc.arg(batch_size);

-- name: ClearExpiredReplayBody :execrows
UPDATE aura.idempotency_operations
SET replay_body = NULL,
    replay_preview = NULL,
    replay_sidecar_ref = NULL,
    replay_bytes = 0,
    updated_at = sqlc.arg(cleared_at),
    version = version + 1
WHERE identity_id = sqlc.arg(identity_id)
  AND operation_scope = sqlc.arg(operation_scope)
  AND operation_key = sqlc.arg(operation_key)
  AND state = 'completed'
  AND replay_expires_at <= sqlc.arg(before)
  AND (replay_body IS NOT NULL OR replay_preview IS NOT NULL OR replay_sidecar_ref IS NOT NULL);
