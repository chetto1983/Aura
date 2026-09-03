-- Persist the context FILL beside the bill.
--
-- conversation_turns.input_tokens holds llm.Usage.PromptTokens, which is the BILL: the
-- sum of every call in a round, because the provider charges each one and each re-sends
-- the prefix. The FILL — how much of the window the request actually occupied — is the
-- prompt of the round's FINAL call, and llm.Usage already carries it as ContextTokens.
-- It was computed on every turn and thrown away.
--
-- The cost of not storing it is a gauge that lies on reload. The runtime footer reads the
-- live turn's context_tokens while a turn is running, but a reloaded conversation has no
-- live turn and falls back to the last input_tokens — the bill — presented as the fill.
-- Measured 2026-09-03 on this deployment: conversation 01a05c23 would read 353,321
-- against a 256,000 window (138%) for a round whose actual occupancy was a fraction of
-- that. The same class of error the footer's own comment records having made before.
--
-- DEFAULT 0 rather than backfilling from input_tokens: the bill is not the fill and
-- copying it would bake the lie into history. 0 means "not recorded", and the read falls
-- back to input_tokens for those rows, which is the upper bound it has always been.

ALTER TABLE aura.conversation_turns
    ADD COLUMN IF NOT EXISTS context_tokens integer NOT NULL DEFAULT 0;

COMMENT ON COLUMN aura.conversation_turns.context_tokens IS
    'Window occupancy of the round''s final call (llm.Usage.ContextTokens). 0 = not recorded; input_tokens is the bill, not this.';
