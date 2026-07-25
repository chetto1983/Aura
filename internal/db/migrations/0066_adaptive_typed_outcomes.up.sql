CREATE FUNCTION aura.adaptive_schema2_evaluator_valid(
    p_evaluator jsonb
) RETURNS boolean
    LANGUAGE plpgsql
    IMMUTABLE
    STRICT
    PARALLEL SAFE
    SET search_path = pg_catalog
AS $$
BEGIN
    RETURN jsonb_typeof(p_evaluator) = 'object'
        AND p_evaluator ?& ARRAY[
            'kind', 'id', 'version', 'rubric_id',
            'calibration_id', 'provenance_sha256'
        ]
        AND (SELECT count(*) FROM jsonb_object_keys(p_evaluator)) = 6
        AND jsonb_typeof(p_evaluator->'kind') = 'string'
        AND p_evaluator->>'kind' IN (
            'deterministic', 'human', 'calibrated_judge'
        )
        AND jsonb_typeof(p_evaluator->'id') = 'string'
        AND aura.adaptive_schema2_ascii_id_valid(p_evaluator->>'id', 128)
        AND jsonb_typeof(p_evaluator->'version') = 'string'
        AND aura.adaptive_schema2_ascii_id_valid(p_evaluator->>'version', 128)
        AND jsonb_typeof(p_evaluator->'rubric_id') = 'string'
        AND aura.adaptive_schema2_ascii_id_valid(p_evaluator->>'rubric_id', 128)
        AND jsonb_typeof(p_evaluator->'calibration_id') = 'string'
        AND aura.adaptive_schema2_ascii_id_valid(
            p_evaluator->>'calibration_id', 128
        )
        AND jsonb_typeof(p_evaluator->'provenance_sha256') = 'string'
        AND p_evaluator->>'provenance_sha256' ~ '^[0-9a-f]{64}$';
EXCEPTION
    WHEN data_exception OR invalid_text_representation
        OR numeric_value_out_of_range THEN
        RETURN false;
END;
$$;

CREATE FUNCTION aura.adaptive_schema2_outcome_measures_valid(
    p_status text,
    p_measures jsonb
) RETURNS boolean
    LANGUAGE plpgsql
    IMMUTABLE
    STRICT
    PARALLEL SAFE
    SET search_path = pg_catalog
AS $$
DECLARE
    quality numeric;
    harm numeric;
    latency_ms numeric;
    tokens numeric;
    cost_microunits numeric;
BEGIN
    IF jsonb_typeof(p_measures) <> 'object'
        OR NOT p_measures ?& ARRAY[
            'quality', 'harm', 'latency_ms', 'tokens', 'cost_microunits'
        ]
        OR (SELECT count(*) FROM jsonb_object_keys(p_measures)) <> 5
        OR p_status NOT IN ('observed', 'censored')
    THEN
        RETURN false;
    END IF;
    IF p_status = 'censored' THEN
        RETURN jsonb_typeof(p_measures->'quality') = 'null'
            AND jsonb_typeof(p_measures->'harm') = 'null'
            AND jsonb_typeof(p_measures->'latency_ms') = 'null'
            AND jsonb_typeof(p_measures->'tokens') = 'null'
            AND jsonb_typeof(p_measures->'cost_microunits') = 'null';
    END IF;
    IF jsonb_typeof(p_measures->'quality') <> 'number'
        OR jsonb_typeof(p_measures->'harm') <> 'number'
    THEN
        RETURN false;
    END IF;
    quality := (p_measures->>'quality')::numeric;
    harm := (p_measures->>'harm')::numeric;
    IF quality NOT BETWEEN 0 AND 1 OR harm NOT BETWEEN 0 AND 1 THEN
        RETURN false;
    END IF;
    IF jsonb_typeof(p_measures->'latency_ms') NOT IN ('null', 'number')
        OR jsonb_typeof(p_measures->'tokens') NOT IN ('null', 'number')
        OR jsonb_typeof(p_measures->'cost_microunits') NOT IN ('null', 'number')
    THEN
        RETURN false;
    END IF;
    IF jsonb_typeof(p_measures->'latency_ms') = 'number' THEN
        latency_ms := (p_measures->>'latency_ms')::numeric;
        IF latency_ms <> trunc(latency_ms)
            OR latency_ms NOT BETWEEN 0 AND 86400000
        THEN
            RETURN false;
        END IF;
    END IF;
    IF jsonb_typeof(p_measures->'tokens') = 'number' THEN
        tokens := (p_measures->>'tokens')::numeric;
        IF tokens <> trunc(tokens) OR tokens NOT BETWEEN 0 AND 1000000000 THEN
            RETURN false;
        END IF;
    END IF;
    IF jsonb_typeof(p_measures->'cost_microunits') = 'number' THEN
        cost_microunits := (p_measures->>'cost_microunits')::numeric;
        IF cost_microunits <> trunc(cost_microunits)
            OR cost_microunits NOT BETWEEN 0 AND 1000000000000
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

CREATE FUNCTION aura.adaptive_schema2_outcome_payload_valid(
    p_payload jsonb,
    p_decision_id uuid
) RETURNS boolean
    LANGUAGE plpgsql
    IMMUTABLE
    STRICT
    PARALLEL SAFE
    SET search_path = pg_catalog
AS $$
BEGIN
    RETURN jsonb_typeof(p_payload) = 'object'
        AND p_payload ?& ARRAY[
            'schema_version', 'assignment_id', 'owner_id', 'domain',
            'provider_id', 'model_id', 'evaluator',
            'source_observation_id', 'status', 'measures', 'observed_at'
        ]
        AND (SELECT count(*) FROM jsonb_object_keys(p_payload)) = 11
        AND jsonb_typeof(p_payload->'schema_version') = 'string'
        AND p_payload->>'schema_version' = '2.0'
        AND jsonb_typeof(p_payload->'assignment_id') = 'string'
        AND (p_payload->>'assignment_id')::uuid = p_decision_id
        AND (p_payload->>'assignment_id')::uuid::text =
            p_payload->>'assignment_id'
        AND jsonb_typeof(p_payload->'owner_id') = 'string'
        AND (p_payload->>'owner_id')::uuid::text = p_payload->>'owner_id'
        AND jsonb_typeof(p_payload->'domain') = 'string'
        AND p_payload->>'domain' IN (
            'reasoning', 'tool_discovery', 'skill_routing',
            'knowledge_retrieval', 'memory_recall'
        )
        AND jsonb_typeof(p_payload->'provider_id') = 'string'
        AND aura.adaptive_schema2_ascii_id_valid(
            p_payload->>'provider_id', 64
        )
        AND jsonb_typeof(p_payload->'model_id') = 'string'
        AND aura.adaptive_schema2_ascii_id_valid(p_payload->>'model_id', 256)
        AND aura.adaptive_schema2_evaluator_valid(p_payload->'evaluator')
        AND jsonb_typeof(p_payload->'source_observation_id') = 'string'
        AND (p_payload->>'source_observation_id')::uuid::text =
            p_payload->>'source_observation_id'
        AND (p_payload->>'source_observation_id')::uuid <>
            '00000000-0000-0000-0000-000000000000'::uuid
        AND jsonb_typeof(p_payload->'status') = 'string'
        AND aura.adaptive_schema2_outcome_measures_valid(
            p_payload->>'status', p_payload->'measures'
        )
        AND jsonb_typeof(p_payload->'observed_at') = 'string'
        AND p_payload->>'observed_at' ~
            '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,9})?Z$'
        AND (p_payload->>'observed_at')::timestamptz IS NOT NULL;
EXCEPTION
    WHEN data_exception OR invalid_text_representation
        OR numeric_value_out_of_range THEN
        RETURN false;
END;
$$;

CREATE FUNCTION aura.adaptive_schema2_correction_payload_valid(
    p_payload jsonb,
    p_decision_id uuid
) RETURNS boolean
    LANGUAGE plpgsql
    IMMUTABLE
    STRICT
    PARALLEL SAFE
    SET search_path = pg_catalog
AS $$
BEGIN
    RETURN jsonb_typeof(p_payload) = 'object'
        AND p_payload ?& ARRAY[
            'schema_version', 'assignment_id', 'owner_id', 'domain',
            'provider_id', 'model_id', 'evaluator',
            'correction_source_id', 'supersedes_event_id',
            'status', 'measures', 'observed_at'
        ]
        AND (SELECT count(*) FROM jsonb_object_keys(p_payload)) = 12
        AND jsonb_typeof(p_payload->'schema_version') = 'string'
        AND p_payload->>'schema_version' = '2.0'
        AND jsonb_typeof(p_payload->'assignment_id') = 'string'
        AND (p_payload->>'assignment_id')::uuid = p_decision_id
        AND (p_payload->>'assignment_id')::uuid::text =
            p_payload->>'assignment_id'
        AND jsonb_typeof(p_payload->'owner_id') = 'string'
        AND (p_payload->>'owner_id')::uuid::text = p_payload->>'owner_id'
        AND jsonb_typeof(p_payload->'domain') = 'string'
        AND p_payload->>'domain' IN (
            'reasoning', 'tool_discovery', 'skill_routing',
            'knowledge_retrieval', 'memory_recall'
        )
        AND jsonb_typeof(p_payload->'provider_id') = 'string'
        AND aura.adaptive_schema2_ascii_id_valid(
            p_payload->>'provider_id', 64
        )
        AND jsonb_typeof(p_payload->'model_id') = 'string'
        AND aura.adaptive_schema2_ascii_id_valid(p_payload->>'model_id', 256)
        AND aura.adaptive_schema2_evaluator_valid(p_payload->'evaluator')
        AND jsonb_typeof(p_payload->'correction_source_id') = 'string'
        AND (p_payload->>'correction_source_id')::uuid::text =
            p_payload->>'correction_source_id'
        AND (p_payload->>'correction_source_id')::uuid <>
            '00000000-0000-0000-0000-000000000000'::uuid
        AND jsonb_typeof(p_payload->'supersedes_event_id') = 'string'
        AND (p_payload->>'supersedes_event_id')::uuid::text =
            p_payload->>'supersedes_event_id'
        AND (p_payload->>'supersedes_event_id')::uuid <>
            '00000000-0000-0000-0000-000000000000'::uuid
        AND jsonb_typeof(p_payload->'status') = 'string'
        AND aura.adaptive_schema2_outcome_measures_valid(
            p_payload->>'status', p_payload->'measures'
        )
        AND jsonb_typeof(p_payload->'observed_at') = 'string'
        AND p_payload->>'observed_at' ~
            '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,9})?Z$'
        AND (p_payload->>'observed_at')::timestamptz IS NOT NULL;
EXCEPTION
    WHEN data_exception OR invalid_text_representation
        OR numeric_value_out_of_range THEN
        RETURN false;
END;
$$;

CREATE FUNCTION aura.adaptive_schema2_sourced_event_id(
    p_assignment_id uuid,
    p_event_kind text,
    p_evaluator jsonb,
    p_source_id uuid
) RETURNS uuid
    LANGUAGE plpgsql
    IMMUTABLE
    STRICT
    PARALLEL SAFE
    SET search_path = pg_catalog
AS $$
DECLARE
    evaluator_identity bytea;
    source_identity text;
BEGIN
    IF p_event_kind NOT IN ('outcome', 'correction')
        OR NOT aura.adaptive_schema2_evaluator_valid(p_evaluator)
        OR p_source_id = '00000000-0000-0000-0000-000000000000'::uuid
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '22023',
            MESSAGE = 'schema-2 sourced event identity is invalid';
    END IF;
    evaluator_identity := convert_to(p_evaluator->>'kind', 'UTF8')
        || '\x00'::bytea
        || convert_to(p_evaluator->>'id', 'UTF8')
        || '\x00'::bytea
        || convert_to(p_evaluator->>'version', 'UTF8')
        || '\x00'::bytea
        || convert_to(p_evaluator->>'rubric_id', 'UTF8')
        || '\x00'::bytea
        || convert_to(p_evaluator->>'calibration_id', 'UTF8')
        || '\x00'::bytea
        || convert_to(p_evaluator->>'provenance_sha256', 'UTF8')
        || '\x00'::bytea
        || convert_to(p_source_id::text, 'UTF8');
    source_identity := 'evaluator:'
        || encode(sha256(evaluator_identity), 'hex');
    RETURN aura.adaptive_uuid_v5_sha256(
        'fb3f7ce9-d343-41fb-a26f-35155b229189'::uuid,
        convert_to('2.0', 'UTF8')
            || '\x00'::bytea
            || uuid_send(p_assignment_id)
            || '\x00'::bytea
            || convert_to(p_event_kind, 'UTF8')
            || '\x00'::bytea
            || convert_to(source_identity, 'UTF8')
    );
END;
$$;

CREATE FUNCTION aura.adaptive_schema2_outcome_row_valid(
    p_id uuid,
    p_payload jsonb,
    p_owner_id uuid,
    p_aggregate_id text,
    p_decision_id uuid,
    p_event_kind text
) RETURNS boolean
    LANGUAGE plpgsql
    STABLE
    STRICT
    PARALLEL SAFE
    SET search_path = pg_catalog
AS $$
DECLARE
    assignment_payload jsonb;
    source_id uuid;
BEGIN
    IF p_event_kind = 'outcome' THEN
        IF NOT aura.adaptive_schema2_outcome_payload_valid(
            p_payload, p_decision_id
        ) THEN
            RETURN false;
        END IF;
        source_id := (p_payload->>'source_observation_id')::uuid;
    ELSIF p_event_kind = 'correction' THEN
        IF NOT aura.adaptive_schema2_correction_payload_valid(
            p_payload, p_decision_id
        ) THEN
            RETURN false;
        END IF;
        source_id := (p_payload->>'correction_source_id')::uuid;
    ELSE
        RETURN false;
    END IF;
    IF p_id <> aura.adaptive_schema2_sourced_event_id(
        p_decision_id, p_event_kind, p_payload->'evaluator', source_id
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
          assignment.id, assignment.payload, assignment.owner_id,
          assignment.aggregate_id, assignment.decision_id
      );
    RETURN assignment_payload IS NOT NULL
        AND (p_payload->>'owner_id')::uuid = p_owner_id
        AND p_payload->>'domain' = assignment_payload->>'domain'
        AND p_payload->>'provider_id' = assignment_payload->>'provider_id'
        AND p_payload->>'model_id' = assignment_payload->>'model_id';
EXCEPTION
    WHEN data_exception OR invalid_text_representation
        OR numeric_value_out_of_range THEN
        RETURN false;
END;
$$;

LOCK TABLE aura.adaptive_outbox IN SHARE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM aura.adaptive_outbox AS fact
        WHERE fact.payload->>'schema_version' = '2.0'
          AND fact.event_kind IN ('outcome', 'correction')
          AND aura.adaptive_schema2_outcome_row_valid(
              fact.id, fact.payload, fact.owner_id, fact.aggregate_id,
              fact.decision_id, fact.event_kind
          ) IS NOT TRUE
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'schema-2 outcome audit found an invalid fact';
    END IF;
END;
$$;

ALTER TABLE aura.adaptive_outbox
    ADD CONSTRAINT adaptive_outbox_schema2_outcome_check CHECK (
        payload->>'schema_version' <> '2.0'
        OR event_kind <> 'outcome'
        OR aura.adaptive_schema2_outcome_row_valid(
            id, payload, owner_id, aggregate_id, decision_id, event_kind
        )
    ) NOT VALID,
    ADD CONSTRAINT adaptive_outbox_schema2_correction_check CHECK (
        payload->>'schema_version' <> '2.0'
        OR event_kind <> 'correction'
        OR aura.adaptive_schema2_outcome_row_valid(
            id, payload, owner_id, aggregate_id, decision_id, event_kind
        )
    ) NOT VALID;

ALTER TABLE aura.adaptive_outbox
    VALIDATE CONSTRAINT adaptive_outbox_schema2_outcome_check;
ALTER TABLE aura.adaptive_outbox
    VALIDATE CONSTRAINT adaptive_outbox_schema2_correction_check;

CREATE UNIQUE INDEX adaptive_outbox_schema2_outcome_source_uidx
    ON aura.adaptive_outbox (
        owner_id,
        decision_id,
        (payload->'evaluator'->>'kind'),
        (payload->'evaluator'->>'id'),
        (payload->'evaluator'->>'version'),
        (payload->'evaluator'->>'rubric_id'),
        (payload->'evaluator'->>'calibration_id'),
        (payload->'evaluator'->>'provenance_sha256'),
        ((payload->>'source_observation_id')::uuid)
    )
    WHERE event_kind = 'outcome'
      AND payload->>'schema_version' = '2.0';

CREATE UNIQUE INDEX adaptive_outbox_schema2_correction_source_uidx
    ON aura.adaptive_outbox (
        owner_id,
        decision_id,
        (payload->'evaluator'->>'kind'),
        (payload->'evaluator'->>'id'),
        (payload->'evaluator'->>'version'),
        (payload->'evaluator'->>'rubric_id'),
        (payload->'evaluator'->>'calibration_id'),
        (payload->'evaluator'->>'provenance_sha256'),
        ((payload->>'correction_source_id')::uuid)
    )
    WHERE event_kind = 'correction'
      AND payload->>'schema_version' = '2.0';

CREATE UNIQUE INDEX adaptive_outbox_schema2_correction_target_uidx
    ON aura.adaptive_outbox (
        owner_id,
        decision_id,
        ((payload->>'supersedes_event_id')::uuid)
    )
    WHERE event_kind = 'correction'
      AND payload->>'schema_version' = '2.0';

CREATE FUNCTION aura.enforce_adaptive_schema2_outcome_chain()
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

CREATE TRIGGER adaptive_outbox_schema2_outcome_chain
    BEFORE INSERT ON aura.adaptive_outbox
    FOR EACH ROW
    EXECUTE FUNCTION aura.enforce_adaptive_schema2_outcome_chain();

REVOKE ALL ON FUNCTION aura.adaptive_schema2_evaluator_valid(jsonb)
    FROM PUBLIC;
REVOKE ALL ON FUNCTION aura.adaptive_schema2_outcome_measures_valid(text, jsonb)
    FROM PUBLIC;
REVOKE ALL ON FUNCTION aura.adaptive_schema2_outcome_payload_valid(jsonb, uuid)
    FROM PUBLIC;
REVOKE ALL ON FUNCTION aura.adaptive_schema2_correction_payload_valid(jsonb, uuid)
    FROM PUBLIC;
REVOKE ALL ON FUNCTION aura.adaptive_schema2_sourced_event_id(
    uuid, text, jsonb, uuid
) FROM PUBLIC;
REVOKE ALL ON FUNCTION aura.adaptive_schema2_outcome_row_valid(
    uuid, jsonb, uuid, text, uuid, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION aura.enforce_adaptive_schema2_outcome_chain()
    FROM PUBLIC;

GRANT EXECUTE ON FUNCTION aura.adaptive_schema2_evaluator_valid(jsonb)
    TO aura_app;
GRANT EXECUTE ON FUNCTION aura.adaptive_schema2_outcome_measures_valid(text, jsonb)
    TO aura_app;
GRANT EXECUTE ON FUNCTION aura.adaptive_schema2_outcome_payload_valid(jsonb, uuid)
    TO aura_app;
GRANT EXECUTE ON FUNCTION aura.adaptive_schema2_correction_payload_valid(jsonb, uuid)
    TO aura_app;
GRANT EXECUTE ON FUNCTION aura.adaptive_schema2_sourced_event_id(
    uuid, text, jsonb, uuid
) TO aura_app;
GRANT EXECUTE ON FUNCTION aura.adaptive_schema2_outcome_row_valid(
    uuid, jsonb, uuid, text, uuid, text
) TO aura_app;
GRANT EXECUTE ON FUNCTION aura.enforce_adaptive_schema2_outcome_chain()
    TO aura_app;
