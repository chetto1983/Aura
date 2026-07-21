-- Reverse of 0042: recreate the Phase-42 compaction tables verbatim from 0036/0038/0039 so
-- MigrateSteps(-1) restores the pre-0042 schema (TestMigrateSteps_DownUpReversible). The engine
-- Go code is gone; this DDL exists only to keep the migration ledger reversible. If a full
-- `migrate down` past this point runs, the 0039/0038/0036 down files (IF EXISTS) drop them again.

-- === 0036 group (checkpoints/claims) ===
CREATE TABLE aura.compaction_claims (
    operation_id uuid PRIMARY KEY,
    conversation_id uuid NOT NULL REFERENCES aura.conversations(id) ON DELETE CASCADE,
    branch_id text NOT NULL,
    idempotency_key text NOT NULL,
    captured_watermark_seq integer NOT NULL CHECK (captured_watermark_seq >= 0),
    governance_watermark bigint NOT NULL DEFAULT 0,
    base_active_generation integer NOT NULL CHECK (base_active_generation >= 0),
    priority text NOT NULL CHECK (priority IN ('automatic','manual')),
    state text NOT NULL CHECK (state IN ('pending','completed','superseded')),
    inference_started boolean NOT NULL DEFAULT false,
    owner_id text NOT NULL,
    lease_until timestamptz NOT NULL,
    outcome_checkpoint_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (conversation_id, branch_id, idempotency_key)
);

CREATE TABLE aura.compaction_checkpoints (
    id uuid PRIMARY KEY,
    conversation_id uuid NOT NULL REFERENCES aura.conversations(id) ON DELETE CASCADE,
    branch_id text NOT NULL,
    generation integer NOT NULL CHECK (generation > 0),
    parent_id uuid REFERENCES aura.compaction_checkpoints(id),
    captured_watermark_seq integer NOT NULL,
    summarized_turn_seqs jsonb NOT NULL,
    tail_turn_seqs jsonb NOT NULL,
    protected_turn_seqs jsonb NOT NULL,
    excluded_turn_seqs jsonb NOT NULL,
    manifest_digest text NOT NULL,
    complete_capture_digest text NOT NULL,
    digest_algorithm text NOT NULL,
    digest_version integer NOT NULL,
    structured_summary jsonb NOT NULL,
    schema_version integer NOT NULL,
    prompt_version integer NOT NULL,
    projection_version integer NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    quality_state text NOT NULL,
    rollout_mode text NOT NULL,
    retention_until timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (conversation_id, branch_id, generation),
    CHECK ((generation = 1 AND parent_id IS NULL) OR (generation > 1 AND parent_id IS NOT NULL))
);

ALTER TABLE aura.compaction_claims ADD CONSTRAINT compaction_claim_outcome_fkey
  FOREIGN KEY (outcome_checkpoint_id) REFERENCES aura.compaction_checkpoints(id);

CREATE TABLE aura.compaction_active_pointers (
    conversation_id uuid NOT NULL REFERENCES aura.conversations(id) ON DELETE CASCADE,
    branch_id text NOT NULL,
    generation integer NOT NULL DEFAULT 0 CHECK (generation >= 0),
    checkpoint_id uuid REFERENCES aura.compaction_checkpoints(id),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (conversation_id, branch_id),
    CHECK ((generation = 0 AND checkpoint_id IS NULL) OR (generation > 0 AND checkpoint_id IS NOT NULL))
);

CREATE TABLE aura.compaction_restore_events (
    id uuid PRIMARY KEY, conversation_id uuid NOT NULL, branch_id text NOT NULL,
    old_checkpoint_id uuid, new_checkpoint_id uuid NOT NULL,
    operation_id uuid NOT NULL, actor_id text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX compaction_one_pending_claim_per_branch
  ON aura.compaction_claims(conversation_id, branch_id) WHERE state = 'pending';
CREATE TABLE aura.compaction_quarantine (
    id uuid PRIMARY KEY, checkpoint_id uuid, artifact_kind text NOT NULL,
    reason text NOT NULL, observed_digest text, created_at timestamptz NOT NULL DEFAULT now()
);

CREATE OR REPLACE FUNCTION aura.compaction_checkpoint_parent_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE parent_generation integer; parent_conversation uuid; parent_branch text;
BEGIN
  IF NEW.parent_id IS NULL THEN RETURN NEW; END IF;
  SELECT generation, conversation_id, branch_id INTO parent_generation, parent_conversation, parent_branch
    FROM aura.compaction_checkpoints WHERE id = NEW.parent_id;
  IF parent_generation + 1 <> NEW.generation OR parent_conversation <> NEW.conversation_id OR parent_branch <> NEW.branch_id THEN
    RAISE EXCEPTION 'compaction checkpoint parent must be adjacent on the same branch';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER compaction_checkpoint_parent_guard BEFORE INSERT ON aura.compaction_checkpoints
FOR EACH ROW EXECUTE FUNCTION aura.compaction_checkpoint_parent_guard();

GRANT SELECT, INSERT, UPDATE ON aura.compaction_claims, aura.compaction_active_pointers TO aura_app;
GRANT SELECT, INSERT ON aura.compaction_checkpoints, aura.compaction_restore_events, aura.compaction_quarantine TO aura_app;
GRANT ALL ON aura.compaction_claims, aura.compaction_checkpoints, aura.compaction_active_pointers, aura.compaction_restore_events, aura.compaction_quarantine TO aura_migrate;

-- === 0038 group (memory) ===
CREATE TABLE aura.compaction_memory_candidates (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    owner_id uuid NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE,
    class text NOT NULL,
    purpose text NOT NULL,
    consent_basis text NOT NULL,
    source_manifest_digest text NOT NULL CHECK (length(source_manifest_digest) = 64),
    evidence_digest text NOT NULL CHECK (length(evidence_digest) = 64),
    confidence double precision NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    authority text NOT NULL CHECK (authority IN ('user','tool')),
    sensitivity text NOT NULL,
    region text NOT NULL,
    encryption_class text NOT NULL,
    retention_class text NOT NULL,
    expires_at timestamptz NOT NULL,
    superseded_by uuid REFERENCES aura.compaction_memory_candidates(id),
    revoked_at timestamptz,
    revocation_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, owner_id, class, purpose, source_manifest_digest, evidence_digest)
);

CREATE TABLE aura.compaction_memory_sources (
    candidate_id uuid NOT NULL REFERENCES aura.compaction_memory_candidates(id) ON DELETE CASCADE,
    source_kind text NOT NULL CHECK (source_kind IN ('turn','checkpoint','artifact')),
    source_id text NOT NULL,
    source_digest text NOT NULL CHECK (length(source_digest) = 64),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (candidate_id, source_kind, source_id)
);

CREATE OR REPLACE FUNCTION aura.compaction_memory_source_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'compaction memory sources are immutable'; END $$;
CREATE TRIGGER compaction_memory_source_immutable BEFORE UPDATE OR DELETE ON aura.compaction_memory_sources
FOR EACH ROW EXECUTE FUNCTION aura.compaction_memory_source_immutable();

CREATE TABLE aura.compaction_memories (
    id uuid PRIMARY KEY,
    candidate_id uuid NOT NULL UNIQUE REFERENCES aura.compaction_memory_candidates(id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL,
    owner_id uuid NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE,
    class text NOT NULL,
    purpose text NOT NULL,
    consent_basis text NOT NULL,
    evidence_digest text NOT NULL CHECK (length(evidence_digest) = 64),
    sensitivity text NOT NULL,
    region text NOT NULL,
    expires_at timestamptz NOT NULL,
    superseded_by uuid REFERENCES aura.compaction_memories(id),
    revoked_at timestamptz,
    revocation_reason text,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX compaction_memory_candidate_lifecycle_idx ON aura.compaction_memory_candidates
    (tenant_id, owner_id, region, purpose, expires_at) WHERE revoked_at IS NULL;
CREATE INDEX compaction_memory_retrieval_idx ON aura.compaction_memories
    (tenant_id, owner_id, region, purpose, expires_at) WHERE revoked_at IS NULL;

GRANT SELECT, INSERT, UPDATE ON aura.compaction_memory_candidates, aura.compaction_memories TO aura_app;
GRANT SELECT, INSERT ON aura.compaction_memory_sources TO aura_app;
GRANT ALL ON aura.compaction_memory_candidates, aura.compaction_memory_sources, aura.compaction_memories TO aura_migrate;

-- === 0039 group (rollout) ===
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
