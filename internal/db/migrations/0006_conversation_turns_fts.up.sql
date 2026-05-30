-- Source: PRD §Slice 1.8.5 + phase 04-SPEC.md Req#13 + 04-RESEARCH.md Pitfall 6.
-- pg_trgm GIN index for conversation FTS. This file MUST contain exactly ONE
-- statement: CREATE INDEX CONCURRENTLY cannot run inside a transaction block, and
-- a multi-statement migration string is sent to Postgres as an implicit tx block.
-- The pg_trgm EXTENSION is created in 0005 (a tx-safe statement) precisely so this
-- file stays single-statement. IF NOT EXISTS keeps re-apply idempotent.
CREATE INDEX CONCURRENTLY IF NOT EXISTS conversation_turns_content_trgm
    ON aura.conversation_turns USING GIN (content gin_trgm_ops);
