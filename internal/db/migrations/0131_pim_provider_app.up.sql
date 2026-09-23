-- One OAuth client per managed PIM provider, set by an admin and injected by Aura into every
-- account-create it forwards to the aura-pim-mcp sidecar
-- (docs/superpowers/specs/2026-09-23-pim-provider-apps-design.md). Install-wide like 0130:
-- no RLS, and 0001's default privileges give aura_app its DML.
CREATE TABLE aura.pim_provider_app (
  provider text PRIMARY KEY CHECK (provider IN ('google', 'microsoft365', 'outlook.com')),
  client_id text NOT NULL CHECK (client_id <> ''),
  tenant_id text NOT NULL DEFAULT '',
  client_secret_ciphertext bytea,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by text NOT NULL DEFAULT '',
  CHECK ((provider = 'google') = (client_secret_ciphertext IS NOT NULL)),
  CHECK ((provider = 'google') = (tenant_id = ''))
);
COMMENT ON COLUMN aura.pim_provider_app.client_secret_ciphertext IS
  'AES-256-GCM with the nonce prepended; key HKDF(AURA_AUTHULA_SECRET, "aura-pim-provider-app-key-v1"). Google only.';
