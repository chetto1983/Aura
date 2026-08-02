-- Deliberate one-way door.
--
-- The up migration drops twelve tables, 43 functions and ~20 triggers that
-- migrations 0052-0076 built up over twenty-five steps. Re-emitting that DDL here
-- would be a second, unversioned copy of those migrations, guaranteed to drift
-- from them and describing a plane whose Go code no longer exists — nothing could
-- read or write the tables even if they came back.
--
-- So this rollback restores nothing and says so, rather than fabricating a schema
-- that would look recoverable and not be. The precedent is 0083/0084: a down
-- migration restores schema admissibility where that is meaningful and refuses to
-- invent deleted data. Here not even the shape is meaningful.
--
-- To genuinely go back: restore from a pg_dump taken before this migration, and
-- revert the code alongside it.
--
-- Rolling back FURTHER than this migration will fail on 0084's down, which
-- COMMENTs on aura.adaptive_outbox. That is a real limitation of stepping back
-- through a decommission, not an oversight.
DO $$
BEGIN
    RAISE NOTICE
        'migration 0086 is irreversible: the adaptive-learning plane was dropped, not archived. Restore from a pre-0086 pg_dump if you need it back.';
END $$;
