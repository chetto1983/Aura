-- Durable conversation compaction (HANDOFF 6.1).
--
-- L2.4 compaction already works: over budget, internal/conversations/compaction.go
-- summarizes the historical rounds into one synthetic turn and the ladder sends that
-- instead of the turns. What it does not do is REMEMBER the summary, and the cost of
-- that shows up twice on every single turn of a long conversation:
--
--   1. An auxiliary LLM call, with a 45s timeout, re-summarizing the same history that
--      was summarized on the previous turn.
--   2. A summary whose wording is regenerated each time, which changes the prompt
--      prefix, which invalidates the provider's prefix cache for everything after it.
--      MEASURED 2026-08-16 on a real 128-turn conversation: 339,434 input tokens
--      cumulative of which 304,640 (90%) were served from cache, and a largest single
--      request of 53,082 tokens. A prefix that moves every turn is how that 90% is lost.
--
-- So the row below is not a cache of a computation; it is the thing that makes the
-- compacted prefix STABLE. That is why the summary is stored verbatim rather than
-- re-derived, and why covers_through_seq is part of the identity of the row.
--
-- The design follows what four independent implementations do (LangGraph's checkpointer
-- keeping every message while RemoveMessage only shrinks the live state; Letta's recall
-- memory holding the full conversation behind a recursive summary; Mem0's raw layer
-- beneath its distilled one; Neo4j agent-memory's session message store): the model's
-- window is a VIEW, and the store keeps everything. Nothing here deletes a turn.
-- aura.conversation_turns remains the record of what was said.

CREATE TABLE aura.conversation_compactions (
    conversation_id    uuid        NOT NULL REFERENCES aura.conversations(id) ON DELETE CASCADE,
    -- The branch this summary describes. A branch is a different history, so it needs a
    -- different summary; the canonical branch uses the same all-zero id the turns do.
    branch_id          uuid        NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000'::uuid,
    -- The watermark: every turn with seq <= this one is represented by the summary
    -- below. It is what makes the next compaction incremental -- fold in the turns after
    -- it rather than re-reading the whole history, the iterative update hermes-agent's
    -- context_compressor.py performs to keep information across successive compactions.
    covers_through_seq integer     NOT NULL CHECK (covers_through_seq > 0),
    summary            text        NOT NULL CHECK (btrim(summary) <> ''),
    -- Which model wrote it. A summary produced by a different model is still valid text,
    -- but knowing the author is what lets an operator explain a change in quality.
    model              text        NOT NULL DEFAULT '',
    -- How many turns went into the summary, for the same reason: a summary that swallowed
    -- 200 turns and one that swallowed 12 are not the same artifact.
    source_turns       integer     NOT NULL DEFAULT 0 CHECK (source_turns >= 0),
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (conversation_id, branch_id)
);

-- ---------------------------------------------------------------------------
-- Owner isolation, in the 0089 shape: name the parent's OWNER, never merely the
-- parent's existence. Two layers that cede together are one layer.
-- ---------------------------------------------------------------------------
ALTER TABLE aura.conversation_compactions ENABLE ROW LEVEL SECURITY;

CREATE POLICY conversation_compactions_owner_isolation ON aura.conversation_compactions
    USING (EXISTS (
        SELECT 1 FROM aura.conversations c
        WHERE c.id = conversation_id
          AND c.identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid
    ));

CREATE POLICY conversation_compactions_requires_identity ON aura.conversation_compactions
    AS RESTRICTIVE FOR ALL TO aura_app
    USING (current_setting('app.current_identity', true) IS NOT NULL
           AND current_setting('app.current_identity', true) <> '');

COMMENT ON POLICY conversation_compactions_requires_identity ON aura.conversation_compactions IS
    'Fail-closed floor (migration 0096): AND-combined with every permissive policy, so no later permissive policy can restore visibility to a caller with no app.current_identity. Set it via internal/db.WithIdentityTx.';

COMMENT ON TABLE aura.conversation_compactions IS
    'One durable summary per conversation branch. Makes the compacted prompt prefix stable across turns (the provider cache depends on it) and the next compaction incremental. Never a substitute for conversation_turns, which keeps every turn.';
COMMENT ON COLUMN aura.conversation_compactions.covers_through_seq IS
    'Every turn with seq <= this value is represented by the summary. The next compaction folds in only what came after.';
