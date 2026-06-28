-- aura.settings is the cockpit-editable runtime override layer for the model-
-- backend knobs (the "Settings" page: rerank/embed/STT/TTS/vision local↔cloud +
-- the OpenRouter key + embed dimensions). Unlike the append-only audit ledgers
-- (0021/0022), this table is MUTABLE: the settings API upserts and deletes rows,
-- so aura_app holds full DML. At daemon boot config.Load reads the environment,
-- then internal/settings overlays these rows on top (DB wins — the operator's UI
-- choice is the effective value); changes take effect on the next restart.
--
-- key is the env-style knob name (e.g. AURA_RERANK_MODEL); value is its string
-- value (ints/bools are parsed by the overlay, mirroring the env readers).
-- is_secret marks values the API must redact in GET responses (e.g. the
-- OpenRouter key) — the value is stored plaintext (same trust boundary as .env)
-- but never echoed back to the browser.

CREATE TABLE aura.settings (
    key        text        PRIMARY KEY,
    value      text        NOT NULL,
    is_secret  boolean     NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- updated_by is the capability-layer principal (principalFrom), NOT the raw
    -- Authula user id (D-03, same choice as the audit tables). NULL for a value
    -- seeded by a migration or CLI rather than the cockpit.
    updated_by text
);

-- Mutable settings table: aura_app needs full DML for the upsert/delete API.
-- aura_migrate owns DDL.
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.settings TO aura_app;
GRANT ALL                          ON aura.settings TO aura_migrate;

COMMENT ON TABLE aura.settings IS
    'Cockpit-editable runtime override layer for model-backend knobs (Settings page). MUTABLE (aura_app full DML). Overlaid onto the environment at boot by internal/settings (DB wins); applied on restart. is_secret rows are redacted in API GET responses.';
