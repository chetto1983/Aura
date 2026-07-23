-- Reasoning persisted for display (PRD Amendment #91 / fix-plan 1.12). The turn's
-- accumulated chain-of-thought is stored DISPLAY-ONLY on the final assistant answer
-- row so the cockpit ReasoningDrawer survives reload/thread-switch; the LLM context
-- rebuild (LoadHistory -> llm.Message) never selects these columns, so the model can
-- structurally never see its own persisted CoT. Both columns nullable: NULL = no
-- drawer (pre-migration turns and redacted/disabled streams degrade correctly).
ALTER TABLE aura.conversation_turns
    ADD COLUMN reasoning text,
    ADD COLUMN reasoning_duration_ms bigint;

COMMENT ON COLUMN aura.conversation_turns.reasoning IS
    'Display-only accumulated CoT (amendment #91): bounded by AURA_REASONING_PERSIST_MAX_RUNES; NULL when redacted, disabled, or pre-migration. Never rehydrated into llm.Message.';
COMMENT ON COLUMN aura.conversation_turns.reasoning_duration_ms IS
    'Wall time (ms) from the first to the last reasoning delta of the turn (amendment #91, UI-spec OQ-1 "Thought for X s"); NULL when unknown.';
