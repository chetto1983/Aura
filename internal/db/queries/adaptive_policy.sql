-- name: GetAdaptivePolicy :one
SELECT epoch, policy_version, mode, rollout_bps, config, updated_at
FROM aura.adaptive_policy_state
WHERE scope = 'global';
