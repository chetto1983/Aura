DROP TRIGGER IF EXISTS compaction_checkpoint_parent_guard ON aura.compaction_checkpoints;
DROP FUNCTION IF EXISTS aura.compaction_checkpoint_parent_guard();
DROP TABLE IF EXISTS aura.compaction_quarantine;
DROP TABLE IF EXISTS aura.compaction_restore_events;
DROP TABLE IF EXISTS aura.compaction_active_pointers;
ALTER TABLE IF EXISTS aura.compaction_claims DROP CONSTRAINT IF EXISTS compaction_claim_outcome_fkey;
DROP TABLE IF EXISTS aura.compaction_checkpoints;
DROP TABLE IF EXISTS aura.compaction_claims;
