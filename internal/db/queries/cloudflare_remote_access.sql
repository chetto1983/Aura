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
    zone_name = sqlc.arg(zone_name), public_label = sqlc.arg(public_label), warp_label = sqlc.arg(warp_label),
    generation = state.generation + 1,
    phase = CASE WHEN intent.deleting THEN 'deleting'
      WHEN sqlc.arg(enabled)::boolean THEN 'validating' ELSE 'disabled' END,
    observed_healthy = false,
    last_error = CASE WHEN intent.deleting THEN state.last_error ELSE '' END,
    updated_at = clock_timestamp(), updated_by = sqlc.arg(updated_by)
FROM intent
WHERE state.singleton = intent.singleton AND state.generation = sqlc.arg(expected_generation)
  AND ((account_id = sqlc.arg(account_id) AND zone_name = sqlc.arg(zone_name)
    AND public_label = sqlc.arg(public_label) AND warp_label = sqlc.arg(warp_label)) OR (
    zone_id = '' AND tunnel_id = '' AND public_dns_id = '' AND warp_dns_id = ''
    AND otp_idp_id = '' AND public_app_id = '' AND public_policy_id = ''
    AND warp_app_id = '' AND warp_policy_id = '' AND warp_posture_id = ''
  ))
RETURNING state.*;

-- name: AdvanceCloudflareRemoteAccess :one
UPDATE aura.cloudflare_remote_access
SET phase = sqlc.arg(phase), zone_id = sqlc.arg(zone_id),
    tunnel_id = sqlc.arg(tunnel_id), tunnel_name = sqlc.arg(tunnel_name),
    public_dns_id = sqlc.arg(public_dns_id), warp_dns_id = sqlc.arg(warp_dns_id),
    otp_idp_id = sqlc.arg(otp_idp_id), public_app_id = sqlc.arg(public_app_id), public_policy_id = sqlc.arg(public_policy_id),
    warp_app_id = sqlc.arg(warp_app_id), warp_policy_id = sqlc.arg(warp_policy_id), warp_posture_id = sqlc.arg(warp_posture_id),
    observed_healthy = sqlc.arg(observed_healthy), last_error = sqlc.arg(last_error),
    last_reconciled_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE singleton = true AND generation = sqlc.arg(expected_generation)
RETURNING *;
