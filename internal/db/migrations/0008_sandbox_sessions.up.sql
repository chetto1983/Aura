-- Source: PRD §Slice 2b file targets + 08-DECISIONS-WAVE0 + 0005_conversations.up.sql.
-- Control-plane registry for session-bound sandbox containers keyed by conversation
-- (D-06). The SessionManager (08-05) records one row per live container; boot recovery
-- marks orphaned `active` rows `terminated` and lazily recreates the container on the
-- next call (parity with agent_job_runs).
--
-- FK is uuid, NOT text (landmine #1): aura.conversations.id is uuid (0005:9). A text
-- FK to a uuid PK is rejected as an incompatible-types constraint. ON DELETE CASCADE
-- ties a session row's lifetime to its conversation. Number is 0008 (landmine #2): the
-- repo migration floor is 0007_cache_metrics; the PRD's "0010" reference is superseded.
--
-- Role separation (INFRA-01, mirrors 0005/0007): aura_app gets DML only, aura_migrate
-- owns DDL (AURA_DB_MIGRATE_URL).

CREATE TABLE aura.sandbox_sessions (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id uuid        NOT NULL REFERENCES aura.conversations (id) ON DELETE CASCADE,
    container_id    text        NOT NULL,
    image_digest    text        NOT NULL,
    started_at      timestamptz NOT NULL DEFAULT now(),
    last_used_at    timestamptz NOT NULL DEFAULT now(),
    status          text        NOT NULL DEFAULT 'active'
                    CHECK (status IN ('active', 'idle', 'terminated', 'evicted'))
);

-- Reaper sweep + boot recovery both scan `WHERE status = 'active' ORDER BY last_used_at`;
-- a plain (status, last_used_at) index serves both the predicate and the ordering.
CREATE INDEX sandbox_sessions_status_last_used_idx
    ON aura.sandbox_sessions (status, last_used_at);

GRANT SELECT, INSERT, UPDATE, DELETE ON aura.sandbox_sessions TO aura_app;
GRANT ALL                            ON aura.sandbox_sessions TO aura_migrate;

COMMENT ON TABLE aura.sandbox_sessions IS
    'Session-bound sandbox control-plane registry (Slice 2b, D-06). One row per live per-conversation container; boot recovery marks orphaned active rows terminated and lazily recreates.';
