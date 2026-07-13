-- name: CreateCompactionRolloutState :one
INSERT INTO aura.compaction_rollout_states (
    scope_id, evaluator_version, scorer_version, config_version, corpus_version,
    active_config, last_known_good_config, last_known_good_policy
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (scope_id) DO NOTHING
RETURNING *;

-- name: GetCompactionRolloutState :one
SELECT * FROM aura.compaction_rollout_states WHERE scope_id = $1;

-- name: AppendCompactionRolloutEvidence :one
INSERT INTO aura.compaction_rollout_evidence (
    scope_id, evidence_digest, evaluator_version, scorer_version,
    config_version, corpus_version, snapshot
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: AppendCompactionRolloutDecision :one
INSERT INTO aura.compaction_rollout_decisions (
    scope_id, evidence_id, expected_version, resulting_version,
    decision_kind, from_stage, to_stage, reason_code
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: CASTransitionCompactionRollout :one
UPDATE aura.compaction_rollout_states
SET version = version + 1,
    stage = sqlc.arg(stage),
    stage_started_at = sqlc.arg(stage_started_at),
    eligible_attempts = sqlc.arg(eligible_attempts),
    evaluator_version = sqlc.arg(evaluator_version),
    scorer_version = sqlc.arg(scorer_version),
    config_version = sqlc.arg(config_version),
    corpus_version = sqlc.arg(corpus_version),
    stratum_snapshots = sqlc.arg(stratum_snapshots),
    failure_window = sqlc.arg(failure_window),
    latency_window = sqlc.arg(latency_window),
    restore_window = sqlc.arg(restore_window),
    active_config = sqlc.arg(active_config),
    last_known_good_config = sqlc.arg(last_known_good_config),
    last_known_good_policy = sqlc.arg(last_known_good_policy),
    updated_at = now()
WHERE scope_id = sqlc.arg(scope_id) AND version = sqlc.arg(expected_version)
RETURNING *;

-- name: CASRollbackCompactionRollout :one
UPDATE aura.compaction_rollout_states
SET version = version + 1,
    stage = 'disabled',
    stage_started_at = now(),
    active_config = last_known_good_config,
    stratum_snapshots = '{}'::jsonb,
    failure_window = '{}'::jsonb,
    latency_window = '{}'::jsonb,
    restore_window = '{}'::jsonb,
    updated_at = now()
WHERE scope_id = sqlc.arg(scope_id) AND version = sqlc.arg(expected_version)
RETURNING *;

-- name: CountCompactionRolloutDecisions :one
SELECT count(*) FROM aura.compaction_rollout_decisions WHERE scope_id = $1;
