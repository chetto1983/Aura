-- Source: PRD §Slice 6 (amendment #46) + phase 10-CONTEXT (D-28 fwd-compat cols,
-- D-04 sentinels) + 10-PATTERNS §migration + 10-RESEARCH §"Schema details".
-- The two scheduler tables: scheduler_tasks (the schedule definitions, polled by the
-- tick loop) and agent_job_runs (the per-run audit ledger). Migration floor is 0008,
-- so scheduler is 0009 (pinned in amendment #46).
--
-- Role separation (T-10-02): aura_app gets DML only — DDL is reserved to aura_migrate.
-- agent_job_runs is audit-forever (T-10-03 / PRD OQ4): aura_app gets no DELETE, so a
-- completed run row can never be removed by the daemon.
--
-- The grammar is the at|every|cron triad with a per-task IANA tz; next_run_at is
-- ALWAYS stored UTC and recomputed in-zone (D-06/D-07), never a fixed offset.

CREATE TABLE aura.scheduler_tasks (
    id                     uuid        PRIMARY KEY,
    kind                   text        NOT NULL CHECK (kind IN ('reminder', 'agent_job', 'backup_postgres', 'backup_neo4j')),
    schedule_kind          text        NOT NULL CHECK (schedule_kind IN ('at', 'every', 'cron')),
    cron_expr              text,
    every_minutes          integer     CHECK (every_minutes IS NULL OR every_minutes >= 1),
    run_at                 timestamptz,
    tz                     text        NOT NULL DEFAULT 'Europe/Rome',
    payload                jsonb       NOT NULL DEFAULT '{}'::jsonb,
    step_budget            integer     CHECK (step_budget IS NULL OR step_budget > 0),
    status                 text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused', 'pending_approval', 'cancelled')),
    next_run_at            timestamptz,
    notify_route           text,
    identity_id            text        NOT NULL DEFAULT 'local',
    origin_conversation_id uuid,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE aura.agent_job_runs (
    id                  uuid        PRIMARY KEY,
    task_id             uuid        NOT NULL REFERENCES aura.scheduler_tasks (id) ON DELETE CASCADE,
    status              text        NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'completed', 'failed', 'unknown_recovery')),
    step_budget         integer,
    started_at          timestamptz NOT NULL DEFAULT now(),
    last_heartbeat_at   timestamptz NOT NULL DEFAULT now(),
    completed_with_hash text        UNIQUE,
    summary             text,
    last_error          text,
    missed_since        timestamptz,
    paused_state_token  uuid        REFERENCES aura.paused_states (token),
    completed_at        timestamptz
);

-- Partial index: the tick loop polls only active rows by next_run_at (RESEARCH
-- DueTasks). Plain (non-CONCURRENT) build on a fresh empty table inside the implicit
-- migration tx — golang-migrate forbids CONCURRENTLY in a tx block (0007 precedent).
CREATE INDEX scheduler_tasks_due_idx ON aura.scheduler_tasks (next_run_at)
    WHERE status = 'active';

-- Run-row lookups by task (orphan scan + last-run queries).
CREATE INDEX agent_job_runs_task_idx ON aura.agent_job_runs (task_id);

-- DML grants (NEVER TRUNCATE/DROP/CREATE to aura_app). scheduler_tasks needs DELETE
-- (cancel-by-removal is operator discretion; status='cancelled' is the soft path).
-- agent_job_runs is audit-forever: SELECT/INSERT/UPDATE only, NO DELETE (PRD OQ4).
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.scheduler_tasks TO aura_app;
GRANT ALL                            ON aura.scheduler_tasks TO aura_migrate;
GRANT SELECT, INSERT, UPDATE         ON aura.agent_job_runs   TO aura_app;
GRANT ALL                            ON aura.agent_job_runs   TO aura_migrate;

COMMENT ON TABLE aura.scheduler_tasks IS
    'Scheduler task definitions (Slice 6 / Phase 10, amendment #46). Grammar triad at|every|cron with per-task IANA tz; next_run_at is UTC, recomputed in-zone (D-06/D-07).';
COMMENT ON TABLE aura.agent_job_runs IS
    'Per-run audit ledger (Slice 6 / Phase 10). Audit-forever (no aura_app DELETE, PRD OQ4). completed_with_hash is the SC#2 idempotency key (UNIQUE; a duplicate completion swallows 23505).';
