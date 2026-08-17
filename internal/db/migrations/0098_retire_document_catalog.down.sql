-- Reverse of 0098: put the document catalog's SHAPE back, so MigrateSteps(-1) lands on a
-- schema golang-migrate recognises and the re-up applies cleanly
-- (TestMigrateSteps_DownUpReversible).
--
-- The ROWS do not come back, and no down migration could bring them back. What they held was
-- Aura's own second naming of objects that live in the bucket -- a catalog uuid, a version
-- number, a search id minted from ("web", sha256(asset id)) -- and that naming was never the
-- one the index uses, which is why 0098 removed it. Recreating the rows would recreate the
-- defect.
--
-- The DDL below is the shape this schema actually carried at 0097, taken from the migrated
-- database rather than reassembled by hand from the migrations that built it up across
-- 0002..0093. Those files stay on disk (migration history is append-only) and their own down
-- files drop these tables again on a full walk down.

-- The guard 0098 dropped along with the table it protected.
CREATE OR REPLACE FUNCTION aura.document_identity_immutable() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF NEW.identity_id IS DISTINCT FROM OLD.identity_id
       OR NEW.source_kind IS DISTINCT FROM OLD.source_kind
       OR NEW.source_key IS DISTINCT FROM OLD.source_key
       OR NEW.search_document_id IS DISTINCT FROM OLD.search_document_id THEN
        RAISE EXCEPTION 'document identity and source fields are immutable' USING ERRCODE = '23514';
    END IF;
    IF NEW.pipeline_generation < OLD.pipeline_generation THEN
        RAISE EXCEPTION 'document pipeline generation cannot decrease' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END
$$;

CREATE TABLE aura.delete_jobs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    document_id uuid,
    version_id uuid,
    scope text NOT NULL,
    status text NOT NULL,
    steps jsonb DEFAULT '{}'::jsonb NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    max_attempts integer DEFAULT 5 NOT NULL,
    locked_by text,
    locked_until timestamp with time zone,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    identity_id uuid NOT NULL,
    idempotency_key text NOT NULL,
    delete_generation bigint DEFAULT 1 NOT NULL,
    lease_generation bigint DEFAULT 0 NOT NULL,
    error_code text DEFAULT ''::text NOT NULL,
    error_message text DEFAULT ''::text NOT NULL,
    CONSTRAINT delete_jobs_attempt_count_check CHECK ((attempt_count >= 0)),
    CONSTRAINT delete_jobs_generation_valid CHECK (((delete_generation > 0) AND (lease_generation >= 0))),
    CONSTRAINT delete_jobs_max_attempts_check CHECK ((max_attempts > 0)),
    CONSTRAINT delete_jobs_scope_check CHECK ((scope = ANY (ARRAY['soft'::text, 'hard'::text, 'archive'::text]))),
    CONSTRAINT delete_jobs_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'running'::text, 'succeeded'::text, 'failed'::text, 'dead_letter'::text, 'canceled'::text])))
);

CREATE TABLE aura.document_chunks (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    document_id uuid NOT NULL,
    version_id uuid NOT NULL,
    chunk_index integer NOT NULL,
    chunk_hash text NOT NULL,
    text text NOT NULL,
    locator jsonb DEFAULT '{}'::jsonb NOT NULL,
    active boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    identity_id uuid NOT NULL,
    search_document_id text NOT NULL,
    version_number integer NOT NULL,
    original_sha256 text NOT NULL,
    pipeline_generation bigint DEFAULT 0 NOT NULL,
    ordinal integer NOT NULL,
    normalized_text_sha256 text NOT NULL,
    self_ref text DEFAULT ''::text NOT NULL,
    heading_path text[] DEFAULT '{}'::text[] NOT NULL,
    captions text[] DEFAULT '{}'::text[] NOT NULL,
    page_number integer,
    bounding_box jsonb,
    char_start integer,
    char_end integer,
    sheet_name text,
    table_name text,
    row_number integer,
    column_number integer,
    cell_ref text,
    CONSTRAINT document_chunks_chunk_hash_check CHECK ((chunk_hash <> ''::text)),
    CONSTRAINT document_chunks_chunk_index_check CHECK ((chunk_index >= 0)),
    CONSTRAINT document_chunks_locator_valid CHECK (((ordinal >= 0) AND (version_number > 0) AND (pipeline_generation >= 0) AND ((page_number IS NULL) OR (page_number > 0)) AND ((char_start IS NULL) OR (char_start >= 0)) AND ((char_end IS NULL) OR (char_end >= COALESCE(char_start, 0)))))
);

CREATE TABLE aura.document_embeddings (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    document_id uuid NOT NULL,
    version_id uuid NOT NULL,
    chunk_id uuid NOT NULL,
    embedding_model text NOT NULL,
    embedding_version text NOT NULL,
    embedding_dim integer NOT NULL,
    vector_namespace text NOT NULL,
    vector_id text NOT NULL,
    active boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    identity_id uuid NOT NULL,
    pipeline_generation bigint DEFAULT 0 NOT NULL,
    embedding_fingerprint text NOT NULL,
    projection_status text DEFAULT 'pending'::text NOT NULL,
    projected_at timestamp with time zone,
    CONSTRAINT document_embeddings_embedding_dim_check CHECK ((embedding_dim > 0)),
    CONSTRAINT document_embeddings_embedding_model_check CHECK ((btrim(embedding_model) <> ''::text)),
    CONSTRAINT document_embeddings_embedding_version_check CHECK ((btrim(embedding_version) <> ''::text)),
    CONSTRAINT document_embeddings_generation_valid CHECK ((pipeline_generation >= 0)),
    CONSTRAINT document_embeddings_projection_status_check CHECK ((projection_status = ANY (ARRAY['pending'::text, 'projecting'::text, 'active'::text, 'tombstoned'::text, 'failed'::text]))),
    CONSTRAINT document_embeddings_vector_id_check CHECK ((btrim(vector_id) <> ''::text)),
    CONSTRAINT document_embeddings_vector_namespace_check CHECK ((btrim(vector_namespace) <> ''::text))
);

CREATE TABLE aura.document_ingest_jobs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    source_id text NOT NULL,
    source_kind text NOT NULL,
    document_id text NOT NULL,
    content_hash text NOT NULL,
    original_path text NOT NULL,
    file_name text NOT NULL,
    mime_type text NOT NULL,
    size_bytes bigint NOT NULL,
    status text NOT NULL,
    sparse_chunks integer DEFAULT 0 NOT NULL,
    embedded_chunks integer DEFAULT 0 NOT NULL,
    error text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    searchable_at timestamp with time zone,
    completed_at timestamp with time zone,
    identity_id uuid NOT NULL,
    asset_id uuid,
    catalog_document_id uuid,
    version_id uuid,
    pipeline_generation bigint DEFAULT 0 NOT NULL,
    CONSTRAINT document_ingest_jobs_embedded_chunks_check CHECK ((embedded_chunks >= 0)),
    CONSTRAINT document_ingest_jobs_generation_valid CHECK ((pipeline_generation >= 0)),
    CONSTRAINT document_ingest_jobs_size_bytes_check CHECK ((size_bytes >= 0)),
    CONSTRAINT document_ingest_jobs_sparse_chunks_check CHECK ((sparse_chunks >= 0)),
    CONSTRAINT document_ingest_jobs_status_check CHECK ((status = ANY (ARRAY['accepted'::text, 'extracting'::text, 'searchable'::text, 'embedding'::text, 'complete'::text, 'failed'::text, 'refused'::text, 'canceled'::text])))
);

COMMENT ON TABLE aura.document_ingest_jobs IS 'Durable document ingestion job state: lifecycle, idempotency and operator-visible progress. Postgres is the only store — a document is one catalog row plus a digest, and the file itself lives in the object store.';

CREATE TABLE aura.document_pipeline_quarantine (
    id bigint NOT NULL,
    source_table text NOT NULL,
    source_pk text NOT NULL,
    reason text NOT NULL,
    row_data jsonb NOT NULL,
    quarantined_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT document_pipeline_quarantine_reason_check CHECK ((btrim(reason) <> ''::text)),
    CONSTRAINT document_pipeline_quarantine_source_table_check CHECK ((source_table = ANY (ARRAY['document_ingest_jobs'::text, 'ingestion_jobs'::text, 'ingestion_events'::text, 'delete_jobs'::text])))
);

COMMENT ON TABLE aura.document_pipeline_quarantine IS 'Migrate-role-only preservation of legacy control-plane rows whose owner cannot be proven through typed foreign keys.';

CREATE SEQUENCE aura.document_pipeline_quarantine_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE aura.document_pipeline_quarantine_id_seq OWNED BY aura.document_pipeline_quarantine.id;

CREATE TABLE aura.document_pipeline_stages (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    identity_id uuid NOT NULL,
    document_id uuid NOT NULL,
    version_id uuid NOT NULL,
    stage text NOT NULL,
    input_fingerprint text NOT NULL,
    producer_version text NOT NULL,
    pipeline_generation bigint NOT NULL,
    artifact_storage_object_id uuid,
    artifact_object_key text DEFAULT ''::text NOT NULL,
    artifact_sha256 text DEFAULT ''::text NOT NULL,
    artifact_size_bytes bigint DEFAULT 0 NOT NULL,
    status text NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    max_attempts integer DEFAULT 5 NOT NULL,
    lease_generation bigint DEFAULT 0 NOT NULL,
    locked_by text,
    locked_until timestamp with time zone,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    error_class text DEFAULT ''::text NOT NULL,
    error_code text DEFAULT ''::text NOT NULL,
    error_message text DEFAULT ''::text NOT NULL,
    diagnostic_state jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    CONSTRAINT document_pipeline_stages_artifact_size_bytes_check CHECK ((artifact_size_bytes >= 0)),
    CONSTRAINT document_pipeline_stages_attempt_count_check CHECK ((attempt_count >= 0)),
    CONSTRAINT document_pipeline_stages_input_fingerprint_check CHECK ((btrim(input_fingerprint) <> ''::text)),
    CONSTRAINT document_pipeline_stages_lease_generation_check CHECK ((lease_generation >= 0)),
    CONSTRAINT document_pipeline_stages_max_attempts_check CHECK ((max_attempts > 0)),
    CONSTRAINT document_pipeline_stages_pipeline_generation_check CHECK ((pipeline_generation >= 0)),
    CONSTRAINT document_pipeline_stages_producer_version_check CHECK ((btrim(producer_version) <> ''::text)),
    CONSTRAINT document_pipeline_stages_stage_check CHECK ((stage = ANY (ARRAY['identify'::text, 'convert'::text, 'chunk'::text, 'embed'::text, 'project'::text, 'activate'::text, 'card'::text]))),
    CONSTRAINT document_pipeline_stages_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'running'::text, 'succeeded'::text, 'failed'::text, 'dead_letter'::text])))
);

COMMENT ON TABLE aura.document_pipeline_stages IS 'Owner-scoped immutable-result stage ledger for identify, convert, chunk, embed, project, activate, and card derivation.';

CREATE TABLE aura.document_tags (
    document_id uuid NOT NULL,
    tag text NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT document_tags_tag_check CHECK ((btrim(tag) <> ''::text))
);

CREATE TABLE aura.document_versions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    document_id uuid NOT NULL,
    asset_id uuid,
    version_number integer NOT NULL,
    status text NOT NULL,
    sha1 text DEFAULT ''::text NOT NULL,
    sha256 text NOT NULL,
    content_type text DEFAULT 'application/octet-stream'::text NOT NULL,
    size_bytes bigint NOT NULL,
    storage_object_id uuid NOT NULL,
    chunking_config_hash text DEFAULT ''::text NOT NULL,
    pipeline_config_hash text DEFAULT ''::text NOT NULL,
    ready_at timestamp with time zone,
    activated_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    error_code text DEFAULT ''::text NOT NULL,
    error_message text DEFAULT ''::text NOT NULL,
    identity_id uuid NOT NULL,
    search_document_id text NOT NULL,
    pipeline_generation bigint DEFAULT 0 NOT NULL,
    CONSTRAINT document_versions_generation_valid CHECK ((pipeline_generation >= 0)),
    CONSTRAINT document_versions_sha256_check CHECK ((sha256 <> ''::text)),
    CONSTRAINT document_versions_size_bytes_check CHECK ((size_bytes >= 0)),
    CONSTRAINT document_versions_status_check CHECK ((status = ANY (ARRAY['uploaded'::text, 'hash_calculated'::text, 'stored'::text, 'queued'::text, 'parsing'::text, 'parsed'::text, 'chunking'::text, 'chunked'::text, 'embedding'::text, 'embedded'::text, 'indexed'::text, 'ready'::text, 'failed'::text, 'deleting'::text, 'deleted'::text, 'archived'::text]))),
    CONSTRAINT document_versions_version_number_check CHECK ((version_number > 0))
);

COMMENT ON TABLE aura.document_versions IS 'Immutable document content versions and processing lifecycle state.';

CREATE TABLE aura.documents (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    identity_id uuid NOT NULL,
    scope text NOT NULL,
    title text NOT NULL,
    tags text[] DEFAULT '{}'::text[] NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    active_version_id uuid,
    status text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    digest text DEFAULT ''::text NOT NULL,
    card text DEFAULT ''::text NOT NULL,
    digest_tsv tsvector GENERATED ALWAYS AS ((((setweight(to_tsvector('simple'::regconfig, aura.searchable_text(title)), 'A'::"char") || setweight(to_tsvector('simple'::regconfig, aura.searchable_text(aura.text_array_words(COALESCE(tags, '{}'::text[])))), 'B'::"char")) || setweight(to_tsvector('simple'::regconfig, aura.searchable_text(digest)), 'C'::"char")) || setweight(to_tsvector('simple'::regconfig, aura.searchable_text(card)), 'D'::"char"))) STORED,
    source_kind text NOT NULL,
    source_key text NOT NULL,
    search_document_id text NOT NULL,
    pipeline_generation bigint DEFAULT 0 NOT NULL,
    error_code text DEFAULT ''::text NOT NULL,
    error_message text DEFAULT ''::text NOT NULL,
    CONSTRAINT documents_generation_valid CHECK (((pipeline_generation >= 0) AND ((status <> 'ready'::text) OR (pipeline_generation > 0)))),
    CONSTRAINT documents_scope_check CHECK ((scope = ANY (ARRAY['thread'::text, 'library'::text]))),
    CONSTRAINT documents_search_id_nonempty CHECK ((btrim(search_document_id) <> ''::text)),
    CONSTRAINT documents_source_key_nonempty CHECK ((btrim(source_key) <> ''::text)),
    CONSTRAINT documents_source_kind_nonempty CHECK ((btrim(source_kind) <> ''::text)),
    CONSTRAINT documents_status_check CHECK ((status = ANY (ARRAY['accepted'::text, 'stored'::text, 'queued'::text, 'converting'::text, 'chunking'::text, 'embedding'::text, 'projecting'::text, 'ready'::text, 'failed'::text, 'dead_letter'::text, 'deleting'::text, 'deleted'::text]))),
    CONSTRAINT documents_title_check CHECK ((btrim(title) <> ''::text))
);

COMMENT ON TABLE aura.documents IS 'Stable logical documents across immutable content versions; tags are operator metadata for library search.';

COMMENT ON COLUMN aura.documents.digest IS 'What this document IS, in enough words to pick it out of a library: sheets and their column headers with row counts for tabular content, the heading outline for prose. Not the content — document_open hands over the file for that.';

COMMENT ON COLUMN aura.documents.card IS 'Machine-written at ingest from the file itself, with no LLM: sheet names, column headers with their frequent values, sampled rows, or the metadata and page count of a format whose text cannot be read without opening it. A proxy for the file, never the file. aura.documents.digest holds the agent''s own note instead.';

CREATE TABLE aura.storage_objects (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    identity_id uuid NOT NULL,
    document_id uuid,
    version_id uuid,
    asset_id uuid,
    bucket text NOT NULL,
    object_key text NOT NULL,
    kind text NOT NULL,
    sha1 text DEFAULT ''::text NOT NULL,
    sha256 text DEFAULT ''::text NOT NULL,
    etag text DEFAULT ''::text NOT NULL,
    size_bytes bigint NOT NULL,
    content_type text DEFAULT 'application/octet-stream'::text NOT NULL,
    retention_class text DEFAULT 'standard'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    status text DEFAULT 'live'::text NOT NULL,
    pipeline_generation bigint DEFAULT 0 NOT NULL,
    deletion_generation bigint DEFAULT 0 NOT NULL,
    deletion_verified_at timestamp with time zone,
    CONSTRAINT storage_objects_bucket_check CHECK ((btrim(bucket) <> ''::text)),
    CONSTRAINT storage_objects_generation_valid CHECK (((pipeline_generation >= 0) AND (deletion_generation >= 0))),
    CONSTRAINT storage_objects_kind_check CHECK ((kind = ANY (ARRAY['raw'::text, 'converted_document'::text, 'extracted_text'::text, 'chunks'::text, 'chunk_manifest'::text, 'embedding_manifest'::text, 'preview'::text, 'temp'::text, 'failed_artifact'::text]))),
    CONSTRAINT storage_objects_object_key_check CHECK ((btrim(object_key) <> ''::text)),
    CONSTRAINT storage_objects_size_bytes_check CHECK ((size_bytes >= 0)),
    CONSTRAINT storage_objects_status_check CHECK ((status = ANY (ARRAY['live'::text, 'delete_pending'::text, 'object_deleted'::text])))
);

COMMENT ON TABLE aura.storage_objects IS 'Ledger of Garage/S3 objects owned by assets, documents, versions, and derived artifacts.';

ALTER TABLE ONLY aura.document_pipeline_quarantine ALTER COLUMN id SET DEFAULT nextval('aura.document_pipeline_quarantine_id_seq'::regclass);

ALTER TABLE ONLY aura.delete_jobs
    ADD CONSTRAINT delete_jobs_identity_idempotency_unique UNIQUE (identity_id, idempotency_key);

ALTER TABLE ONLY aura.delete_jobs
    ADD CONSTRAINT delete_jobs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY aura.document_chunks
    ADD CONSTRAINT document_chunks_id_identity_unique UNIQUE (id, identity_id);

ALTER TABLE ONLY aura.document_chunks
    ADD CONSTRAINT document_chunks_pkey PRIMARY KEY (id);

ALTER TABLE ONLY aura.document_chunks
    ADD CONSTRAINT document_chunks_version_id_chunk_index_key UNIQUE (version_id, chunk_index);

ALTER TABLE ONLY aura.document_chunks
    ADD CONSTRAINT document_chunks_version_ordinal_unique UNIQUE (identity_id, version_id, ordinal);

ALTER TABLE ONLY aura.document_embeddings
    ADD CONSTRAINT document_embeddings_pkey PRIMARY KEY (id);

ALTER TABLE ONLY aura.document_embeddings
    ADD CONSTRAINT document_embeddings_projection_unique UNIQUE (identity_id, chunk_id, pipeline_generation, embedding_fingerprint);

ALTER TABLE ONLY aura.document_embeddings
    ADD CONSTRAINT document_embeddings_vector_namespace_vector_id_key UNIQUE (vector_namespace, vector_id);

ALTER TABLE ONLY aura.document_ingest_jobs
    ADD CONSTRAINT document_ingest_jobs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY aura.document_pipeline_quarantine
    ADD CONSTRAINT document_pipeline_quarantine_pkey PRIMARY KEY (id);

ALTER TABLE ONLY aura.document_pipeline_quarantine
    ADD CONSTRAINT document_pipeline_quarantine_source_table_source_pk_key UNIQUE (source_table, source_pk);

ALTER TABLE ONLY aura.document_pipeline_stages
    ADD CONSTRAINT document_pipeline_stages_identity_id_version_id_stage_input_key UNIQUE (identity_id, version_id, stage, input_fingerprint);

ALTER TABLE ONLY aura.document_pipeline_stages
    ADD CONSTRAINT document_pipeline_stages_pkey PRIMARY KEY (id);

ALTER TABLE ONLY aura.document_tags
    ADD CONSTRAINT document_tags_pkey PRIMARY KEY (document_id, tag);

ALTER TABLE ONLY aura.document_versions
    ADD CONSTRAINT document_versions_document_id_sha256_pipeline_config_hash_key UNIQUE (document_id, sha256, pipeline_config_hash);

ALTER TABLE ONLY aura.document_versions
    ADD CONSTRAINT document_versions_document_id_version_number_key UNIQUE (document_id, version_number);

ALTER TABLE ONLY aura.document_versions
    ADD CONSTRAINT document_versions_id_identity_unique UNIQUE (id, identity_id);

ALTER TABLE ONLY aura.document_versions
    ADD CONSTRAINT document_versions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY aura.documents
    ADD CONSTRAINT documents_id_identity_unique UNIQUE (id, identity_id);

ALTER TABLE ONLY aura.documents
    ADD CONSTRAINT documents_pkey PRIMARY KEY (id);

ALTER TABLE ONLY aura.storage_objects
    ADD CONSTRAINT storage_objects_bucket_object_key_key UNIQUE (bucket, object_key);

ALTER TABLE ONLY aura.storage_objects
    ADD CONSTRAINT storage_objects_id_identity_unique UNIQUE (id, identity_id);

ALTER TABLE ONLY aura.storage_objects
    ADD CONSTRAINT storage_objects_pkey PRIMARY KEY (id);

CREATE INDEX delete_jobs_owner_claim_idx ON aura.delete_jobs USING btree (identity_id, status, next_attempt_at, locked_until, created_at);

CREATE INDEX delete_jobs_status_next_attempt_idx ON aura.delete_jobs USING btree (status, next_attempt_at, created_at);

CREATE INDEX document_chunks_document_active_idx ON aura.document_chunks USING btree (document_id, active);

CREATE INDEX document_embeddings_document_version_active_idx ON aura.document_embeddings USING btree (document_id, version_id, active);

CREATE INDEX document_ingest_jobs_document_id_idx ON aura.document_ingest_jobs USING btree (document_id);

CREATE UNIQUE INDEX document_ingest_jobs_owner_source_document_hash_idx ON aura.document_ingest_jobs USING btree (identity_id, source_id, document_id, content_hash);

CREATE INDEX document_ingest_jobs_status_created_idx ON aura.document_ingest_jobs USING btree (status, created_at DESC);

CREATE INDEX document_pipeline_stages_claim_idx ON aura.document_pipeline_stages USING btree (identity_id, status, next_attempt_at, locked_until, created_at);

CREATE INDEX document_tags_tag_document_idx ON aura.document_tags USING btree (tag, document_id);

CREATE UNIQUE INDEX document_versions_document_sha256_live_idx ON aura.document_versions USING btree (document_id, sha256) WHERE (deleted_at IS NULL);

CREATE INDEX document_versions_document_status_idx ON aura.document_versions USING btree (document_id, status, created_at DESC);

CREATE INDEX document_versions_sha1_idx ON aura.document_versions USING btree (sha1) WHERE (sha1 <> ''::text);

CREATE INDEX document_versions_sha256_idx ON aura.document_versions USING btree (sha256);

CREATE INDEX document_versions_status_created_idx ON aura.document_versions USING btree (status, created_at DESC);

CREATE INDEX documents_digest_tsv_idx ON aura.documents USING gin (digest_tsv) WITH (fastupdate=off);

CREATE INDEX documents_identity_created_idx ON aura.documents USING btree (identity_id, created_at DESC);

CREATE INDEX documents_identity_scope_created_idx ON aura.documents USING btree (identity_id, scope, created_at DESC);

CREATE UNIQUE INDEX documents_identity_search_document_live_idx ON aura.documents USING btree (identity_id, search_document_id) WHERE (deleted_at IS NULL);

CREATE UNIQUE INDEX documents_identity_source_live_idx ON aura.documents USING btree (identity_id, source_kind, source_key) WHERE (deleted_at IS NULL);

CREATE INDEX documents_identity_title_idx ON aura.documents USING btree (identity_id, title);

CREATE INDEX documents_tags_gin_idx ON aura.documents USING gin (tags);

CREATE INDEX storage_objects_deleted_idx ON aura.storage_objects USING btree (deleted_at) WHERE (deleted_at IS NOT NULL);

CREATE INDEX storage_objects_identity_document_version_idx ON aura.storage_objects USING btree (identity_id, document_id, version_id);

CREATE INDEX storage_objects_kind_created_idx ON aura.storage_objects USING btree (kind, created_at DESC);

CREATE TRIGGER documents_identity_immutable BEFORE UPDATE ON aura.documents FOR EACH ROW EXECUTE FUNCTION aura.document_identity_immutable();

ALTER TABLE ONLY aura.delete_jobs
    ADD CONSTRAINT delete_jobs_document_id_fkey FOREIGN KEY (document_id) REFERENCES aura.documents(id) ON DELETE SET NULL;

ALTER TABLE ONLY aura.delete_jobs
    ADD CONSTRAINT delete_jobs_document_identity_fkey FOREIGN KEY (document_id, identity_id) REFERENCES aura.documents(id, identity_id) ON DELETE SET NULL (document_id);

ALTER TABLE ONLY aura.delete_jobs
    ADD CONSTRAINT delete_jobs_identity_id_fkey FOREIGN KEY (identity_id) REFERENCES aura.identities(id) ON DELETE CASCADE;

ALTER TABLE ONLY aura.delete_jobs
    ADD CONSTRAINT delete_jobs_version_id_fkey FOREIGN KEY (version_id) REFERENCES aura.document_versions(id) ON DELETE SET NULL;

ALTER TABLE ONLY aura.delete_jobs
    ADD CONSTRAINT delete_jobs_version_identity_fkey FOREIGN KEY (version_id, identity_id) REFERENCES aura.document_versions(id, identity_id) ON DELETE SET NULL (version_id);

ALTER TABLE ONLY aura.document_chunks
    ADD CONSTRAINT document_chunks_document_id_fkey FOREIGN KEY (document_id) REFERENCES aura.documents(id) ON DELETE CASCADE;

ALTER TABLE ONLY aura.document_chunks
    ADD CONSTRAINT document_chunks_document_identity_fkey FOREIGN KEY (document_id, identity_id) REFERENCES aura.documents(id, identity_id) ON DELETE CASCADE;

ALTER TABLE ONLY aura.document_chunks
    ADD CONSTRAINT document_chunks_version_id_fkey FOREIGN KEY (version_id) REFERENCES aura.document_versions(id) ON DELETE CASCADE;

ALTER TABLE ONLY aura.document_chunks
    ADD CONSTRAINT document_chunks_version_identity_fkey FOREIGN KEY (version_id, identity_id) REFERENCES aura.document_versions(id, identity_id) ON DELETE CASCADE;

ALTER TABLE ONLY aura.document_embeddings
    ADD CONSTRAINT document_embeddings_chunk_id_fkey FOREIGN KEY (chunk_id) REFERENCES aura.document_chunks(id) ON DELETE CASCADE;

ALTER TABLE ONLY aura.document_embeddings
    ADD CONSTRAINT document_embeddings_chunk_identity_fkey FOREIGN KEY (chunk_id, identity_id) REFERENCES aura.document_chunks(id, identity_id) ON DELETE CASCADE;

ALTER TABLE ONLY aura.document_embeddings
    ADD CONSTRAINT document_embeddings_document_id_fkey FOREIGN KEY (document_id) REFERENCES aura.documents(id) ON DELETE CASCADE;

ALTER TABLE ONLY aura.document_embeddings
    ADD CONSTRAINT document_embeddings_document_identity_fkey FOREIGN KEY (document_id, identity_id) REFERENCES aura.documents(id, identity_id) ON DELETE CASCADE;

ALTER TABLE ONLY aura.document_embeddings
    ADD CONSTRAINT document_embeddings_version_id_fkey FOREIGN KEY (version_id) REFERENCES aura.document_versions(id) ON DELETE CASCADE;

ALTER TABLE ONLY aura.document_embeddings
    ADD CONSTRAINT document_embeddings_version_identity_fkey FOREIGN KEY (version_id, identity_id) REFERENCES aura.document_versions(id, identity_id) ON DELETE CASCADE;

ALTER TABLE ONLY aura.document_ingest_jobs
    ADD CONSTRAINT document_ingest_jobs_asset_identity_fkey FOREIGN KEY (asset_id, identity_id) REFERENCES aura.assets(id, identity_id) ON DELETE CASCADE;

ALTER TABLE ONLY aura.document_ingest_jobs
    ADD CONSTRAINT document_ingest_jobs_document_identity_fkey FOREIGN KEY (catalog_document_id, identity_id) REFERENCES aura.documents(id, identity_id) ON DELETE CASCADE;

ALTER TABLE ONLY aura.document_ingest_jobs
    ADD CONSTRAINT document_ingest_jobs_identity_id_fkey FOREIGN KEY (identity_id) REFERENCES aura.identities(id) ON DELETE CASCADE;

ALTER TABLE ONLY aura.document_ingest_jobs
    ADD CONSTRAINT document_ingest_jobs_version_identity_fkey FOREIGN KEY (version_id, identity_id) REFERENCES aura.document_versions(id, identity_id) ON DELETE CASCADE;

ALTER TABLE ONLY aura.document_pipeline_stages
    ADD CONSTRAINT document_pipeline_stages_artifact_storage_object_id_identi_fkey FOREIGN KEY (artifact_storage_object_id, identity_id) REFERENCES aura.storage_objects(id, identity_id) ON DELETE SET NULL (artifact_storage_object_id);

ALTER TABLE ONLY aura.document_pipeline_stages
    ADD CONSTRAINT document_pipeline_stages_document_id_identity_id_fkey FOREIGN KEY (document_id, identity_id) REFERENCES aura.documents(id, identity_id) ON DELETE CASCADE;

ALTER TABLE ONLY aura.document_pipeline_stages
    ADD CONSTRAINT document_pipeline_stages_identity_id_fkey FOREIGN KEY (identity_id) REFERENCES aura.identities(id) ON DELETE CASCADE;

ALTER TABLE ONLY aura.document_pipeline_stages
    ADD CONSTRAINT document_pipeline_stages_version_id_identity_id_fkey FOREIGN KEY (version_id, identity_id) REFERENCES aura.document_versions(id, identity_id) ON DELETE CASCADE;

ALTER TABLE ONLY aura.document_tags
    ADD CONSTRAINT document_tags_created_by_fkey FOREIGN KEY (created_by) REFERENCES aura.identities(id) ON DELETE SET NULL;

ALTER TABLE ONLY aura.document_tags
    ADD CONSTRAINT document_tags_document_id_fkey FOREIGN KEY (document_id) REFERENCES aura.documents(id) ON DELETE CASCADE;

ALTER TABLE ONLY aura.document_versions
    ADD CONSTRAINT document_versions_asset_id_fkey FOREIGN KEY (asset_id) REFERENCES aura.assets(id) ON DELETE SET NULL;

ALTER TABLE ONLY aura.document_versions
    ADD CONSTRAINT document_versions_asset_identity_fkey FOREIGN KEY (asset_id, identity_id) REFERENCES aura.assets(id, identity_id) ON DELETE SET NULL (asset_id);

ALTER TABLE ONLY aura.document_versions
    ADD CONSTRAINT document_versions_document_id_fkey FOREIGN KEY (document_id) REFERENCES aura.documents(id) ON DELETE CASCADE;

ALTER TABLE ONLY aura.document_versions
    ADD CONSTRAINT document_versions_document_identity_fkey FOREIGN KEY (document_id, identity_id) REFERENCES aura.documents(id, identity_id) ON DELETE CASCADE;

ALTER TABLE ONLY aura.document_versions
    ADD CONSTRAINT document_versions_storage_identity_fkey FOREIGN KEY (storage_object_id, identity_id) REFERENCES aura.storage_objects(id, identity_id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE ONLY aura.document_versions
    ADD CONSTRAINT document_versions_storage_object_id_fkey FOREIGN KEY (storage_object_id) REFERENCES aura.storage_objects(id);

ALTER TABLE ONLY aura.documents
    ADD CONSTRAINT documents_active_version_id_fkey FOREIGN KEY (active_version_id) REFERENCES aura.document_versions(id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE ONLY aura.documents
    ADD CONSTRAINT documents_active_version_identity_fkey FOREIGN KEY (active_version_id, identity_id) REFERENCES aura.document_versions(id, identity_id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE ONLY aura.documents
    ADD CONSTRAINT documents_identity_id_fkey FOREIGN KEY (identity_id) REFERENCES aura.identities(id) ON DELETE CASCADE;

ALTER TABLE ONLY aura.storage_objects
    ADD CONSTRAINT storage_objects_asset_id_fkey FOREIGN KEY (asset_id) REFERENCES aura.assets(id) ON DELETE SET NULL;

ALTER TABLE ONLY aura.storage_objects
    ADD CONSTRAINT storage_objects_asset_identity_fkey FOREIGN KEY (asset_id, identity_id) REFERENCES aura.assets(id, identity_id) ON DELETE SET NULL (asset_id);

ALTER TABLE ONLY aura.storage_objects
    ADD CONSTRAINT storage_objects_document_id_fkey FOREIGN KEY (document_id) REFERENCES aura.documents(id) ON DELETE SET NULL;

ALTER TABLE ONLY aura.storage_objects
    ADD CONSTRAINT storage_objects_document_identity_fkey FOREIGN KEY (document_id, identity_id) REFERENCES aura.documents(id, identity_id) ON DELETE SET NULL (document_id);

ALTER TABLE ONLY aura.storage_objects
    ADD CONSTRAINT storage_objects_identity_id_fkey FOREIGN KEY (identity_id) REFERENCES aura.identities(id) ON DELETE CASCADE;

ALTER TABLE ONLY aura.storage_objects
    ADD CONSTRAINT storage_objects_version_id_fkey FOREIGN KEY (version_id) REFERENCES aura.document_versions(id) ON DELETE SET NULL DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE ONLY aura.storage_objects
    ADD CONSTRAINT storage_objects_version_identity_fkey FOREIGN KEY (version_id, identity_id) REFERENCES aura.document_versions(id, identity_id) ON DELETE SET NULL (version_id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE aura.delete_jobs ENABLE ROW LEVEL SECURITY;

CREATE POLICY delete_jobs_owner_isolation ON aura.delete_jobs USING ((identity_id = (NULLIF(current_setting('app.current_identity'::text, true), ''::text))::uuid));

CREATE POLICY delete_jobs_requires_identity ON aura.delete_jobs AS RESTRICTIVE TO aura_app USING (((current_setting('app.current_identity'::text, true) IS NOT NULL) AND (current_setting('app.current_identity'::text, true) <> ''::text)));

ALTER TABLE aura.document_chunks ENABLE ROW LEVEL SECURITY;

CREATE POLICY document_chunks_owner_isolation ON aura.document_chunks USING ((identity_id = (NULLIF(current_setting('app.current_identity'::text, true), ''::text))::uuid));

CREATE POLICY document_chunks_requires_identity ON aura.document_chunks AS RESTRICTIVE TO aura_app USING (((current_setting('app.current_identity'::text, true) IS NOT NULL) AND (current_setting('app.current_identity'::text, true) <> ''::text)));

COMMENT ON POLICY document_chunks_requires_identity ON aura.document_chunks IS 'Fail-closed floor (migration 0087): AND-combined with every permissive policy, so no later permissive policy can restore visibility to a caller with no app.current_identity. Set it via internal/db.WithIdentityTx / WithIdentityTxRaw.';

ALTER TABLE aura.document_embeddings ENABLE ROW LEVEL SECURITY;

CREATE POLICY document_embeddings_owner_isolation ON aura.document_embeddings USING ((identity_id = (NULLIF(current_setting('app.current_identity'::text, true), ''::text))::uuid));

CREATE POLICY document_embeddings_requires_identity ON aura.document_embeddings AS RESTRICTIVE TO aura_app USING (((current_setting('app.current_identity'::text, true) IS NOT NULL) AND (current_setting('app.current_identity'::text, true) <> ''::text)));

COMMENT ON POLICY document_embeddings_requires_identity ON aura.document_embeddings IS 'Fail-closed floor (migration 0087): AND-combined with every permissive policy, so no later permissive policy can restore visibility to a caller with no app.current_identity. Set it via internal/db.WithIdentityTx / WithIdentityTxRaw.';

ALTER TABLE aura.document_ingest_jobs ENABLE ROW LEVEL SECURITY;

CREATE POLICY document_ingest_jobs_owner_isolation ON aura.document_ingest_jobs USING ((identity_id = (NULLIF(current_setting('app.current_identity'::text, true), ''::text))::uuid));

CREATE POLICY document_ingest_jobs_requires_identity ON aura.document_ingest_jobs AS RESTRICTIVE TO aura_app USING (((current_setting('app.current_identity'::text, true) IS NOT NULL) AND (current_setting('app.current_identity'::text, true) <> ''::text)));

ALTER TABLE aura.document_pipeline_stages ENABLE ROW LEVEL SECURITY;

CREATE POLICY document_pipeline_stages_owner_isolation ON aura.document_pipeline_stages USING ((identity_id = (NULLIF(current_setting('app.current_identity'::text, true), ''::text))::uuid));

CREATE POLICY document_pipeline_stages_requires_identity ON aura.document_pipeline_stages AS RESTRICTIVE TO aura_app USING (((current_setting('app.current_identity'::text, true) IS NOT NULL) AND (current_setting('app.current_identity'::text, true) <> ''::text)));

ALTER TABLE aura.document_tags ENABLE ROW LEVEL SECURITY;

CREATE POLICY document_tags_owner_isolation ON aura.document_tags USING ((EXISTS ( SELECT 1
   FROM aura.documents d
  WHERE ((d.id = document_tags.document_id) AND (d.identity_id = (NULLIF(current_setting('app.current_identity'::text, true), ''::text))::uuid)))));

CREATE POLICY document_tags_requires_identity ON aura.document_tags AS RESTRICTIVE TO aura_app USING (((current_setting('app.current_identity'::text, true) IS NOT NULL) AND (current_setting('app.current_identity'::text, true) <> ''::text)));

COMMENT ON POLICY document_tags_requires_identity ON aura.document_tags IS 'Fail-closed floor (migration 0087): AND-combined with every permissive policy, so no later permissive policy can restore visibility to a caller with no app.current_identity. Set it via internal/db.WithIdentityTx / WithIdentityTxRaw.';

ALTER TABLE aura.document_versions ENABLE ROW LEVEL SECURITY;

CREATE POLICY document_versions_owner_isolation ON aura.document_versions USING ((identity_id = (NULLIF(current_setting('app.current_identity'::text, true), ''::text))::uuid));

CREATE POLICY document_versions_requires_identity ON aura.document_versions AS RESTRICTIVE TO aura_app USING (((current_setting('app.current_identity'::text, true) IS NOT NULL) AND (current_setting('app.current_identity'::text, true) <> ''::text)));

COMMENT ON POLICY document_versions_requires_identity ON aura.document_versions IS 'Fail-closed floor (migration 0087): AND-combined with every permissive policy, so no later permissive policy can restore visibility to a caller with no app.current_identity. Set it via internal/db.WithIdentityTx / WithIdentityTxRaw.';

ALTER TABLE aura.documents ENABLE ROW LEVEL SECURITY;

CREATE POLICY documents_owner_isolation ON aura.documents USING ((identity_id = (NULLIF(current_setting('app.current_identity'::text, true), ''::text))::uuid));

CREATE POLICY documents_requires_identity ON aura.documents AS RESTRICTIVE TO aura_app USING (((current_setting('app.current_identity'::text, true) IS NOT NULL) AND (current_setting('app.current_identity'::text, true) <> ''::text)));

COMMENT ON POLICY documents_requires_identity ON aura.documents IS 'Fail-closed floor (migration 0087): AND-combined with every permissive policy, so no later permissive policy can restore visibility to a caller with no app.current_identity. Set it via internal/db.WithIdentityTx / WithIdentityTxRaw.';

ALTER TABLE aura.storage_objects ENABLE ROW LEVEL SECURITY;

CREATE POLICY storage_objects_owner_isolation ON aura.storage_objects USING ((identity_id = (NULLIF(current_setting('app.current_identity'::text, true), ''::text))::uuid));

CREATE POLICY storage_objects_requires_identity ON aura.storage_objects AS RESTRICTIVE TO aura_app USING (((current_setting('app.current_identity'::text, true) IS NOT NULL) AND (current_setting('app.current_identity'::text, true) <> ''::text)));

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE aura.delete_jobs TO aura_app;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE aura.document_chunks TO aura_app;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE aura.document_embeddings TO aura_app;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE aura.document_ingest_jobs TO aura_app;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE aura.document_pipeline_stages TO aura_app;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE aura.document_tags TO aura_app;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE aura.document_versions TO aura_app;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE aura.documents TO aura_app;

GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE aura.storage_objects TO aura_app;

--

-- The links back from the two tables 0098 left standing. Added LAST: both reference tables
-- created above, and the columns are nullable with no default, so re-adding them to populated
-- tables cannot fail on existing rows.
ALTER TABLE IF EXISTS aura.assets
    ADD COLUMN IF NOT EXISTS catalog_document_id uuid,
    ADD COLUMN IF NOT EXISTS document_version_id uuid;

ALTER TABLE aura.assets
    ADD CONSTRAINT assets_catalog_document_identity_fkey
        FOREIGN KEY (catalog_document_id, identity_id)
        REFERENCES aura.documents(id, identity_id) ON DELETE SET NULL (catalog_document_id),
    ADD CONSTRAINT assets_version_identity_fkey
        FOREIGN KEY (document_version_id, identity_id)
        REFERENCES aura.document_versions(id, identity_id) ON DELETE SET NULL (document_version_id);

ALTER TABLE IF EXISTS aura.ingestion_jobs
    ADD COLUMN IF NOT EXISTS document_id uuid,
    ADD COLUMN IF NOT EXISTS version_id uuid;

ALTER TABLE aura.ingestion_jobs
    ADD CONSTRAINT ingestion_jobs_document_id_fkey
        FOREIGN KEY (document_id) REFERENCES aura.documents(id) ON DELETE CASCADE,
    ADD CONSTRAINT ingestion_jobs_document_identity_fkey
        FOREIGN KEY (document_id, identity_id) REFERENCES aura.documents(id, identity_id) ON DELETE CASCADE,
    ADD CONSTRAINT ingestion_jobs_version_id_fkey
        FOREIGN KEY (version_id) REFERENCES aura.document_versions(id) ON DELETE CASCADE,
    ADD CONSTRAINT ingestion_jobs_version_identity_fkey
        FOREIGN KEY (version_id, identity_id) REFERENCES aura.document_versions(id, identity_id) ON DELETE CASCADE;
