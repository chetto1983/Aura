-- Roll back amendment #114's control-plane foundation before verified hard deletion.
-- Data that cannot fit the old duplicate-collapsing schema makes rollback fail before DDL.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM aura.storage_objects
        WHERE status = 'object_deleted' OR deletion_verified_at IS NOT NULL
    ) THEN
        RAISE EXCEPTION '0093 rollback blocked: verified object deletion requires forward repair';
    END IF;
    IF EXISTS (
        SELECT 1 FROM aura.document_chunks
        GROUP BY version_id, chunk_hash HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION '0093 rollback blocked: duplicate chunk text locators require forward repair';
    END IF;
    IF EXISTS (
        SELECT 1 FROM aura.document_embeddings
        GROUP BY chunk_id, embedding_model, embedding_version HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION '0093 rollback blocked: multiple projection generations require forward repair';
    END IF;
END
$$;

DROP TABLE aura.document_pipeline_stages;

DO $$
DECLARE
    target text;
BEGIN
    FOREACH target IN ARRAY ARRAY[
        'storage_objects', 'ingestion_jobs', 'ingestion_events', 'delete_jobs',
        'document_ingest_jobs'
    ]
    LOOP
        EXECUTE format('DROP POLICY IF EXISTS %I ON aura.%I', target || '_requires_identity', target);
        EXECUTE format('DROP POLICY IF EXISTS %I ON aura.%I', target || '_owner_isolation', target);
        EXECUTE format('ALTER TABLE aura.%I DISABLE ROW LEVEL SECURITY', target);
    END LOOP;
END
$$;

DROP POLICY document_versions_owner_isolation ON aura.document_versions;
CREATE POLICY document_versions_owner_isolation ON aura.document_versions
    USING (EXISTS (
        SELECT 1 FROM aura.documents d
        WHERE d.id = document_id
          AND d.identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid
    ));

DROP POLICY document_chunks_owner_isolation ON aura.document_chunks;
CREATE POLICY document_chunks_owner_isolation ON aura.document_chunks
    USING (EXISTS (
        SELECT 1 FROM aura.documents d
        WHERE d.id = document_id
          AND d.identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid
    ));

DROP POLICY document_embeddings_owner_isolation ON aura.document_embeddings;
CREATE POLICY document_embeddings_owner_isolation ON aura.document_embeddings
    USING (EXISTS (
        SELECT 1 FROM aura.documents d
        WHERE d.id = document_id
          AND d.identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid
    ));

DROP TRIGGER documents_identity_immutable ON aura.documents;
DROP FUNCTION aura.document_identity_immutable();
DROP INDEX aura.documents_identity_search_document_live_idx;
DROP INDEX aura.document_versions_document_sha256_live_idx;
DROP INDEX aura.document_ingest_jobs_owner_source_document_hash_idx;
DROP INDEX aura.ingestion_jobs_owner_claim_idx;
DROP INDEX aura.delete_jobs_owner_claim_idx;

ALTER TABLE aura.assets
    DROP CONSTRAINT assets_catalog_document_identity_fkey,
    DROP CONSTRAINT assets_version_identity_fkey;

ALTER TABLE aura.documents
    DROP CONSTRAINT documents_active_version_identity_fkey;

ALTER TABLE aura.storage_objects
    DROP CONSTRAINT storage_objects_version_identity_fkey,
    DROP CONSTRAINT storage_objects_document_identity_fkey,
    DROP CONSTRAINT storage_objects_asset_identity_fkey;

ALTER TABLE aura.document_embeddings
    DROP CONSTRAINT document_embeddings_document_identity_fkey,
    DROP CONSTRAINT document_embeddings_version_identity_fkey,
    DROP CONSTRAINT document_embeddings_chunk_identity_fkey,
    DROP CONSTRAINT document_embeddings_projection_unique,
    DROP CONSTRAINT document_embeddings_generation_valid,
    DROP CONSTRAINT document_embeddings_projection_status_check;

ALTER TABLE aura.document_chunks
    DROP CONSTRAINT document_chunks_document_identity_fkey,
    DROP CONSTRAINT document_chunks_version_identity_fkey,
    DROP CONSTRAINT document_chunks_version_ordinal_unique,
    DROP CONSTRAINT document_chunks_id_identity_unique,
    DROP CONSTRAINT document_chunks_locator_valid;

ALTER TABLE aura.ingestion_events
    DROP CONSTRAINT ingestion_events_job_identity_fkey,
    DROP CONSTRAINT ingestion_events_identity_id_fkey,
    DROP CONSTRAINT ingestion_events_generation_valid,
    ALTER COLUMN identity_id DROP NOT NULL;

ALTER TABLE aura.ingestion_jobs
    DROP CONSTRAINT ingestion_jobs_document_identity_fkey,
    DROP CONSTRAINT ingestion_jobs_version_identity_fkey,
    DROP CONSTRAINT ingestion_jobs_asset_identity_fkey,
    DROP CONSTRAINT ingestion_jobs_identity_id_fkey,
    DROP CONSTRAINT ingestion_jobs_identity_idempotency_unique,
    DROP CONSTRAINT ingestion_jobs_id_identity_unique,
    DROP CONSTRAINT ingestion_jobs_generation_valid,
    ALTER COLUMN identity_id DROP NOT NULL;

ALTER TABLE aura.delete_jobs
    DROP CONSTRAINT delete_jobs_document_identity_fkey,
    DROP CONSTRAINT delete_jobs_version_identity_fkey,
    DROP CONSTRAINT delete_jobs_identity_id_fkey,
    DROP CONSTRAINT delete_jobs_identity_idempotency_unique,
    DROP CONSTRAINT delete_jobs_generation_valid,
    ALTER COLUMN identity_id DROP NOT NULL,
    ALTER COLUMN idempotency_key DROP NOT NULL;

ALTER TABLE aura.document_ingest_jobs
    DROP CONSTRAINT document_ingest_jobs_asset_identity_fkey,
    DROP CONSTRAINT document_ingest_jobs_document_identity_fkey,
    DROP CONSTRAINT document_ingest_jobs_version_identity_fkey,
    DROP CONSTRAINT document_ingest_jobs_identity_id_fkey,
    DROP CONSTRAINT document_ingest_jobs_generation_valid,
    ALTER COLUMN identity_id DROP NOT NULL;

ALTER TABLE aura.document_versions
    DROP CONSTRAINT document_versions_asset_identity_fkey,
    DROP CONSTRAINT document_versions_storage_identity_fkey,
    DROP CONSTRAINT document_versions_document_identity_fkey,
    DROP CONSTRAINT document_versions_id_identity_unique,
    DROP CONSTRAINT document_versions_generation_valid;

-- Restore quarantined rows while their 0093 columns still exist; their JSON snapshots were
-- taken after those nullable columns were added, so populate_record is shape-exact.
INSERT INTO aura.ingestion_jobs
SELECT (jsonb_populate_record(NULL::aura.ingestion_jobs, row_data)).*
FROM aura.document_pipeline_quarantine
WHERE source_table = 'ingestion_jobs';

INSERT INTO aura.ingestion_events
SELECT (jsonb_populate_record(NULL::aura.ingestion_events, row_data)).*
FROM aura.document_pipeline_quarantine
WHERE source_table = 'ingestion_events';

INSERT INTO aura.delete_jobs
SELECT (jsonb_populate_record(NULL::aura.delete_jobs, row_data)).*
FROM aura.document_pipeline_quarantine
WHERE source_table = 'delete_jobs';

INSERT INTO aura.document_ingest_jobs
SELECT (jsonb_populate_record(NULL::aura.document_ingest_jobs, row_data)).*
FROM aura.document_pipeline_quarantine
WHERE source_table = 'document_ingest_jobs';

DROP TABLE aura.document_pipeline_quarantine;

ALTER TABLE aura.ingestion_events
    DROP COLUMN identity_id,
    DROP COLUMN pipeline_generation,
    DROP COLUMN attempt_generation,
    DROP COLUMN lease_generation;

ALTER TABLE aura.ingestion_jobs
    DROP COLUMN identity_id,
    DROP COLUMN asset_id,
    DROP COLUMN pipeline_generation,
    DROP COLUMN attempt_generation,
    DROP COLUMN lease_generation,
    ADD CONSTRAINT ingestion_jobs_job_type_idempotency_key_key
        UNIQUE (job_type, idempotency_key);

ALTER TABLE aura.delete_jobs
    DROP COLUMN identity_id,
    DROP COLUMN idempotency_key,
    DROP COLUMN delete_generation,
    DROP COLUMN lease_generation,
    DROP COLUMN error_code,
    DROP COLUMN error_message;

ALTER TABLE aura.document_ingest_jobs
    DROP COLUMN identity_id,
    DROP COLUMN asset_id,
    DROP COLUMN catalog_document_id,
    DROP COLUMN version_id,
    DROP COLUMN pipeline_generation;

CREATE UNIQUE INDEX document_ingest_jobs_source_document_hash_idx
    ON aura.document_ingest_jobs (source_id, document_id, content_hash);

ALTER TABLE aura.document_embeddings
    DROP COLUMN identity_id,
    DROP COLUMN pipeline_generation,
    DROP COLUMN embedding_fingerprint,
    DROP COLUMN projection_status,
    DROP COLUMN projected_at,
    ADD CONSTRAINT document_embeddings_chunk_id_embedding_model_embedding_vers_key
        UNIQUE (chunk_id, embedding_model, embedding_version);

ALTER TABLE aura.document_chunks
    DROP COLUMN identity_id,
    DROP COLUMN search_document_id,
    DROP COLUMN version_number,
    DROP COLUMN original_sha256,
    DROP COLUMN pipeline_generation,
    DROP COLUMN ordinal,
    DROP COLUMN normalized_text_sha256,
    DROP COLUMN self_ref,
    DROP COLUMN heading_path,
    DROP COLUMN captions,
    DROP COLUMN page_number,
    DROP COLUMN bounding_box,
    DROP COLUMN char_start,
    DROP COLUMN char_end,
    DROP COLUMN sheet_name,
    DROP COLUMN table_name,
    DROP COLUMN row_number,
    DROP COLUMN column_number,
    DROP COLUMN cell_ref,
    ADD CONSTRAINT document_chunks_version_id_chunk_hash_key UNIQUE (version_id, chunk_hash);

UPDATE aura.assets SET status = 'deleted' WHERE status = 'deleting';
ALTER TABLE aura.assets
    DROP CONSTRAINT assets_status_check,
    DROP CONSTRAINT assets_id_identity_unique,
    DROP CONSTRAINT assets_generation_valid,
    DROP COLUMN catalog_document_id,
    DROP COLUMN document_version_id,
    DROP COLUMN pipeline_generation,
    ADD CONSTRAINT assets_status_check CHECK (status IN (
        'created', 'presigned', 'uploaded', 'accepted', 'processing',
        'searchable', 'embedding', 'complete', 'failed', 'refused',
        'deleted', 'canceled'
    ));

UPDATE aura.storage_objects
SET kind = CASE kind
    WHEN 'converted_document' THEN 'extracted_text'
    WHEN 'chunk_manifest' THEN 'chunks'
    WHEN 'embedding_manifest' THEN 'chunks'
    ELSE kind
END;

ALTER TABLE aura.storage_objects
    DROP CONSTRAINT storage_objects_id_identity_unique,
    DROP CONSTRAINT storage_objects_generation_valid,
    DROP CONSTRAINT storage_objects_status_check,
    DROP CONSTRAINT storage_objects_kind_check,
    DROP COLUMN status,
    DROP COLUMN pipeline_generation,
    DROP COLUMN deletion_generation,
    DROP COLUMN deletion_verified_at,
    ADD CONSTRAINT storage_objects_kind_check CHECK (kind IN (
        'raw', 'extracted_text', 'chunks', 'preview', 'temp', 'failed_artifact'
    ));

ALTER TABLE aura.document_versions
    DROP COLUMN identity_id,
    DROP COLUMN search_document_id,
    DROP COLUMN pipeline_generation;

ALTER TABLE aura.documents DROP CONSTRAINT documents_status_check;
UPDATE aura.documents
SET status = CASE status
    WHEN 'accepted' THEN 'draft'
    WHEN 'stored' THEN 'queued'
    WHEN 'converting' THEN 'processing'
    WHEN 'chunking' THEN 'processing'
    WHEN 'embedding' THEN 'processing'
    WHEN 'projecting' THEN 'processing'
    WHEN 'dead_letter' THEN 'failed'
    ELSE status
END;

ALTER TABLE aura.documents
    DROP CONSTRAINT documents_source_unique,
    DROP CONSTRAINT documents_id_identity_unique,
    DROP CONSTRAINT documents_generation_valid,
    DROP CONSTRAINT documents_search_id_nonempty,
    DROP CONSTRAINT documents_source_key_nonempty,
    DROP CONSTRAINT documents_source_kind_nonempty,
    DROP COLUMN source_kind,
    DROP COLUMN source_key,
    DROP COLUMN search_document_id,
    DROP COLUMN pipeline_generation,
    DROP COLUMN error_code,
    DROP COLUMN error_message,
    ADD CONSTRAINT documents_status_check CHECK (status IN (
        'draft', 'queued', 'processing', 'ready', 'failed',
        'deleting', 'deleted', 'archived'
    ));
