-- Reverses 0124_widen_cost_usd_scale.up.sql.
--
-- THIS DIRECTION LOSES DATA. Narrowing numeric(24,12) back to numeric(10,4) rounds
-- every value to 4 fractional digits: any sub-$0.0001 cost written while the wider
-- column was in force (which is the whole point of the widening -- see the up.sql
-- comment) rounds to 0.0000 on the way back down. The operator accepted this
-- one-way cost explicitly at the checkpoint that approved this migration
-- (02-07-PLAN.md, "that direction loses data" / "one-way"). Do not run this in
-- production against a database that has accumulated sub-$0.0001 rows unless that
-- precision loss is intended.
ALTER TABLE aura.cache_metrics
    ALTER COLUMN cost_usd TYPE numeric(10, 4);
