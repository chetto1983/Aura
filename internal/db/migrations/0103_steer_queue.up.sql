-- Move the mid-turn steer inbox from an in-memory, single-replica-by-construction map
-- to Postgres with a TTL (D-06, operator decision: "io metterei tutto su postgres con
-- una ttl piu sicuro e meno fragile"), as ONE table whose rows are typed by kind with
-- two TTL knobs and one sweep (D-07), where an expired row always leaves a readable
-- trace written in the same transaction that expires it (D-08).
--
-- WHY NOW: internal/steer is documented in-tree as "in-memory, single-replica-by-
-- construction" and "a steer is consumed by Drain, never replayed" -- a background
-- delegation's completion pushed while no turn is running (the normal case for a
-- delegation that outlives its parent turn, SWARM-03/09) is silently lost today. SC#1
-- forbids that. This migration is the durable half of that fix; internal/steer/pg_store.go
-- and internal/steer/queue_sweep.go are the Go half (phase 51, plan 02).
--
-- WHY ONE TABLE, NOT TWO: `kind` is NOT NULL with no default, so a future row variant
-- cannot be inserted by forgetting to name itself, and every read site (Drain, the
-- sweep) derives behavior from `kind` rather than assuming the steer TTL. Column set is
-- designed fresh, informed by but NOT copied from aura.ingestion_jobs (51-PATTERNS.md
-- "No Analog Found"): a delegation result is delivered-once via Drain, never claimed or
-- leased, so it carries NO locked_by / locked_until / lease_generation.
--
-- WHY NO ROW-LEVEL SECURITY: Push and Drain satisfy a LOCKED, unwidenable interface
-- contract -- Push(conv, source, text string) error and Drain(conv string) []Message,
-- literally as called by internal/agui/server_run_steer.go and
-- internal/channels/telegram/bot_dispatch_steer.go, neither of which may change. Neither
-- signature carries an identity or even a context.Context, so there is no principal to
-- set app.current_identity to at the point these run, and aura.conversations' own
-- fail-closed RLS (migration 0089) means a plain aura_app read of it with no identity
-- set returns ZERO rows for every conversation -- the identity cannot be looked up on
-- the caller's connection either. aura.conversation_owner() below is the established
-- escape hatch for exactly this shape (mirrors 0089's reject_tool_invocations_mutation:
-- a narrow, SECURITY DEFINER function owned by aura_migrate, so it alone bypasses RLS on
-- aura.conversations to answer one safe question -- "who owns this conversation" --
-- never exposing any other row or column). Every legitimate Push/Drain caller already
-- resolved conv through its OWN owner-scoped path before calling in (resolveRunSession
-- for the cockpit, the bot's own per-chat session for Telegram); this function's role is
-- narrower still: derive the CORRECT identity_id to store/filter by, not authorize
-- access to conv in the first place. This is the same reasoning aura.ingestion_jobs
-- already rests on (0087's own "GLOBAL / CONTROL PLANE" footnote) generalized one step:
-- Go-level identity derivation IS the backstop here, not the RLS session variable.
CREATE TABLE aura.steer_queue (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id     uuid        NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE,
    conversation_id text        NOT NULL,
    kind            text        NOT NULL CHECK (kind IN ('steer', 'delegation_result')),
    source          text        NOT NULL,
    body            text        NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    expires_at      timestamptz,
    drained_at      timestamptz,
    expired_at      timestamptz,
    expiry_reason   text,
    nudged_at       timestamptz
);

COMMENT ON COLUMN aura.steer_queue.kind IS
    'D-07: one table, rows typed by kind (steer|delegation_result), each deriving its '
    'OWN TTL from its own configured knob (AURA_STEER_QUEUE_TTL_SEC / '
    'AURA_DELEGATION_RESULT_TTL_SEC) at Push time -- never a single shared cutoff. NOT '
    'NULL with no default so a future variant cannot be inserted by forgetting to name '
    'itself; every read site derives behavior from this column, never assumes the steer '
    'TTL.';

COMMENT ON COLUMN aura.steer_queue.expires_at IS
    'D-07: computed from kind''s configured TTL at Push time (now() + TTL). NULL means '
    'never expires -- a TTL knob <= 0 disables expiry for that kind, the shipped '
    'AURA_ASKUSER_PAUSE_TTL_SEC precedent. Drain excludes a row past expires_at even '
    'before the sweep catches up (lazy expiry on read).';

COMMENT ON COLUMN aura.steer_queue.expiry_reason IS
    'D-08: set in the SAME transaction that sets expired_at, alongside the readable '
    'trace the sweep appends to the row''s own conversation -- an expired row is never '
    'silently dropped ("errors should never pass silently").';

COMMENT ON COLUMN aura.steer_queue.nudged_at IS
    'Consumed by plan 51-10 (absent-operator push to the owning channel after a grace '
    'window). Landed in THIS migration, not 51-10''s own, because 51-10 runs in the same '
    'wave as another migration-creating plan and two executors deriving the next slot '
    'from `ls internal/db/migrations | tail -1` in parallel would collide on it. '
    'Distinct from drained_at on purpose: drained_at means "the operator received this '
    'inside a turn"; nudged_at means "we pushed it to a channel because nobody did".';

-- Serves BOTH Drain (conversation_id = $1, further filtered by expires_at at read time)
-- and the sweep's due-row scan (drained_at/expired_at IS NULL is the shared predicate
-- either query needs; expires_at is compared in the query body, not the index
-- definition, so one partial index covers both access patterns without a second one
-- keyed purely on expires_at).
CREATE INDEX steer_queue_undrained_idx
    ON aura.steer_queue (identity_id, conversation_id, created_at)
    WHERE drained_at IS NULL AND expired_at IS NULL;

GRANT SELECT, INSERT, UPDATE, DELETE ON aura.steer_queue TO aura_app;
GRANT ALL                           ON aura.steer_queue TO aura_migrate;

-- aura.conversation_owner(): the ONE safe question aura_app is allowed to ask about a
-- conversation it does not (yet) have a principal for: "which identity owns it". No
-- other column is exposed. SECURITY DEFINER runs it as its owner (aura_migrate), who
-- owns aura.conversations and therefore bypasses its RLS; search_path is pinned per the
-- Postgres manual's "Writing SECURITY DEFINER Functions Safely" (a SECURITY DEFINER
-- function must never resolve an identifier through a caller-controlled path). STABLE
-- (not VOLATILE) lets the planner fold repeat calls with the same argument inside one
-- statement.
--
-- Takes TEXT, not uuid, and casts INSIDE the body: aura.steer_queue.conversation_id is
-- itself text (the shipped Push/Drain contract treats conv as an opaque string, never
-- asserts it parses as a uuid), and every caller in internal/db/queries/steer_queue.sql
-- also compares that same parameter against the text column in the SAME statement.
-- Postgres resolves one $N placeholder to ONE type per prepared statement; a text-typed
-- comparison alongside an explicit ::uuid cast on the identical placeholder made
-- Postgres pick uuid globally and then fail "operator does not exist: text = uuid" on
-- the other usage (measured live, spike-free -- this is the fix, not a hypothetical).
-- Taking text here removes the ambiguity at its source instead of threading a second
-- bind parameter through every query that also needs conv as text.
CREATE FUNCTION aura.conversation_owner(p_conversation_id text) RETURNS uuid
    LANGUAGE sql
    SECURITY DEFINER
    STABLE
    SET search_path = pg_catalog, aura
    AS $$
    SELECT identity_id FROM aura.conversations WHERE id = p_conversation_id::uuid;
$$;

REVOKE ALL ON FUNCTION aura.conversation_owner(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION aura.conversation_owner(text) TO aura_app;

COMMENT ON FUNCTION aura.conversation_owner(text) IS
    'Migration 0103: the narrow, audited escape hatch that lets aura_app derive a '
    'conversation''s owning identity_id for aura.steer_queue.Push/Drain, whose LOCKED '
    'interface contract (steer.PostgresStore satisfying the unchanged Push/Drain '
    'signatures) carries neither an identity nor a context.Context to set '
    'app.current_identity with. Takes text (not uuid) so every caller can pass the SAME '
    'bind parameter it also compares against aura.steer_queue.conversation_id (itself '
    'text) without Postgres''s single-type-per-placeholder rule conflicting across the '
    'two usages. Exposes exactly one column of one table for one lookup; never wrap '
    'this in a wider view.';
