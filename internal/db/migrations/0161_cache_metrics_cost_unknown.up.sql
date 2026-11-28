-- Source: fix wave B of docs/superpowers/plans/2026-11-27-anthropic-provider.md (D-18).
-- Lands at 0161, the slot after `ls internal/db/migrations/ | tail -1` (0160) at landing time.
--
-- aura.cache_metrics.cost_usd was NOT NULL DEFAULT 0, so a turn with no honest price (a Claude
-- turn none of whose calls was priced, a model with no catalogue entry) was stored as 0, and the
-- daily sum read it as a free turn: Telegram /cost answered "$0.000000" for a day of unpriced
-- Claude turns. NULL now means unknown. sum() skips it, so every spend read stays the priced
-- sum, and AggregateCacheMetricsSince counts the unpriced rows beside it. The default goes too:
-- a row written without a cost is an unknown one, not a free one. Rows written before this
-- migration keep their 0; which of them were unpriced is not recorded anywhere.
ALTER TABLE aura.cache_metrics
    ALTER COLUMN cost_usd DROP DEFAULT,
    ALTER COLUMN cost_usd DROP NOT NULL;

COMMENT ON COLUMN aura.cache_metrics.cost_usd IS
    'Per-turn cost at full provider precision (migration 0124, numeric(24,12), T-02-36); NULL when the turn has no honest price (migration 0161).';
