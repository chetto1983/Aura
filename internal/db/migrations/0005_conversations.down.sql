-- Reverse 0005: restore paused_states to its 0003 shape (drop FK + resumed_answer,
-- revert conversation_id uuid -> text), then drop the three new tables.
ALTER TABLE aura.paused_states
    DROP CONSTRAINT IF EXISTS paused_states_conversation_id_fkey;
ALTER TABLE aura.paused_states
    DROP COLUMN IF EXISTS resumed_answer;
ALTER TABLE aura.paused_states
    ALTER COLUMN conversation_id TYPE text USING conversation_id::text;

DROP TABLE IF EXISTS aura.context_rot_events;
DROP TABLE IF EXISTS aura.conversation_turns;
DROP TABLE IF EXISTS aura.conversations;

-- pg_trgm was created in 0005 up; drop it here (the 0006 GIN index is already gone
-- by the time this down runs, since migrations roll back in reverse order).
DROP EXTENSION IF EXISTS pg_trgm;
