-- Amendment #114: make PostgreSQL the tenant-scoped document pipeline control plane.
-- Unowned legacy queue rows are preserved for operator repair, never attributed from JSON,
-- object keys, paths, or the seeded `local` identity.

CREATE TABLE aura.document_pipeline_quarantine (
    id             bigserial   PRIMARY KEY,
    source_table   text        NOT NULL CHECK (source_table IN (
        'document_ingest_jobs', 'ingestion_jobs', 'ingestion_events', 'delete_jobs'
    )),
    source_pk      text        NOT NULL,
    reason         text        NOT NULL CHECK (btrim(reason) <> ''),
    row_data       jsonb       NOT NULL,
    quarantined_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source_table, source_pk)
);
REVOKE ALL ON aura.document_pipeline_quarantine FROM aura_app;
REVOKE ALL ON SEQUENCE aura.document_pipeline_quarantine_id_seq FROM aura_app;
ALTER TABLE aura.documents
    ADD COLUMN source_kind text,
    ADD COLUMN source_key text,
    ADD COLUMN search_document_id text,
    ADD COLUMN pipeline_generation bigint NOT NULL DEFAULT 0,
    ADD COLUMN error_code text NOT NULL DEFAULT '',
    ADD COLUMN error_message text NOT NULL DEFAULT '';
ALTER TABLE aura.document_versions
    ADD COLUMN identity_id uuid,
    ADD COLUMN search_document_id text,
    ADD COLUMN pipeline_generation bigint NOT NULL DEFAULT 0;
ALTER TABLE aura.storage_objects
    ADD COLUMN status text NOT NULL DEFAULT 'live',
    ADD COLUMN pipeline_generation bigint NOT NULL DEFAULT 0,
    ADD COLUMN deletion_generation bigint NOT NULL DEFAULT 0,
    ADD COLUMN deletion_verified_at timestamptz;
ALTER TABLE aura.assets
    ADD COLUMN catalog_document_id uuid,
    ADD COLUMN document_version_id uuid,
    ADD COLUMN pipeline_generation bigint NOT NULL DEFAULT 0;
ALTER TABLE aura.document_chunks
    ADD COLUMN identity_id uuid,
    ADD COLUMN search_document_id text,
    ADD COLUMN version_number integer,
    ADD COLUMN original_sha256 text,
    ADD COLUMN pipeline_generation bigint NOT NULL DEFAULT 0,
    ADD COLUMN ordinal integer,
    ADD COLUMN normalized_text_sha256 text,
    ADD COLUMN self_ref text NOT NULL DEFAULT '',
    ADD COLUMN heading_path text[] NOT NULL DEFAULT '{}'::text[],
    ADD COLUMN captions text[] NOT NULL DEFAULT '{}'::text[],
    ADD COLUMN page_number integer,
    ADD COLUMN bounding_box jsonb,
    ADD COLUMN char_start integer,
    ADD COLUMN char_end integer,
    ADD COLUMN sheet_name text,
    ADD COLUMN table_name text,
    ADD COLUMN row_number integer,
    ADD COLUMN column_number integer,
    ADD COLUMN cell_ref text;
ALTER TABLE aura.document_embeddings
    ADD COLUMN identity_id uuid,
    ADD COLUMN pipeline_generation bigint NOT NULL DEFAULT 0,
    ADD COLUMN embedding_fingerprint text,
    ADD COLUMN projection_status text NOT NULL DEFAULT 'pending',
    ADD COLUMN projected_at timestamptz;
ALTER TABLE aura.ingestion_jobs
    ADD COLUMN identity_id uuid,
    ADD COLUMN asset_id uuid,
    ADD COLUMN pipeline_generation bigint NOT NULL DEFAULT 0,
    ADD COLUMN attempt_generation bigint NOT NULL DEFAULT 1,
    ADD COLUMN lease_generation bigint NOT NULL DEFAULT 0;
ALTER TABLE aura.ingestion_events
    ADD COLUMN identity_id uuid,
    ADD COLUMN pipeline_generation bigint NOT NULL DEFAULT 0,
    ADD COLUMN attempt_generation bigint NOT NULL DEFAULT 1,
    ADD COLUMN lease_generation bigint NOT NULL DEFAULT 0;
ALTER TABLE aura.delete_jobs
    ADD COLUMN identity_id uuid,
    ADD COLUMN idempotency_key text,
    ADD COLUMN delete_generation bigint NOT NULL DEFAULT 1,
    ADD COLUMN lease_generation bigint NOT NULL DEFAULT 0,
    ADD COLUMN error_code text NOT NULL DEFAULT '',
    ADD COLUMN error_message text NOT NULL DEFAULT '';
ALTER TABLE aura.document_ingest_jobs
    ADD COLUMN identity_id uuid,
    ADD COLUMN asset_id uuid,
    ADD COLUMN catalog_document_id uuid,
    ADD COLUMN version_id uuid,
    ADD COLUMN pipeline_generation bigint NOT NULL DEFAULT 0;
WITH candidates AS (
    SELECT id,
           NULLIF(btrim(metadata->>'search_document_id'), '') AS candidate,
           count(*) OVER (
               PARTITION BY identity_id, NULLIF(btrim(metadata->>'search_document_id'), '')
           ) AS copies
    FROM aura.documents
), resolved AS (
    SELECT id,
           CASE WHEN candidate IS NOT NULL AND copies = 1
                THEN candidate ELSE 'legacy:' || id::text END AS search_id,
           candidate IS NOT NULL AND copies > 1 AS ambiguous
    FROM candidates
)
UPDATE aura.documents d
SET search_document_id = r.search_id,
    status = CASE WHEN r.ambiguous AND d.status = 'ready' THEN 'failed' ELSE d.status END,
    error_code = CASE WHEN r.ambiguous THEN 'ambiguous_search_document_id' ELSE d.error_code END
FROM resolved r
WHERE r.id = d.id;
WITH source_candidates AS (
    SELECT v.document_id,
           (array_agg(a.source_kind ORDER BY v.version_number))[1] AS source_kind,
           (array_agg(a.source_ref ORDER BY v.version_number))[1] AS source_ref
    FROM aura.document_versions v
    JOIN aura.assets a ON a.id = v.asset_id
    WHERE btrim(a.source_ref) <> ''
    GROUP BY v.document_id
    HAVING count(DISTINCT (a.source_kind, a.source_ref)) = 1
)
UPDATE aura.documents d
SET source_kind = s.source_kind,
    source_key = 'sha256:' || encode(public.digest(s.source_ref, 'sha256'), 'hex')
FROM source_candidates s
WHERE s.document_id = d.id;
UPDATE aura.documents
SET source_kind = COALESCE(source_kind, 'legacy'),
    source_key = COALESCE(source_key, 'document:' || id::text);
WITH duplicated AS (
    SELECT identity_id, source_kind, source_key
    FROM aura.documents
    GROUP BY identity_id, source_kind, source_key HAVING count(*) > 1
)
UPDATE aura.documents d
SET source_kind = 'legacy', source_key = 'document:' || d.id::text,
    status = CASE WHEN d.status = 'ready' THEN 'failed' ELSE d.status END,
    error_code = 'ambiguous_source'
FROM duplicated x
WHERE (x.identity_id, x.source_kind, x.source_key) =
      (d.identity_id, d.source_kind, d.source_key);
UPDATE aura.document_versions v
SET identity_id = d.identity_id,
    search_document_id = d.search_document_id
FROM aura.documents d
WHERE d.id = v.document_id;
UPDATE aura.documents d
SET status = 'failed',
    error_code = 'original_unavailable',
    error_message = 'legacy ready row has no owner-coherent openable version',
    pipeline_generation = 0,
    updated_at = now()
WHERE d.status = 'ready'
  AND NOT EXISTS (
      SELECT 1
      FROM aura.document_versions v
      JOIN aura.storage_objects o ON o.id = v.storage_object_id
      JOIN aura.assets a ON a.id = v.asset_id
      WHERE v.id = d.active_version_id
        AND v.document_id = d.id
        AND v.identity_id = d.identity_id
        AND o.identity_id = d.identity_id
        AND o.deleted_at IS NULL
        AND a.identity_id = d.identity_id
        AND a.deleted_at IS NULL
        AND a.status NOT IN ('deleted', 'canceled')
  );
UPDATE aura.documents SET pipeline_generation = 1 WHERE status = 'ready';
UPDATE aura.document_versions v
SET pipeline_generation = d.pipeline_generation
FROM aura.documents d
WHERE d.active_version_id = v.id AND d.status = 'ready';
WITH ranked AS (
    SELECT v.id,
           row_number() OVER (
               PARTITION BY v.document_id, v.sha256
               ORDER BY (d.active_version_id = v.id) DESC, v.version_number, v.id
           ) AS copy_number
    FROM aura.document_versions v
    JOIN aura.documents d ON d.id = v.document_id
    WHERE v.deleted_at IS NULL
)
UPDATE aura.document_versions v
SET status = 'deleted', deleted_at = now(), error_code = 'duplicate_raw_version', updated_at = now()
FROM ranked r WHERE r.id = v.id AND r.copy_number > 1;
UPDATE aura.storage_objects
SET status = CASE WHEN deleted_at IS NULL THEN 'live' ELSE 'delete_pending' END;
WITH single_version AS (
    SELECT asset_id,
           (array_agg(id ORDER BY version_number))[1] AS version_id,
           (array_agg(document_id ORDER BY version_number))[1] AS document_id,
           (array_agg(pipeline_generation ORDER BY version_number))[1] AS generation
    FROM aura.document_versions
    WHERE asset_id IS NOT NULL AND deleted_at IS NULL
    GROUP BY asset_id
    HAVING count(*) = 1
)
UPDATE aura.assets a
SET catalog_document_id = s.document_id,
    document_version_id = s.version_id,
    pipeline_generation = s.generation
FROM single_version s
WHERE s.asset_id = a.id;
UPDATE aura.document_chunks c
SET identity_id = v.identity_id,
    search_document_id = v.search_document_id,
    version_number = v.version_number,
    original_sha256 = v.sha256,
    pipeline_generation = v.pipeline_generation,
    ordinal = c.chunk_index,
    normalized_text_sha256 = c.chunk_hash
FROM aura.document_versions v
WHERE v.id = c.version_id;
UPDATE aura.document_embeddings e
SET identity_id = c.identity_id,
    pipeline_generation = c.pipeline_generation,
    embedding_fingerprint = concat_ws(':', e.embedding_model, e.embedding_version, e.embedding_dim),
    projection_status = CASE WHEN e.active THEN 'active' ELSE 'pending' END,
    projected_at = CASE WHEN e.active THEN e.created_at ELSE NULL END
FROM aura.document_chunks c
WHERE c.id = e.chunk_id;
UPDATE aura.ingestion_jobs j
SET identity_id = d.identity_id
FROM aura.documents d
WHERE j.document_id = d.id;
UPDATE aura.ingestion_jobs j
SET identity_id = v.identity_id,
    asset_id = COALESCE(j.asset_id, v.asset_id),
    pipeline_generation = v.pipeline_generation
FROM aura.document_versions v
WHERE j.version_id = v.id AND (j.identity_id IS NULL OR j.identity_id = v.identity_id);
UPDATE aura.ingestion_events e
SET identity_id = j.identity_id,
    pipeline_generation = j.pipeline_generation,
    attempt_generation = j.attempt_generation,
    lease_generation = j.lease_generation
FROM aura.ingestion_jobs j
WHERE e.job_id = j.id AND j.identity_id IS NOT NULL;
UPDATE aura.delete_jobs j
SET identity_id = d.identity_id,
    idempotency_key = 'legacy:' || j.id::text
FROM aura.documents d
WHERE j.document_id = d.id;
UPDATE aura.delete_jobs j
SET identity_id = v.identity_id,
    idempotency_key = COALESCE(j.idempotency_key, 'legacy:' || j.id::text)
FROM aura.document_versions v
WHERE j.version_id = v.id AND (j.identity_id IS NULL OR j.identity_id = v.identity_id);
-- Preserve every row that cannot be mapped without guessing, then remove it from the live
-- tenant queues before applying NOT NULL and RLS.
INSERT INTO aura.document_pipeline_quarantine (source_table, source_pk, reason, row_data)
SELECT 'ingestion_events', id::text, 'no owner through ingestion_events.job_id FK', to_jsonb(e)
FROM aura.ingestion_events e WHERE identity_id IS NULL
ON CONFLICT DO NOTHING;
DELETE FROM aura.ingestion_events WHERE identity_id IS NULL;
INSERT INTO aura.document_pipeline_quarantine (source_table, source_pk, reason, row_data)
SELECT 'ingestion_jobs', id::text, 'no unambiguous document/version owner FK', to_jsonb(j)
FROM aura.ingestion_jobs j
WHERE identity_id IS NULL OR EXISTS (
    SELECT 1 FROM aura.document_versions v WHERE v.id = j.version_id
      AND (v.identity_id <> j.identity_id OR
           (j.document_id IS NOT NULL AND v.document_id <> j.document_id))
)
ON CONFLICT DO NOTHING;
DELETE FROM aura.ingestion_jobs j
WHERE identity_id IS NULL OR EXISTS (
    SELECT 1 FROM aura.document_versions v WHERE v.id = j.version_id
      AND (v.identity_id <> j.identity_id OR
           (j.document_id IS NOT NULL AND v.document_id <> j.document_id))
);
INSERT INTO aura.document_pipeline_quarantine (source_table, source_pk, reason, row_data)
SELECT 'delete_jobs', id::text, 'no unambiguous document/version owner FK', to_jsonb(j)
FROM aura.delete_jobs j WHERE identity_id IS NULL
ON CONFLICT DO NOTHING;
DELETE FROM aura.delete_jobs WHERE identity_id IS NULL;

INSERT INTO aura.document_pipeline_quarantine (source_table, source_pk, reason, row_data)
SELECT 'document_ingest_jobs', id::text, 'legacy table has no typed owner FK', to_jsonb(j)
FROM aura.document_ingest_jobs j
ON CONFLICT DO NOTHING;
DELETE FROM aura.document_ingest_jobs;

UPDATE aura.documents
SET status = CASE status
        WHEN 'draft' THEN 'accepted'
        WHEN 'processing' THEN 'converting'
        WHEN 'archived' THEN 'failed'
        ELSE status
    END,
    error_code = CASE WHEN status = 'archived' THEN 'legacy_archived' ELSE error_code END,
    updated_at = now()
WHERE status IN ('draft', 'processing', 'archived');

-- aura.documents carries a DEFERRABLE INITIALLY DEFERRED FK
-- (documents_active_version_id_fkey), so every UPDATE above queues an RI check event that
-- stays pending until COMMIT, and ALTER TABLE refuses to run while any are outstanding
-- (SQLSTATE 55006). Flush them here. No DML follows this point, so one flush covers the
-- rest of the migration.
SET CONSTRAINTS ALL IMMEDIATE;

ALTER TABLE aura.documents
    ALTER COLUMN source_kind SET NOT NULL,
    ALTER COLUMN source_key SET NOT NULL,
    ALTER COLUMN search_document_id SET NOT NULL,
    DROP CONSTRAINT documents_status_check,
    ADD CONSTRAINT documents_status_check CHECK (status IN (
        'accepted', 'stored', 'queued', 'converting', 'chunking', 'embedding',
        'projecting', 'ready', 'failed', 'dead_letter', 'deleting', 'deleted'
    )),
    ADD CONSTRAINT documents_source_kind_nonempty CHECK (btrim(source_kind) <> ''),
    ADD CONSTRAINT documents_source_key_nonempty CHECK (btrim(source_key) <> ''),
    ADD CONSTRAINT documents_search_id_nonempty CHECK (btrim(search_document_id) <> ''),
    ADD CONSTRAINT documents_generation_valid CHECK (
        pipeline_generation >= 0 AND (status <> 'ready' OR pipeline_generation > 0)
    ),
    ADD CONSTRAINT documents_id_identity_unique UNIQUE (id, identity_id);

CREATE UNIQUE INDEX documents_identity_search_document_live_idx
    ON aura.documents (identity_id, search_document_id)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX documents_identity_source_live_idx
    ON aura.documents (identity_id, source_kind, source_key)
    WHERE deleted_at IS NULL;

CREATE FUNCTION aura.document_identity_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
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

CREATE TRIGGER documents_identity_immutable
BEFORE UPDATE ON aura.documents
FOR EACH ROW EXECUTE FUNCTION aura.document_identity_immutable();

ALTER TABLE aura.document_versions
    ALTER COLUMN identity_id SET NOT NULL,
    ALTER COLUMN search_document_id SET NOT NULL,
    ADD CONSTRAINT document_versions_generation_valid CHECK (pipeline_generation >= 0),
    ADD CONSTRAINT document_versions_id_identity_unique UNIQUE (id, identity_id),
    ADD CONSTRAINT document_versions_document_identity_fkey
        FOREIGN KEY (document_id, identity_id)
        REFERENCES aura.documents(id, identity_id) ON DELETE CASCADE;

CREATE UNIQUE INDEX document_versions_document_sha256_live_idx
    ON aura.document_versions (document_id, sha256)
    WHERE deleted_at IS NULL;

ALTER TABLE aura.assets
    DROP CONSTRAINT assets_status_check,
    ADD CONSTRAINT assets_status_check CHECK (status IN (
        'created', 'presigned', 'uploaded', 'accepted', 'processing', 'searchable',
        'embedding', 'complete', 'failed', 'refused', 'deleting', 'deleted', 'canceled'
    )),
    ADD CONSTRAINT assets_generation_valid CHECK (pipeline_generation >= 0),
    ADD CONSTRAINT assets_id_identity_unique UNIQUE (id, identity_id);

ALTER TABLE aura.storage_objects
    DROP CONSTRAINT storage_objects_kind_check,
    ADD CONSTRAINT storage_objects_kind_check CHECK (kind IN (
        'raw', 'converted_document', 'extracted_text', 'chunks', 'chunk_manifest',
        'embedding_manifest', 'preview', 'temp', 'failed_artifact'
    )),
    ADD CONSTRAINT storage_objects_status_check CHECK (status IN (
        'live', 'delete_pending', 'object_deleted'
    )),
    ADD CONSTRAINT storage_objects_generation_valid CHECK (
        pipeline_generation >= 0 AND deletion_generation >= 0
    ),
    ADD CONSTRAINT storage_objects_id_identity_unique UNIQUE (id, identity_id),
    ADD CONSTRAINT storage_objects_document_identity_fkey
        FOREIGN KEY (document_id, identity_id)
        REFERENCES aura.documents(id, identity_id) ON DELETE SET NULL (document_id),
    ADD CONSTRAINT storage_objects_asset_identity_fkey
        FOREIGN KEY (asset_id, identity_id)
        REFERENCES aura.assets(id, identity_id) ON DELETE SET NULL (asset_id);

ALTER TABLE aura.document_versions
    ADD CONSTRAINT document_versions_asset_identity_fkey
        FOREIGN KEY (asset_id, identity_id)
        REFERENCES aura.assets(id, identity_id) ON DELETE SET NULL (asset_id),
    ADD CONSTRAINT document_versions_storage_identity_fkey
        FOREIGN KEY (storage_object_id, identity_id)
        REFERENCES aura.storage_objects(id, identity_id)
        DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE aura.storage_objects
    ADD CONSTRAINT storage_objects_version_identity_fkey
        FOREIGN KEY (version_id, identity_id)
        REFERENCES aura.document_versions(id, identity_id)
        ON DELETE SET NULL (version_id)
        DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE aura.documents
    ADD CONSTRAINT documents_active_version_identity_fkey
        FOREIGN KEY (active_version_id, identity_id)
        REFERENCES aura.document_versions(id, identity_id)
        DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE aura.assets
    ADD CONSTRAINT assets_catalog_document_identity_fkey
        FOREIGN KEY (catalog_document_id, identity_id)
        REFERENCES aura.documents(id, identity_id) ON DELETE SET NULL (catalog_document_id),
    ADD CONSTRAINT assets_version_identity_fkey
        FOREIGN KEY (document_version_id, identity_id)
        REFERENCES aura.document_versions(id, identity_id) ON DELETE SET NULL (document_version_id);

ALTER TABLE aura.document_chunks
    ALTER COLUMN identity_id SET NOT NULL,
    ALTER COLUMN search_document_id SET NOT NULL,
    ALTER COLUMN version_number SET NOT NULL,
    ALTER COLUMN original_sha256 SET NOT NULL,
    ALTER COLUMN ordinal SET NOT NULL,
    ALTER COLUMN normalized_text_sha256 SET NOT NULL,
    DROP CONSTRAINT document_chunks_version_id_chunk_hash_key,
    ADD CONSTRAINT document_chunks_locator_valid CHECK (
        ordinal >= 0 AND version_number > 0 AND pipeline_generation >= 0
        AND (page_number IS NULL OR page_number > 0)
        AND (char_start IS NULL OR char_start >= 0)
        AND (char_end IS NULL OR char_end >= COALESCE(char_start, 0))
    ),
    ADD CONSTRAINT document_chunks_id_identity_unique UNIQUE (id, identity_id),
    ADD CONSTRAINT document_chunks_version_ordinal_unique
        UNIQUE (identity_id, version_id, ordinal),
    ADD CONSTRAINT document_chunks_document_identity_fkey
        FOREIGN KEY (document_id, identity_id)
        REFERENCES aura.documents(id, identity_id) ON DELETE CASCADE,
    ADD CONSTRAINT document_chunks_version_identity_fkey
        FOREIGN KEY (version_id, identity_id)
        REFERENCES aura.document_versions(id, identity_id) ON DELETE CASCADE;

ALTER TABLE aura.document_embeddings
    ALTER COLUMN identity_id SET NOT NULL,
    ALTER COLUMN embedding_fingerprint SET NOT NULL,
    DROP CONSTRAINT document_embeddings_chunk_id_embedding_model_embedding_vers_key,
    ADD CONSTRAINT document_embeddings_generation_valid CHECK (pipeline_generation >= 0),
    ADD CONSTRAINT document_embeddings_projection_status_check CHECK (
        projection_status IN ('pending', 'projecting', 'active', 'tombstoned', 'failed')
    ),
    ADD CONSTRAINT document_embeddings_projection_unique
        UNIQUE (identity_id, chunk_id, pipeline_generation, embedding_fingerprint),
    ADD CONSTRAINT document_embeddings_document_identity_fkey
        FOREIGN KEY (document_id, identity_id)
        REFERENCES aura.documents(id, identity_id) ON DELETE CASCADE,
    ADD CONSTRAINT document_embeddings_version_identity_fkey
        FOREIGN KEY (version_id, identity_id)
        REFERENCES aura.document_versions(id, identity_id) ON DELETE CASCADE,
    ADD CONSTRAINT document_embeddings_chunk_identity_fkey
        FOREIGN KEY (chunk_id, identity_id)
        REFERENCES aura.document_chunks(id, identity_id) ON DELETE CASCADE;

ALTER TABLE aura.ingestion_jobs
    ALTER COLUMN identity_id SET NOT NULL,
    DROP CONSTRAINT ingestion_jobs_job_type_idempotency_key_key,
    ADD CONSTRAINT ingestion_jobs_generation_valid CHECK (
        pipeline_generation >= 0 AND attempt_generation > 0 AND lease_generation >= 0
    ),
    ADD CONSTRAINT ingestion_jobs_id_identity_unique UNIQUE (id, identity_id),
    ADD CONSTRAINT ingestion_jobs_identity_idempotency_unique
        UNIQUE (identity_id, job_type, idempotency_key),
    ADD CONSTRAINT ingestion_jobs_identity_id_fkey
        FOREIGN KEY (identity_id) REFERENCES aura.identities(id) ON DELETE CASCADE,
    ADD CONSTRAINT ingestion_jobs_document_identity_fkey
        FOREIGN KEY (document_id, identity_id)
        REFERENCES aura.documents(id, identity_id) ON DELETE CASCADE,
    ADD CONSTRAINT ingestion_jobs_version_identity_fkey
        FOREIGN KEY (version_id, identity_id)
        REFERENCES aura.document_versions(id, identity_id) ON DELETE CASCADE,
    ADD CONSTRAINT ingestion_jobs_asset_identity_fkey
        FOREIGN KEY (asset_id, identity_id)
        REFERENCES aura.assets(id, identity_id) ON DELETE CASCADE;

ALTER TABLE aura.ingestion_events
    ALTER COLUMN identity_id SET NOT NULL,
    ADD CONSTRAINT ingestion_events_generation_valid CHECK (
        pipeline_generation >= 0 AND attempt_generation > 0 AND lease_generation >= 0
    ),
    ADD CONSTRAINT ingestion_events_identity_id_fkey
        FOREIGN KEY (identity_id) REFERENCES aura.identities(id) ON DELETE CASCADE,
    ADD CONSTRAINT ingestion_events_job_identity_fkey
        FOREIGN KEY (job_id, identity_id)
        REFERENCES aura.ingestion_jobs(id, identity_id) ON DELETE SET NULL (job_id);

ALTER TABLE aura.delete_jobs
    ALTER COLUMN identity_id SET NOT NULL,
    ALTER COLUMN idempotency_key SET NOT NULL,
    ADD CONSTRAINT delete_jobs_generation_valid CHECK (
        delete_generation > 0 AND lease_generation >= 0
    ),
    ADD CONSTRAINT delete_jobs_identity_idempotency_unique
        UNIQUE (identity_id, idempotency_key),
    ADD CONSTRAINT delete_jobs_identity_id_fkey
        FOREIGN KEY (identity_id) REFERENCES aura.identities(id) ON DELETE CASCADE,
    ADD CONSTRAINT delete_jobs_document_identity_fkey
        FOREIGN KEY (document_id, identity_id)
        REFERENCES aura.documents(id, identity_id) ON DELETE SET NULL (document_id),
    ADD CONSTRAINT delete_jobs_version_identity_fkey
        FOREIGN KEY (version_id, identity_id)
        REFERENCES aura.document_versions(id, identity_id) ON DELETE SET NULL (version_id);

ALTER TABLE aura.document_ingest_jobs
    ALTER COLUMN identity_id SET NOT NULL,
    ADD CONSTRAINT document_ingest_jobs_generation_valid CHECK (pipeline_generation >= 0),
    ADD CONSTRAINT document_ingest_jobs_identity_id_fkey
        FOREIGN KEY (identity_id) REFERENCES aura.identities(id) ON DELETE CASCADE,
    ADD CONSTRAINT document_ingest_jobs_asset_identity_fkey
        FOREIGN KEY (asset_id, identity_id)
        REFERENCES aura.assets(id, identity_id) ON DELETE CASCADE,
    ADD CONSTRAINT document_ingest_jobs_document_identity_fkey
        FOREIGN KEY (catalog_document_id, identity_id)
        REFERENCES aura.documents(id, identity_id) ON DELETE CASCADE,
    ADD CONSTRAINT document_ingest_jobs_version_identity_fkey
        FOREIGN KEY (version_id, identity_id)
        REFERENCES aura.document_versions(id, identity_id) ON DELETE CASCADE;

DROP INDEX aura.document_ingest_jobs_source_document_hash_idx;
CREATE UNIQUE INDEX document_ingest_jobs_owner_source_document_hash_idx
    ON aura.document_ingest_jobs (identity_id, source_id, document_id, content_hash);

CREATE TABLE aura.document_pipeline_stages (
    id                         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id                uuid        NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE,
    document_id                uuid        NOT NULL,
    version_id                 uuid        NOT NULL,
    stage                      text        NOT NULL CHECK (stage IN (
        'identify', 'convert', 'chunk', 'embed', 'project', 'activate', 'card'
    )),
    input_fingerprint          text        NOT NULL CHECK (btrim(input_fingerprint) <> ''),
    producer_version           text        NOT NULL CHECK (btrim(producer_version) <> ''),
    pipeline_generation        bigint      NOT NULL CHECK (pipeline_generation >= 0),
    artifact_storage_object_id uuid,
    artifact_object_key        text        NOT NULL DEFAULT '',
    artifact_sha256            text        NOT NULL DEFAULT '',
    artifact_size_bytes        bigint      NOT NULL DEFAULT 0 CHECK (artifact_size_bytes >= 0),
    status                     text        NOT NULL CHECK (status IN (
        'pending', 'running', 'succeeded', 'failed', 'dead_letter'
    )),
    attempt_count              integer     NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    max_attempts               integer     NOT NULL DEFAULT 5 CHECK (max_attempts > 0),
    lease_generation           bigint      NOT NULL DEFAULT 0 CHECK (lease_generation >= 0),
    locked_by                  text,
    locked_until               timestamptz,
    next_attempt_at            timestamptz NOT NULL DEFAULT now(),
    error_class                text        NOT NULL DEFAULT '',
    error_code                 text        NOT NULL DEFAULT '',
    error_message              text        NOT NULL DEFAULT '',
    diagnostic_state           jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at                 timestamptz NOT NULL DEFAULT now(),
    updated_at                 timestamptz NOT NULL DEFAULT now(),
    completed_at               timestamptz,
    UNIQUE (identity_id, version_id, stage, input_fingerprint),
    FOREIGN KEY (document_id, identity_id)
        REFERENCES aura.documents(id, identity_id) ON DELETE CASCADE,
    FOREIGN KEY (version_id, identity_id)
        REFERENCES aura.document_versions(id, identity_id) ON DELETE CASCADE,
    FOREIGN KEY (artifact_storage_object_id, identity_id)
        REFERENCES aura.storage_objects(id, identity_id) ON DELETE SET NULL (artifact_storage_object_id)
);

CREATE INDEX document_pipeline_stages_claim_idx
    ON aura.document_pipeline_stages (identity_id, status, next_attempt_at, locked_until, created_at);
CREATE INDEX ingestion_jobs_owner_claim_idx
    ON aura.ingestion_jobs (identity_id, status, next_attempt_at, locked_until, created_at);
CREATE INDEX delete_jobs_owner_claim_idx
    ON aura.delete_jobs (identity_id, status, next_attempt_at, locked_until, created_at);

DROP POLICY document_versions_owner_isolation ON aura.document_versions;
CREATE POLICY document_versions_owner_isolation ON aura.document_versions
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);
DROP POLICY document_chunks_owner_isolation ON aura.document_chunks;
CREATE POLICY document_chunks_owner_isolation ON aura.document_chunks
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);
DROP POLICY document_embeddings_owner_isolation ON aura.document_embeddings;
CREATE POLICY document_embeddings_owner_isolation ON aura.document_embeddings
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);

DO $$
DECLARE
    target text;
BEGIN
    FOREACH target IN ARRAY ARRAY[
        'storage_objects', 'ingestion_jobs', 'ingestion_events', 'delete_jobs',
        'document_ingest_jobs', 'document_pipeline_stages'
    ]
    LOOP
        EXECUTE format('ALTER TABLE aura.%I ENABLE ROW LEVEL SECURITY', target);
        EXECUTE format($fmt$
            CREATE POLICY %I ON aura.%I
            USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid)
        $fmt$, target || '_owner_isolation', target);
        EXECUTE format($fmt$
            CREATE POLICY %I ON aura.%I AS RESTRICTIVE FOR ALL TO aura_app
            USING (current_setting('app.current_identity', true) IS NOT NULL
                   AND current_setting('app.current_identity', true) <> '')
        $fmt$, target || '_requires_identity', target);
    END LOOP;
END
$$;

COMMENT ON TABLE aura.document_pipeline_quarantine IS
    'Migrate-role-only preservation of legacy control-plane rows whose owner cannot be proven through typed foreign keys.';
COMMENT ON TABLE aura.document_pipeline_stages IS
    'Owner-scoped immutable-result stage ledger for identify, convert, chunk, embed, project, activate, and card derivation.';
