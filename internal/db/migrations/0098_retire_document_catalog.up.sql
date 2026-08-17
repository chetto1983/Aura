-- Retire the document catalog: ten tables nothing writes and nothing reads.
--
-- This is the schema half of the two-store design's sub-project B
-- (docs/superpowers/specs/2026-08-08-document-plane-two-store-design.md §3). The code half
-- landed in the same change: internal/assets/document_processor.go stopped minting a second
-- document identity and writing a catalog row, and documents.Service, CatalogService and the
-- PostgresCatalogStore went with it.
--
-- WHY THESE TEN, measured 2026-08-17 against this schema and the surviving Go tree. Every
-- statement that touched them is dead — SetDocumentCard, CreateDocument, GetDocumentBySearchID,
-- DeleteDocumentTags, UpsertDocumentTag, ReservePipelineCandidateVersion and the four
-- *DocumentIngestJob statements have no non-test caller left — and five of the ten
-- (document_chunks, document_embeddings, document_pipeline_stages, document_pipeline_quarantine,
-- delete_jobs) had no SQL statement AT ALL even before this change: they were dropped from the
-- code when the in-process pipeline was deleted and only the tables remained.
--
-- WHAT REPLACED THEM. The catalog answered three questions and each is now answered by the
-- store that actually holds the thing:
--   where are the bytes?          the object key IS the answer; source_key on the passage
--   are these the indexed bytes?  raw_sha256 on the passage
--   which documents are indexed?  SELECT FROM IndexedDocument in the identity's ArcadeDB
-- Measured live 2026-08-17: an object PUT into the bucket at 12:42:47 was answerable at
-- 12:43:24 — 37 seconds, 22 passages — with no row in any table below.
--
-- WHAT IS DELIBERATELY KEPT. aura.ingestion_jobs is the GENERIC asset queue: image and audio
-- ride it too, and deleting it would take vision summaries and STT down with the catalog.
-- aura.ingestion_events is its audit trail, written inside those same statements. Neither is
-- touched here.
--
-- NOT A CLEANUP, A BEHAVIOUR CHANGE, recorded as such: document titles and tags stop existing.
-- A document's name is its object key, and anything richer has to live in S3 object metadata.

-- 1. Break the links from the two SURVIVING tables first, so the drop below dangles nothing.
--
-- Both are safe on the data as well as on the schema: measured 2026-08-17, all 4
-- aura.ingestion_jobs rows carry NULL in both columns (the generic queue never used them), and
-- the 4 aura.assets rows that do carry a link point into tables this migration removes.
ALTER TABLE IF EXISTS aura.assets
    DROP CONSTRAINT IF EXISTS assets_catalog_document_identity_fkey,
    DROP CONSTRAINT IF EXISTS assets_version_identity_fkey,
    DROP COLUMN IF EXISTS catalog_document_id,
    DROP COLUMN IF EXISTS document_version_id;

ALTER TABLE IF EXISTS aura.ingestion_jobs
    DROP CONSTRAINT IF EXISTS ingestion_jobs_document_id_fkey,
    DROP CONSTRAINT IF EXISTS ingestion_jobs_document_identity_fkey,
    DROP CONSTRAINT IF EXISTS ingestion_jobs_version_id_fkey,
    DROP CONSTRAINT IF EXISTS ingestion_jobs_version_identity_fkey,
    DROP COLUMN IF EXISTS document_id,
    DROP COLUMN IF EXISTS version_id;

-- 2. Drop the ten in ONE statement rather than in a hand-ordered sequence.
--
-- aura.documents and aura.document_versions reference each other (documents.active_version_id
-- against the version, document_versions.document_id back at the document), so no ordering of
-- separate DROP TABLE statements can satisfy both. Naming them together lets Postgres resolve
-- the cycle itself, and makes the remaining eight independent of the order they are written in.
DROP TABLE IF EXISTS
    aura.document_embeddings,
    aura.document_chunks,
    aura.document_pipeline_stages,
    aura.document_pipeline_quarantine,
    aura.delete_jobs,
    aura.document_ingest_jobs,
    aura.storage_objects,
    aura.document_tags,
    aura.document_versions,
    aura.documents;

-- 3. The guard that outlived the table it guarded. documents_identity_immutable was its only
-- trigger (measured: pg_trigger has no other reference to this function), and the trigger went
-- with the table above.
DROP FUNCTION IF EXISTS aura.document_identity_immutable();
