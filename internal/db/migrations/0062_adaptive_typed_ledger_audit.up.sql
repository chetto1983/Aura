DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM aura.adaptive_outbox AS assignment
        WHERE assignment.event_kind = 'decision'
          AND assignment.payload->>'schema_version' = '2.0'
          AND aura.adaptive_schema2_assignment_payload_valid(
              assignment.payload,
              assignment.owner_id,
              assignment.aggregate_id,
              assignment.decision_id
          ) IS NOT TRUE
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'schema-2 audit found an invalid assignment';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM aura.adaptive_outbox AS delivery
        WHERE delivery.event_kind = 'delivery'
          AND delivery.payload->>'schema_version' = '2.0'
          AND aura.adaptive_schema2_delivery_payload_valid(
              delivery.payload,
              delivery.decision_id
          ) IS NOT TRUE
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'schema-2 audit found an invalid delivery';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM aura.adaptive_outbox AS delivery
        WHERE delivery.event_kind = 'delivery'
          AND delivery.payload->>'schema_version' = '2.0'
          AND NOT EXISTS (
              SELECT 1
              FROM aura.adaptive_outbox AS assignment
              WHERE assignment.owner_id = delivery.owner_id
                AND assignment.aggregate_id = delivery.aggregate_id
                AND assignment.decision_id = delivery.decision_id
                AND assignment.event_kind = 'decision'
                AND assignment.payload->>'schema_version' = '2.0'
                AND delivery.payload->>'intended_action_id' =
                    assignment.payload->>'intended_action_id'
                AND (
                    delivery.payload->>'actual_action_id' = 'none'
                    OR (assignment.payload->'eligible_actions') ?
                        (delivery.payload->>'actual_action_id')
                )
                AND (
                    delivery.payload->>'status' <> 'success'
                    OR NOT (delivery.payload->>'exposure_known')::boolean
                    OR EXISTS (
                        SELECT 1
                        FROM jsonb_array_elements(
                            assignment.payload->'action_probabilities'
                        ) AS probability
                        WHERE probability->>'action_id' =
                            delivery.payload->>'intended_action_id'
                          AND (probability->>'probability')::numeric =
                              (delivery.payload->>'exposure_probability')::numeric
                    )
                )
          )
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'schema-2 audit found a delivery not bound to its assignment';
    END IF;
END;
$$;
