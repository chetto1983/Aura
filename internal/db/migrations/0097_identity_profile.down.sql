-- Drop the operator profile table.
--
-- The data is recoverable by re-saving the onboarding form, and every identity that
-- onboarded before this migration still has the same answers as facts in its memory graph
-- (the write path kept both until this landed). Rolling back therefore costs the
-- deterministic always-block profile and the runtime timezone, not the answers themselves.

DROP TABLE IF EXISTS aura.identity_profiles;
