CREATE TABLE aura.assets (
    id                  uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id         uuid        NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE,
    source_kind         text        NOT NULL CHECK (source_kind IN ('web', 'telegram', 'cli')),
    source_ref          text        NOT NULL DEFAULT '',
    thread_id           text        NOT NULL DEFAULT '',
    scope               text        NOT NULL CHECK (scope IN ('thread', 'library')),
    modality            text        NOT NULL CHECK (modality IN ('document', 'image', 'audio', 'unknown')),
    status              text        NOT NULL CHECK (status IN (
        'created', 'presigned', 'uploaded', 'accepted', 'processing',
        'searchable', 'embedding', 'complete', 'failed', 'refused',
        'deleted', 'canceled'
    )),
    file_name           text        NOT NULL,
    mime_type           text        NOT NULL DEFAULT 'application/octet-stream',
    declared_size_bytes bigint      NOT NULL DEFAULT 0 CHECK (declared_size_bytes >= 0),
    size_bytes          bigint      NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
    content_hash        text        NOT NULL DEFAULT '',
    object_bucket       text        NOT NULL,
    object_key          text        NOT NULL,
    object_etag         text        NOT NULL DEFAULT '',
    document_id         text        NOT NULL DEFAULT '',
    summary             text        NOT NULL DEFAULT '',
    metadata            jsonb       NOT NULL DEFAULT '{}'::jsonb,
    error_code          text        NOT NULL DEFAULT '',
    error_message       text        NOT NULL DEFAULT '',
    created_at          timestamptz NOT NULL DEFAULT now(),
    uploaded_at         timestamptz,
    accepted_at         timestamptz,
    processed_at        timestamptz,
    searchable_at       timestamptz,
    completed_at        timestamptz,
    deleted_at          timestamptz,
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE aura.asset_events (
    asset_id     uuid        NOT NULL REFERENCES aura.assets(id) ON DELETE CASCADE,
    seq          integer     NOT NULL CHECK (seq > 0),
    from_status  text        NOT NULL DEFAULT '',
    to_status    text        NOT NULL,
    reason       text        NOT NULL DEFAULT '',
    detail       jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (asset_id, seq)
);

CREATE INDEX assets_identity_created_idx
    ON aura.assets (identity_id, created_at DESC);

CREATE INDEX assets_thread_created_idx
    ON aura.assets (thread_id, created_at ASC)
    WHERE thread_id <> '';

CREATE INDEX assets_identity_scope_created_idx
    ON aura.assets (identity_id, scope, created_at DESC);

CREATE INDEX assets_identity_content_hash_idx
    ON aura.assets (identity_id, content_hash)
    WHERE content_hash <> '';

CREATE INDEX assets_status_created_idx
    ON aura.assets (status, created_at ASC);

CREATE UNIQUE INDEX assets_identity_object_key_idx
    ON aura.assets (identity_id, object_key);

GRANT SELECT, INSERT, UPDATE ON aura.assets TO aura_app;
GRANT SELECT, INSERT ON aura.asset_events TO aura_app;
GRANT ALL ON aura.assets TO aura_migrate;
GRANT ALL ON aura.asset_events TO aura_migrate;
