CREATE FUNCTION aura.adaptive_schema2_assignment_payload_valid(
    p_payload jsonb,
    p_owner_id uuid,
    p_aggregate_id text,
    p_decision_id uuid
) RETURNS boolean
    LANGUAGE plpgsql
    IMMUTABLE
    STRICT
    SET search_path = pg_catalog
AS $$
DECLARE
    action_count integer;
    action_index integer;
    action_id text;
    probability numeric;
    probability_entry jsonb;
    probability_total numeric := 0;
    feature_value jsonb;
BEGIN
    IF jsonb_typeof(p_payload) <> 'object'
        OR NOT p_payload ?& ARRAY[
            'schema_version', 'assignment_id', 'owner_id', 'request_id',
            'domain', 'point', 'point_ordinal', 'policy_epoch',
            'policy_version', 'policy_mode', 'snapshot_id',
            'snapshot_sha256', 'environment', 'provider_id', 'model_id',
            'cohort_id', 'eligible_actions', 'eligibility_sha256',
            'catalog_sha256', 'champion_action_id',
            'recommended_action_id', 'intended_action_id',
            'experiment_id', 'arm_id', 'arm_probability',
            'action_probabilities', 'selection_reason', 'override',
            'features'
        ]
        OR p_payload->>'schema_version' <> '2.0'
        OR jsonb_typeof(p_payload->'assignment_id') <> 'string'
        OR (p_payload->>'assignment_id')::uuid <> p_decision_id
        OR jsonb_typeof(p_payload->'owner_id') <> 'string'
        OR (p_payload->>'owner_id')::uuid <> p_owner_id
        OR jsonb_typeof(p_payload->'request_id') <> 'string'
        OR (p_payload->>'request_id')::uuid::text <> p_aggregate_id
        OR jsonb_typeof(p_payload->'domain') <> 'string'
        OR p_payload->>'domain' = ''
        OR jsonb_typeof(p_payload->'point') <> 'string'
        OR p_payload->>'point' = ''
        OR jsonb_typeof(p_payload->'point_ordinal') <> 'number'
        OR (p_payload->>'point_ordinal')::numeric <> trunc((p_payload->>'point_ordinal')::numeric)
        OR (p_payload->>'point_ordinal')::numeric NOT BETWEEN 0 AND 4294967295
        OR jsonb_typeof(p_payload->'policy_epoch') <> 'number'
        OR (p_payload->>'policy_epoch')::numeric <> trunc((p_payload->>'policy_epoch')::numeric)
        OR (p_payload->>'policy_epoch')::numeric <= 0
        OR jsonb_typeof(p_payload->'policy_version') <> 'string'
        OR p_payload->>'policy_version' = ''
        OR jsonb_typeof(p_payload->'policy_mode') <> 'string'
        OR p_payload->>'policy_mode' = ''
        OR jsonb_typeof(p_payload->'snapshot_id') <> 'string'
        OR (p_payload->>'snapshot_id')::uuid = '00000000-0000-0000-0000-000000000000'::uuid
        OR jsonb_typeof(p_payload->'snapshot_sha256') <> 'string'
        OR p_payload->>'snapshot_sha256' !~ '^[0-9a-f]{64}$'
        OR jsonb_typeof(p_payload->'environment') <> 'string'
        OR p_payload->>'environment' = ''
        OR jsonb_typeof(p_payload->'provider_id') <> 'string'
        OR p_payload->>'provider_id' = ''
        OR jsonb_typeof(p_payload->'model_id') <> 'string'
        OR p_payload->>'model_id' = ''
        OR jsonb_typeof(p_payload->'cohort_id') NOT IN ('null', 'string')
        OR (
            jsonb_typeof(p_payload->'cohort_id') = 'string'
            AND (p_payload->>'cohort_id')::uuid =
                '00000000-0000-0000-0000-000000000000'::uuid
        )
        OR jsonb_typeof(p_payload->'eligible_actions') <> 'array'
        OR jsonb_array_length(p_payload->'eligible_actions') = 0
        OR jsonb_typeof(p_payload->'eligibility_sha256') <> 'string'
        OR p_payload->>'eligibility_sha256' !~ '^[0-9a-f]{64}$'
        OR jsonb_typeof(p_payload->'catalog_sha256') <> 'string'
        OR p_payload->>'catalog_sha256' !~ '^[0-9a-f]{64}$'
        OR jsonb_typeof(p_payload->'champion_action_id') <> 'string'
        OR p_payload->>'champion_action_id' = ''
        OR jsonb_typeof(p_payload->'recommended_action_id') <> 'string'
        OR p_payload->>'recommended_action_id' = ''
        OR jsonb_typeof(p_payload->'intended_action_id') <> 'string'
        OR p_payload->>'intended_action_id' = ''
        OR jsonb_typeof(p_payload->'experiment_id') <> 'string'
        OR jsonb_typeof(p_payload->'arm_id') <> 'string'
        OR jsonb_typeof(p_payload->'arm_probability') NOT IN ('null', 'number')
        OR (
            jsonb_typeof(p_payload->'arm_probability') = 'number'
            AND (
                (p_payload->>'arm_probability')::numeric <= 0
                OR (p_payload->>'arm_probability')::numeric > 1
            )
        )
        OR jsonb_typeof(p_payload->'action_probabilities') <> 'array'
        OR jsonb_typeof(p_payload->'selection_reason') <> 'string'
        OR p_payload->>'selection_reason' = ''
        OR jsonb_typeof(p_payload->'override') <> 'boolean'
        OR jsonb_typeof(p_payload->'features') <> 'object'
    THEN
        RETURN false;
    END IF;

    action_count := jsonb_array_length(p_payload->'eligible_actions');
    IF jsonb_array_length(p_payload->'action_probabilities') <> action_count THEN
        RETURN false;
    END IF;
    FOR action_index IN 0..action_count - 1 LOOP
        IF jsonb_typeof(p_payload->'eligible_actions'->action_index) <> 'string'
            OR p_payload->'eligible_actions'->>action_index = ''
        THEN
            RETURN false;
        END IF;
        action_id := p_payload->'eligible_actions'->>action_index;
        probability_entry := p_payload->'action_probabilities'->action_index;
        IF jsonb_typeof(probability_entry) <> 'object'
            OR NOT probability_entry ?& ARRAY['action_id', 'probability']
            OR jsonb_typeof(probability_entry->'action_id') <> 'string'
            OR probability_entry->>'action_id' <> action_id
            OR jsonb_typeof(probability_entry->'probability') <> 'number'
        THEN
            RETURN false;
        END IF;
        probability := (probability_entry->>'probability')::numeric;
        IF probability < 0 OR probability > 1 THEN
            RETURN false;
        END IF;
        probability_total := probability_total + probability;
    END LOOP;
    IF abs(probability_total - 1) > 0.000000001 THEN
        RETURN false;
    END IF;
    IF NOT (p_payload->'eligible_actions' ? (p_payload->>'champion_action_id'))
        OR NOT (p_payload->'eligible_actions' ? (p_payload->>'recommended_action_id'))
        OR NOT (p_payload->'eligible_actions' ? (p_payload->>'intended_action_id'))
    THEN
        RETURN false;
    END IF;
    FOR feature_value IN SELECT value FROM jsonb_each(p_payload->'features') LOOP
        IF jsonb_typeof(feature_value) <> 'number' THEN
            RETURN false;
        END IF;
    END LOOP;
    RETURN true;
EXCEPTION
    WHEN data_exception OR invalid_text_representation OR numeric_value_out_of_range THEN
        RETURN false;
END;
$$;

CREATE FUNCTION aura.adaptive_schema2_delivery_payload_valid(
    p_payload jsonb,
    p_decision_id uuid
) RETURNS boolean
    LANGUAGE plpgsql
    IMMUTABLE
    STRICT
    SET search_path = pg_catalog
AS $$
DECLARE
    effective_limit jsonb;
    result_id jsonb;
    result_count numeric;
    revision_value jsonb;
BEGIN
    IF jsonb_typeof(p_payload) <> 'object'
        OR NOT p_payload ?& ARRAY[
            'schema_version', 'assignment_id', 'intended_action_id',
            'actual_action_id', 'status', 'exposure_known',
            'exposure_probability', 'fallback_reason', 'result_count',
            'result_ids', 'revisions', 'effective_limits'
        ]
        OR p_payload->>'schema_version' <> '2.0'
        OR jsonb_typeof(p_payload->'assignment_id') <> 'string'
        OR (p_payload->>'assignment_id')::uuid <> p_decision_id
        OR jsonb_typeof(p_payload->'intended_action_id') <> 'string'
        OR p_payload->>'intended_action_id' = ''
        OR jsonb_typeof(p_payload->'actual_action_id') <> 'string'
        OR p_payload->>'actual_action_id' = ''
        OR jsonb_typeof(p_payload->'status') <> 'string'
        OR p_payload->>'status' = ''
        OR jsonb_typeof(p_payload->'exposure_known') <> 'boolean'
        OR jsonb_typeof(p_payload->'exposure_probability') NOT IN ('null', 'number')
        OR (
            (p_payload->>'exposure_known')::boolean
            AND (
                jsonb_typeof(p_payload->'exposure_probability') <> 'number'
                OR (p_payload->>'exposure_probability')::numeric <= 0
                OR (p_payload->>'exposure_probability')::numeric > 1
            )
        )
        OR (
            NOT (p_payload->>'exposure_known')::boolean
            AND jsonb_typeof(p_payload->'exposure_probability') <> 'null'
        )
        OR jsonb_typeof(p_payload->'fallback_reason') <> 'string'
        OR jsonb_typeof(p_payload->'result_count') <> 'number'
        OR jsonb_typeof(p_payload->'result_ids') <> 'array'
        OR jsonb_typeof(p_payload->'revisions') <> 'object'
        OR jsonb_typeof(p_payload->'effective_limits') <> 'object'
    THEN
        RETURN false;
    END IF;

    result_count := (p_payload->>'result_count')::numeric;
    IF result_count <> trunc(result_count)
        OR result_count < 0
        OR jsonb_array_length(p_payload->'result_ids') > result_count
    THEN
        RETURN false;
    END IF;
    FOR result_id IN SELECT value FROM jsonb_array_elements(p_payload->'result_ids') LOOP
        IF jsonb_typeof(result_id) <> 'object'
            OR NOT result_id ?& ARRAY['kind', 'id']
            OR jsonb_typeof(result_id->'kind') <> 'string'
            OR result_id->>'kind' = ''
            OR jsonb_typeof(result_id->'id') <> 'string'
            OR result_id->>'id' = ''
        THEN
            RETURN false;
        END IF;
    END LOOP;
    FOR revision_value IN SELECT value FROM jsonb_each(p_payload->'revisions') LOOP
        IF jsonb_typeof(revision_value) <> 'string'
            OR revision_value #>> '{}' = ''
        THEN
            RETURN false;
        END IF;
    END LOOP;
    FOR effective_limit IN SELECT value FROM jsonb_each(p_payload->'effective_limits') LOOP
        IF jsonb_typeof(effective_limit) <> 'number'
            OR (effective_limit #>> '{}')::numeric <>
                trunc((effective_limit #>> '{}')::numeric)
            OR (effective_limit #>> '{}')::numeric NOT BETWEEN 0 AND 1000000000
        THEN
            RETURN false;
        END IF;
    END LOOP;
    RETURN true;
EXCEPTION
    WHEN data_exception OR invalid_text_representation OR numeric_value_out_of_range THEN
        RETURN false;
END;
$$;

ALTER TABLE aura.adaptive_outbox
    DROP CONSTRAINT adaptive_outbox_event_kind_check;
ALTER TABLE aura.adaptive_outbox
    ADD CONSTRAINT adaptive_outbox_event_kind_check CHECK (
        event_kind IN ('decision', 'delivery', 'outcome', 'correction', 'promotion', 'rollback')
    ),
    ADD CONSTRAINT adaptive_outbox_delivery_schema_check CHECK (
        event_kind <> 'delivery' OR payload->>'schema_version' = '2.0'
    ),
    ADD CONSTRAINT adaptive_outbox_schema2_assignment_check CHECK (
        payload->>'schema_version' <> '2.0'
        OR event_kind <> 'decision'
        OR aura.adaptive_schema2_assignment_payload_valid(
            payload, owner_id, aggregate_id, decision_id
        )
    ),
    ADD CONSTRAINT adaptive_outbox_schema2_delivery_check CHECK (
        payload->>'schema_version' <> '2.0'
        OR event_kind <> 'delivery'
        OR aura.adaptive_schema2_delivery_payload_valid(payload, decision_id)
    );

CREATE UNIQUE INDEX adaptive_outbox_schema2_assignment_owner_decision_uidx
    ON aura.adaptive_outbox (owner_id, decision_id)
    WHERE event_kind = 'decision' AND payload->>'schema_version' = '2.0';

CREATE UNIQUE INDEX adaptive_outbox_schema2_delivery_owner_decision_uidx
    ON aura.adaptive_outbox (owner_id, decision_id)
    WHERE event_kind = 'delivery' AND payload->>'schema_version' = '2.0';

CREATE FUNCTION aura.enforce_adaptive_schema2_delivery_assignment() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog
AS $$
BEGIN
    IF NEW.event_kind = 'delivery'
        AND NEW.payload->>'schema_version' = '2.0'
        AND NOT EXISTS (
            SELECT 1
            FROM aura.adaptive_outbox AS assignment
            WHERE assignment.owner_id = NEW.owner_id
              AND assignment.aggregate_id = NEW.aggregate_id
              AND assignment.decision_id = NEW.decision_id
              AND assignment.event_kind = 'decision'
              AND assignment.payload->>'schema_version' = '2.0'
        )
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '23503',
            MESSAGE = 'schema-2 adaptive delivery has no matching assignment';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER adaptive_outbox_schema2_delivery_assignment
    BEFORE INSERT ON aura.adaptive_outbox
    FOR EACH ROW EXECUTE FUNCTION aura.enforce_adaptive_schema2_delivery_assignment();
