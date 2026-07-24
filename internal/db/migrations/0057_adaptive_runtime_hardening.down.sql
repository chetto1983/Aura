REVOKE EXECUTE ON FUNCTION aura.next_adaptive_aggregate_sequence(uuid, text) FROM aura_app;

DROP TRIGGER IF EXISTS adaptive_outbox_fact_columns_immutable ON aura.adaptive_outbox;
DROP FUNCTION IF EXISTS aura.reject_adaptive_outbox_fact_update();
DROP FUNCTION IF EXISTS aura.next_adaptive_aggregate_sequence(uuid, text);

GRANT SELECT, INSERT, UPDATE, DELETE ON aura.adaptive_aggregate_sequences TO aura_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.adaptive_outbox TO aura_app;
