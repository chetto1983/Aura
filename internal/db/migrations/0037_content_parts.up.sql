CREATE TABLE aura.content_parts (
    id uuid PRIMARY KEY,
    storage_id text NOT NULL UNIQUE,
    tenant_id uuid NOT NULL,
    owner_id uuid NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE,
    mime_type text NOT NULL,
    digest_sha256 text NOT NULL CHECK (length(digest_sha256) = 64),
    byte_length bigint NOT NULL CHECK (byte_length >= 0),
    encryption_class text NOT NULL DEFAULT 'identity',
    retention_class text NOT NULL DEFAULT 'durable',
    provider_requirements jsonb NOT NULL DEFAULT '[]'::jsonb,
    fallback_text text NOT NULL DEFAULT '',
    state text NOT NULL DEFAULT 'available' CHECK (state IN ('available','quarantined','migrated')),
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE aura.content_part_links (
    part_id uuid NOT NULL REFERENCES aura.content_parts(id) ON DELETE RESTRICT,
    reachability_kind text NOT NULL CHECK (reachability_kind IN ('turn','checkpoint','memory')),
    reachability_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (part_id, reachability_kind, reachability_id)
);
CREATE OR REPLACE FUNCTION aura.content_part_link_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'content part links are immutable'; END $$;
CREATE TRIGGER content_part_link_immutable BEFORE UPDATE OR DELETE ON aura.content_part_links
FOR EACH ROW EXECUTE FUNCTION aura.content_part_link_immutable();
CREATE INDEX content_parts_gc_idx ON aura.content_parts(expires_at) WHERE state = 'available';
GRANT SELECT, INSERT, UPDATE ON aura.content_parts TO aura_app;
GRANT SELECT, INSERT ON aura.content_part_links TO aura_app;
GRANT ALL ON aura.content_parts, aura.content_part_links TO aura_migrate;
