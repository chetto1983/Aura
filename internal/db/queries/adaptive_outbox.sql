-- name: LockAdaptiveEvent :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(event_id)::text, 0));

-- name: AdaptiveIdentityTombstoned :one
SELECT EXISTS (
    SELECT 1
    FROM aura.adaptive_identity_tombstones
    WHERE owner_id = $1
) AS tombstoned;

-- name: GetAdaptiveOutboxByID :one
SELECT id, owner_id, aggregate_id, sequence, decision_id, event_kind,
       payload, payload_hash, status, attempts, available_at,
       lease_owner, lease_expires_at, created_at, projected_at,
       dead_letter_at, last_error_class
FROM aura.adaptive_outbox
WHERE id = $1;

-- name: NextAdaptiveAggregateSequence :one
SELECT aura.next_adaptive_aggregate_sequence(
    sqlc.arg(owner_id), sqlc.arg(aggregate_id)
)::bigint AS sequence;

-- name: InsertAdaptiveOutbox :execrows
INSERT INTO aura.adaptive_outbox (
    id, owner_id, aggregate_id, sequence, decision_id, event_kind,
    payload, payload_hash, status, attempts, available_at, created_at
) SELECT
    $1, $2, $3, $4, $5, $6,
    $7, $8, 'pending', 0, $9, $10
WHERE NOT EXISTS (
    SELECT 1
    FROM aura.adaptive_identity_tombstones
    WHERE owner_id = $2
)
ON CONFLICT (id) DO NOTHING;

-- name: ListAdaptiveAggregate :many
SELECT id, owner_id, aggregate_id, sequence, decision_id, event_kind,
       payload, payload_hash, status, attempts, available_at,
       lease_owner, lease_expires_at, created_at, projected_at,
       dead_letter_at, last_error_class
FROM aura.adaptive_outbox
WHERE owner_id = $1 AND aggregate_id = $2
ORDER BY sequence ASC;

-- name: ClaimAdaptiveOutbox :one
WITH candidate AS (
    SELECT current.id
    FROM aura.adaptive_outbox AS current
    WHERE (
            (current.status = 'pending' AND current.available_at <= sqlc.arg(claimed_at))
            OR
            (current.status = 'leased' AND current.lease_expires_at <= sqlc.arg(claimed_at))
          )
      AND current.attempts < sqlc.arg(max_attempts)
      AND NOT EXISTS (
          SELECT 1
          FROM aura.adaptive_outbox AS earlier
          WHERE earlier.owner_id = current.owner_id
            AND earlier.aggregate_id = current.aggregate_id
            AND earlier.sequence < current.sequence
            AND earlier.status <> 'projected'
      )
    ORDER BY current.available_at, current.created_at,
             current.owner_id, current.aggregate_id, current.sequence
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE aura.adaptive_outbox AS claimed
SET status = 'leased',
    attempts = claimed.attempts + 1,
    lease_owner = sqlc.arg(worker_id),
    lease_expires_at = sqlc.arg(lease_expires_at),
    last_error_class = NULL
FROM candidate
WHERE claimed.id = candidate.id
RETURNING claimed.id, claimed.owner_id, claimed.aggregate_id, claimed.sequence,
          claimed.decision_id, claimed.event_kind, claimed.payload,
          claimed.payload_hash, claimed.status, claimed.attempts,
          claimed.available_at, claimed.lease_owner, claimed.lease_expires_at,
          claimed.created_at, claimed.projected_at, claimed.dead_letter_at,
          claimed.last_error_class;

-- name: MarkAdaptiveProjected :execrows
UPDATE aura.adaptive_outbox
SET status = 'projected',
    lease_owner = NULL,
    lease_expires_at = NULL,
    projected_at = sqlc.arg(projected_at),
    dead_letter_at = NULL,
    last_error_class = NULL
WHERE id = sqlc.arg(event_id)
  AND status = 'leased'
  AND lease_owner = sqlc.arg(worker_id);

-- name: RetryAdaptiveOutbox :execrows
UPDATE aura.adaptive_outbox
SET status = CASE
        WHEN attempts >= sqlc.arg(max_attempts) THEN 'dead_letter'
        ELSE 'pending'
    END,
    available_at = sqlc.arg(available_at),
    lease_owner = NULL,
    lease_expires_at = NULL,
    projected_at = NULL,
    dead_letter_at = CASE
        WHEN attempts >= sqlc.arg(max_attempts) THEN sqlc.arg(failed_at)::timestamptz
        ELSE NULL
    END,
    last_error_class = sqlc.arg(error_class)
WHERE id = sqlc.arg(event_id)
  AND status = 'leased'
  AND lease_owner = sqlc.arg(worker_id);
