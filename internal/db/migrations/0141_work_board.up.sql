-- The work board: one Kanban per identity, worked by the operator in the cockpit and by the
-- agent through the `board` tool (prd.md §16, "A work board for the identity and its agent",
-- 2026-10-09; docs/superpowers/specs/2026-10-09-work-board-design.md).
--
-- Measured before this table existed: Aura had no durable unit of work the operator and the
-- agent share. `todo` is a per-session scratchpad nothing persists, `task` is the scheduler's
-- verb, a delegation ends as a record card in conversation_turns.
--
-- One board per identity in this release, created on first use; the board_id on every card
-- leaves several boards a change without a migration. identity_id is on the card as well as
-- the board so the RLS policy is one predicate, not a join.

CREATE TABLE aura.boards (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id uuid        NOT NULL REFERENCES aura.identities (id) ON DELETE CASCADE,
    name        text        NOT NULL,
    -- [{"id": "todo", "label": "To do", "cardLimit": 0}], in display order. The shape is the
    -- widget's ColumnConfig, so the cockpit hands it over unchanged.
    columns     jsonb       NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (identity_id, name)
);

CREATE TABLE aura.board_cards (
    id              uuid             PRIMARY KEY DEFAULT gen_random_uuid(),
    board_id        uuid             NOT NULL REFERENCES aura.boards (id) ON DELETE CASCADE,
    identity_id     uuid             NOT NULL REFERENCES aura.identities (id) ON DELETE CASCADE,
    column_id       text             NOT NULL,
    -- A float so a move writes one row, the midpoint of its neighbours; the store renumbers a
    -- column when two positions come within 1e-6.
    position        double precision NOT NULL,
    label           text             NOT NULL CHECK (char_length(label) BETWEEN 1 AND 200),
    description     text             NOT NULL DEFAULT '' CHECK (char_length(description) <= 4000),
    -- The widget's own scale (getPriorityOptions): 1 Low, 2 Medium, 3 High.
    priority        smallint         NOT NULL DEFAULT 2 CHECK (priority BETWEEN 1 AND 3),
    tags            text[]           NOT NULL DEFAULT '{}',
    due_at          timestamptz,
    conversation_id uuid             REFERENCES aura.conversations (id) ON DELETE SET NULL,
    task_id         uuid             REFERENCES aura.scheduler_tasks (id) ON DELETE SET NULL,
    -- Where the card came from: the operator on the board, the agent in a conversation with
    -- a live responder, or the agent in a scheduled job or a swarm child.
    source          text             NOT NULL CHECK (source IN ('cockpit', 'chat', 'background')),
    updated_by      text             NOT NULL CHECK (updated_by IN ('operator', 'agent')),
    created_at      timestamptz      NOT NULL DEFAULT now(),
    updated_at      timestamptz      NOT NULL DEFAULT now()
);

CREATE INDEX board_cards_board_column ON aura.board_cards (board_id, column_id, position);

-- Saved filters over the board, the smart views PMSync's task pillar measured as useful.
-- filters is the cockpit's own shape ({priority, tags, source, due}); the server stores it
-- and never interprets it.
CREATE TABLE aura.board_views (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id uuid        NOT NULL REFERENCES aura.identities (id) ON DELETE CASCADE,
    name        text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    filters     jsonb       NOT NULL DEFAULT '{}'::jsonb,
    pinned      boolean     NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (identity_id, name)
);

GRANT SELECT, INSERT, UPDATE, DELETE ON aura.boards, aura.board_cards, aura.board_views TO aura_app;
GRANT ALL ON aura.boards, aura.board_cards, aura.board_views TO aura_migrate;

-- Fail-closed RLS, the 0087 pair, on all three: a board is a statement by one principal about
-- their own work; a connection that has not said whose board it means must see none.
ALTER TABLE aura.boards ENABLE ROW LEVEL SECURITY;
ALTER TABLE aura.board_cards ENABLE ROW LEVEL SECURITY;
ALTER TABLE aura.board_views ENABLE ROW LEVEL SECURITY;

CREATE POLICY boards_owner_isolation ON aura.boards
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);
CREATE POLICY boards_require_identity ON aura.boards
    AS RESTRICTIVE FOR ALL TO aura_app
    USING (current_setting('app.current_identity', true) IS NOT NULL
           AND current_setting('app.current_identity', true) <> '');

CREATE POLICY board_cards_owner_isolation ON aura.board_cards
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);
CREATE POLICY board_cards_require_identity ON aura.board_cards
    AS RESTRICTIVE FOR ALL TO aura_app
    USING (current_setting('app.current_identity', true) IS NOT NULL
           AND current_setting('app.current_identity', true) <> '');

CREATE POLICY board_views_owner_isolation ON aura.board_views
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);
CREATE POLICY board_views_require_identity ON aura.board_views
    AS RESTRICTIVE FOR ALL TO aura_app
    USING (current_setting('app.current_identity', true) IS NOT NULL
           AND current_setting('app.current_identity', true) <> '');
