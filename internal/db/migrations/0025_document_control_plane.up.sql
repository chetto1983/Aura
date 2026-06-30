-- Append-only document control plane: logical documents, versions, object
-- ledger rows, durable ingestion jobs, lifecycle metadata, and audit trails.

CREATE TABLE aura.documents (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id       uuid        NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE,
    scope             text        NOT NULL CHECK (scope IN ('thread', 'library')),
    title             text        NOT NULL CHECK (btrim(title) <> ''),
    tags              text[]      NOT NULL DEFAULT '{}'::text[],
    metadata          jsonb       NOT NULL DEFAULT '{}'::jsonb,
    active_version_id uuid,
    status            text        NOT NULL CHECK (status IN (
        'draft', 'queued', 'processing', 'ready', 'failed',
        'deleting', 'deleted', 'archived'
    )),
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    deleted_at        timestamptz
);

CREATE TABLE aura.document_tags (
    document_id uuid        NOT NULL REFERENCES aura.documents(id) ON DELETE CASCADE,
    tag         text        NOT NULL CHECK (btrim(tag) <> ''),
    created_by  uuid        REFERENCES aura.identities(id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (document_id, tag)
);

CREATE TABLE aura.storage_objects (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id     uuid        NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE,
    document_id     uuid        REFERENCES aura.documents(id) ON DELETE SET NULL,
    version_id      uuid,
    asset_id        uuid        REFERENCES aura.assets(id) ON DELETE SET NULL,
    bucket          text        NOT NULL CHECK (btrim(bucket) <> ''),
    object_key      text        NOT NULL CHECK (btrim(object_key) <> ''),
    kind            text        NOT NULL CHECK (kind IN (
        'raw', 'extracted_text', 'chunks', 'preview', 'temp', 'failed_artifact'
    )),
    sha1            text        NOT NULL DEFAULT '',
    sha256          text        NOT NULL DEFAULT '',
    etag            text        NOT NULL DEFAULT '',
    size_bytes      bigint      NOT NULL CHECK (size_bytes >= 0),
    content_type    text        NOT NULL DEFAULT 'application/octet-stream',
    retention_class text        NOT NULL DEFAULT 'standard',
    created_at      timestamptz NOT NULL DEFAULT now(),
    deleted_at      timestamptz,
    UNIQUE (bucket, object_key)
);

CREATE TABLE aura.document_versions (
    id                    uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id           uuid        NOT NULL REFERENCES aura.documents(id) ON DELETE CASCADE,
    asset_id              uuid        REFERENCES aura.assets(id) ON DELETE SET NULL,
    version_number        integer     NOT NULL CHECK (version_number > 0),
    status                text        NOT NULL CHECK (status IN (
        'uploaded', 'hash_calculated', 'stored', 'queued', 'parsing',
        'parsed', 'chunking', 'chunked', 'embedding', 'embedded',
        'indexed', 'ready', 'failed', 'deleting', 'deleted', 'archived'
    )),
    sha1                  text        NOT NULL DEFAULT '',
    sha256                text        NOT NULL CHECK (sha256 <> ''),
    content_type          text        NOT NULL DEFAULT 'application/octet-stream',
    size_bytes            bigint      NOT NULL CHECK (size_bytes >= 0),
    storage_object_id     uuid        NOT NULL REFERENCES aura.storage_objects(id),
    chunking_config_hash  text        NOT NULL DEFAULT '',
    pipeline_config_hash  text        NOT NULL DEFAULT '',
    ready_at              timestamptz,
    activated_at          timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    deleted_at            timestamptz,
    error_code            text        NOT NULL DEFAULT '',
    error_message         text        NOT NULL DEFAULT '',
    UNIQUE (document_id, version_number),
    UNIQUE (document_id, sha256, pipeline_config_hash)
);

ALTER TABLE aura.documents
    ADD CONSTRAINT documents_active_version_id_fkey
    FOREIGN KEY (active_version_id)
    REFERENCES aura.document_versions(id)
    DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE aura.storage_objects
    ADD CONSTRAINT storage_objects_version_id_fkey
    FOREIGN KEY (version_id)
    REFERENCES aura.document_versions(id)
    ON DELETE SET NULL
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE aura.ingestion_jobs (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    job_type        text        NOT NULL CHECK (btrim(job_type) <> ''),
    document_id     uuid        REFERENCES aura.documents(id) ON DELETE CASCADE,
    version_id      uuid        REFERENCES aura.document_versions(id) ON DELETE CASCADE,
    status          text        NOT NULL CHECK (status IN (
        'queued', 'running', 'succeeded', 'failed', 'dead_letter', 'canceled'
    )),
    idempotency_key text        NOT NULL CHECK (btrim(idempotency_key) <> ''),
    stage           text        NOT NULL DEFAULT '',
    attempt_count   integer     NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    max_attempts    integer     NOT NULL DEFAULT 5 CHECK (max_attempts > 0),
    locked_by       text,
    locked_until    timestamptz,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    payload         jsonb       NOT NULL DEFAULT '{}'::jsonb,
    error_code      text        NOT NULL DEFAULT '',
    error_message   text        NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    completed_at    timestamptz,
    UNIQUE (job_type, idempotency_key)
);

CREATE TABLE aura.ingestion_events (
    id          bigserial   PRIMARY KEY,
    entity_type text        NOT NULL CHECK (btrim(entity_type) <> ''),
    entity_id   uuid        NOT NULL,
    job_id      uuid        REFERENCES aura.ingestion_jobs(id) ON DELETE SET NULL,
    from_status text,
    to_status   text,
    event_type  text        NOT NULL CHECK (btrim(event_type) <> ''),
    message     text        NOT NULL DEFAULT '',
    detail      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    trace_id    text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE aura.document_chunks (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id uuid        NOT NULL REFERENCES aura.documents(id) ON DELETE CASCADE,
    version_id  uuid        NOT NULL REFERENCES aura.document_versions(id) ON DELETE CASCADE,
    chunk_index integer     NOT NULL CHECK (chunk_index >= 0),
    chunk_hash  text        NOT NULL CHECK (chunk_hash <> ''),
    text        text        NOT NULL,
    locator     jsonb       NOT NULL DEFAULT '{}'::jsonb,
    active      boolean     NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz,
    UNIQUE (version_id, chunk_index),
    UNIQUE (version_id, chunk_hash)
);

CREATE TABLE aura.document_embeddings (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id       uuid        NOT NULL REFERENCES aura.documents(id) ON DELETE CASCADE,
    version_id        uuid        NOT NULL REFERENCES aura.document_versions(id) ON DELETE CASCADE,
    chunk_id          uuid        NOT NULL REFERENCES aura.document_chunks(id) ON DELETE CASCADE,
    embedding_model   text        NOT NULL CHECK (btrim(embedding_model) <> ''),
    embedding_version text        NOT NULL CHECK (btrim(embedding_version) <> ''),
    embedding_dim     integer     NOT NULL CHECK (embedding_dim > 0),
    vector_namespace  text        NOT NULL CHECK (btrim(vector_namespace) <> ''),
    vector_id         text        NOT NULL CHECK (btrim(vector_id) <> ''),
    active            boolean     NOT NULL DEFAULT false,
    created_at        timestamptz NOT NULL DEFAULT now(),
    deleted_at        timestamptz,
    UNIQUE (vector_namespace, vector_id),
    UNIQUE (chunk_id, embedding_model, embedding_version)
);

CREATE TABLE aura.delete_jobs (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id     uuid        REFERENCES aura.documents(id) ON DELETE SET NULL,
    version_id      uuid        REFERENCES aura.document_versions(id) ON DELETE SET NULL,
    scope           text        NOT NULL CHECK (scope IN ('soft', 'hard', 'archive')),
    status          text        NOT NULL CHECK (status IN (
        'queued', 'running', 'succeeded', 'failed', 'dead_letter', 'canceled'
    )),
    steps           jsonb       NOT NULL DEFAULT '{}'::jsonb,
    attempt_count   integer     NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    max_attempts    integer     NOT NULL DEFAULT 5 CHECK (max_attempts > 0),
    locked_by       text,
    locked_until    timestamptz,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    completed_at    timestamptz
);

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

CREATE INDEX documents_identity_created_idx
    ON aura.documents (identity_id, created_at DESC);

CREATE INDEX documents_identity_scope_created_idx
    ON aura.documents (identity_id, scope, created_at DESC);

CREATE INDEX documents_identity_title_idx
    ON aura.documents (identity_id, title);

CREATE INDEX documents_tags_gin_idx
    ON aura.documents USING GIN (tags);

CREATE INDEX document_tags_tag_document_idx
    ON aura.document_tags (tag, document_id);

CREATE INDEX document_versions_document_status_idx
    ON aura.document_versions (document_id, status, created_at DESC);

CREATE INDEX document_versions_status_created_idx
    ON aura.document_versions (status, created_at DESC);

CREATE INDEX document_versions_sha256_idx
    ON aura.document_versions (sha256);

CREATE INDEX document_versions_sha1_idx
    ON aura.document_versions (sha1)
    WHERE sha1 <> '';

CREATE INDEX storage_objects_identity_document_version_idx
    ON aura.storage_objects (identity_id, document_id, version_id);

CREATE INDEX storage_objects_kind_created_idx
    ON aura.storage_objects (kind, created_at DESC);

CREATE INDEX storage_objects_deleted_idx
    ON aura.storage_objects (deleted_at)
    WHERE deleted_at IS NOT NULL;

CREATE INDEX ingestion_jobs_claim_idx
    ON aura.ingestion_jobs (status, next_attempt_at, created_at);

CREATE INDEX ingestion_jobs_locked_until_idx
    ON aura.ingestion_jobs (locked_until)
    WHERE locked_until IS NOT NULL;

CREATE INDEX ingestion_events_entity_created_idx
    ON aura.ingestion_events (entity_type, entity_id, created_at);

CREATE INDEX ingestion_events_job_created_idx
    ON aura.ingestion_events (job_id, created_at)
    WHERE job_id IS NOT NULL;

CREATE INDEX document_chunks_document_active_idx
    ON aura.document_chunks (document_id, active);

CREATE INDEX document_embeddings_document_version_active_idx
    ON aura.document_embeddings (document_id, version_id, active);

CREATE INDEX delete_jobs_status_next_attempt_idx
    ON aura.delete_jobs (status, next_attempt_at, created_at);

CREATE INDEX audit_logs_entity_created_idx
    ON aura.audit_logs (entity_type, entity_id, created_at);

GRANT SELECT, INSERT, UPDATE, DELETE ON aura.documents TO aura_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.document_tags TO aura_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.document_versions TO aura_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.storage_objects TO aura_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.ingestion_jobs TO aura_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.ingestion_events TO aura_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.document_chunks TO aura_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.document_embeddings TO aura_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.delete_jobs TO aura_app;
GRANT SELECT, INSERT ON aura.audit_logs TO aura_app;

GRANT ALL ON aura.documents TO aura_migrate;
GRANT ALL ON aura.document_tags TO aura_migrate;
GRANT ALL ON aura.document_versions TO aura_migrate;
GRANT ALL ON aura.storage_objects TO aura_migrate;
GRANT ALL ON aura.ingestion_jobs TO aura_migrate;
GRANT ALL ON aura.ingestion_events TO aura_migrate;
GRANT ALL ON aura.document_chunks TO aura_migrate;
GRANT ALL ON aura.document_embeddings TO aura_migrate;
GRANT ALL ON aura.delete_jobs TO aura_migrate;
GRANT ALL ON aura.audit_logs TO aura_migrate;

COMMENT ON TABLE aura.documents IS
    'Stable logical documents across immutable content versions; tags are operator metadata for library search.';
COMMENT ON TABLE aura.document_versions IS
    'Immutable document content versions and processing lifecycle state.';
COMMENT ON TABLE aura.storage_objects IS
    'Ledger of Garage/S3 objects owned by assets, documents, versions, and derived artifacts.';
COMMENT ON TABLE aura.ingestion_jobs IS
    'Durable document ingestion and cleanup queue with idempotency keys and worker leases.';
COMMENT ON TABLE aura.ingestion_events IS
    'Operator-visible progress and lifecycle events for documents, versions, jobs, and assets.';
