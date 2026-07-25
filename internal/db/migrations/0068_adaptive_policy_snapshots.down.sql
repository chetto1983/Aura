DROP TABLE IF EXISTS aura.adaptive_policy_snapshots;
DROP FUNCTION IF EXISTS aura.adaptive_policy_snapshot_artifact_valid(
    bytea, uuid, text, text, text, text, text, timestamptz, jsonb, jsonb, text
);
