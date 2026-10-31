-- 0148: a refusal at the tunnel or Access step has its own reason. Both read token_rejected on the
-- lab VM (2026-10-31) and sent the operator to paste a token that was fine.
ALTER TABLE aura.cloudflare_remote_access
  DROP CONSTRAINT cloudflare_remote_access_reason_check,
  ADD CONSTRAINT cloudflare_remote_access_reason_check CHECK (reason IN ('',
    'token_rejected','tunnel_permission','access_unavailable','no_accounts','account_not_visible',
    'domain_invalid','domain_elsewhere','hostname_taken','not_owned','no_administrator',
    'cloudflare_unreachable','tunnel_down','old_address_present','unknown'));
