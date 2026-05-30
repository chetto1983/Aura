-- Source: PRD §Slice 1.5 + phase 04-SPEC.md Req#3 + 04-PATTERNS.md (0002 grant style).
-- ask_user pause/resume persistence. conversation_id is plain `text` here (NO FK)
-- for 1.5 <-> 1.8 migration independence (04-SPEC Constraints); 0005 ALTERs it to
-- uuid + FK once aura.conversations exists. proxied_* columns are NULL for direct
-- calls (D-A1-08 swarm forward-compat; Phase 9 populates). resumed_answer is added
-- in 0005 (AM-02: jsonb {action,content}), NOT here.

CREATE TABLE aura.paused_states (
    token                 uuid        PRIMARY KEY,
    conversation_id       text        NOT NULL,
    kind                  text        NOT NULL CHECK (kind IN ('clarification', 'approval', 'choice')),
    question              text        NOT NULL,
    options               jsonb,
    priority              integer     NOT NULL DEFAULT 0 CHECK (priority BETWEEN 0 AND 100),
    resume_context        jsonb,
    tool_call_id          text        NOT NULL,
    proxied_from_child_id uuid,
    proxied_tool_call_id  text,
    created_at            timestamptz NOT NULL DEFAULT now(),
    resumed_at            timestamptz
);

-- Belt + suspenders (DEFAULT PRIVILEGES from 0001 already cover this for aura_app;
-- explicit grants documented for forensic clarity). NEVER grant TRUNCATE/DROP/CREATE
-- to aura_app (T-04-01 / Pitfall 7).
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.paused_states TO aura_app;
GRANT ALL                            ON aura.paused_states TO aura_migrate;

-- Partial index for the O(log n) pending-scan (resumed_at IS NULL = still pending).
CREATE INDEX paused_states_pending_idx
    ON aura.paused_states (conversation_id, resumed_at)
    WHERE resumed_at IS NULL;

COMMENT ON TABLE aura.paused_states IS
    'ask_user HITL pauses (Slice 1.5). Pending = resumed_at IS NULL. FIFO order: priority DESC, created_at ASC, token ASC.';
