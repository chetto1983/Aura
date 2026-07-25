CREATE OR REPLACE FUNCTION aura.enforce_adaptive_schema2_outcome_chain()
RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog
AS $$
DECLARE
    assignment_payload jsonb;
    target_payload jsonb;
    target_kind text;
    chain_depth integer;
    chain_cycle boolean;
BEGIN
    IF NEW.payload->>'schema_version' <> '2.0'
        OR NEW.event_kind NOT IN ('outcome', 'correction')
    THEN
        RETURN NEW;
    END IF;
    PERFORM 1
    FROM aura.identities
    WHERE id = NEW.owner_id
    FOR KEY SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING
            ERRCODE = '23503',
            MESSAGE = 'schema-2 adaptive outcome owner does not exist';
    END IF;
    SELECT assignment.payload INTO assignment_payload
    FROM aura.adaptive_outbox AS assignment
    WHERE assignment.owner_id = NEW.owner_id
      AND assignment.aggregate_id = NEW.aggregate_id
      AND assignment.decision_id = NEW.decision_id
      AND assignment.event_kind = 'decision'
      AND assignment.payload->>'schema_version' = '2.0'
      AND aura.adaptive_schema2_assignment_row_valid(
          assignment.id, assignment.payload, assignment.owner_id,
          assignment.aggregate_id, assignment.decision_id
      )
    FOR UPDATE;
    IF assignment_payload IS NULL
        OR NOT aura.adaptive_schema2_outcome_row_valid(
            NEW.id, NEW.payload, NEW.owner_id, NEW.aggregate_id,
            NEW.decision_id, NEW.event_kind
        )
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'schema-2 adaptive outcome does not match its assignment';
    END IF;
    IF NEW.event_kind = 'outcome' THEN
        RETURN NEW;
    END IF;
    SELECT target.payload, target.event_kind
    INTO target_payload, target_kind
    FROM aura.adaptive_outbox AS target
    WHERE target.id = (NEW.payload->>'supersedes_event_id')::uuid
      AND target.owner_id = NEW.owner_id
      AND target.decision_id = NEW.decision_id
      AND target.event_kind IN ('outcome', 'correction')
      AND target.payload->>'schema_version' = '2.0'
    FOR SHARE;
    IF target_payload IS NULL THEN
        RAISE EXCEPTION USING
            ERRCODE = '23503',
            MESSAGE = 'schema-2 adaptive correction target is missing';
    END IF;
    IF target_payload->'evaluator' <> NEW.payload->'evaluator'
        OR target_payload->>'domain' <> NEW.payload->>'domain'
        OR target_payload->>'provider_id' <> NEW.payload->>'provider_id'
        OR target_payload->>'model_id' <> NEW.payload->>'model_id'
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'schema-2 adaptive correction target scope is invalid';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM aura.adaptive_outbox AS child
        WHERE child.owner_id = NEW.owner_id
          AND child.decision_id = NEW.decision_id
          AND child.event_kind = 'correction'
          AND child.payload->>'schema_version' = '2.0'
          AND (child.payload->>'supersedes_event_id')::uuid =
              (NEW.payload->>'supersedes_event_id')::uuid
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23505',
            MESSAGE = 'schema-2 adaptive correction would fork its chain';
    END IF;
    WITH RECURSIVE ancestors AS (
        SELECT target.id, target.event_kind, target.payload,
               1 AS depth, ARRAY[target.id] AS path, false AS cycle
        FROM aura.adaptive_outbox AS target
        WHERE target.id = (NEW.payload->>'supersedes_event_id')::uuid
        UNION ALL
        SELECT parent.id, parent.event_kind, parent.payload,
               ancestors.depth + 1,
               ancestors.path || parent.id,
               parent.id = ANY(ancestors.path)
        FROM ancestors
        JOIN aura.adaptive_outbox AS parent
          ON ancestors.event_kind = 'correction'
         AND parent.id =
             (ancestors.payload->>'supersedes_event_id')::uuid
        WHERE NOT ancestors.cycle
          AND ancestors.depth <= 64
    )
    SELECT max(depth), bool_or(cycle)
    INTO chain_depth, chain_cycle
    FROM ancestors;
    IF COALESCE(chain_cycle, false) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'schema-2 adaptive correction chain cycles';
    END IF;
    IF chain_depth > 64 THEN
        RAISE EXCEPTION USING
            ERRCODE = '54000',
            MESSAGE = 'schema-2 adaptive correction chain is too long';
    END IF;
    RETURN NEW;
END;
$$;
