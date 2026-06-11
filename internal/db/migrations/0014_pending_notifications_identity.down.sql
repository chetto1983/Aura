-- Reverse 0014: drop the additive identity_id column. Legacy rows had it NULL, so the
-- revert restores byte-identical pre-Phase-20 pending_notifications shape.
ALTER TABLE aura.pending_notifications DROP COLUMN IF EXISTS identity_id;
