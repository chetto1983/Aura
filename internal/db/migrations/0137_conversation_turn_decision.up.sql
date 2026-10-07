-- Turn recall (docs/superpowers/specs/2026-10-06-turn-recall-design.md, "Persisting what was
-- learned"): every user turn records how its reasoning effort was decided, so a later
-- paraphrase can reuse a decision instead of guessing again. Slot measured with
-- `ls internal/db/migrations/ | tail -1`: 0136 was the head.
--
-- All nullable and nothing is backfilled: a row without provenance can never become a
-- reusable label, which is the point. NULL means "no effort field"; the explicit effort
-- `none` is stored as 'none'. Only `user` and `teacher` decisions are labels; the other four
-- sources record how a turn was decided and are never copied.

ALTER TABLE aura.conversation_turns
    ADD COLUMN recall_context_key text,
    ADD COLUMN reasoning_effort text,
    ADD COLUMN reasoning_effort_requested text,
    ADD COLUMN reasoning_effort_source text,
    ADD COLUMN reasoning_effort_route_key text,
    ADD COLUMN reasoning_effort_policy_version text,
    ADD COLUMN reasoning_effort_origin_ref text,
    ADD CONSTRAINT conversation_turns_reasoning_effort_source_check CHECK (
        reasoning_effort_source IS NULL OR reasoning_effort_source IN (
            'user', 'teacher', 'memory', 'seeds', 'greeting', 'fallback'
        )
    );

COMMENT ON COLUMN aura.conversation_turns.recall_context_key IS
    'Digest of what the model read before this user message (turn recall). NULL = not eligible for recall.';
COMMENT ON COLUMN aura.conversation_turns.reasoning_effort IS
    'Effort sent on the first accepted main request, after the clamp. NULL = no effort field.';
COMMENT ON COLUMN aura.conversation_turns.reasoning_effort_requested IS
    'Effort the decision asked for, before the clamp. A reused label is clamped again, never inferred from reasoning_effort.';
COMMENT ON COLUMN aura.conversation_turns.reasoning_effort_source IS
    'user, teacher, memory, seeds, greeting or fallback. Only user and teacher rows are reusable labels.';
