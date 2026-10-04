-- `searchable` and `embedding` stop being asset statuses. Slot measured with
-- `ls internal/db/migrations/ | tail -1`: 0135 was the head.
--
-- WHY. Both belonged to the in-process ingestion pipeline. It was deleted, searchability
-- became ArcadeDB's answer (the knowledge catalog asks the index, not this column), and
-- nothing has written either value to aura.assets since. They survived anyway: in this CHECK,
-- in searchable_at, in the Studio's asset filter and in the cockpit's status union. Measured
-- 2026-10-04 on a local database: no row in either status; the live deployment counted 0
-- `searchable` on 2026-08-13.
--
-- A row that still holds one is not rewritten. Neither value says what the asset became, so
-- the migration stops and names the count instead of guessing. The file runs as one implicit
-- transaction, so the schema stays at 0135; golang-migrate marks the tracker dirty, and the
-- message says how to clear it.

DO $$
DECLARE
    stranded bigint;
BEGIN
    SELECT count(*) INTO stranded FROM aura.assets WHERE status IN ('searchable', 'embedding');
    IF stranded > 0 THEN
        RAISE EXCEPTION 'migration 0136: % aura.assets rows still hold status searchable or embedding. Move each to the status it really has, mark the tracker clean at 135 (UPDATE schema_migrations SET version = 135, dirty = false) and migrate again', stranded;
    END IF;
END
$$;

ALTER TABLE aura.assets
    DROP CONSTRAINT assets_status_check,
    ADD CONSTRAINT assets_status_check CHECK (status IN (
        'created', 'presigned', 'uploaded', 'accepted', 'processing',
        'complete', 'failed', 'refused', 'deleting', 'deleted', 'canceled'
    )),
    DROP COLUMN searchable_at;
