LOCK TABLE aura.adaptive_outbox IN SHARE MODE;

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
            MESSAGE = 'schema-2 locked identity audit found an invalid fact';
    END IF;
END;
$$;
