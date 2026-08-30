-- Delegation terminal reports are retried independently from worker execution.
-- A stable source_ref makes their asset row/object placement idempotent across
-- archive success followed by a process death before the queue checkpoint.
CREATE UNIQUE INDEX assets_identity_agent_source_ref_idx
    ON aura.assets (identity_id, source_kind, source_ref)
    WHERE source_kind = 'agent' AND source_ref <> '';
