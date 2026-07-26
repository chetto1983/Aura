CREATE TABLE aura.benchmark_settings_overrides (
    run_id            uuid PRIMARY KEY,
    owner_id          uuid NOT NULL REFERENCES aura.identities(id) ON DELETE RESTRICT,
    state             text NOT NULL CHECK (
        state IN ('active', 'restoring', 'completed', 'recovered')
    ),
    lease_expires_at  timestamptz NOT NULL,
    heartbeat_at      timestamptz NOT NULL,
    started_at        timestamptz NOT NULL,
    updated_at        timestamptz NOT NULL,
    completed_at      timestamptz,
    recovery_count    integer NOT NULL DEFAULT 0 CHECK (recovery_count BETWEEN 0 AND 1),
    CHECK (
        (state IN ('active', 'restoring') AND completed_at IS NULL)
        OR
        (state IN ('completed', 'recovered') AND completed_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX benchmark_settings_overrides_one_open_idx
    ON aura.benchmark_settings_overrides ((true))
    WHERE state IN ('active', 'restoring');

CREATE TABLE aura.benchmark_settings_override_rows (
    run_id       uuid NOT NULL
        REFERENCES aura.benchmark_settings_overrides(run_id) ON DELETE CASCADE,
    key          text NOT NULL CHECK (key IN (
        'AURA_LLM_PROVIDER', 'AURA_LLM_MODEL', 'AURA_LLM_BASE_URL'
    )),
    existed      boolean NOT NULL,
    value        text,
    is_secret    boolean,
    updated_at   timestamptz,
    updated_by   text,
    PRIMARY KEY (run_id, key),
    CHECK (
        (existed AND value IS NOT NULL AND is_secret IS NOT NULL AND updated_at IS NOT NULL)
        OR
        (NOT existed AND value IS NULL AND is_secret IS NULL
         AND updated_at IS NULL AND updated_by IS NULL)
    )
);

REVOKE ALL ON aura.benchmark_settings_overrides FROM aura_app;
REVOKE ALL ON aura.benchmark_settings_override_rows FROM aura_app;
GRANT SELECT, INSERT, UPDATE ON aura.benchmark_settings_overrides TO aura_app;
GRANT SELECT, INSERT ON aura.benchmark_settings_override_rows TO aura_app;

COMMENT ON TABLE aura.benchmark_settings_overrides IS
    'Bounded audit and recovery journal for the three temporary Qwen portability settings.';
COMMENT ON TABLE aura.benchmark_settings_override_rows IS
    'Exact pre-override settings rows; the key check prevents any secret or unrelated setting capture.';
