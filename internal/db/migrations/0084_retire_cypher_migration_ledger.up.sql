-- aura.knowledge_migrations was the audit ledger of applied Cypher migrations: which
-- version ran, its checksum, when. There are no Cypher migrations. The graph store, its
-- migration sequence and the CLI that wrote this table were all removed, so every future
-- read returns the same frozen rows describing a database that is gone — and sqlc keeps
-- generating a model for it, which is how a dead table stays alive in the codebase.
--
-- Dropping it is safe in the way that matters: nothing writes it, nothing reads it, and
-- it is a LEDGER, not data. Its rows only ever meant "this Cypher file already ran
-- against a store you no longer have". The down migration recreates the empty shape so a
-- rollback lands on a schema golang-migrate recognises, not on a missing relation.
DROP TABLE IF EXISTS aura.knowledge_migrations;

-- The comment below describes a topology that no longer exists. It is not decoration:
-- sqlc copies COMMENT ON straight into internal/db/sqlc/models.go, so a stale comment
-- here is a stale comment shipped in generated Go that nobody can fix by editing the
-- generated file.
--
-- A second COMMENT ON, for aura.adaptive_outbox, stood here until 2026-08-03. It was
-- removed with the twenty-five migrations that created the adaptive plane and the one
-- that dropped it again: no migration in this tree creates that table any more, so the
-- statement addressed nothing and made a fresh Migrate fail at 42P01.
COMMENT ON TABLE aura.document_ingest_jobs IS
    'Durable document ingestion job state: lifecycle, idempotency and operator-visible progress. Postgres is the only store — a document is one catalog row plus a digest, and the file itself lives in the object store.';
