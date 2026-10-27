-- Drops four tables and three columns that no production code reads or writes. Slot measured
-- with `ls internal/db/migrations/ | tail -1`: 0142 was the head.
--
-- WHY. The 2026-10-26 scan (docs/unwired-and-duplicates-2026-10-26.md) found each one, and each
-- was read in the source before it was dropped:
-- - aura.benchmark_settings_overrides and aura.benchmark_settings_override_rows: the journal of
--   a benchmark that overrode three LLM settings. Its eleven queries had no caller and went in
--   d32a4d1.
-- - aura.asset_events: its insert query had no caller (also d32a4d1), and nothing reads it.
-- - aura.audit_logs: nothing outside tests wrote or read it, and its ON DELETE RESTRICT key
--   would have failed every de-provisioning once something did (serve_provisioning.go).
-- - pipeline_generation on aura.assets, aura.ingestion_jobs and aura.ingestion_events: the
--   generation of the document catalog 0098 retired. No producer sets it, so new rows hold 0.
--   A non-zero value can only be 0093's backfill from aura.documents, which no longer exists,
--   so the column goes whatever it holds.
-- Measured 2026-10-27 on a database migrated to 0142: all four tables empty, every one owned by
-- aura_migrate without FORCE ROW LEVEL SECURITY, so the count below sees every row.
--
-- A table that still holds a row is not emptied. A row there means "nothing writes it" was
-- wrong on this deployment, and an open benchmark journal is a settings override still waiting
-- to be restored. The migration stops and names the counts instead. The file runs as one
-- implicit transaction, so the schema stays at 0142; golang-migrate marks the tracker dirty,
-- and the message says how to clear it.

-- The count and the drops run under one lock, so a row a writer commits between them cannot
-- be dropped unseen. The file is one implicit transaction, which holds it to the end.
LOCK TABLE aura.benchmark_settings_overrides, aura.benchmark_settings_override_rows,
    aura.asset_events, aura.audit_logs IN ACCESS EXCLUSIVE MODE;

DO $$
DECLARE
    held text;
BEGIN
    SELECT string_agg(format('%s %s', n, t), ', ') INTO held FROM (
        SELECT 'aura.benchmark_settings_overrides' AS t, count(*) AS n
            FROM aura.benchmark_settings_overrides
        UNION ALL SELECT 'aura.benchmark_settings_override_rows', count(*)
            FROM aura.benchmark_settings_override_rows
        UNION ALL SELECT 'aura.asset_events', count(*) FROM aura.asset_events
        UNION ALL SELECT 'aura.audit_logs', count(*) FROM aura.audit_logs
    ) counts WHERE n > 0;
    IF held IS NOT NULL THEN
        RAISE EXCEPTION 'migration 0143: rows in tables it would drop: %. Keep a copy (pg_dump --data-only -t <table>), delete them, mark the tracker clean at 142 (UPDATE schema_migrations SET version = 142, dirty = false) and migrate again', held;
    END IF;
END
$$;

DROP TABLE aura.benchmark_settings_override_rows;
DROP TABLE aura.benchmark_settings_overrides;
DROP TABLE aura.asset_events;
DROP TABLE aura.audit_logs;

ALTER TABLE aura.assets
    DROP CONSTRAINT assets_generation_valid,
    DROP COLUMN pipeline_generation;

-- The queue's CHECK also bounds the attempt and lease fences. Dropping the column would take
-- the whole constraint with it, so it is restated without the column.
ALTER TABLE aura.ingestion_jobs
    DROP CONSTRAINT ingestion_jobs_generation_valid,
    DROP COLUMN pipeline_generation,
    ADD CONSTRAINT ingestion_jobs_generation_valid
        CHECK (attempt_generation > 0 AND lease_generation >= 0);

ALTER TABLE aura.ingestion_events
    DROP CONSTRAINT ingestion_events_generation_valid,
    DROP COLUMN pipeline_generation,
    ADD CONSTRAINT ingestion_events_generation_valid
        CHECK (attempt_generation > 0 AND lease_generation >= 0);
