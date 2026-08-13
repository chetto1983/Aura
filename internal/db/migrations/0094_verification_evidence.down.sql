-- Roll back the verification evidence ledger.
--
-- No guard clause and no data check: the ledger is derived, passive and reconstructible
-- by working again. Nothing references it (the policy that reads it fails open when the
-- ledger is unavailable), so dropping it costs history, never correctness. Contrast
-- 0093's rollback, which refuses when verified deletion has happened, because there the
-- rows ARE the record of an irreversible act.
--
-- verification_state first: its last_event_id references verification_events.

DROP TABLE IF EXISTS aura.verification_state;
DROP TABLE IF EXISTS aura.verification_events;
