CREATE TABLE aura.cloudflare_remote_access (
  singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  enabled boolean NOT NULL DEFAULT false,
  generation bigint NOT NULL DEFAULT 0 CHECK (generation >= 0),
  phase text NOT NULL DEFAULT 'disabled' CHECK (phase IN
    ('disabled','validating','waiting_nameservers','provisioning','connecting','healthy','degraded','error','deleting')),
  account_id text NOT NULL DEFAULT '', zone_id text NOT NULL DEFAULT '', zone_name text NOT NULL DEFAULT '',
  tunnel_id text NOT NULL DEFAULT '', tunnel_name text NOT NULL DEFAULT '',
  public_label text NOT NULL DEFAULT 'aura', warp_label text NOT NULL DEFAULT 'aura-warp',
  public_dns_id text NOT NULL DEFAULT '', warp_dns_id text NOT NULL DEFAULT '',
  otp_idp_id text NOT NULL DEFAULT '', public_app_id text NOT NULL DEFAULT '', public_policy_id text NOT NULL DEFAULT '',
  warp_app_id text NOT NULL DEFAULT '', warp_policy_id text NOT NULL DEFAULT '', warp_posture_id text NOT NULL DEFAULT '',
  last_error text NOT NULL DEFAULT '', observed_healthy boolean NOT NULL DEFAULT false,
  last_reconciled_at timestamptz, updated_at timestamptz NOT NULL DEFAULT clock_timestamp(), updated_by text NOT NULL DEFAULT ''
);
INSERT INTO aura.cloudflare_remote_access (singleton) VALUES (true);
COMMENT ON COLUMN aura.cloudflare_remote_access.warp_posture_id IS
  'Gateway posture ID for the WARP-required hostname; requires this Zero Trust organization, not consumer WARP.';
