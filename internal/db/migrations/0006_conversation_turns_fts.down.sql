-- Single statement (DROP INDEX CONCURRENTLY also cannot run inside a tx block).
-- The pg_trgm extension is dropped by 0005 down (it was created there); leaving it
-- here would be a second statement and re-introduce the tx-block hazard.
DROP INDEX CONCURRENTLY IF EXISTS aura.conversation_turns_content_trgm;
