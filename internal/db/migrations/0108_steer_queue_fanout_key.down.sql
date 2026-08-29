-- Roll back 0108: drop the fan-out grouping index, then the column. No data-loss guard is
-- needed here (unlike 0107's rollback block): fanout_key is a derived grouping key, never
-- the sole record of anything -- every row it would strand is still fully described by its
-- own body/kind/source/timestamps, and a rolled-back column simply returns every future
-- sweep to the pre-0108 per-row nudgeOne path.
DROP INDEX IF EXISTS aura.steer_queue_fanout_undrained_idx;

ALTER TABLE aura.steer_queue
    DROP COLUMN IF EXISTS fanout_key;
