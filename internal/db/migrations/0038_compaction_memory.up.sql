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
