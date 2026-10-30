ALTER TABLE aura.cloudflare_remote_access
  DROP COLUMN legacy_warp_hostname,
  DROP COLUMN owned_zone_name,
  DROP COLUMN resource_account_id;
