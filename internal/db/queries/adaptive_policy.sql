-- name: GetAdaptivePolicy :one
SELECT epoch, policy_version, mode, rollout_bps, config, updated_at
FROM aura.adaptive_policy_state
WHERE scope = 'global';

-- name: InsertAdaptiveRandomizationReceipt :one
INSERT INTO aura.adaptive_randomization_receipts (
    id, owner_id, cohort_id, request_id, claim_id, assignment_id,
    randomization_plan_sha256, analysis_stratum_schema_sha256,
    analysis_stratum_id, sha256, artifact, artifact_json
) VALUES (
    sqlc.arg(receipt_id), sqlc.arg(owner_id), sqlc.arg(cohort_id),
    sqlc.arg(request_id), sqlc.arg(claim_id), sqlc.arg(assignment_id),
    sqlc.arg(randomization_plan_sha256),
    sqlc.arg(analysis_stratum_schema_sha256),
    sqlc.arg(analysis_stratum_id), sqlc.arg(receipt_sha256),
    sqlc.arg(artifact), sqlc.arg(artifact_json)
)
ON CONFLICT (owner_id, assignment_id) DO NOTHING
RETURNING id, owner_id, cohort_id, request_id, claim_id, assignment_id,
          randomization_plan_sha256, analysis_stratum_schema_sha256,
          analysis_stratum_id, sha256, artifact, artifact_json, created_at;

-- name: LockAdaptiveRandomizationReceipt :one
SELECT id, owner_id, cohort_id, request_id, claim_id, assignment_id,
       randomization_plan_sha256, analysis_stratum_schema_sha256,
       analysis_stratum_id, sha256, artifact, artifact_json, created_at
FROM aura.adaptive_randomization_receipts
WHERE owner_id = sqlc.arg(owner_id)
  AND assignment_id = sqlc.arg(assignment_id);
