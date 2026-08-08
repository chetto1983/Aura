-- The document rows the ingest sidecar reconciles from the bucket.
--
-- WHY A NEW TABLE INSTEAD OF aura.documents. The catalog was built around a model this
-- rewrite removes: a document with versions, an active version, a pipeline generation and
-- a lifecycle status driven by an in-process stage machine. None of that survives -- the
-- bucket is the truth and CocoIndex reconciles a projection of it -- so pointing the
-- reconciler at aura.documents would mean satisfying NOT NULLs for concepts that no longer
-- exist. This table holds exactly what the reconciler knows, and nothing it would have to
-- invent.
--
-- WHY THE SCHEMA IS OURS AND THE ROWS ARE NOT. CocoIndex's postgres target takes a
-- managed_by argument: SYSTEM has it create and drop the table itself, USER requires the
-- table to exist and manages only rows. USER is deliberate. SYSTEM would put DDL outside
-- golang-migrate, which is where every other table in this database is defined, and a
-- table CocoIndex created would carry no row-level security -- the isolation this file
-- exists to grant. So Aura owns the shape and the policy; the sidecar owns the contents.
--
-- MEASURED 2026-08-08 (spikes/two-store-catalog/FINDINGS.md): the connector's pool is
-- constructed by us, so app.current_identity is set in asyncpg's per-connection init hook.
-- Writing as a non-owner WITHOUT it is refused outright -- "new row violates row-level
-- security policy" -- and a reader without it sees nothing. The sidecar therefore connects
-- as aura_app, never as aura_migrate, which owns these tables and bypasses RLS because
-- 0087 uses ENABLE rather than FORCE.

CREATE TABLE aura.ingested_documents (
    -- The passage's own document identity: SHA-256 over
    -- (domain, identity, source_kind, source_key), truncated, prefixed doc_. It is a pure
    -- function of the bucket key, which is why no uuid is minted here and no lookup table
    -- is needed to get from a retrieval hit back to an object.
    search_document_id text        PRIMARY KEY CHECK (search_document_id ~ '^doc_[0-9a-f]{32}$'),
    identity_id        uuid        NOT NULL,
    source_kind        text        NOT NULL CHECK (source_kind = 's3'),
    -- The exact object key, nested prefixes included. This is the one hop from a passage
    -- to its bytes that replaced the catalog's five.
    source_key         text        NOT NULL CHECK (btrim(source_key) <> ''),
    -- Of the object, not of any extracted text: it is what proves the bytes retrieval
    -- quoted are the bytes still in the bucket.
    raw_sha256         text        NOT NULL CHECK (raw_sha256 ~ '^[0-9a-f]{64}$'),
    size_bytes         bigint      NOT NULL CHECK (size_bytes >= 0),
    passage_count      integer     NOT NULL CHECK (passage_count >= 0),
    indexed_at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (identity_id, source_key)
);

-- Listing a folder is the file manager's most common read, and it is a prefix scan.
CREATE INDEX ingested_documents_identity_source_key_idx
    ON aura.ingested_documents (identity_id, source_key text_pattern_ops);

GRANT SELECT, INSERT, UPDATE, DELETE ON aura.ingested_documents TO aura_app;

-- Both policies, in the shape 0087 established: one permissive owner predicate, and the
-- RESTRICTIVE floor that AND-combines with it so no later permissive policy can restore
-- visibility to a caller that never set an identity.
ALTER TABLE aura.ingested_documents ENABLE ROW LEVEL SECURITY;

CREATE POLICY ingested_documents_owner_isolation ON aura.ingested_documents
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid)
    WITH CHECK (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);

CREATE POLICY ingested_documents_requires_identity ON aura.ingested_documents
    AS RESTRICTIVE FOR ALL TO aura_app
    USING (current_setting('app.current_identity', true) IS NOT NULL
           AND current_setting('app.current_identity', true) <> '');

COMMENT ON POLICY ingested_documents_requires_identity ON aura.ingested_documents IS
    'Fail-closed floor (migration 0087 pattern): AND-combined with every permissive policy, so no later permissive policy can restore visibility to a caller with no app.current_identity.';

COMMENT ON TABLE aura.ingested_documents IS
    'One row per object the ingest sidecar has indexed. Rows are written by CocoIndex''s postgres target (managed_by=user) and are a projection of the bucket, never a second source of truth: an object that leaves the bucket loses its row in the same reconciliation pass.';
