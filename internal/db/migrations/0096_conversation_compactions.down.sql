-- Roll back durable conversation compaction.
--
-- No guard clause: every row here is derived. The summary was computed from
-- aura.conversation_turns, which this migration never touched and which still holds
-- every turn, so dropping the table costs the next compaction one auxiliary LLM call
-- and costs the operator nothing. Contrast 0093's rollback, which refuses when verified
-- deletion has happened, because there the rows ARE the record of an irreversible act.

DROP TABLE IF EXISTS aura.conversation_compactions;
