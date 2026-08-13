-- Coding verification evidence ledger.
--
-- A port of NousResearch/hermes-agent `agent/verification_evidence.py` (MIT, read at
-- commit 9d4ef04ed), whose ledger is SQLite. The schema below is the same two tables
-- with the same keys and the same invariant; what changed is the store (Postgres, this
-- migration) and the isolation, because every table in aura.* carries an owner and an
-- RLS policy and this one has no reason to be the exception.
--
-- The ledger is deliberately PASSIVE. It records what the agent actually proved while
-- working in a code workspace: it never decides to run a suite, never blocks completion,
-- and never upgrades a targeted check into "repo green". The policy that reads it lives
-- in internal/agent/verification_stop.go.
--
-- The whole point is one invariant, and it is worth stating before the DDL because the
-- columns only make sense in its light:
--
--   a passing verification event CLEARS last_edit_at;
--   an edit SETS it;
--   so "was this workspace edited after its last passing verification" is a single
--   column read, not a timestamp comparison that can disagree with itself.

CREATE TABLE aura.verification_events (
    id                bigserial   PRIMARY KEY,
    identity_id       uuid        NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE,
    created_at        timestamptz NOT NULL DEFAULT now(),
    session_id        text        NOT NULL CHECK (btrim(session_id) <> ''),
    -- cwd is where the command ran; root is the project it belongs to. They differ when
    -- a suite is invoked from a subdirectory, and the state below is keyed on ROOT so
    -- two invocations of the same suite from different directories are one workspace.
    cwd               text        NOT NULL,
    root              text        NOT NULL CHECK (btrim(root) <> ''),
    command           text        NOT NULL,
    canonical_command text        NOT NULL,
    -- kind: test | lint | typecheck | build | format | check | ad_hoc
    kind              text        NOT NULL CHECK (btrim(kind) <> ''),
    -- scope is 'targeted' when the command named a file/path argument. It exists so a
    -- single-file test run can never be read back as evidence that the repo is green.
    scope             text        NOT NULL CHECK (scope IN ('targeted', 'full')),
    status            text        NOT NULL CHECK (status IN ('passed', 'failed')),
    exit_code         integer     NOT NULL,
    output_summary    text        NOT NULL DEFAULT ''
);

-- The read the policy makes is "latest event for this session and root", so the index
-- carries the ordering rather than leaving it to a sort.
CREATE INDEX idx_verification_events_session_root
    ON aura.verification_events (identity_id, session_id, root, id DESC);

CREATE TABLE aura.verification_state (
    identity_id        uuid        NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE,
    session_id         text        NOT NULL CHECK (btrim(session_id) <> ''),
    root               text        NOT NULL CHECK (btrim(root) <> ''),
    -- The event that last verified this workspace. NULL once an edit has staled it.
    last_event_id      bigint      REFERENCES aura.verification_events(id) ON DELETE SET NULL,
    -- Set by an edit, cleared by a passing verification. Its presence IS the staleness.
    last_edit_at       timestamptz,
    changed_paths      jsonb       NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(changed_paths) = 'array'),
    PRIMARY KEY (identity_id, session_id, root)
);

-- ---------------------------------------------------------------------------
-- Owner isolation, following migrations 0087/0090/0093.
-- ---------------------------------------------------------------------------
ALTER TABLE aura.verification_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE aura.verification_state  ENABLE ROW LEVEL SECURITY;

CREATE POLICY verification_events_owner_isolation ON aura.verification_events
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);

CREATE POLICY verification_state_owner_isolation ON aura.verification_state
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);

DO $$
DECLARE
    target text;
BEGIN
    FOREACH target IN ARRAY ARRAY[
        'verification_events',
        'verification_state'
    ]
    LOOP
        EXECUTE format($fmt$
            CREATE POLICY %I ON aura.%I AS RESTRICTIVE FOR ALL TO aura_app
            USING (current_setting('app.current_identity', true) IS NOT NULL
                   AND current_setting('app.current_identity', true) <> '')
        $fmt$, target || '_requires_identity', target);
        EXECUTE format(
            'COMMENT ON POLICY %I ON aura.%I IS %L',
            target || '_requires_identity', target,
            $doc$Fail-closed floor (migration 0094): AND-combined with every permissive policy, so no later permissive policy can restore visibility to a caller with no app.current_identity. Set it via internal/db.WithIdentityTx.$doc$);
    END LOOP;
END
$$;

COMMENT ON TABLE aura.verification_events IS
    'Passive ledger of classified verification commands (port of hermes-agent verification_evidence.py). Never decides to run anything.';
COMMENT ON TABLE aura.verification_state IS
    'One row per identity+session+root. last_edit_at set by an edit and cleared by a passing verification: its presence IS the staleness.';
COMMENT ON COLUMN aura.verification_events.scope IS
    'targeted when the command named a file/path argument, so a single-file run is never read back as repo-green evidence.';
