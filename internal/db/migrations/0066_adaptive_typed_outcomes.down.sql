DROP TRIGGER adaptive_outbox_schema2_outcome_chain
    ON aura.adaptive_outbox;
DROP FUNCTION aura.enforce_adaptive_schema2_outcome_chain();

DROP INDEX aura.adaptive_outbox_schema2_correction_target_uidx;
DROP INDEX aura.adaptive_outbox_schema2_correction_source_uidx;
DROP INDEX aura.adaptive_outbox_schema2_outcome_source_uidx;

ALTER TABLE aura.adaptive_outbox
    DROP CONSTRAINT adaptive_outbox_schema2_correction_check,
    DROP CONSTRAINT adaptive_outbox_schema2_outcome_check;

DROP FUNCTION aura.adaptive_schema2_outcome_row_valid(
    uuid, jsonb, uuid, text, uuid, text
);
DROP FUNCTION aura.adaptive_schema2_sourced_event_id(
    uuid, text, jsonb, uuid
);
DROP FUNCTION aura.adaptive_schema2_correction_payload_valid(jsonb, uuid);
DROP FUNCTION aura.adaptive_schema2_outcome_payload_valid(jsonb, uuid);
DROP FUNCTION aura.adaptive_schema2_outcome_measures_valid(text, jsonb);
DROP FUNCTION aura.adaptive_schema2_evaluator_valid(jsonb);
