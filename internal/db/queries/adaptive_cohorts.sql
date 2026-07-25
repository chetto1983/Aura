-- name: InsertAdaptiveFocalCohort :execrows
INSERT INTO aura.adaptive_focal_cohorts (
    id, owner_id, provider_id, model_id, policy_epoch, policy_version,
    snapshot_id, snapshot_sha256, environment, domain, decision_point,
    point_ordinal, predicate_sha256, experiment_id, cutoff, sha256,
    artifact, artifact_json
) VALUES (
    sqlc.arg(cohort_id), sqlc.arg(owner_id), sqlc.arg(provider_id),
    sqlc.arg(model_id), sqlc.arg(policy_epoch), sqlc.arg(policy_version),
    sqlc.arg(snapshot_id), sqlc.arg(snapshot_sha256), sqlc.arg(environment),
    sqlc.arg(domain), sqlc.arg(decision_point), sqlc.arg(point_ordinal),
    sqlc.arg(predicate_sha256), sqlc.arg(experiment_id), sqlc.arg(cutoff),
    sqlc.arg(cohort_sha256), sqlc.arg(artifact), sqlc.arg(artifact_json)
)
ON CONFLICT (id) DO NOTHING;

-- name: GetAdaptiveFocalCohort :one
SELECT id, owner_id, provider_id, model_id, policy_epoch, policy_version,
       snapshot_id, snapshot_sha256, environment, domain, decision_point,
       point_ordinal, predicate_sha256, experiment_id, cutoff, sha256,
       artifact, artifact_json, created_at
FROM aura.adaptive_focal_cohorts
WHERE owner_id = sqlc.arg(owner_id)
  AND id = sqlc.arg(cohort_id);

-- name: LockAdaptiveOwnerForCohortReconstruction :one
SELECT id
FROM aura.identities
WHERE id = sqlc.arg(owner_id)
FOR UPDATE;

-- name: AdaptiveCohortReconstructionTime :one
SELECT statement_timestamp()::timestamptz AS closure_time;

-- name: GetAdaptiveFocalCohortForReconstruction :one
SELECT id, owner_id, provider_id, model_id, policy_epoch, policy_version,
       snapshot_id, snapshot_sha256, environment, domain, decision_point,
       point_ordinal, predicate_sha256, experiment_id, cutoff, sha256,
       artifact, artifact_json, created_at
FROM aura.adaptive_focal_cohorts
WHERE owner_id = sqlc.arg(owner_id)
  AND id = sqlc.arg(cohort_id);

-- name: ListAdaptiveFocalCohortClaimsForReconstruction :many
SELECT id, owner_id, cohort_id, request_id, evaluation_conversation_id,
       assignment_id, domain, decision_point, point_ordinal, session_id,
       episode_id, time_block_start, claimed_at
FROM aura.adaptive_focal_cohort_claims
WHERE owner_id = sqlc.arg(owner_id)
  AND cohort_id = sqlc.arg(cohort_id)
ORDER BY request_id, assignment_id, id;

-- name: ListAdaptiveFocalCohortFactsForReconstruction :many
WITH cohort_assignments AS (
    SELECT decision_id
    FROM aura.adaptive_outbox
    WHERE owner_id = sqlc.arg(owner_id)
      AND event_kind = 'decision'
      AND payload->>'cohort_id' = sqlc.arg(cohort_id)::text
)
SELECT fact.id, fact.owner_id, fact.aggregate_id, fact.sequence,
       fact.decision_id, fact.event_kind, fact.payload, fact.payload_hash,
       fact.status, fact.attempts, fact.available_at, fact.lease_owner,
       fact.lease_expires_at, fact.created_at, fact.projected_at,
       fact.dead_letter_at, fact.last_error_class, fact.recorded_at
FROM aura.adaptive_outbox AS fact
WHERE fact.owner_id = sqlc.arg(owner_id)
  AND (
      (fact.event_kind = 'decision' AND fact.payload->>'cohort_id' = sqlc.arg(cohort_id)::text)
      OR (
          fact.event_kind IN ('delivery', 'outcome', 'correction')
          AND fact.decision_id IN (SELECT decision_id FROM cohort_assignments)
      )
  )
ORDER BY fact.decision_id, fact.event_kind, fact.id;

-- name: InsertAdaptiveFocalCohortClaim :one
INSERT INTO aura.adaptive_focal_cohort_claims (
    owner_id, cohort_id, request_id, evaluation_conversation_id,
    assignment_id, domain, decision_point, point_ordinal, session_id,
    episode_id, time_block_start
) VALUES (
    sqlc.arg(owner_id), sqlc.arg(cohort_id), sqlc.arg(request_id),
    sqlc.arg(evaluation_conversation_id), sqlc.arg(assignment_id),
    sqlc.arg(domain), sqlc.arg(decision_point), sqlc.arg(point_ordinal),
    sqlc.narg(session_id), sqlc.narg(episode_id), sqlc.arg(time_block_start)
)
ON CONFLICT DO NOTHING
RETURNING id, owner_id, cohort_id, request_id, evaluation_conversation_id,
          assignment_id, domain, decision_point, point_ordinal, session_id,
          episode_id, time_block_start, claimed_at;

-- name: ListAdaptiveFocalCohortClaimConflicts :many
SELECT id, owner_id, cohort_id, request_id, evaluation_conversation_id,
       assignment_id, domain, decision_point, point_ordinal, session_id,
       episode_id, time_block_start, claimed_at
FROM aura.adaptive_focal_cohort_claims
WHERE owner_id = sqlc.arg(owner_id)
  AND (
      evaluation_conversation_id = sqlc.arg(evaluation_conversation_id)
      OR (
          cohort_id = sqlc.arg(cohort_id)
          AND (
              request_id = sqlc.arg(request_id)
              OR assignment_id = sqlc.arg(assignment_id)
          )
      )
  )
ORDER BY id;

-- name: LockSchema2AdaptiveAssignmentEvents :many
SELECT id, owner_id, aggregate_id, sequence, decision_id, event_kind,
       payload, payload_hash, status, attempts, available_at,
       lease_owner, lease_expires_at, created_at, projected_at,
       dead_letter_at, last_error_class, recorded_at
FROM aura.adaptive_outbox
WHERE owner_id = sqlc.arg(owner_id)
  AND decision_id = sqlc.arg(assignment_id)
  AND event_kind = 'decision'
  AND payload->>'schema_version' = '2.0'
FOR UPDATE;
