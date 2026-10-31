UPDATE aura.cloudflare_remote_access
SET reason = 'token_rejected' WHERE reason IN ('tunnel_permission','access_unavailable');
ALTER TABLE aura.cloudflare_remote_access
  DROP CONSTRAINT cloudflare_remote_access_reason_check,
  ADD CONSTRAINT cloudflare_remote_access_reason_check CHECK (reason IN ('',
    'token_rejected','no_accounts','account_not_visible','domain_invalid','domain_elsewhere',
    'hostname_taken','not_owned','no_administrator','cloudflare_unreachable','tunnel_down',
    'old_address_present','unknown'));
