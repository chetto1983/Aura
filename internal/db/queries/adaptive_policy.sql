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

-- name: StoreAdaptiveEvidenceArtifact :one
SELECT *
FROM aura.store_adaptive_evidence_artifact(
    sqlc.arg(actor_id), sqlc.arg(owner_id), sqlc.arg(cohort_id),
    sqlc.arg(kind), sqlc.arg(artifact_id), sqlc.arg(schema_id),
    sqlc.arg(revision), sqlc.arg(artifact_sha256),
    sqlc.arg(artifact), sqlc.arg(artifact_json)
);

-- name: GetAdaptiveEvidenceArtifact :one
SELECT id, owner_id, cohort_id, kind, schema_id, revision,
       sha256, artifact, artifact_json, created_at
FROM aura.adaptive_evidence_artifacts
WHERE owner_id = sqlc.arg(owner_id)
  AND cohort_id = sqlc.arg(cohort_id)
  AND id = sqlc.arg(artifact_id)
  AND kind = sqlc.arg(kind);

-- name: ListAdaptiveEvidenceArtifactsByKind :many
SELECT id, owner_id, cohort_id, kind, schema_id, revision,
       sha256, artifact, artifact_json, created_at
FROM aura.adaptive_evidence_artifacts
WHERE owner_id = sqlc.arg(owner_id)
  AND cohort_id = sqlc.arg(cohort_id)
  AND kind = sqlc.arg(kind)
ORDER BY id;

-- name: AdaptiveEvidenceTransactionTime :one
SELECT transaction_timestamp()::timestamptz AS evidence_time;

-- name: SealAdaptiveEvidence :one
SELECT *
FROM aura.seal_adaptive_evidence(
    sqlc.arg(actor_id), sqlc.arg(kind), sqlc.arg(evidence_id),
    sqlc.arg(owner_id), sqlc.arg(cohort_id), sqlc.arg(cohort_sha256),
    sqlc.arg(domain), sqlc.arg(provider_id), sqlc.arg(model_id),
    sqlc.arg(policy_epoch), sqlc.arg(policy_version),
    sqlc.arg(snapshot_id), sqlc.arg(snapshot_sha256),
    sqlc.narg(look_number), sqlc.narg(look_cutoff),
    sqlc.narg(predecessor_evidence_id), sqlc.narg(predecessor_sha256),
    sqlc.narg(disposition), sqlc.arg(eligible),
    sqlc.arg(body_sha256), sqlc.arg(evidence_sha256),
    sqlc.arg(artifact), sqlc.arg(artifact_json),
    sqlc.arg(sealer_capability)
);

-- name: GetAdaptiveSealedEvidence :one
SELECT id, kind, owner_id, cohort_id, cohort_sha256, domain, provider_id,
       model_id, policy_epoch, policy_version, snapshot_id, snapshot_sha256,
       look_number, look_cutoff, predecessor_evidence_id, predecessor_sha256,
       disposition, eligible, body_sha256, sha256, artifact, artifact_json,
       sealer_actor_id, sealer_capability, created_at
FROM aura.adaptive_sealed_evidence
WHERE owner_id = sqlc.arg(owner_id)
  AND id = sqlc.arg(evidence_id);

-- name: GetAdaptiveSealedOutcomeLook :one
SELECT id, kind, owner_id, cohort_id, cohort_sha256, domain, provider_id,
       model_id, policy_epoch, policy_version, snapshot_id, snapshot_sha256,
       look_number, look_cutoff, predecessor_evidence_id, predecessor_sha256,
       disposition, eligible, body_sha256, sha256, artifact, artifact_json,
       sealer_actor_id, sealer_capability, created_at
FROM aura.adaptive_sealed_evidence
WHERE owner_id = sqlc.arg(owner_id)
  AND cohort_id = sqlc.arg(cohort_id)
  AND kind = 'canary_outcome'
  AND look_number = sqlc.arg(look_number);

-- name: GetAdaptiveSealedAdmission :one
SELECT id, kind, owner_id, cohort_id, cohort_sha256, domain, provider_id,
       model_id, policy_epoch, policy_version, snapshot_id, snapshot_sha256,
       look_number, look_cutoff, predecessor_evidence_id, predecessor_sha256,
       disposition, eligible, body_sha256, sha256, artifact, artifact_json,
       sealer_actor_id, sealer_capability, created_at
FROM aura.adaptive_sealed_evidence
WHERE owner_id = sqlc.arg(owner_id)
  AND cohort_id = sqlc.arg(cohort_id)
  AND kind = 'canary_admission';

-- name: ApplyAdaptivePolicyTransition :one
WITH applied AS MATERIALIZED (
    SELECT aura.apply_adaptive_policy_transition(
        sqlc.arg(actor_id), sqlc.arg(expected_epoch), sqlc.arg(evidence_id),
        sqlc.arg(rollout_bps), sqlc.arg(reason)
    ) AS result
)
SELECT policy.epoch, policy.policy_version, policy.mode,
       policy.rollout_bps, policy.config, policy.updated_at
FROM (
    SELECT
        (to_jsonb(applied.result)->>'epoch')::bigint AS epoch,
        (to_jsonb(applied.result)->>'policy_version')::text AS policy_version,
        (to_jsonb(applied.result)->>'mode')::text AS mode,
        (to_jsonb(applied.result)->>'rollout_bps')::integer AS rollout_bps,
        (to_jsonb(applied.result)->'config')::jsonb AS config,
        (to_jsonb(applied.result)->>'updated_at')::timestamptz AS updated_at
    FROM applied
) AS policy;
