-- Drops the record of which Go data upgrades have run. Going up again re-runs the PIM tool split
-- upgrade once, which finds no old-shape row left to rewrite, but does delete every grant and
-- policy named after a PIM tool as if it had been typed by hand before the split.
DROP TABLE IF EXISTS aura.data_upgrades;
