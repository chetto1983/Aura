-- Durable document ingestion control plane. Document/chunk payloads live in
-- Neo4j; Postgres stores job state, idempotency, and operator-visible progress.

CREATE TABLE aura.document_ingest_jobs (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id       text        NOT NULL,
    source_kind     text        NOT NULL,
    document_id     text        NOT NULL,
    content_hash    text        NOT NULL,
    original_path   text        NOT NULL,
    file_name       text        NOT NULL,
    mime_type       text        NOT NULL,
    size_bytes      bigint      NOT NULL CHECK (size_bytes >= 0),
    status          text        NOT NULL CHECK (
        status IN (
            'accepted',
            'extracting',
            'searchable',
            'embedding',
            'complete',
            'failed',
            'refused',
            'canceled'
        )
    ),
    sparse_chunks   integer     NOT NULL DEFAULT 0 CHECK (sparse_chunks >= 0),
    embedded_chunks integer     NOT NULL DEFAULT 0 CHECK (embedded_chunks >= 0),
    error           text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    searchable_at   timestamptz,
    completed_at    timestamptz
);

CREATE UNIQUE INDEX document_ingest_jobs_source_document_hash_idx
    ON aura.document_ingest_jobs (source_id, document_id, content_hash);

CREATE INDEX document_ingest_jobs_status_created_idx
    ON aura.document_ingest_jobs (status, created_at DESC);

CREATE INDEX document_ingest_jobs_document_id_idx
    ON aura.document_ingest_jobs (document_id);

GRANT SELECT, INSERT, UPDATE ON aura.document_ingest_jobs TO aura_app;
GRANT ALL                    ON aura.document_ingest_jobs TO aura_migrate;

COMMENT ON TABLE aura.document_ingest_jobs IS
    'Durable document ingestion job state. Neo4j stores document/chunk graph data; Postgres tracks lifecycle and progress.';
