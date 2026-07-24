CREATE FUNCTION aura.adaptive_uuid_v5_sha256(
    p_namespace uuid,
    p_identity bytea
) RETURNS uuid
    LANGUAGE sql
    IMMUTABLE
    STRICT
    PARALLEL SAFE
    SET search_path = pg_catalog
AS $$
WITH digest AS (
    SELECT substring(
        sha256(uuid_send(p_namespace) || p_identity)
        FROM 1 FOR 16
    ) AS value
),
versioned AS (
    SELECT set_byte(
        value,
        6,
        (get_byte(value, 6) & 15) | 80
    ) AS value
    FROM digest
)
SELECT encode(
    set_byte(
        value,
        8,
        (get_byte(value, 8) & 63) | 128
    ),
    'hex'
)::uuid
FROM versioned;
$$;

CREATE FUNCTION aura.adaptive_schema2_assignment_id(
    p_owner_id uuid,
    p_request_id uuid,
    p_point text,
    p_point_ordinal bigint
) RETURNS uuid
    LANGUAGE plpgsql
    IMMUTABLE
    STRICT
    PARALLEL SAFE
    SET search_path = pg_catalog
AS $$
BEGIN
    IF p_point_ordinal NOT BETWEEN 0 AND 4294967295 THEN
        RAISE EXCEPTION USING
            ERRCODE = '22003',
            MESSAGE = 'schema-2 point ordinal is outside uint32 range';
    END IF;
    RETURN aura.adaptive_uuid_v5_sha256(
        'c5370396-c73f-4a44-ae7b-112f070523ae'::uuid,
        uuid_send(p_owner_id)
            || uuid_send(p_request_id)
            || '\x00'::bytea
            || convert_to(p_point, 'UTF8')
            || '\x00'::bytea
            || substring(int8send(p_point_ordinal) FROM 5 FOR 4)
    );
END;
$$;

CREATE FUNCTION aura.adaptive_schema2_event_id(
    p_assignment_id uuid,
    p_event_kind text
) RETURNS uuid
    LANGUAGE plpgsql
    IMMUTABLE
    STRICT
    PARALLEL SAFE
    SET search_path = pg_catalog
AS $$
BEGIN
    IF p_event_kind NOT IN ('decision', 'delivery') THEN
        RAISE EXCEPTION USING
            ERRCODE = '22023',
            MESSAGE = 'schema-2 deterministic event kind is unsupported';
    END IF;
    RETURN aura.adaptive_uuid_v5_sha256(
        'fb3f7ce9-d343-41fb-a26f-35155b229189'::uuid,
        convert_to('2.0', 'UTF8')
            || '\x00'::bytea
            || uuid_send(p_assignment_id)
            || '\x00'::bytea
            || convert_to(p_event_kind, 'UTF8')
            || '\x00'::bytea
            || convert_to('assignment', 'UTF8')
    );
END;
$$;

CREATE FUNCTION aura.adaptive_schema2_assignment_row_valid(
    p_id uuid,
    p_payload jsonb,
    p_owner_id uuid,
    p_aggregate_id text,
    p_decision_id uuid
) RETURNS boolean
    LANGUAGE plpgsql
    IMMUTABLE
    STRICT
    PARALLEL SAFE
    SET search_path = pg_catalog
AS $$
DECLARE
    expected_assignment_id uuid;
BEGIN
    IF NOT aura.adaptive_schema2_assignment_payload_valid(
        p_payload, p_owner_id, p_aggregate_id, p_decision_id
    ) THEN
        RETURN false;
    END IF;
    expected_assignment_id := aura.adaptive_schema2_assignment_id(
        p_owner_id,
        (p_payload->>'request_id')::uuid,
        p_payload->>'point',
        (p_payload->>'point_ordinal')::bigint
    );
    RETURN p_decision_id = expected_assignment_id
        AND (p_payload->>'assignment_id')::uuid = expected_assignment_id
        AND p_id = aura.adaptive_schema2_event_id(
            expected_assignment_id, 'decision'
        );
EXCEPTION
    WHEN data_exception OR invalid_text_representation
        OR numeric_value_out_of_range THEN
        RETURN false;
END;
$$;

CREATE FUNCTION aura.adaptive_schema2_delivery_row_valid(
    p_id uuid,
    p_payload jsonb,
    p_owner_id uuid,
    p_aggregate_id text,
    p_decision_id uuid
) RETURNS boolean
    LANGUAGE plpgsql
    STABLE
    STRICT
    PARALLEL SAFE
    SET search_path = pg_catalog
AS $$
DECLARE
    assignment_payload jsonb;
    expected_probability numeric;
BEGIN
    IF NOT aura.adaptive_schema2_delivery_payload_valid(
        p_payload, p_decision_id
    ) OR p_id <> aura.adaptive_schema2_event_id(
        p_decision_id, 'delivery'
    ) THEN
        RETURN false;
    END IF;
    SELECT assignment.payload INTO assignment_payload
    FROM aura.adaptive_outbox AS assignment
    WHERE assignment.owner_id = p_owner_id
      AND assignment.aggregate_id = p_aggregate_id
      AND assignment.decision_id = p_decision_id
      AND assignment.event_kind = 'decision'
      AND assignment.payload->>'schema_version' = '2.0'
      AND aura.adaptive_schema2_assignment_row_valid(
          assignment.id,
          assignment.payload,
          assignment.owner_id,
          assignment.aggregate_id,
          assignment.decision_id
      );
    IF assignment_payload IS NULL
        OR p_payload->>'intended_action_id'
            <> assignment_payload->>'intended_action_id'
        OR (
            p_payload->>'actual_action_id' <> 'none'
            AND NOT assignment_payload->'eligible_actions'
                ? (p_payload->>'actual_action_id')
        )
    THEN
        RETURN false;
    END IF;
    IF p_payload->>'status' = 'success'
        AND (p_payload->>'exposure_known')::boolean
    THEN
        SELECT (entry->>'probability')::numeric INTO expected_probability
        FROM jsonb_array_elements(
            assignment_payload->'action_probabilities'
        ) AS entry
        WHERE entry->>'action_id' = p_payload->>'intended_action_id';
        IF expected_probability IS NULL
            OR expected_probability <>
                (p_payload->>'exposure_probability')::numeric
        THEN
            RETURN false;
        END IF;
    END IF;
    RETURN true;
EXCEPTION
    WHEN data_exception OR invalid_text_representation
        OR numeric_value_out_of_range THEN
        RETURN false;
END;
$$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM aura.adaptive_outbox AS fact
        WHERE fact.payload->>'schema_version' = '2.0'
          AND (
              (
                  fact.event_kind = 'decision'
                  AND aura.adaptive_schema2_assignment_row_valid(
                      fact.id,
                      fact.payload,
                      fact.owner_id,
                      fact.aggregate_id,
                      fact.decision_id
                  ) IS NOT TRUE
              )
              OR (
                  fact.event_kind = 'delivery'
                  AND aura.adaptive_schema2_delivery_row_valid(
                      fact.id,
                      fact.payload,
                      fact.owner_id,
                      fact.aggregate_id,
                      fact.decision_id
                  ) IS NOT TRUE
              )
          )
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'schema-2 identity audit found an invalid fact';
    END IF;
END;
$$;

ALTER TABLE aura.adaptive_outbox
    ADD CONSTRAINT adaptive_outbox_schema2_assignment_identity_check CHECK (
        payload->>'schema_version' <> '2.0'
        OR event_kind <> 'decision'
        OR aura.adaptive_schema2_assignment_row_valid(
            id, payload, owner_id, aggregate_id, decision_id
        )
    ) NOT VALID;

CREATE UNIQUE INDEX adaptive_outbox_schema2_assignment_tuple_uidx
    ON aura.adaptive_outbox (
        owner_id,
        aggregate_id,
        (payload->>'point'),
        ((payload->>'point_ordinal')::numeric)
    )
    WHERE event_kind = 'decision'
      AND payload->>'schema_version' = '2.0';

CREATE OR REPLACE FUNCTION aura.enforce_adaptive_schema2_delivery_assignment()
RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog
AS $$
DECLARE
    assignment_payload jsonb;
    expected_probability numeric;
BEGIN
    IF NEW.event_kind <> 'delivery'
        OR NEW.payload->>'schema_version' <> '2.0'
    THEN
        RETURN NEW;
    END IF;
    IF NOT aura.adaptive_schema2_delivery_payload_valid(
        NEW.payload, NEW.decision_id
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'schema-2 adaptive delivery payload is invalid';
    END IF;
    IF NEW.id <> aura.adaptive_schema2_event_id(
        NEW.decision_id, 'delivery'
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'schema-2 adaptive delivery identity is invalid';
    END IF;
    SELECT assignment.payload INTO assignment_payload
    FROM aura.adaptive_outbox AS assignment
    WHERE assignment.owner_id = NEW.owner_id
      AND assignment.aggregate_id = NEW.aggregate_id
      AND assignment.decision_id = NEW.decision_id
      AND assignment.event_kind = 'decision'
      AND assignment.payload->>'schema_version' = '2.0'
      AND aura.adaptive_schema2_assignment_row_valid(
          assignment.id,
          assignment.payload,
          assignment.owner_id,
          assignment.aggregate_id,
          assignment.decision_id
      );
    IF assignment_payload IS NULL THEN
        RAISE EXCEPTION USING
            ERRCODE = '23503',
            MESSAGE = 'schema-2 adaptive delivery has no matching assignment';
    END IF;
    IF NEW.payload->>'intended_action_id'
            <> assignment_payload->>'intended_action_id'
        OR (
            NEW.payload->>'actual_action_id' <> 'none'
            AND NOT assignment_payload->'eligible_actions'
                ? (NEW.payload->>'actual_action_id')
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
