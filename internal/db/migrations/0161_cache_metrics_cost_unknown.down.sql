-- Reverts 0161: an unknown cost reads as 0 again, as it did before.
UPDATE aura.cache_metrics SET cost_usd = 0 WHERE cost_usd IS NULL;

ALTER TABLE aura.cache_metrics
    ALTER COLUMN cost_usd SET DEFAULT 0,
    ALTER COLUMN cost_usd SET NOT NULL;

COMMENT ON COLUMN aura.cache_metrics.cost_usd IS
    'Per-turn cost at full provider precision (migration 0124, numeric(24,12), T-02-36). Widened from numeric(10,4): the pre-widening scale rounded any per-call cost under $0.0001 to zero, including the measured 0.000004158 (02-CONTEXT.md M-09).';
