-- Source: PRD §Slice 0.7 file targets row + amendment #17 grant pattern.
-- Audit table consumed by Slice 0.7 internal/knowledge/migrate.go.

CREATE TABLE aura.knowledge_migrations (
    version    integer        PRIMARY KEY,
    name       text           NOT NULL,
    checksum   text           NOT NULL,                       -- SHA-256 of file content
    applied_at timestamptz    NOT NULL DEFAULT now()
);

-- Belt + suspenders (DEFAULT PRIVILEGES from 0001 should already cover this;
-- explicit grants documented for forensic clarity).
GRANT SELECT, INSERT ON aura.knowledge_migrations TO aura_app;
GRANT ALL            ON aura.knowledge_migrations TO aura_migrate;

CREATE INDEX knowledge_migrations_applied_at_idx
    ON aura.knowledge_migrations (applied_at DESC);

COMMENT ON TABLE aura.knowledge_migrations IS
    'Audit of applied Cypher migrations. Written by aura neo4j migrate; read by aura neo4j status.';
