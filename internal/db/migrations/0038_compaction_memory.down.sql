DROP TABLE IF EXISTS aura.compaction_memories;
DROP TRIGGER IF EXISTS compaction_memory_source_immutable ON aura.compaction_memory_sources;
DROP FUNCTION IF EXISTS aura.compaction_memory_source_immutable();
DROP TABLE IF EXISTS aura.compaction_memory_sources;
DROP TABLE IF EXISTS aura.compaction_memory_candidates;
