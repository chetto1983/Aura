-- RESET, not `SET (fastupdate = on)`.
--
-- 0082 created the index with no storage parameter at all, so its prior state was
-- an EMPTY reloptions — not `{fastupdate=on}`. Writing the default back explicitly
-- would leave a reloption behind that was never there and would make
-- `select reloptions from pg_class` disagree with a database migrated fresh to 0084.
-- ALTER INDEX ... RESET drops the entry and the index falls back to the documented
-- default, which CREATE INDEX states is `ON` — the same behaviour, recorded the
-- same way.
ALTER INDEX aura.documents_digest_tsv_idx RESET (fastupdate);

-- The pending-list flush in the up step is deliberately NOT undone. Emptying it
-- moved entries into the main GIN structure, where they are simply indexed; there
-- is no operation that puts them back, and inventing one would corrupt nothing but
-- would slow the index down for no reason. Rolling back restores the SETTING, which
-- is the thing this migration changed.
