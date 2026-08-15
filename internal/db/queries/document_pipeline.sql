-- Reserves the version that owns these exact bytes AND writes the asset's storage-object
-- ledger row AND binds the asset to the resulting document+version -- in one statement.
--
-- One statement rather than four because the four could interleave: a repeat upload of
-- identical bytes would create a fresh raw object and then attach it to the OLD immutable
-- version, and a repeat ingest would redo conversion, embedding and projection that the
-- recorded version already owns.
--
-- The object INSERT names a version that does not exist yet, and the version names that
-- object, because both foreign keys are DEFERRABLE INITIALLY DEFERRED (0093:378-394) and
-- the remaining immediate one is checked at end of statement, by which time both rows are
-- present.
--
-- Data-modifying CTEs cannot observe each other's effects on the underlying tables, so
-- every hand-off here travels through a RETURNING projection -- `inserted` reads
-- `asset_object.id`, never aura.storage_objects a second time.
-- name: ReservePipelineCandidateVersion :one
WITH locked_document AS MATERIALIZED (
    SELECT document.id, document.identity_id, document.search_document_id,
           document.pipeline_generation, document.active_version_id
    FROM aura.documents document
    WHERE document.id = sqlc.arg(document_id)
      AND document.identity_id = sqlc.arg(identity_id)
      AND document.deleted_at IS NULL
      AND document.status NOT IN ('deleting', 'deleted')
    FOR UPDATE
), bound_asset AS MATERIALIZED (
    SELECT asset.id, asset.identity_id
    FROM aura.assets asset
    JOIN locked_document document ON document.identity_id = asset.identity_id
    WHERE asset.id = sqlc.arg(asset_id)
      AND asset.deleted_at IS NULL
      AND asset.status NOT IN ('refused', 'deleting', 'deleted', 'canceled')
), existing AS MATERIALIZED (
    -- replay_is_active is the ONLY safe permission to skip processing. Identity alone is
    -- not: a version can carry these bytes while still `processing`, and a caller that
    -- short-circuits on that publishes a document with no passages behind it.
    -- IS NOT DISTINCT FROM, not `=`: a document with no active version yet yields SQL NULL
    -- under `=`, and a NULL that decodes to Go false is a guarantee resting on luck rather
    -- than on the statement. This conjunction is total.
    SELECT version.*,
           (version.status = 'ready'
            AND version.activated_at IS NOT NULL
            AND document.active_version_id IS NOT DISTINCT FROM version.id) AS replay_is_active
    FROM aura.document_versions version
    JOIN locked_document document ON document.id = version.document_id
    JOIN aura.assets asset
      ON asset.id = version.asset_id
     AND asset.identity_id = version.identity_id
     AND asset.deleted_at IS NULL
     AND asset.status NOT IN ('refused', 'deleting', 'deleted', 'canceled')
    JOIN aura.storage_objects raw_object
      ON raw_object.id = version.storage_object_id
     AND raw_object.identity_id = version.identity_id
     AND raw_object.status = 'live'
     AND raw_object.deleted_at IS NULL
     AND raw_object.kind = 'raw'
    WHERE version.sha256 = sqlc.arg(sha256)
      AND version.deleted_at IS NULL
    LIMIT 1
), next_slot AS MATERIALIZED (
    SELECT document.id AS document_id, document.identity_id,
           document.search_document_id,
           COALESCE(max(version.version_number), 0)::integer + 1 AS version_number,
           GREATEST(
               document.pipeline_generation,
               COALESCE(max(version.pipeline_generation), 0),
               COALESCE(max(version.version_number), 0)
           ) + 1 AS pipeline_generation
    FROM locked_document document
    JOIN bound_asset asset ON asset.identity_id = document.identity_id
    LEFT JOIN aura.document_versions version ON version.document_id = document.id
    WHERE NOT EXISTS (SELECT 1 FROM existing)
    GROUP BY document.id, document.identity_id, document.search_document_id,
             document.pipeline_generation
), binding AS MATERIALIZED (
    -- Which version these bytes belong to, and therefore what the incoming object is,
    -- decided ONCE. Exactly one of `existing` and `next_slot` has a row, so the outer
    -- joins yield a single binding whenever the document and asset both qualify.
    --
    -- A replay's bytes DUPLICATE the version's raw object, so they are ledgered under a
    -- non-raw kind: `existing` resolves the raw object by kind, and a second 'raw' row
    -- would make that resolution ambiguous. The row still has to exist, because
    -- SoftDeleteDocument sweeps aura.storage_objects by document_id with no kind filter --
    -- without it this asset's bytes would outlive the document they belong to.
    SELECT document.id AS document_id, document.identity_id,
           asset.id AS asset_id,
           -- The ::uuid cast is not cosmetic: sqlc's static analyzer cannot name or type a
           -- SELECT-list expression that wraps a named parameter, and fails the whole file
           -- with "*ast.ResTarget has nil name" without it (sqlc-dev/sqlc#1646, #3991).
           COALESCE(existing.id, sqlc.arg(id)::uuid) AS version_id,
           CASE WHEN existing.id IS NULL THEN 'raw' ELSE 'temp' END AS object_kind,
           COALESCE(existing.pipeline_generation, slot.pipeline_generation) AS pipeline_generation
    FROM locked_document document
    JOIN bound_asset asset ON asset.identity_id = document.identity_id
    LEFT JOIN existing ON true
    LEFT JOIN next_slot slot ON true
), asset_object AS (
    INSERT INTO aura.storage_objects (
        identity_id, document_id, version_id, asset_id, bucket, object_key, kind,
        sha1, sha256, etag, size_bytes, content_type, retention_class, status,
        pipeline_generation
    )
    SELECT binding.identity_id, binding.document_id, binding.version_id, binding.asset_id,
           sqlc.arg(bucket), sqlc.arg(object_key), binding.object_kind,
           sqlc.arg(sha1), sqlc.arg(sha256), sqlc.arg(etag), sqlc.arg(size_bytes),
           sqlc.arg(content_type), sqlc.arg(retention_class), 'live',
           binding.pipeline_generation
    FROM binding
    -- (bucket, object_key) is unique schema-wide, so a re-drive of the same upload lands
    -- here. kind is deliberately NOT updated: demoting the version's own raw object to
    -- 'temp' would break the resolution `existing` depends on. The guard is stricter than
    -- CreateStorageObject's because this row is about to become a version's body -- other
    -- bytes under this key, or a key already scheduled for deletion, return no row and the
    -- whole statement resolves to ErrPipelineCandidateRejected rather than binding them.
    ON CONFLICT (bucket, object_key) DO UPDATE SET
        etag = EXCLUDED.etag,
        size_bytes = EXCLUDED.size_bytes,
        content_type = EXCLUDED.content_type,
        version_id = EXCLUDED.version_id,
        pipeline_generation = GREATEST(
            aura.storage_objects.pipeline_generation, EXCLUDED.pipeline_generation
        )
    WHERE aura.storage_objects.identity_id = EXCLUDED.identity_id
      AND aura.storage_objects.sha256 = EXCLUDED.sha256
      AND aura.storage_objects.status = 'live'
      AND aura.storage_objects.deleted_at IS NULL
    RETURNING id, identity_id
), inserted AS (
    INSERT INTO aura.document_versions (
        id, identity_id, document_id, asset_id, version_number, status,
        sha1, sha256, content_type, size_bytes, storage_object_id,
        chunking_config_hash, pipeline_config_hash, search_document_id,
        pipeline_generation
    )
    SELECT sqlc.arg(id), slot.identity_id, slot.document_id, sqlc.arg(asset_id),
           slot.version_number, sqlc.arg(status), sqlc.arg(sha1), sqlc.arg(sha256),
           sqlc.arg(content_type), sqlc.arg(size_bytes), object.id,
           sqlc.arg(chunking_config_hash), sqlc.arg(pipeline_config_hash),
           slot.search_document_id, slot.pipeline_generation
    FROM next_slot slot
    JOIN asset_object object ON object.identity_id = slot.identity_id
    -- Reaching a conflict here means a row already holds these bytes yet `existing`
    -- refused it -- its asset or its raw object is no longer live. That state is
    -- incoherent, not retryable in place, so it resolves to the statement's own empty
    -- result (ErrPipelineCandidateRejected) instead of a raw 23505 the caller would
    -- retry until dead-letter.
    ON CONFLICT DO NOTHING
    RETURNING *
), selected AS MATERIALIZED (
    SELECT existing.*, true AS replayed FROM existing
    UNION ALL
    SELECT inserted.*, false AS replay_is_active, false AS replayed FROM inserted
), linked_asset AS (
    UPDATE aura.assets asset
    SET document_id = selected.search_document_id,
        catalog_document_id = selected.document_id,
        document_version_id = selected.id,
        -- GREATEST, never assignment: binding a second asset onto an older replayed
        -- generation must not walk that asset's generation backwards, which is the same
        -- rule the document trigger enforces at 0093:321-323.
        pipeline_generation = GREATEST(
            asset.pipeline_generation, selected.pipeline_generation
        ),
        updated_at = now()
    FROM selected
    JOIN bound_asset ON bound_asset.identity_id = selected.identity_id
    -- The INCOMING asset, not selected.asset_id: on a replay those differ, and binding
    -- selected.asset_id would leave the uploader's asset attached to nothing.
    WHERE asset.id = bound_asset.id
      AND asset.identity_id = selected.identity_id
    RETURNING asset.id, asset.identity_id
)
SELECT selected.id, selected.document_id, selected.asset_id,
       selected.version_number, selected.status, selected.sha1, selected.sha256,
       selected.content_type, selected.size_bytes, selected.storage_object_id,
       selected.chunking_config_hash, selected.pipeline_config_hash,
       selected.ready_at, selected.activated_at, selected.created_at,
       selected.updated_at, selected.deleted_at, selected.error_code,
       selected.error_message, selected.identity_id, selected.search_document_id,
       selected.pipeline_generation, selected.replayed, selected.replay_is_active
FROM selected
JOIN asset_object ON asset_object.identity_id = selected.identity_id
JOIN linked_asset ON linked_asset.identity_id = selected.identity_id;
