-- The operator's profile, moved out of the memory graph.
--
-- Onboarding collects a form (internal/onboarding/answers.go) and writes every answer into
-- the agent-memory graph as entities, facts and preferences. That made the deterministic
-- profile compete with everything the agent has actually LEARNED: a recall for "what did we
-- decide about the invoices" ranks against "role: programmatore" and "timezone:
-- Europe/Rome", which are neither memories nor retrievable knowledge -- they are settings.
-- Operator's words, 2026-08-16: "non serve all'agente, e' solo rumore".
--
-- Two consequences the graph could not give:
--
--   * The runtime can read it. The clock the model reads was UTC because the timezone lived
--     in a per-identity ArcadeDB database that nothing on the turn path opens; the agent
--     was left to convert, and on 2026-08-16 it converted wrong (15:49Z read as "+1 ora"
--     for Rome, which is +2 in August, and then answered with the UTC figure anyway).
--   * The profile is ALWAYS present rather than retrieved. A fact the agent must never
--     fail to know cannot depend on a similarity search returning it.
--
-- One row per identity, typed columns rather than a JSON blob: these fields are read by
-- SQL (the timezone on the turn path), edited field-by-field by the profile editor, and a
-- blob would make both of those a parse.

CREATE TABLE IF NOT EXISTS aura.identity_profiles (
    identity_id         uuid        PRIMARY KEY REFERENCES aura.identities(id) ON DELETE CASCADE,
    display_name        text        NOT NULL DEFAULT '',
    role                text        NOT NULL DEFAULT '',
    company             text        NOT NULL DEFAULT '',
    location            text        NOT NULL DEFAULT '',
    -- IANA zone name. No CHECK: the valid set lives in tzdata, which the operating system
    -- updates several times a year, so a constraint written today would one day reject a
    -- zone that has become valid. The resolver degrades to UTC on a name it cannot load.
    timezone            text        NOT NULL DEFAULT '',
    lang                text        NOT NULL DEFAULT '',
    tone_preference     text        NOT NULL DEFAULT '',
    response_length     text        NOT NULL DEFAULT '',
    custom_instructions text        NOT NULL DEFAULT '',
    -- Nullable booleans on purpose: "not answered" is a different state from "no", and the
    -- form leaves them unset until the operator chooses.
    voice_mode            boolean,
    can_proactive_message boolean,
    -- Lists stay arrays rather than a joined string: the profile editor edits them as
    -- lists, and re-splitting a comma-joined field would corrupt any entry containing one.
    expertise           text[]      NOT NULL DEFAULT '{}',
    stack               text[]      NOT NULL DEFAULT '{}',
    projects            text[]      NOT NULL DEFAULT '{}',
    goals               text[]      NOT NULL DEFAULT '{}',
    interests           text[]      NOT NULL DEFAULT '{}',
    people              text[]      NOT NULL DEFAULT '{}',
    -- Hard "never do" rules. They ride the always-block, never a retrieval: a veto the
    -- agent only sometimes remembers is not a veto.
    vetoes              text[]      NOT NULL DEFAULT '{}',
    -- The onboarding gate itself, moved here with the answers it guards. It used to be a
    -- sentinel fact in the memory graph, which meant the "has this operator onboarded"
    -- check was an MCP round trip into a per-identity ArcadeDB database -- on a path that
    -- runs before the operator can do anything at all.
    completed_at        timestamptz,
    skipped_at          timestamptz,
    -- Set when a channel that cannot render the form (Telegram) has pointed the operator at
    -- it. It lives here rather than in the channel so the channel stays a wrapper: an
    -- in-process latch made "once" mean "once per daemon restart", and every restart
    -- re-nudged an operator who had already been told.
    seed_nudged_at      timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- Owner isolation, following 0087/0089/0094: the row IS the identity's, and the policy
-- names the owner directly rather than a parent row.
-- ---------------------------------------------------------------------------
ALTER TABLE aura.identity_profiles ENABLE ROW LEVEL SECURITY;

CREATE POLICY identity_profiles_owner_isolation ON aura.identity_profiles
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);

CREATE POLICY identity_profiles_requires_identity ON aura.identity_profiles
    AS RESTRICTIVE FOR ALL TO aura_app
    USING (current_setting('app.current_identity', true) IS NOT NULL
           AND current_setting('app.current_identity', true) <> '');

COMMENT ON POLICY identity_profiles_requires_identity ON aura.identity_profiles IS
    'Fail-closed floor (migration 0097): AND-combined with every permissive policy, so no later permissive policy can restore visibility to a caller with no app.current_identity. Set it via internal/db.WithIdentityTx.';

COMMENT ON TABLE aura.identity_profiles IS
    'Deterministic operator profile from onboarding. Rendered into the always-block, never retrieved: settings, not memories.';
