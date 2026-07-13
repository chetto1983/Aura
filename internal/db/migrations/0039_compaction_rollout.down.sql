DROP TRIGGER IF EXISTS compaction_rollout_decisions_immutable ON aura.compaction_rollout_decisions;
DROP TRIGGER IF EXISTS compaction_rollout_evidence_immutable ON aura.compaction_rollout_evidence;
DROP FUNCTION IF EXISTS aura.compaction_rollout_ledger_immutable();
DROP TABLE IF EXISTS aura.compaction_rollout_decisions;
DROP TABLE IF EXISTS aura.compaction_rollout_evidence;
DROP TABLE IF EXISTS aura.compaction_rollout_states;
