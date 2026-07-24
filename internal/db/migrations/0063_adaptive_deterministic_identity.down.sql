CREATE OR REPLACE FUNCTION aura.enforce_adaptive_schema2_delivery_assignment()
RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog
AS $$
DECLARE
    assignment_payload jsonb;
    expected_probability numeric;
BEGIN
    IF NEW.event_kind <> 'delivery' OR NEW.payload->>'schema_version' <> '2.0' THEN
        RETURN NEW;
    END IF;
    SELECT assignment.payload INTO assignment_payload
    FROM aura.adaptive_outbox AS assignment
    WHERE assignment.owner_id = NEW.owner_id
      AND assignment.aggregate_id = NEW.aggregate_id
      AND assignment.decision_id = NEW.decision_id
      AND assignment.event_kind = 'decision'
      AND assignment.payload->>'schema_version' = '2.0';
    IF assignment_payload IS NULL THEN
        RAISE EXCEPTION USING
            ERRCODE = '23503',
            MESSAGE = 'schema-2 adaptive delivery has no matching assignment';
    END IF;
    IF NEW.payload->>'intended_action_id' <> assignment_payload->>'intended_action_id'
        OR (
            NEW.payload->>'actual_action_id' <> 'none'
            AND NOT assignment_payload->'eligible_actions' ?
                (NEW.payload->>'actual_action_id')
        )
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'schema-2 adaptive delivery does not match its assignment';
    END IF;
    IF NEW.payload->>'status' = 'success'
        AND (NEW.payload->>'exposure_known')::boolean
    THEN
        SELECT (entry->>'probability')::numeric INTO expected_probability
        FROM jsonb_array_elements(
            assignment_payload->'action_probabilities'
        ) AS entry
        WHERE entry->>'action_id' = NEW.payload->>'intended_action_id';
        IF expected_probability IS NULL
            OR expected_probability <>
                (NEW.payload->>'exposure_probability')::numeric
        THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE = 'schema-2 adaptive delivery exposure does not match its assignment';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

DROP INDEX IF EXISTS aura.adaptive_outbox_schema2_assignment_tuple_uidx;

ALTER TABLE aura.adaptive_outbox
    DROP CONSTRAINT IF EXISTS adaptive_outbox_schema2_assignment_identity_check;

DROP FUNCTION IF EXISTS aura.adaptive_schema2_delivery_row_valid(
    uuid, jsonb, uuid, text, uuid
);
DROP FUNCTION IF EXISTS aura.adaptive_schema2_assignment_row_valid(
    uuid, jsonb, uuid, text, uuid
);
DROP FUNCTION IF EXISTS aura.adaptive_schema2_event_id(uuid, text);
DROP FUNCTION IF EXISTS aura.adaptive_schema2_assignment_id(
    uuid, uuid, text, bigint
);
DROP FUNCTION IF EXISTS aura.adaptive_uuid_v5_sha256(uuid, bytea);
