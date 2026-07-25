-- name: InsertAdaptivePolicySnapshot :execrows
INSERT INTO aura.adaptive_policy_snapshots (
    id, owner_id, sha256, schema_version, domain, decision_point,
    provider_id, model_id, policy_version, training_cutoff,
    feature_schema, action_catalog, champion_action_id, artifact
) VALUES (
    sqlc.arg(snapshot_id), sqlc.arg(owner_id), sqlc.arg(snapshot_sha256),
    sqlc.arg(schema_version), sqlc.arg(domain), sqlc.arg(decision_point),
    sqlc.arg(provider_id), sqlc.arg(model_id), sqlc.arg(policy_version),
    sqlc.arg(training_cutoff), sqlc.arg(feature_schema),
    sqlc.arg(action_catalog), sqlc.arg(champion_action_id), sqlc.arg(artifact)
)
ON CONFLICT (id) DO NOTHING;

-- name: GetAdaptivePolicySnapshot :one
SELECT id, owner_id, sha256, schema_version, domain, decision_point,
       provider_id, model_id, policy_version, training_cutoff,
       feature_schema, action_catalog, champion_action_id, artifact, created_at
FROM aura.adaptive_policy_snapshots
WHERE owner_id = sqlc.arg(owner_id)
  AND id = sqlc.arg(snapshot_id);
