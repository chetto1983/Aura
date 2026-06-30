ALTER TABLE IF EXISTS aura.documents
    DROP CONSTRAINT IF EXISTS documents_active_version_id_fkey;

ALTER TABLE IF EXISTS aura.storage_objects
    DROP CONSTRAINT IF EXISTS storage_objects_version_id_fkey;

DROP TABLE IF EXISTS aura.audit_logs;
DROP TABLE IF EXISTS aura.delete_jobs;
DROP TABLE IF EXISTS aura.document_embeddings;
DROP TABLE IF EXISTS aura.document_chunks;
DROP TABLE IF EXISTS aura.ingestion_events;
DROP TABLE IF EXISTS aura.ingestion_jobs;
DROP TABLE IF EXISTS aura.document_versions;
DROP TABLE IF EXISTS aura.storage_objects;
DROP TABLE IF EXISTS aura.document_tags;
DROP TABLE IF EXISTS aura.documents;
