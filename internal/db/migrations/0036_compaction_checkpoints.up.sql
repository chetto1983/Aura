-- Durable, branch-aware semantic-compaction persistence. Activation remains disabled.
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
