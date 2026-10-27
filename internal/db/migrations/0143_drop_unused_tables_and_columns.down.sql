-- Restores the schema 0143 dropped, as it stood at 0142: the original DDL (0020, 0025, 0077)
-- with what later migrations changed (0090's row-level security on asset_events, 0113's
-- cascading benchmark owner). The rows are not restored; 0143 refused to run over any. The
-- three columns come back last in their tables, which nothing reads by position.

ALTER TABLE aura.ingestion_events
    DROP CONSTRAINT ingestion_events_generation_valid,
    ADD COLUMN pipeline_generation bigint NOT NULL DEFAULT 0,
    ADD CONSTRAINT ingestion_events_generation_valid
        CHECK (pipeline_generation >= 0 AND attempt_generation > 0 AND lease_generation >= 0);

ALTER TABLE aura.ingestion_jobs
    DROP CONSTRAINT ingestion_jobs_generation_valid,
    ADD COLUMN pipeline_generation bigint NOT NULL DEFAULT 0,
    ADD CONSTRAINT ingestion_jobs_generation_valid
        CHECK (pipeline_generation >= 0 AND attempt_generation > 0 AND lease_generation >= 0);

ALTER TABLE aura.assets
    ADD COLUMN pipeline_generation bigint NOT NULL DEFAULT 0,
    ADD CONSTRAINT assets_generation_valid CHECK (pipeline_generation >= 0);

CREATE TABLE aura.audit_logs (
    id                bigserial   PRIMARY KEY,
    actor_identity_id uuid        NOT NULL REFERENCES aura.identities(id) ON DELETE RESTRICT,
    action            text        NOT NULL CHECK (btrim(action) <> ''),
    entity_type       text        NOT NULL CHECK (btrim(entity_type) <> ''),
    entity_id         uuid        NOT NULL,
    before            jsonb,
    after             jsonb,
    reason            text        NOT NULL DEFAULT '',
    created_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_logs_entity_created_idx
    ON aura.audit_logs (entity_type, entity_id, created_at);
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.audit_logs TO aura_app;
GRANT SELECT, USAGE ON SEQUENCE aura.audit_logs_id_seq TO aura_app;

CREATE TABLE aura.asset_events (
    asset_id     uuid        NOT NULL REFERENCES aura.assets(id) ON DELETE CASCADE,
    seq          integer     NOT NULL CHECK (seq > 0),
    from_status  text        NOT NULL DEFAULT '',
    to_status    text        NOT NULL,
    reason       text        NOT NULL DEFAULT '',
    detail       jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (asset_id, seq)
);
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.asset_events TO aura_app;
ALTER TABLE aura.asset_events ENABLE ROW LEVEL SECURITY;
CREATE POLICY asset_events_owner_isolation ON aura.asset_events
    USING (EXISTS (
        SELECT 1 FROM aura.assets a
        WHERE a.id = asset_events.asset_id
          AND a.identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid
    ));
COMMENT ON POLICY asset_events_owner_isolation ON aura.asset_events IS
    'Fail-closed owner isolation (migration 0090) naming the parent asset OWNER directly, not merely the parent row: asset_events has no identity column of its own.';
CREATE POLICY asset_events_requires_identity ON aura.asset_events
    AS RESTRICTIVE TO aura_app
    USING (current_setting('app.current_identity', true) IS NOT NULL
           AND current_setting('app.current_identity', true) <> '');
COMMENT ON POLICY asset_events_requires_identity ON aura.asset_events IS
    'Fail-closed floor (migration 0090): AND-combined with every permissive policy, so no later permissive policy can restore visibility to a caller with no app.current_identity. Set it via internal/db.WithIdentityTx / WithIdentityTxRaw / WithCallerIdentityTx.';

CREATE TABLE aura.benchmark_settings_overrides (
    run_id            uuid PRIMARY KEY,
    owner_id          uuid NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE,
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
-- The default privileges grant aura_app every write on a new table; 0077 narrowed these two.
REVOKE ALL ON aura.benchmark_settings_overrides FROM aura_app;
REVOKE ALL ON aura.benchmark_settings_override_rows FROM aura_app;
GRANT SELECT, INSERT, UPDATE ON aura.benchmark_settings_overrides TO aura_app;
GRANT SELECT, INSERT ON aura.benchmark_settings_override_rows TO aura_app;
COMMENT ON TABLE aura.benchmark_settings_overrides IS
    'Bounded audit and recovery journal for the three temporary Qwen portability settings.';
COMMENT ON TABLE aura.benchmark_settings_override_rows IS
    'Exact pre-override settings rows; the key check prevents any secret or unrelated setting capture.';
