-- Reverse 0017: drop the D-09 branch-tree pointer columns. IF EXISTS keeps the down
-- idempotent; dropping the columns also drops their COMMENTs. No data is preserved —
-- the branch topology is a pure additive overlay over the seq-ordered linear history,
-- which LoadHistory reconstructs from seq alone.
ALTER TABLE aura.conversation_turns
    DROP COLUMN IF EXISTS parent_seq;
ALTER TABLE aura.conversation_turns
    DROP COLUMN IF EXISTS branch_id;
