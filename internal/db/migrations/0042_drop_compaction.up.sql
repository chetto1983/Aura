-- Drop the dark Phase-42 durable semantic-compaction engine (Amendment #86, SPIKE = REMOVE).
-- The engine was dark end-to-end (FinalizeCompaction had zero non-test callers; the tables
-- held zero rows) and cost one P0 (BUG-6a, the rollout evaluator's 23505 crash-storm). The
-- original 0036/0038/0039 up+down files stay on disk (migration history is append-only); this
-- forward migration drops their 11 tables + 3 trigger functions in FK-safe order.
-- 0037_content_parts is independent (typed-multimodal) and untouched. No FK from any surviving
-- table points into these tables, so the drop dangles nothing. The .down recreates them verbatim
-- (the reversibility contract, TestMigrateSteps_DownUpReversible).

-- 0036 group (checkpoints/claims). Break the claims<->checkpoints outcome cycle first.
ALTER TABLE IF EXISTS aura.compaction_claims DROP CONSTRAINT IF EXISTS compaction_claim_outcome_fkey;
DROP TRIGGER IF EXISTS compaction_checkpoint_parent_guard ON aura.compaction_checkpoints;
DROP FUNCTION IF EXISTS aura.compaction_checkpoint_parent_guard();
DROP TABLE IF EXISTS aura.compaction_quarantine;
DROP TABLE IF EXISTS aura.compaction_restore_events;
DROP TABLE IF EXISTS aura.compaction_active_pointers;
DROP TABLE IF EXISTS aura.compaction_checkpoints;
DROP TABLE IF EXISTS aura.compaction_claims;

-- 0038 group (memory).
DROP TRIGGER IF EXISTS compaction_memory_source_immutable ON aura.compaction_memory_sources;
DROP FUNCTION IF EXISTS aura.compaction_memory_source_immutable();
DROP TABLE IF EXISTS aura.compaction_memories;
DROP TABLE IF EXISTS aura.compaction_memory_sources;
DROP TABLE IF EXISTS aura.compaction_memory_candidates;

-- 0039 group (rollout).
DROP TRIGGER IF EXISTS compaction_rollout_decisions_immutable ON aura.compaction_rollout_decisions;
DROP TRIGGER IF EXISTS compaction_rollout_evidence_immutable ON aura.compaction_rollout_evidence;
DROP FUNCTION IF EXISTS aura.compaction_rollout_ledger_immutable();
DROP TABLE IF EXISTS aura.compaction_rollout_decisions;
DROP TABLE IF EXISTS aura.compaction_rollout_evidence;
DROP TABLE IF EXISTS aura.compaction_rollout_states;
