DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM aura.adaptive_outbox
        WHERE event_kind = 'delivery'
    ) THEN
        RAISE EXCEPTION
            'cannot downgrade adaptive typed ledger while live delivery rows exist';
    END IF;
END;
$$;

DROP TRIGGER adaptive_outbox_schema2_delivery_assignment
    ON aura.adaptive_outbox;
DROP FUNCTION aura.enforce_adaptive_schema2_delivery_assignment();

DROP INDEX aura.adaptive_outbox_schema2_delivery_owner_decision_uidx;
DROP INDEX aura.adaptive_outbox_schema2_assignment_owner_decision_uidx;

ALTER TABLE aura.adaptive_outbox
    DROP CONSTRAINT adaptive_outbox_schema2_delivery_check,
    DROP CONSTRAINT adaptive_outbox_schema2_assignment_check,
    DROP CONSTRAINT adaptive_outbox_delivery_schema_check,
    DROP CONSTRAINT adaptive_outbox_event_kind_check;
ALTER TABLE aura.adaptive_outbox
    ADD CONSTRAINT adaptive_outbox_event_kind_check CHECK (
        event_kind IN ('decision', 'outcome', 'correction', 'promotion', 'rollback')
    );

DROP FUNCTION aura.adaptive_schema2_delivery_payload_valid(jsonb, uuid);
DROP FUNCTION aura.adaptive_schema2_assignment_payload_valid(jsonb, uuid, text, uuid);
