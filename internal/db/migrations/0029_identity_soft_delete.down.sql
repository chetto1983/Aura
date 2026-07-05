-- Reverse 0029: drop the additive soft-delete columns. Live identities had them
-- NULL, so the revert restores the byte-identical pre-Phase-36 aura.identities shape.
ALTER TABLE aura.identities DROP COLUMN IF EXISTS purge_after;
ALTER TABLE aura.identities DROP COLUMN IF EXISTS deactivated_at;
