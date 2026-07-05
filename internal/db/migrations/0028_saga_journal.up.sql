-- Source: PRD Phase 36 (D-14/D-27 resumable provisioning) + 36-02-PLAN Task 1 +
-- 36-RESEARCH Pattern 4 (saga journal DDL). The migration floor before this is 0027
-- (paused_states_identity); the saga journal lands at 0028.
--
-- aura.provisioning_saga is the forward-recovery journal for identity provisioning and
-- de-provisioning (plan 08). Unlike the append-only audit ledgers (0010/0011/0021/0022),
-- this table is MUTABLE: a step row transitions pending -> done (or pending -> failed) as
-- the saga advances, so a crash mid-provision leaves a durable record the resume path can
-- replay. It therefore uses the 0023 mutable-table grant shape (SELECT, INSERT, UPDATE),
-- NOT the append-only trigger + SELECT/INSERT-only posture.
--
-- No FK on identity_id (deliberate, mirrors pending_notifications.identity_id, 0014): the
-- de-provision saga must survive the deletion of the very identity it is tearing down, so
-- an ON DELETE CASCADE FK would erase the journal that is driving the teardown. The
-- primary key (saga_id, step) makes each step idempotently upsertable by the resume loop.

CREATE TABLE aura.provisioning_saga (
    saga_id     uuid        NOT NULL,
    identity_id uuid        NOT NULL,
    kind        text        NOT NULL CHECK (kind IN ('provision', 'deprovision')),
    step        text        NOT NULL,
    status      text        NOT NULL CHECK (status IN ('pending', 'done', 'failed')),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (saga_id, step)
);

-- Mutable-table grant shape (0023 model): aura_app advances step status in place.
GRANT SELECT, INSERT, UPDATE ON aura.provisioning_saga TO aura_app;
GRANT ALL                    ON aura.provisioning_saga TO aura_migrate;

COMMENT ON TABLE aura.provisioning_saga IS
    'Resumable provisioning/de-provisioning saga journal (Phase 36, D-14/D-27). MUTABLE: step status transitions pending -> done/failed for forward recovery. identity_id has NO FK so a de-provision saga survives the identity deletion it is executing.';
