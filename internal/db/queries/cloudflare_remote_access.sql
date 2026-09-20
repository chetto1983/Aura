-- name: GetCloudflareRemoteAccess :one
SELECT * FROM aura.cloudflare_remote_access WHERE singleton = true;

-- name: SaveCloudflareRemoteAccessDesired :one
UPDATE aura.cloudflare_remote_access
SET enabled = sqlc.arg(enabled), account_id = sqlc.arg(account_id),
    zone_name = sqlc.arg(zone_name), public_label = sqlc.arg(public_label), warp_label = sqlc.arg(warp_label),
    generation = generation + 1,
    phase = CASE WHEN sqlc.arg(enabled)::boolean THEN 'validating' ELSE 'disabled' END,
    observed_healthy = false, last_error = '', updated_at = clock_timestamp(), updated_by = sqlc.arg(updated_by)
WHERE singleton = true AND generation = sqlc.arg(expected_generation)
RETURNING *;

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
