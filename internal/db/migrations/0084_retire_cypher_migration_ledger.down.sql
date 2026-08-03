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

-- Restoring the pre-0084 comment is EXISTENCE-GUARDED, and that guard is load-bearing
-- rather than defensive style. A downward walk reaches this file with later migrations
-- already reverted, so a bare COMMENT ON here raised 42P01 and left the database DIRTY at
-- 83. Measured on 2026-08-02: that single failure cascaded into 177 failures across the
-- whole db_integration tier, because every later package's Migrate then refused a dirty
-- database. A comment is cosmetic; it must never be the statement that wedges a rollback.
--
-- The aura.adaptive_outbox arm of this block went with the adaptive migrations themselves
-- on 2026-08-03. Its guard could only ever be false now, and a branch that cannot be taken
-- is dead code even when it is three lines of SQL.
DO $$
BEGIN
    IF to_regclass('aura.document_ingest_jobs') IS NOT NULL THEN
        COMMENT ON TABLE aura.document_ingest_jobs IS
            'Durable document ingestion job state. Neo4j stores document/chunk graph data; Postgres tracks lifecycle and progress.';
    END IF;
END
$$;
