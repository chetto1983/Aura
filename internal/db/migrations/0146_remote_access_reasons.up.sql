-- 0146: the remote-access row carries a reason code instead of a diagnostic sentence, the
-- hostname its resources were created for, the hostname the operator accepted, and the list of
-- resources a replaced token can no longer reach.
-- docs/superpowers/specs/2026-10-27-remote-access-from-phone-design.md.
ALTER TABLE aura.cloudflare_remote_access
  ADD COLUMN owned_hostname text NOT NULL DEFAULT '',
  ADD COLUMN accepted_hostname text NOT NULL DEFAULT '',
  ADD COLUMN abandoned jsonb NOT NULL DEFAULT '[]',
  ADD COLUMN reason text NOT NULL DEFAULT '' CHECK (reason IN ('',
    'token_rejected','no_accounts','account_not_visible','domain_invalid','domain_elsewhere',
    'hostname_taken','not_owned','no_administrator','cloudflare_unreachable','tunnel_down',
    'old_address_present','unknown'));

UPDATE aura.cloudflare_remote_access
SET owned_hostname = CASE WHEN public_dns_id <> '' THEN public_label || '.' || zone_name ELSE '' END;
UPDATE aura.cloudflare_remote_access
SET accepted_hostname = owned_hostname WHERE observed_healthy;
UPDATE aura.cloudflare_remote_access
SET reason = 'unknown' WHERE last_error <> '';

ALTER TABLE aura.cloudflare_remote_access DROP COLUMN last_error;
