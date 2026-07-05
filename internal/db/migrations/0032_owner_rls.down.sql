-- Reverse 0032: drop the paused_states auto-population trigger + function, drop every
-- owner-isolation policy, and DISABLE row-level security on the three tables, restoring
-- the pre-Phase-36 (RLS-free) access shape. The identity_id values backfilled into
-- aura.paused_states are LEFT in place — the column itself is owned by 0027, and the
-- values are harmless once no policy reads them (a re-up re-installs the trigger).

DROP TRIGGER IF EXISTS paused_states_set_identity_trg ON aura.paused_states;
DROP FUNCTION IF EXISTS aura.paused_states_set_identity();

DROP POLICY IF EXISTS conversation_turns_owner_isolation ON aura.conversation_turns;
ALTER TABLE aura.conversation_turns DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS paused_states_owner_isolation ON aura.paused_states;
ALTER TABLE aura.paused_states DISABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS conversations_owner_isolation ON aura.conversations;
ALTER TABLE aura.conversations DISABLE ROW LEVEL SECURITY;
