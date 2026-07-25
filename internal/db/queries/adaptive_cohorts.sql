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
       dead_letter_at, last_error_class
FROM aura.adaptive_outbox
WHERE owner_id = sqlc.arg(owner_id)
  AND decision_id = sqlc.arg(assignment_id)
  AND event_kind = 'decision'
  AND payload->>'schema_version' = '2.0'
FOR UPDATE;
