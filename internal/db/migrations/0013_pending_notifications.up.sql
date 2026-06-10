-- Source: Phase 19 plan 19-07 (H6/H7). Durable notification state backs
-- quiet-hours deferral and bounded retry for undelivered scheduler notices.
--
-- Role separation mirrors 0009_scheduler: aura_app gets DML only and no DELETE;
-- DDL stays with aura_migrate. Rows are cleaned up by agent_job_runs cascade.

CREATE TABLE aura.pending_notifications (
    id           uuid        PRIMARY KEY,
    run_id       uuid        NOT NULL REFERENCES aura.agent_job_runs (id) ON DELETE CASCADE,
    notify_route text,
    body         text        NOT NULL,
    notify_after timestamptz NOT NULL,
    attempts     integer     NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error   text,
    status       text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'failed', 'delivered')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

-- Partial index: the tick sweep polls only pending rows whose notify_after is due.
-- Plain (non-CONCURRENT) build inside the implicit migration tx; see 0009.
CREATE INDEX pending_notifications_due_idx
    ON aura.pending_notifications (notify_after)
    WHERE status = 'pending';

GRANT SELECT, INSERT, UPDATE ON aura.pending_notifications TO aura_app;
GRANT ALL                    ON aura.pending_notifications TO aura_migrate;

COMMENT ON TABLE aura.pending_notifications IS
    'Durable scheduler notification queue (Phase 19 H6/H7): quiet-hours deferred notifications and failed self-send retry state.';
