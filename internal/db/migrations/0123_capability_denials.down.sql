-- Reverses 0123_capability_denials.up.sql. The ledger is derived evidence, not state
-- anything else depends on for correctness (02-04-PLAN reversibility rating: costly, not
-- one-way) -- dropping it loses the recorded denials but nothing else.
DROP TABLE IF EXISTS aura.capability_denials;
