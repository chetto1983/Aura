ALTER TABLE aura.conversation_turns
    DROP COLUMN IF EXISTS reasoning_duration_ms,
    DROP COLUMN IF EXISTS reasoning;
