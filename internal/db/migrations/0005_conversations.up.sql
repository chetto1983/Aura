-- Source: PRD §Slice 1.8 + phase 04-SPEC.md Req#7/#8/#10 + 04-CONTEXT.md (AM-02, OQ3).
-- Multi-thread conversation persistence + per-conversation token/USD aggregation.
-- This migration ALSO promotes aura.paused_states.conversation_id from text to uuid
-- + FK (now that aura.conversations exists, 04-SPEC Constraints) and adds the AM-02
-- resumed_answer jsonb column. NO aura.conversation_spillover table: spillover is a
-- sidecar FILE via conversation_turns.content_sidecar_path (OQ3, follows SPEC).

CREATE TABLE aura.conversations (
    id                  uuid          PRIMARY KEY,
    title               text,
    identity_id         uuid          NOT NULL REFERENCES aura.identities (id) ON DELETE CASCADE,
    created_at          timestamptz   NOT NULL DEFAULT now(),
    last_active_at      timestamptz   NOT NULL DEFAULT now(),
    status              text          NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived', 'deleted')),
    model               text          NOT NULL DEFAULT '',
    total_input_tokens  bigint        NOT NULL DEFAULT 0,
    total_output_tokens bigint        NOT NULL DEFAULT 0,
    total_cached_tokens bigint        NOT NULL DEFAULT 0,
    total_cost_usd      numeric(10, 4) NOT NULL DEFAULT 0,
    metadata            jsonb
);

CREATE TABLE aura.conversation_turns (
    conversation_id      uuid        NOT NULL REFERENCES aura.conversations (id) ON DELETE CASCADE,
    seq                  integer     NOT NULL,
    role                 text        NOT NULL CHECK (role IN ('system', 'user', 'assistant', 'tool')),
    content              text,
    content_sidecar_path text,
    tool_call_id         text,
    tool_calls           jsonb,
    created_at           timestamptz NOT NULL DEFAULT now(),
    input_tokens         integer     NOT NULL DEFAULT 0,
    output_tokens        integer     NOT NULL DEFAULT 0,
    cached_tokens        integer     NOT NULL DEFAULT 0,
    PRIMARY KEY (conversation_id, seq)
);

-- context_rot_events: L2.5 hard-rolling-buffer audit (amendment #22). Written only
-- when L1+L2 are insufficient and oldest pair(s) are dropped (SC-1: never on L1-alone).
CREATE TABLE aura.context_rot_events (
    ts              timestamptz NOT NULL DEFAULT now(),
    conversation_id uuid        NOT NULL,
    action          text        NOT NULL,
    pairs_dropped   integer     NOT NULL,
    tokens_before   integer     NOT NULL,
    tokens_after    integer     NOT NULL
);

-- Promote paused_states.conversation_id text -> uuid + FK (1.5 <-> 1.8 join), and
-- add the AM-02 three-action resumed_answer (jsonb {action,content}). The USING cast
-- is safe on a fresh DB (no rows) and on real data the values are conversation UUIDs.
ALTER TABLE aura.paused_states
    ALTER COLUMN conversation_id TYPE uuid USING conversation_id::uuid;
ALTER TABLE aura.paused_states
    ADD CONSTRAINT paused_states_conversation_id_fkey
        FOREIGN KEY (conversation_id) REFERENCES aura.conversations (id) ON DELETE CASCADE;
ALTER TABLE aura.paused_states
    ADD COLUMN resumed_answer jsonb;

GRANT SELECT, INSERT, UPDATE, DELETE ON aura.conversations       TO aura_app;
GRANT ALL                            ON aura.conversations       TO aura_migrate;
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.conversation_turns  TO aura_app;
GRANT ALL                            ON aura.conversation_turns  TO aura_migrate;
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.context_rot_events  TO aura_app;
GRANT ALL                            ON aura.context_rot_events  TO aura_migrate;

CREATE INDEX conversations_identity_status_idx
    ON aura.conversations (identity_id, status, last_active_at DESC);

-- pg_trgm extension lands HERE (a regular tx-safe statement) so that 0006 can keep
-- CREATE INDEX CONCURRENTLY as its SOLE statement — CONCURRENTLY cannot run inside a
-- multi-statement implicit transaction block (golang-migrate v4.19.1 Pitfall 6).
CREATE EXTENSION IF NOT EXISTS pg_trgm;

COMMENT ON TABLE aura.conversations IS
    'Multi-thread persisted conversations (Slice 1.8). Aggregates token + USD totals per thread.';
COMMENT ON TABLE aura.conversation_turns IS
    'Per-turn rows (Slice 1.8). content NULL + content_sidecar_path set when content > AURA_CONVERSATION_TURN_CAP_BYTES.';
COMMENT ON TABLE aura.context_rot_events IS
    'L2.5 hard-rolling-buffer audit (amendment #22). One row per pair-drop; never written when L1 alone suffices (SC-1).';
