-- 0147: the remote-access resources remember the account, zone and legacy WARP hostname they
-- belong to. Until now they were read back from the desired columns, so changing the desired
-- account or domain redefined what the existing resources were.
-- docs/superpowers/specs/2026-10-27-remote-access-from-phone-design.md.
ALTER TABLE aura.cloudflare_remote_access
  ADD COLUMN resource_account_id text NOT NULL DEFAULT '',
  ADD COLUMN owned_zone_name text NOT NULL DEFAULT '',
  ADD COLUMN legacy_warp_hostname text NOT NULL DEFAULT '';

-- Only a row that holds a resource has something to remember. The ten columns are the ones
-- SaveCloudflareRemoteAccessDesired reads as deletion intent.
UPDATE aura.cloudflare_remote_access
SET resource_account_id = account_id,
    owned_zone_name = zone_name,
    owned_hostname = CASE WHEN owned_hostname = '' THEN public_label || '.' || zone_name ELSE owned_hostname END
WHERE zone_id <> '' OR tunnel_id <> '' OR public_dns_id <> '' OR warp_dns_id <> ''
   OR otp_idp_id <> '' OR public_app_id <> '' OR public_policy_id <> ''
   OR warp_app_id <> '' OR warp_policy_id <> '' OR warp_posture_id <> '';

UPDATE aura.cloudflare_remote_access
SET legacy_warp_hostname = warp_label || '.' || zone_name
WHERE warp_dns_id <> '' OR warp_app_id <> '' OR warp_policy_id <> '' OR warp_posture_id <> '';
