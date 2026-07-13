CREATE TABLE aura.compaction_rollout_states (
    scope_id                 text        PRIMARY KEY CHECK (btrim(scope_id) <> ''),
    version                  bigint      NOT NULL DEFAULT 1 CHECK (version > 0),
    stage                    text        NOT NULL DEFAULT 'disabled' CHECK (stage IN ('disabled', 'shadow', 'canary_1', 'canary_5', 'canary_20', 'canary_50', 'enabled')),
    stage_started_at         timestamptz NOT NULL DEFAULT now(),
    eligible_attempts        bigint      NOT NULL DEFAULT 0 CHECK (eligible_attempts >= 0),
    evaluator_version        text        NOT NULL CHECK (btrim(evaluator_version) <> ''),
    scorer_version           text        NOT NULL CHECK (btrim(scorer_version) <> ''),
    config_version           text        NOT NULL CHECK (btrim(config_version) <> ''),
    corpus_version           text        NOT NULL CHECK (btrim(corpus_version) <> ''),
    stratum_snapshots        jsonb       NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(stratum_snapshots) = 'object'),
    failure_window           jsonb       NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(failure_window) = 'object'),
    latency_window           jsonb       NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(latency_window) = 'object'),
    restore_window           jsonb       NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(restore_window) = 'object'),
    active_config            jsonb       NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(active_config) = 'object'),
    last_known_good_config   jsonb       NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(last_known_good_config) = 'object'),
    last_known_good_policy   jsonb       NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(last_known_good_policy) = 'object'),
    created_at               timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE aura.compaction_rollout_evidence (
    id                    uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    scope_id              text        NOT NULL REFERENCES aura.compaction_rollout_states(scope_id) ON DELETE RESTRICT,
    evidence_digest       text        NOT NULL CHECK (evidence_digest ~ '^[0-9a-f]{64}$'),
    evaluator_version     text        NOT NULL CHECK (btrim(evaluator_version) <> ''),
    scorer_version        text        NOT NULL CHECK (btrim(scorer_version) <> ''),
    config_version        text        NOT NULL CHECK (btrim(config_version) <> ''),
    corpus_version        text        NOT NULL CHECK (btrim(corpus_version) <> ''),
    snapshot              jsonb       NOT NULL CHECK (jsonb_typeof(snapshot) = 'object'),
    created_at            timestamptz NOT NULL DEFAULT now(),
    UNIQUE (scope_id, evidence_digest)
);

CREATE TABLE aura.compaction_rollout_decisions (
    id                    uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    scope_id              text        NOT NULL REFERENCES aura.compaction_rollout_states(scope_id) ON DELETE RESTRICT,
    evidence_id           uuid        NOT NULL REFERENCES aura.compaction_rollout_evidence(id) ON DELETE RESTRICT,
    expected_version      bigint      NOT NULL CHECK (expected_version > 0),
    resulting_version     bigint      NOT NULL CHECK (resulting_version = expected_version + 1),
    decision_kind         text        NOT NULL CHECK (decision_kind IN ('transition', 'rollback')),
    from_stage            text        NOT NULL,
    to_stage              text        NOT NULL,
    reason_code           text        NOT NULL CHECK (reason_code ~ '^[a-z0-9_]+$'),
    created_at            timestamptz NOT NULL DEFAULT now(),
    UNIQUE (scope_id, resulting_version)
);

CREATE OR REPLACE FUNCTION aura.compaction_rollout_ledger_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'compaction rollout evidence and decisions are immutable'; END $$;

CREATE TRIGGER compaction_rollout_evidence_immutable BEFORE UPDATE OR DELETE ON aura.compaction_rollout_evidence
FOR EACH ROW EXECUTE FUNCTION aura.compaction_rollout_ledger_immutable();
CREATE TRIGGER compaction_rollout_decisions_immutable BEFORE UPDATE OR DELETE ON aura.compaction_rollout_decisions
FOR EACH ROW EXECUTE FUNCTION aura.compaction_rollout_ledger_immutable();

CREATE INDEX compaction_rollout_evidence_scope_created_idx
    ON aura.compaction_rollout_evidence (scope_id, created_at, id);
CREATE INDEX compaction_rollout_decisions_scope_created_idx
    ON aura.compaction_rollout_decisions (scope_id, created_at, id);

GRANT SELECT, INSERT, UPDATE ON aura.compaction_rollout_states TO aura_app;
GRANT SELECT, INSERT ON aura.compaction_rollout_evidence TO aura_app;
GRANT SELECT, INSERT ON aura.compaction_rollout_decisions TO aura_app;
GRANT ALL ON aura.compaction_rollout_states TO aura_migrate;
GRANT ALL ON aura.compaction_rollout_evidence TO aura_migrate;
GRANT ALL ON aura.compaction_rollout_decisions TO aura_migrate;

COMMENT ON TABLE aura.compaction_rollout_states IS 'Replica-shared version-CAS effective state for the compaction rollout control plane.';
COMMENT ON TABLE aura.compaction_rollout_evidence IS 'Immutable locale-neutral evaluator evidence; prose and translated presentation text are forbidden.';
COMMENT ON TABLE aura.compaction_rollout_decisions IS 'Immutable transition and rollback ledger keyed by stable reason codes.';
