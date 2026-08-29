-- Widens aura.steer_queue (migration 0103) by exactly one nullable column, nothing else
-- (51-11 Task 3, CONTEXT D-15 / PRD Amendment #172 point 2: "uno per fan-out"). The
-- absent-operator nudge sweep (delegation_delivery.go's NudgeUndrained) groups its
-- candidate rows and sends ONE Telegram message per fan-out, not one per worker; this
-- column is the grouping key that makes the grouping a plain WHERE/GROUP BY instead of an
-- application-side join back to aura.ingestion_jobs.
--
-- conversation_id cannot do this job: two swarm_spawn calls issued in the SAME
-- conversation are two DIFFERENT fan-outs (a second delegation while the first is still
-- running is ordinary usage, not an edge case), so grouping by conversation_id alone would
-- merge them into one message covering unrelated goals.
--
-- NULL means "not part of a fan-out" -- every steer-kind row (a fan-out key only ever
-- means something for a delegation_result row) and every delegation_result row written
-- before this migration ran. A NULL-keyed row is swept one row at a time through the
-- pre-existing per-row nudgeOne path, exactly as it was before this column existed; no
-- pre-migration row is ever stranded by the grouped path.
ALTER TABLE aura.steer_queue
    ADD COLUMN fanout_key text;

COMMENT ON COLUMN aura.steer_queue.fanout_key IS
    '51-11 Task 3 (CONTEXT D-15): groups the N delegation_result rows produced by ONE '
    'swarm_spawn call so the absent-operator nudge sweep sends ONE Telegram message for '
    'the whole fan-out, not one per worker. conversation_id cannot do this job -- two '
    'swarm_spawn calls in one conversation are two DIFFERENT fan-outs. NULL means "not '
    'part of a fan-out": every steer-kind row, and every delegation_result row written '
    'before this migration -- both keep sweeping through the pre-existing per-row path.';

-- The grouped claim (MarkFanoutNudged) is the only query that reads this column, and it is
-- the sweep's hot path (fires on every NudgeUndrained tick): restricted to undrained
-- delegation_result rows carrying a non-null key, mirroring steer_queue_undrained_idx's
-- own restriction to the rows a sweep can act on.
CREATE INDEX steer_queue_fanout_undrained_idx
    ON aura.steer_queue (identity_id, fanout_key)
    WHERE drained_at IS NULL AND expired_at IS NULL AND fanout_key IS NOT NULL;

-- No GRANT statement: fanout_key is a column on an existing table, and aura_app /
-- aura_migrate already hold table-level SELECT/INSERT/UPDATE/DELETE and ALL respectively
-- (migration 0103) -- a column-level grant would be redundant and, per Postgres's own
-- privilege model, is never required for a column added to a table the role already has
-- table-level access to.
