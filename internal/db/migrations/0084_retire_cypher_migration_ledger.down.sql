-- Recreate the empty ledger shape (grants and index included, as 0002 made it) so a
-- rollback lands on a schema golang-migrate recognises. The ROWS do not come back: they
-- recorded which Cypher file ran against a store that no longer exists, and inventing
-- them would be worse than an empty table.
CREATE TABLE IF NOT EXISTS aura.knowledge_migrations (
    version    integer        PRIMARY KEY,
    name       text           NOT NULL,
    checksum   text           NOT NULL,
    applied_at timestamptz    NOT NULL DEFAULT now()
);

GRANT SELECT, INSERT ON aura.knowledge_migrations TO aura_app;
GRANT ALL            ON aura.knowledge_migrations TO aura_migrate;

CREATE INDEX IF NOT EXISTS knowledge_migrations_applied_at_idx
    ON aura.knowledge_migrations (applied_at DESC);

COMMENT ON TABLE aura.document_ingest_jobs IS
    'Durable document ingestion job state. Neo4j stores document/chunk graph data; Postgres tracks lifecycle and progress.';

COMMENT ON TABLE aura.adaptive_outbox IS
    'Authoritative owner-scoped adaptive decision/outcome outbox. Neo4j is a replayable projection.';
