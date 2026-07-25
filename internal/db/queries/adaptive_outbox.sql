-- name: LockAdaptiveEvent :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(event_id)::text, 0));

-- name: LockAdaptiveOwner :one
SELECT id
FROM aura.identities
WHERE id = sqlc.arg(owner_id)
FOR KEY SHARE;

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

-- name: LockSchema2AdaptiveAssignment :one
WITH valid_assignments AS MATERIALIZED (
    SELECT assignment.id, assignment.owner_id, assignment.aggregate_id,
           assignment.decision_id, assignment.payload
    FROM aura.adaptive_outbox AS assignment
    WHERE assignment.owner_id = sqlc.arg(owner_id)
      AND assignment.decision_id = sqlc.arg(assignment_id)
      AND assignment.event_kind = 'decision'
      AND assignment.payload->>'schema_version' = '2.0'
      AND aura.adaptive_schema2_assignment_payload_valid(
          assignment.payload, assignment.owner_id,
          assignment.aggregate_id, assignment.decision_id
      ) IS TRUE
),
digests AS (
    SELECT valid_assignments.*,
           substring(sha256(
               uuid_send('c5370396-c73f-4a44-ae7b-112f070523ae'::uuid)
               || uuid_send(owner_id)
               || uuid_send((payload->>'request_id')::uuid)
               || decode('00', 'hex')
               || convert_to(payload->>'point', 'UTF8')
               || decode('00', 'hex')
               || substring(int8send(
                   ((payload->>'point_ordinal')::numeric)::bigint
               ) FROM 5 FOR 4)
           ) FROM 1 FOR 16) AS assignment_digest,
           substring(sha256(
               uuid_send('fb3f7ce9-d343-41fb-a26f-35155b229189'::uuid)
               || convert_to('2.0', 'UTF8')
               || decode('00', 'hex')
               || uuid_send(decision_id)
               || decode('00', 'hex')
               || convert_to('decision', 'UTF8')
               || decode('00', 'hex')
               || convert_to('assignment', 'UTF8')
           ) FROM 1 FOR 16) AS event_digest
    FROM valid_assignments
),
canonical_assignments AS (
    SELECT digests.*,
           encode(set_byte(set_byte(
               assignment_digest, 6,
               (get_byte(assignment_digest, 6) & 15) | 80
           ), 8, (get_byte(assignment_digest, 8) & 63) | 128), 'hex')::uuid
               AS canonical_assignment_id,
           encode(set_byte(set_byte(
               event_digest, 6,
               (get_byte(event_digest, 6) & 15) | 80
           ), 8, (get_byte(event_digest, 8) & 63) | 128), 'hex')::uuid
               AS canonical_event_id
    FROM digests
)
SELECT assignment.id, assignment.owner_id, assignment.aggregate_id,
       assignment.sequence, assignment.decision_id, assignment.event_kind,
       assignment.payload, assignment.payload_hash, assignment.status,
       assignment.attempts, assignment.available_at, assignment.lease_owner,
       assignment.lease_expires_at, assignment.created_at,
       assignment.projected_at, assignment.dead_letter_at,
       assignment.last_error_class
FROM aura.adaptive_outbox AS assignment
JOIN canonical_assignments AS canonical
  ON canonical.id = assignment.id
WHERE canonical.decision_id = canonical.canonical_assignment_id
  AND canonical.id = canonical.canonical_event_id
FOR UPDATE OF assignment;

-- name: GetSchema2AdaptiveDelivery :one
WITH valid_deliveries AS MATERIALIZED (
    SELECT delivery.id, delivery.owner_id, delivery.aggregate_id,
           delivery.decision_id, delivery.payload
    FROM aura.adaptive_outbox AS delivery
    WHERE delivery.owner_id = sqlc.arg(owner_id)
      AND delivery.decision_id = sqlc.arg(assignment_id)
      AND delivery.event_kind = 'delivery'
      AND delivery.payload->>'schema_version' = '2.0'
      AND aura.adaptive_schema2_delivery_payload_valid(
          delivery.payload, delivery.decision_id
      ) IS TRUE
),
valid_assignments AS MATERIALIZED (
    SELECT assignment.id, assignment.owner_id, assignment.aggregate_id,
           assignment.decision_id, assignment.payload
    FROM aura.adaptive_outbox AS assignment
    WHERE assignment.owner_id = sqlc.arg(owner_id)
      AND assignment.decision_id = sqlc.arg(assignment_id)
      AND assignment.event_kind = 'decision'
      AND assignment.payload->>'schema_version' = '2.0'
      AND aura.adaptive_schema2_assignment_payload_valid(
          assignment.payload, assignment.owner_id,
          assignment.aggregate_id, assignment.decision_id
      ) IS TRUE
),
assignment_digests AS (
    SELECT valid_assignments.*,
           substring(sha256(
               uuid_send('c5370396-c73f-4a44-ae7b-112f070523ae'::uuid)
               || uuid_send(owner_id)
               || uuid_send((payload->>'request_id')::uuid)
               || decode('00', 'hex')
               || convert_to(payload->>'point', 'UTF8')
               || decode('00', 'hex')
               || substring(int8send(
                   ((payload->>'point_ordinal')::numeric)::bigint
               ) FROM 5 FOR 4)
           ) FROM 1 FOR 16) AS assignment_digest,
           substring(sha256(
               uuid_send('fb3f7ce9-d343-41fb-a26f-35155b229189'::uuid)
               || convert_to('2.0', 'UTF8')
               || decode('00', 'hex')
               || uuid_send(decision_id)
               || decode('00', 'hex')
               || convert_to('decision', 'UTF8')
               || decode('00', 'hex')
               || convert_to('assignment', 'UTF8')
           ) FROM 1 FOR 16) AS event_digest
    FROM valid_assignments
),
canonical_assignments AS (
    SELECT assignment_digests.*,
           encode(set_byte(set_byte(
               assignment_digest, 6,
               (get_byte(assignment_digest, 6) & 15) | 80
           ), 8, (get_byte(assignment_digest, 8) & 63) | 128), 'hex')::uuid
               AS canonical_assignment_id,
           encode(set_byte(set_byte(
               event_digest, 6,
               (get_byte(event_digest, 6) & 15) | 80
           ), 8, (get_byte(event_digest, 8) & 63) | 128), 'hex')::uuid
               AS canonical_event_id
    FROM assignment_digests
),
delivery_digests AS (
    SELECT valid_deliveries.*,
           substring(sha256(
               uuid_send('fb3f7ce9-d343-41fb-a26f-35155b229189'::uuid)
               || convert_to('2.0', 'UTF8')
               || decode('00', 'hex')
               || uuid_send(decision_id)
               || decode('00', 'hex')
               || convert_to('delivery', 'UTF8')
               || decode('00', 'hex')
               || convert_to('assignment', 'UTF8')
           ) FROM 1 FOR 16) AS event_digest
    FROM valid_deliveries
),
canonical_deliveries AS (
    SELECT delivery_digests.*,
           encode(set_byte(set_byte(
               event_digest, 6,
               (get_byte(event_digest, 6) & 15) | 80
           ), 8, (get_byte(event_digest, 8) & 63) | 128), 'hex')::uuid
               AS canonical_event_id
    FROM delivery_digests
)
SELECT delivery.id, delivery.owner_id, delivery.aggregate_id,
       delivery.sequence, delivery.decision_id, delivery.event_kind,
       delivery.payload, delivery.payload_hash, delivery.status,
       delivery.attempts, delivery.available_at, delivery.lease_owner,
       delivery.lease_expires_at, delivery.created_at,
       delivery.projected_at, delivery.dead_letter_at,
       delivery.last_error_class
FROM aura.adaptive_outbox AS delivery
JOIN canonical_deliveries AS canonical_delivery
  ON canonical_delivery.id = delivery.id
JOIN canonical_assignments AS assignment
  ON assignment.owner_id = canonical_delivery.owner_id
 AND assignment.aggregate_id = canonical_delivery.aggregate_id
 AND assignment.decision_id = canonical_delivery.decision_id
WHERE assignment.decision_id = assignment.canonical_assignment_id
  AND assignment.id = assignment.canonical_event_id
  AND canonical_delivery.id = canonical_delivery.canonical_event_id
  AND canonical_delivery.payload->>'intended_action_id' =
      assignment.payload->>'intended_action_id'
  AND (
      canonical_delivery.payload->>'actual_action_id' = 'none'
      OR (assignment.payload->'eligible_actions') ?
          (canonical_delivery.payload->>'actual_action_id')
  )
  AND (
      canonical_delivery.payload->>'status' <> 'success'
      OR NOT (canonical_delivery.payload->>'exposure_known')::boolean
      OR EXISTS (
          SELECT 1
          FROM jsonb_array_elements(
              assignment.payload->'action_probabilities'
          ) AS probability
          WHERE probability->>'action_id' =
              canonical_delivery.payload->>'intended_action_id'
            AND (probability->>'probability')::numeric =
                (canonical_delivery.payload->>'exposure_probability')::numeric
      )
  );

-- name: LockSchema2AdaptiveOutcomeChain :many
SELECT id, owner_id, aggregate_id, sequence, decision_id, event_kind,
       payload, payload_hash, status, attempts, available_at,
       lease_owner, lease_expires_at, created_at, projected_at,
       dead_letter_at, last_error_class
FROM aura.adaptive_outbox
WHERE owner_id = sqlc.arg(owner_id)
  AND decision_id = sqlc.arg(assignment_id)
  AND event_kind IN ('outcome', 'correction')
  AND payload->>'schema_version' = '2.0'
ORDER BY sequence ASC
FOR UPDATE;

-- name: ListEligibleSchema2AdaptiveAggregateFacts :many
WITH valid_assignments AS MATERIALIZED (
    SELECT assignment.id, assignment.owner_id, assignment.aggregate_id,
           assignment.decision_id, assignment.payload
    FROM aura.adaptive_outbox AS assignment
    WHERE assignment.owner_id = sqlc.arg(owner_id)
      AND assignment.aggregate_id = sqlc.arg(aggregate_id)
      AND assignment.event_kind = 'decision'
      AND assignment.payload->>'schema_version' = '2.0'
      AND aura.adaptive_schema2_assignment_payload_valid(
          assignment.payload, assignment.owner_id,
          assignment.aggregate_id, assignment.decision_id
      ) IS TRUE
),
assignment_digests AS (
    SELECT valid_assignments.*,
           substring(sha256(
               uuid_send('c5370396-c73f-4a44-ae7b-112f070523ae'::uuid)
               || uuid_send(owner_id)
               || uuid_send((payload->>'request_id')::uuid)
               || decode('00', 'hex')
               || convert_to(payload->>'point', 'UTF8')
               || decode('00', 'hex')
               || substring(int8send(
                   ((payload->>'point_ordinal')::numeric)::bigint
               ) FROM 5 FOR 4)
           ) FROM 1 FOR 16) AS assignment_digest,
           substring(sha256(
               uuid_send('fb3f7ce9-d343-41fb-a26f-35155b229189'::uuid)
               || convert_to('2.0', 'UTF8')
               || decode('00', 'hex')
               || uuid_send(decision_id)
               || decode('00', 'hex')
               || convert_to('decision', 'UTF8')
               || decode('00', 'hex')
               || convert_to('assignment', 'UTF8')
           ) FROM 1 FOR 16) AS event_digest
    FROM valid_assignments
),
canonical_assignments AS (
    SELECT assignment_digests.*,
           encode(set_byte(set_byte(
               assignment_digest, 6,
               (get_byte(assignment_digest, 6) & 15) | 80
           ), 8, (get_byte(assignment_digest, 8) & 63) | 128), 'hex')::uuid
               AS canonical_assignment_id,
           encode(set_byte(set_byte(
               event_digest, 6,
               (get_byte(event_digest, 6) & 15) | 80
           ), 8, (get_byte(event_digest, 8) & 63) | 128), 'hex')::uuid
               AS canonical_event_id
    FROM assignment_digests
),
valid_deliveries AS MATERIALIZED (
    SELECT delivery.id, delivery.owner_id, delivery.aggregate_id,
           delivery.decision_id, delivery.payload
    FROM aura.adaptive_outbox AS delivery
    WHERE delivery.owner_id = sqlc.arg(owner_id)
      AND delivery.aggregate_id = sqlc.arg(aggregate_id)
      AND delivery.event_kind = 'delivery'
      AND delivery.payload->>'schema_version' = '2.0'
      AND aura.adaptive_schema2_delivery_payload_valid(
          delivery.payload, delivery.decision_id
      ) IS TRUE
),
delivery_digests AS (
    SELECT valid_deliveries.*,
           substring(sha256(
               uuid_send('fb3f7ce9-d343-41fb-a26f-35155b229189'::uuid)
               || convert_to('2.0', 'UTF8')
               || decode('00', 'hex')
               || uuid_send(decision_id)
               || decode('00', 'hex')
               || convert_to('delivery', 'UTF8')
               || decode('00', 'hex')
               || convert_to('assignment', 'UTF8')
           ) FROM 1 FOR 16) AS event_digest
    FROM valid_deliveries
),
canonical_deliveries AS (
    SELECT delivery_digests.*,
           encode(set_byte(set_byte(
               event_digest, 6,
               (get_byte(event_digest, 6) & 15) | 80
           ), 8, (get_byte(event_digest, 8) & 63) | 128), 'hex')::uuid
               AS canonical_event_id
    FROM delivery_digests
),
eligible_ids AS (
    SELECT assignment.id
    FROM canonical_assignments AS assignment
    WHERE assignment.decision_id = assignment.canonical_assignment_id
      AND assignment.id = assignment.canonical_event_id
    UNION ALL
    SELECT delivery.id
    FROM canonical_deliveries AS delivery
    JOIN canonical_assignments AS assignment
      ON assignment.owner_id = delivery.owner_id
     AND assignment.aggregate_id = delivery.aggregate_id
     AND assignment.decision_id = delivery.decision_id
    WHERE assignment.decision_id = assignment.canonical_assignment_id
      AND assignment.id = assignment.canonical_event_id
      AND delivery.id = delivery.canonical_event_id
      AND delivery.payload->>'intended_action_id' =
          assignment.payload->>'intended_action_id'
      AND (
          delivery.payload->>'actual_action_id' = 'none'
          OR (assignment.payload->'eligible_actions') ?
              (delivery.payload->>'actual_action_id')
      )
      AND (
          delivery.payload->>'status' <> 'success'
          OR NOT (delivery.payload->>'exposure_known')::boolean
          OR EXISTS (
              SELECT 1
              FROM jsonb_array_elements(
                  assignment.payload->'action_probabilities'
              ) AS probability
              WHERE probability->>'action_id' =
                  delivery.payload->>'intended_action_id'
                AND (probability->>'probability')::numeric =
                    (delivery.payload->>'exposure_probability')::numeric
          )
      )
)
SELECT fact.id, fact.owner_id, fact.aggregate_id, fact.sequence,
       fact.decision_id, fact.event_kind, fact.payload, fact.payload_hash,
       fact.status, fact.attempts, fact.available_at, fact.lease_owner,
       fact.lease_expires_at, fact.created_at, fact.projected_at,
       fact.dead_letter_at, fact.last_error_class
FROM aura.adaptive_outbox AS fact
JOIN eligible_ids ON eligible_ids.id = fact.id
WHERE fact.owner_id = sqlc.arg(owner_id)
  AND fact.aggregate_id = sqlc.arg(aggregate_id)
  AND fact.event_kind IN ('decision', 'delivery')
ORDER BY fact.sequence ASC;

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
