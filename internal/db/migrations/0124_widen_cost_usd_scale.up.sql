-- Source: phase 02-two-roles-and-a-budget plan 07's checkpoint (T-02-36, CRED-06).
-- Migration floor before this is 0123 (capability_denials); lands at 0124, measured
-- from `ls internal/db/migrations/ | tail -1` at landing time, per CLAUDE.md C-04.
--
-- Both aura.cache_metrics.cost_usd (0007) and aura.conversations.total_cost_usd (0005)
-- were numeric(10,4) -- four fractional digits. The per-call cost measured live on
-- 2026-09-08 (02-CONTEXT.md M-09) is 0.000004158, SEVEN fractional digits: every cheap
-- call was rounding to 0.0000 in the in-band ledger D-08 makes the source of the
-- displayed balance AND the CRED-05 pre-flight refusal.
--
-- The checkpoint's approved scale is numeric(24,12) -- not the plan's own recommended
-- numeric(20,10) -- decided by the operator at dispatch time: 24 total digits, 12
-- fractional, giving five digits of headroom past the measured value's 7 while keeping
-- 12 integer digits for account-scale totals. Widening a numeric column's precision/
-- scale upward is lossless and needs no data rewrite (existing values are exact
-- subsets of the new, wider representation).
--
-- ALTER COLUMN ... TYPE numeric(24,12) on cache_metrics.cost_usd only. Widening
-- conversations.total_cost_usd is a SEPARATE statement (below, still commented out --
-- see the plan 02-07-PLAN.md Task 1 read_first note on 0047's trigger) intentionally
-- landing in the immediately following migration edit, not this one.
ALTER TABLE aura.cache_metrics
    ALTER COLUMN cost_usd TYPE numeric(24, 12);

COMMENT ON COLUMN aura.cache_metrics.cost_usd IS
    'Per-turn cost at full provider precision (migration 0124, numeric(24,12), T-02-36). Widened from numeric(10,4): the pre-widening scale rounded any per-call cost under $0.0001 to zero, including the measured 0.000004158 (02-CONTEXT.md M-09).';
