-- Deliberately empty.
--
-- The up migration moves rows to 'complete', the status the document processor itself now
-- writes; it adds no structure to remove. The table does not record which rows were
-- 'processing' before, and putting any back would restore a status nothing moves on from.
-- The up migration is idempotent (its WHERE clause matches nothing on a second run), so
-- re-applying it after a rollback is safe.
SELECT 1;
