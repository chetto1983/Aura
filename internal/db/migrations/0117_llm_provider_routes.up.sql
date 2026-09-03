-- What each provider was last pointed at.
--
-- aura.settings holds ONE route: AURA_LLM_PROVIDER + AURA_LLM_BASE_URL + AURA_LLM_MODEL,
-- the one the daemon is serving. So the cockpit's Cloud/Local/Ollama buttons had nothing
-- to restore from and filled the two fields with constants compiled into the browser
-- bundle (web/src/settings/modelSettingsDefs.ts). Measured 2026-09-03 on this host: the
-- stored local route was http://host.docker.internal:8084/v1, the constant said
-- http://aura-llm:8084/v1, and a round trip Cloud -> Local overwrote the working URL with
-- a hostname that resolves nowhere from the browser's side of the stack.
--
-- One row per provider, so switching back returns the route that provider actually ran
-- with. It is a memory of past routes, NOT the active one -- aura.settings stays the only
-- answer to "what is serving right now", and nothing reads this table at boot.
--
-- Deployment-global like aura.settings, hence no RLS: the model route is a property of the
-- daemon, not of an identity.

CREATE TABLE aura.llm_provider_routes (
    provider   text        PRIMARY KEY,
    base_url   text        NOT NULL,
    model      text        NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- The capability-layer principal that saved it (the aura.settings attribution), NOT a
    -- raw auth-provider user id. NULL for a row seeded by this migration.
    updated_by text
);

GRANT SELECT, INSERT, UPDATE, DELETE ON aura.llm_provider_routes TO aura_app;
GRANT ALL                            ON aura.llm_provider_routes TO aura_migrate;

-- Seed the route the operator is on right now. Without it the first switch away and back
-- would still land on a constant, which is the bug this table exists to end.
INSERT INTO aura.llm_provider_routes (provider, base_url, model)
SELECT btrim(provider.value), btrim(base_url.value), btrim(model.value)
FROM aura.settings AS provider
JOIN aura.settings AS base_url ON base_url.key = 'AURA_LLM_BASE_URL'
JOIN aura.settings AS model    ON model.key    = 'AURA_LLM_MODEL'
WHERE provider.key = 'AURA_LLM_PROVIDER'
  AND btrim(provider.value) <> ''
  AND btrim(base_url.value) <> ''
  AND btrim(model.value) <> ''
ON CONFLICT (provider) DO NOTHING;

COMMENT ON TABLE aura.llm_provider_routes IS
    'Last base URL + model saved for each primary-LLM provider, so the cockpit restores a real route when the operator switches provider instead of a hard-coded placeholder. Not read at boot: aura.settings owns the active route.';
