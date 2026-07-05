-- Reverse 0027: drop the identity index and the additive identity_id column.
-- Legacy rows had it NULL/backfilled, so the revert restores the byte-identical
-- pre-Phase-36 aura.paused_states shape.
DROP INDEX IF EXISTS aura.paused_states_identity_idx;
ALTER TABLE aura.paused_states DROP COLUMN IF EXISTS identity_id;
