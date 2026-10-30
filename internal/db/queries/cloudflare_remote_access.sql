-- name: GetCloudflareRemoteAccess :one
SELECT * FROM aura.cloudflare_remote_access WHERE singleton = true;

-- name: SaveCloudflareRemoteAccessDesired :one
WITH intent AS (
  SELECT singleton, phase = 'deleting' OR (phase = 'error' AND NOT enabled AND (
    zone_id <> '' OR tunnel_id <> '' OR public_dns_id <> '' OR warp_dns_id <> ''
    OR otp_idp_id <> '' OR public_app_id <> '' OR public_policy_id <> ''
    OR warp_app_id <> '' OR warp_policy_id <> '' OR warp_posture_id <> ''
  )) AS deleting
  FROM aura.cloudflare_remote_access WHERE singleton = true FOR UPDATE
)
UPDATE aura.cloudflare_remote_access AS state
SET enabled = CASE WHEN intent.deleting THEN false ELSE sqlc.arg(enabled)::boolean END,
    account_id = sqlc.arg(account_id),
    zone_name = sqlc.arg(zone_name), public_label = sqlc.arg(public_label),
    accepted_hostname = CASE WHEN state.zone_name = sqlc.arg(zone_name) AND state.public_label = sqlc.arg(public_label)
      THEN state.accepted_hostname ELSE '' END,
    generation = state.generation + 1,
    phase = CASE WHEN intent.deleting THEN 'deleting'
      WHEN sqlc.arg(enabled)::boolean THEN 'validating' ELSE 'disabled' END,
    observed_healthy = false,
    reason = CASE WHEN intent.deleting THEN state.reason ELSE '' END,
    updated_at = clock_timestamp(), updated_by = sqlc.arg(updated_by)
FROM intent
WHERE state.singleton = intent.singleton AND state.generation = sqlc.arg(expected_generation)
RETURNING state.*;

-- name: AdvanceCloudflareRemoteAccess :one
UPDATE aura.cloudflare_remote_access
SET phase = sqlc.arg(phase), zone_id = sqlc.arg(zone_id),
    tunnel_id = sqlc.arg(tunnel_id), tunnel_name = sqlc.arg(tunnel_name),
    public_dns_id = sqlc.arg(public_dns_id), warp_dns_id = sqlc.arg(warp_dns_id),
    otp_idp_id = sqlc.arg(otp_idp_id), public_app_id = sqlc.arg(public_app_id), public_policy_id = sqlc.arg(public_policy_id),
    warp_app_id = sqlc.arg(warp_app_id), warp_policy_id = sqlc.arg(warp_policy_id), warp_posture_id = sqlc.arg(warp_posture_id),
    owned_hostname = sqlc.arg(owned_hostname), accepted_hostname = sqlc.arg(accepted_hostname),
    observed_healthy = sqlc.arg(observed_healthy), reason = sqlc.arg(reason), abandoned = sqlc.arg(abandoned)::jsonb,
    last_reconciled_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE singleton = true AND generation = sqlc.arg(expected_generation)
RETURNING *;

-- name: AbandonCloudflareRemoteAccess :one
UPDATE aura.cloudflare_remote_access
SET zone_id = '', tunnel_id = '', tunnel_name = '', public_dns_id = '', warp_dns_id = '',
    otp_idp_id = '', public_app_id = '', public_policy_id = '',
    warp_app_id = '', warp_policy_id = '', warp_posture_id = '',
    owned_hostname = '', accepted_hostname = '', abandoned = sqlc.arg(abandoned)::jsonb,
    updated_at = clock_timestamp()
WHERE singleton = true AND generation = sqlc.arg(expected_generation)
RETURNING *;

-- name: DismissCloudflareRemoteAccessAbandoned :one
UPDATE aura.cloudflare_remote_access SET abandoned = '[]', updated_at = clock_timestamp()
WHERE singleton = true
RETURNING *;
