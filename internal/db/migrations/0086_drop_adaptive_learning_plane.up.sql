-- Drop the adaptive-learning plane: twelve tables, their triggers and validation
-- functions, and the tombstone trigger they hung on aura.identities.
--
-- Measured on the live deployment over 648 turns / 32 conversations between
-- 2026-07-29 and 2026-08-01: eleven of the twelve tables took ZERO writes.
-- adaptive_policy_state held one row (`global | bootstrap-off | off`) and
-- adaptive_identity_tombstones held 17, every one written by the deprovisioning
-- code that DELETES the plane. It could not bootstrap out of `off` either:
-- promotion needs evidence, evidence needs cohorts and randomization receipts,
-- and those are only produced while it is already on.
--
-- The per-turn adaptive REASONING tier is a different feature and is untouched by
-- this migration — it keeps no state in Postgres at all.

DROP TRIGGER IF EXISTS identities_adaptive_tombstone_trg ON aura.identities;

DROP TABLE IF EXISTS aura.adaptive_sealed_evidence          CASCADE;
DROP TABLE IF EXISTS aura.adaptive_evidence_artifacts       CASCADE;
DROP TABLE IF EXISTS aura.adaptive_randomization_receipts   CASCADE;
DROP TABLE IF EXISTS aura.adaptive_focal_cohort_claims      CASCADE;
DROP TABLE IF EXISTS aura.adaptive_focal_cohorts            CASCADE;
DROP TABLE IF EXISTS aura.adaptive_policy_snapshots         CASCADE;
DROP TABLE IF EXISTS aura.adaptive_promotion_evidence       CASCADE;
DROP TABLE IF EXISTS aura.adaptive_policy_transitions       CASCADE;
DROP TABLE IF EXISTS aura.adaptive_policy_state             CASCADE;
DROP TABLE IF EXISTS aura.adaptive_aggregate_sequences      CASCADE;
DROP TABLE IF EXISTS aura.adaptive_identity_tombstones      CASCADE;
DROP TABLE IF EXISTS aura.adaptive_outbox                   CASCADE;

-- The plane installed 43 functions across migrations 0052-0076 under several
-- prefixes (adaptive_*, enforce_adaptive_*, reject_adaptive_*, set_adaptive_*,
-- apply_/seal_/store_/fence_/tombstone_/next_adaptive_*), a number of them
-- overloaded. Enumerating pg_proc drops every signature without a hand-maintained
-- list that would rot; the name filter is safe because NO other object in schema
-- aura contains "adaptive" (verified against the live schema, 2026-08-02).
DO $$
DECLARE
    routine record;
BEGIN
    FOR routine IN
        SELECT p.oid::regprocedure AS signature
        FROM pg_proc p
        JOIN pg_namespace n ON n.oid = p.pronamespace
        WHERE n.nspname = 'aura'
          AND p.proname LIKE '%adaptive%'
    LOOP
        EXECUTE format('DROP FUNCTION IF EXISTS %s CASCADE', routine.signature);
    END LOOP;
END $$;
