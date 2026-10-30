ALTER TABLE aura.cloudflare_remote_access
  ADD COLUMN last_error text NOT NULL DEFAULT '';
UPDATE aura.cloudflare_remote_access
SET last_error = 'Remote access reconciliation failed; retry or check credentials.' WHERE reason <> '';
ALTER TABLE aura.cloudflare_remote_access
  DROP COLUMN reason, DROP COLUMN abandoned, DROP COLUMN accepted_hostname, DROP COLUMN owned_hostname;
