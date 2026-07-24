CREATE OR REPLACE FUNCTION aura.adaptive_schema2_ascii_id_valid(
    p_value text, p_maximum_length integer
) RETURNS boolean
    LANGUAGE sql IMMUTABLE STRICT SET search_path = pg_catalog
RETURN length(p_value) BETWEEN 1 AND p_maximum_length
    AND p_value ~ '^[A-Za-z0-9._:/@+-]+$';

CREATE OR REPLACE FUNCTION aura.adaptive_schema2_safe_slug_valid(
    p_value text, p_maximum_length integer
) RETURNS boolean
    LANGUAGE plpgsql IMMUTABLE STRICT SET search_path = pg_catalog
AS $$
DECLARE
    normalized text;
    token text;
BEGIN
    IF length(p_value) NOT BETWEEN 1 AND p_maximum_length
        OR p_value !~ '^[a-z0-9][a-z0-9._-]*$'
    THEN
        RETURN false;
    END IF;
    normalized := replace(replace(lower(p_value), '_', '-'), '.', '-');
    IF normalized LIKE '%api-key%' OR normalized LIKE '%private-summary%' THEN
        RETURN false;
    END IF;
    FOREACH token IN ARRAY regexp_split_to_array(normalized, '-') LOOP
        IF token ~ '^(apikey|secret|password|credential|bearer|token|content)' THEN
            RETURN false;
        END IF;
    END LOOP;
    RETURN true;
END;
$$;

CREATE OR REPLACE FUNCTION aura.adaptive_schema2_revision_value_valid(
    p_kind text, p_value text
) RETURNS boolean
    LANGUAGE plpgsql IMMUTABLE STRICT SET search_path = pg_catalog
AS $$
BEGIN
    IF p_kind = 'corpus' THEN
        RETURN p_value ~ '^[1-9][0-9]*$'
            AND p_value::numeric <= 18446744073709551615;
    END IF;
    RETURN p_kind IN ('embedding', 'index', 'reranker', 'retriever', 'schema')
        AND aura.adaptive_schema2_safe_slug_valid(p_value, 128)
        AND p_value ~ '(^|[-_.])(v[0-9]+|[0-9a-f]{12,})([-_.]|$)';
EXCEPTION
    WHEN data_exception OR numeric_value_out_of_range THEN
        RETURN false;
END;
$$;

CREATE OR REPLACE FUNCTION aura.adaptive_schema2_assignment_payload_valid(
    p_payload jsonb,
    p_owner_id uuid,
    p_aggregate_id text,
    p_decision_id uuid
) RETURNS boolean
    LANGUAGE plpgsql IMMUTABLE STRICT SET search_path = pg_catalog
AS $$
DECLARE
    action_count integer;
    action_index integer;
    action_id text;
    feature_key text;
    previous_action_id text;
    selection_reason text;
    all_actions_supported boolean := true;
    deterministic_intended boolean := true;
    feature_value jsonb;
    probability_entry jsonb;
    has_static boolean := false;
    non_randomized boolean;
    feature_number numeric;
    probability numeric;
    probability_total numeric := 0;
BEGIN
    IF jsonb_typeof(p_payload) <> 'object'
        OR NOT p_payload ?& ARRAY[
            'schema_version', 'assignment_id', 'owner_id', 'request_id',
            'domain', 'point', 'point_ordinal', 'policy_epoch', 'policy_version',
            'policy_mode', 'snapshot_id', 'snapshot_sha256', 'environment',
            'provider_id', 'model_id', 'cohort_id', 'eligible_actions',
            'eligibility_sha256', 'catalog_sha256', 'champion_action_id',
            'recommended_action_id', 'intended_action_id', 'experiment_id',
            'arm_id', 'arm_probability',
            'action_probabilities', 'selection_reason', 'override', 'features'
        ]
        OR (SELECT count(*) FROM jsonb_object_keys(p_payload)) <> 29
        OR jsonb_typeof(p_payload->'schema_version') <> 'string'
        OR p_payload->>'schema_version' <> '2.0'
        OR jsonb_typeof(p_payload->'assignment_id') <> 'string'
        OR (p_payload->>'assignment_id')::uuid <> p_decision_id
        OR (p_payload->>'assignment_id')::uuid::text <> p_payload->>'assignment_id'
        OR jsonb_typeof(p_payload->'owner_id') <> 'string'
        OR (p_payload->>'owner_id')::uuid <> p_owner_id
        OR (p_payload->>'owner_id')::uuid::text <> p_payload->>'owner_id'
        OR jsonb_typeof(p_payload->'request_id') <> 'string'
        OR (p_payload->>'request_id')::uuid::text <> p_aggregate_id
        OR (p_payload->>'request_id')::uuid::text <> p_payload->>'request_id'
        OR jsonb_typeof(p_payload->'domain') <> 'string'
        OR jsonb_typeof(p_payload->'point') <> 'string'
        OR (p_payload->>'domain', p_payload->>'point') NOT IN (
            ('reasoning', 'reasoning'), ('tool_discovery', 'tool_discovery'),
            ('skill_routing', 'skill_routing'), ('knowledge_retrieval', 'knowledge_retrieval'),
            ('memory_recall', 'memory_recall')
        )
        OR jsonb_typeof(p_payload->'point_ordinal') <> 'number'
        OR (p_payload->>'point_ordinal')::numeric <>
            trunc((p_payload->>'point_ordinal')::numeric)
        OR (p_payload->>'point_ordinal')::numeric NOT BETWEEN 0 AND 4294967295
        OR jsonb_typeof(p_payload->'policy_epoch') <> 'number'
        OR (p_payload->>'policy_epoch')::numeric <>
            trunc((p_payload->>'policy_epoch')::numeric)
        OR (p_payload->>'policy_epoch')::numeric NOT BETWEEN 1 AND 18446744073709551615
        OR jsonb_typeof(p_payload->'policy_version') <> 'string'
        OR NOT aura.adaptive_schema2_ascii_id_valid(p_payload->>'policy_version', 128)
        OR jsonb_typeof(p_payload->'policy_mode') <> 'string'
        OR p_payload->>'policy_mode' NOT IN ('off', 'shadow', 'canary', 'active', 'rollback')
        OR jsonb_typeof(p_payload->'snapshot_id') <> 'string'
        OR (p_payload->>'snapshot_id')::uuid::text <> p_payload->>'snapshot_id'
        OR (p_payload->>'snapshot_id')::uuid =
            '00000000-0000-0000-0000-000000000000'::uuid
        OR jsonb_typeof(p_payload->'snapshot_sha256') <> 'string'
        OR p_payload->>'snapshot_sha256' !~ '^[0-9a-f]{64}$'
        OR jsonb_typeof(p_payload->'environment') <> 'string'
        OR p_payload->>'environment' NOT IN ('spike', 'offline', 'production_canary')
        OR jsonb_typeof(p_payload->'provider_id') <> 'string'
        OR NOT aura.adaptive_schema2_ascii_id_valid(p_payload->>'provider_id', 64)
        OR jsonb_typeof(p_payload->'model_id') <> 'string'
        OR NOT aura.adaptive_schema2_ascii_id_valid(p_payload->>'model_id', 256)
        OR jsonb_typeof(p_payload->'cohort_id') NOT IN ('null', 'string')
        OR (
            jsonb_typeof(p_payload->'cohort_id') = 'string'
            AND (
                (p_payload->>'cohort_id')::uuid::text <> p_payload->>'cohort_id'
                OR (p_payload->>'cohort_id')::uuid =
                    '00000000-0000-0000-0000-000000000000'::uuid
            )
        )
        OR jsonb_typeof(p_payload->'eligible_actions') <> 'array'
        OR jsonb_array_length(p_payload->'eligible_actions') = 0
        OR jsonb_typeof(p_payload->'eligibility_sha256') <> 'string'
        OR p_payload->>'eligibility_sha256' !~ '^[0-9a-f]{64}$'
        OR jsonb_typeof(p_payload->'catalog_sha256') <> 'string'
        OR p_payload->>'catalog_sha256' !~ '^[0-9a-f]{64}$'
        OR jsonb_typeof(p_payload->'champion_action_id') <> 'string'
        OR NOT aura.adaptive_schema2_ascii_id_valid(p_payload->>'champion_action_id', 128)
        OR jsonb_typeof(p_payload->'recommended_action_id') <> 'string'
        OR NOT aura.adaptive_schema2_ascii_id_valid(p_payload->>'recommended_action_id', 128)
        OR jsonb_typeof(p_payload->'intended_action_id') <> 'string'
        OR NOT aura.adaptive_schema2_ascii_id_valid(p_payload->>'intended_action_id', 128)
        OR jsonb_typeof(p_payload->'experiment_id') <> 'string'
        OR (
            p_payload->>'experiment_id' <> ''
            AND NOT aura.adaptive_schema2_ascii_id_valid(p_payload->>'experiment_id', 128)
        )
        OR jsonb_typeof(p_payload->'arm_id') <> 'string'
        OR (
            p_payload->>'arm_id' <> ''
            AND NOT aura.adaptive_schema2_ascii_id_valid(p_payload->>'arm_id', 128)
        )
        OR jsonb_typeof(p_payload->'arm_probability') NOT IN ('null', 'number')
        OR (
            jsonb_typeof(p_payload->'arm_probability') = 'number'
            AND ((p_payload->>'arm_probability')::numeric <= 0
                OR (p_payload->>'arm_probability')::numeric > 1)
        )
        OR jsonb_typeof(p_payload->'action_probabilities') <> 'array'
        OR jsonb_typeof(p_payload->'selection_reason') <> 'string'
        OR p_payload->>'selection_reason' NOT IN (
            'shadow_static', 'canary_assignment', 'canary_diagnostic',
            'operator_override', 'active_policy', 'policy_off', 'policy_rollback',
            'state_missing', 'state_invalid', 'state_stale', 'owner_mismatch',
            'model_mismatch', 'provider_mismatch', 'unsupported', 'checksum_mismatch'
        )
        OR jsonb_typeof(p_payload->'override') <> 'boolean'
        OR jsonb_typeof(p_payload->'features') <> 'object'
        OR (SELECT count(*) FROM jsonb_object_keys(p_payload->'features')) > 128
    THEN
        RETURN false;
    END IF;
    action_count := jsonb_array_length(p_payload->'eligible_actions');
    IF jsonb_array_length(p_payload->'action_probabilities') <> action_count THEN
        RETURN false;
    END IF;
    FOR action_index IN 0..action_count - 1 LOOP
        IF jsonb_typeof(p_payload->'eligible_actions'->action_index) <> 'string' THEN
            RETURN false;
        END IF;
        action_id := p_payload->'eligible_actions'->>action_index;
        IF NOT aura.adaptive_schema2_ascii_id_valid(action_id, 128)
            OR action_id = 'none'
            OR (previous_action_id IS NOT NULL
                AND previous_action_id COLLATE "C" >= action_id COLLATE "C")
        THEN
            RETURN false;
        END IF;
        previous_action_id := action_id;
        has_static := has_static OR action_id = 'static';
        probability_entry := p_payload->'action_probabilities'->action_index;
        IF jsonb_typeof(probability_entry) <> 'object'
            OR NOT probability_entry ?& ARRAY['action_id', 'probability']
            OR probability_entry - ARRAY['action_id', 'probability'] <> '{}'::jsonb
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
        all_actions_supported := all_actions_supported AND probability > 0;
        deterministic_intended := deterministic_intended AND probability = CASE
            WHEN action_id = p_payload->>'intended_action_id' THEN 1
            ELSE 0
        END;
    END LOOP;
    IF NOT has_static
        OR abs(probability_total - 1) > 0.000000001
        OR NOT (p_payload->'eligible_actions' ? (p_payload->>'champion_action_id'))
        OR NOT (p_payload->'eligible_actions' ? (p_payload->>'recommended_action_id'))
        OR NOT (p_payload->'eligible_actions' ? (p_payload->>'intended_action_id'))
    THEN
        RETURN false;
    END IF;
    FOR feature_key, feature_value IN SELECT key, value FROM jsonb_each(p_payload->'features') LOOP
        IF jsonb_typeof(feature_value) <> 'number' THEN
            RETURN false;
        END IF;
        feature_number := (feature_value #>> '{}')::numeric;
        IF feature_key = 'knn_similarity' THEN
            IF feature_number NOT BETWEEN 0 AND 1 THEN
                RETURN false;
            END IF;
        ELSIF feature_key IN (
            'available_skill_count', 'available_tool_count', 'candidate_count',
            'context_length', 'context_token_count', 'deferred_tool_count', 'document_count',
            'input_token_count', 'matched_skill_count', 'memory_count', 'message_count',
            'prompt_length', 'query_length', 'recall_limit', 'request_length', 'result_count',
            'retrieval_limit', 'retrieved_result_count', 'retry_count', 'skill_candidate_count',
            'tool_argument_count', 'tool_result_count'
        ) THEN
            IF feature_number <> trunc(feature_number)
                OR feature_number NOT BETWEEN 0 AND 1000000000
            THEN
                RETURN false;
            END IF;
        ELSE
            RETURN false;
        END IF;
    END LOOP;
    selection_reason := p_payload->>'selection_reason';
    non_randomized := p_payload->>'experiment_id' = ''
        AND p_payload->>'arm_id' = ''
        AND jsonb_typeof(p_payload->'arm_probability') = 'null';
    IF (p_payload->>'override')::boolean THEN
        RETURN p_payload->>'policy_mode' IN ('shadow', 'canary', 'active')
            AND selection_reason = 'operator_override'
            AND non_randomized
            AND deterministic_intended;
    END IF;
    IF selection_reason = 'operator_override' THEN
        RETURN false;
    END IF;
    IF p_payload->>'policy_mode' = 'shadow' THEN
        RETURN non_randomized AND deterministic_intended AND (
            (selection_reason = 'shadow_static'
                AND p_payload->>'intended_action_id' = p_payload->>'champion_action_id')
            OR (selection_reason IN (
                    'state_missing', 'state_invalid', 'state_stale', 'owner_mismatch',
                    'model_mismatch', 'provider_mismatch', 'unsupported', 'checksum_mismatch'
                )
                AND p_payload->>'intended_action_id' = 'static')
        );
    ELSIF p_payload->>'policy_mode' = 'canary' THEN
        RETURN (
            selection_reason = 'canary_assignment'
            AND jsonb_typeof(p_payload->'cohort_id') = 'string'
            AND p_payload->>'experiment_id' <> ''
            AND p_payload->>'arm_id' <> ''
            AND jsonb_typeof(p_payload->'arm_probability') = 'number'
            AND all_actions_supported
        ) OR (
            non_randomized AND deterministic_intended AND (
                (selection_reason = 'canary_diagnostic'
                    AND jsonb_typeof(p_payload->'cohort_id') = 'null'
                    AND p_payload->>'intended_action_id' = p_payload->>'champion_action_id')
                OR (selection_reason IN (
                        'state_missing', 'state_invalid', 'state_stale', 'owner_mismatch',
                        'model_mismatch', 'provider_mismatch', 'unsupported', 'checksum_mismatch'
                    )
                    AND p_payload->>'intended_action_id' = 'static')
            )
        );
    ELSIF p_payload->>'policy_mode' = 'off' THEN
        RETURN selection_reason = 'policy_off' AND non_randomized
            AND deterministic_intended AND p_payload->>'intended_action_id' = 'static';
    ELSIF p_payload->>'policy_mode' = 'rollback' THEN
        RETURN selection_reason = 'policy_rollback' AND non_randomized
            AND deterministic_intended AND p_payload->>'intended_action_id' = 'static';
    END IF;
    RETURN non_randomized AND deterministic_intended AND (
        selection_reason = 'active_policy'
        OR (
            selection_reason IN (
                'state_missing', 'state_invalid', 'state_stale', 'owner_mismatch',
                'model_mismatch', 'provider_mismatch', 'unsupported', 'checksum_mismatch'
            )
            AND p_payload->>'intended_action_id' = 'static'
        )
    );
EXCEPTION
    WHEN data_exception OR invalid_text_representation OR numeric_value_out_of_range THEN
        RETURN false;
END;
$$;

CREATE OR REPLACE FUNCTION aura.adaptive_schema2_delivery_payload_valid(
    p_payload jsonb,
    p_decision_id uuid
) RETURNS boolean
    LANGUAGE plpgsql IMMUTABLE STRICT SET search_path = pg_catalog
AS $$
DECLARE
    actual_count integer;
    effective_limit_key text;
    memory_kind text;
    memory_prefix text;
    result_key text;
    revision_key text;
    effective_limit_value jsonb;
    result_id jsonb;
    revision_value jsonb;
    effective_limit_number numeric;
    memory_count numeric;
    memory_effective numeric;
    memory_metadata_present boolean := false;
    memory_requested numeric;
    result_count numeric;
    memory_total numeric := 0;
    result_keys text[] := ARRAY[]::text[];
BEGIN
    IF jsonb_typeof(p_payload) <> 'object'
        OR NOT p_payload ?& ARRAY[
            'schema_version', 'assignment_id', 'intended_action_id',
            'actual_action_id', 'status', 'exposure_known', 'exposure_probability',
            'fallback_reason', 'result_count',
            'result_ids', 'revisions', 'effective_limits'
        ]
        OR (SELECT count(*) FROM jsonb_object_keys(p_payload)) <> 12
        OR jsonb_typeof(p_payload->'schema_version') <> 'string'
        OR p_payload->>'schema_version' <> '2.0'
        OR jsonb_typeof(p_payload->'assignment_id') <> 'string'
        OR (p_payload->>'assignment_id')::uuid <> p_decision_id
        OR (p_payload->>'assignment_id')::uuid::text <> p_payload->>'assignment_id'
        OR jsonb_typeof(p_payload->'intended_action_id') <> 'string'
        OR NOT aura.adaptive_schema2_ascii_id_valid(p_payload->>'intended_action_id', 128)
        OR p_payload->>'intended_action_id' = 'none'
        OR jsonb_typeof(p_payload->'actual_action_id') <> 'string'
        OR NOT aura.adaptive_schema2_ascii_id_valid(p_payload->>'actual_action_id', 128)
        OR jsonb_typeof(p_payload->'status') <> 'string'
        OR p_payload->>'status' NOT IN ('success', 'fallback')
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
        OR result_count NOT BETWEEN 0 AND 9223372036854775807
        OR jsonb_array_length(p_payload->'result_ids') > result_count
        OR (
            p_payload->>'status' = 'success'
            AND (
                p_payload->>'actual_action_id' <> p_payload->>'intended_action_id'
                OR p_payload->>'fallback_reason' <> ''
            )
        )
        OR (
            p_payload->>'status' = 'fallback'
            AND (
                p_payload->>'actual_action_id' = p_payload->>'intended_action_id'
                OR p_payload->>'fallback_reason' NOT IN (
                    'candidate_unavailable', 'strategy_failed', 'state_invalid',
                    'state_stale', 'owner_mismatch', 'model_mismatch', 'provider_mismatch',
                    'unsupported', 'checksum_mismatch',
                    'context_budget', 'result_persist_failed'
                )
            )
        )
        OR (
            p_payload->>'actual_action_id' = 'none'
            AND (
                p_payload->>'status' <> 'fallback'
                OR (p_payload->>'exposure_known')::boolean
                OR jsonb_typeof(p_payload->'exposure_probability') <> 'null'
                OR result_count <> 0
                OR jsonb_array_length(p_payload->'result_ids') <> 0
            )
        )
    THEN
        RETURN false;
    END IF;
    FOR result_id IN SELECT value FROM jsonb_array_elements(p_payload->'result_ids') LOOP
        IF jsonb_typeof(result_id) <> 'object'
            OR NOT result_id ?& ARRAY['kind', 'id']
            OR result_id - ARRAY['kind', 'id'] <> '{}'::jsonb
            OR jsonb_typeof(result_id->'kind') <> 'string'
            OR result_id->>'kind' NOT IN (
                'artifact', 'node', 'tool', 'skill', 'memory_entity',
                'memory_preference', 'memory_message', 'memory_reasoning_trace'
            )
            OR jsonb_typeof(result_id->'id') <> 'string'
        THEN
            RETURN false;
        END IF;
        IF result_id->>'kind' IN (
            'artifact', 'node', 'memory_entity', 'memory_preference',
            'memory_message', 'memory_reasoning_trace'
        ) THEN
            IF (result_id->>'id')::uuid::text <> result_id->>'id'
                OR (result_id->>'id')::uuid =
                    '00000000-0000-0000-0000-000000000000'::uuid
            THEN
                RETURN false;
            END IF;
        ELSIF NOT aura.adaptive_schema2_safe_slug_valid(result_id->>'id', 256) THEN
            RETURN false;
        END IF;
        result_key := (result_id->>'kind') || ':' || (result_id->>'id');
        IF result_key = ANY(result_keys) THEN
            RETURN false;
        END IF;
        result_keys := array_append(result_keys, result_key);
    END LOOP;
    FOR revision_key, revision_value IN
        SELECT key, value FROM jsonb_each(p_payload->'revisions')
    LOOP
        IF jsonb_typeof(revision_value) <> 'string'
            OR NOT aura.adaptive_schema2_revision_value_valid(
                revision_key, revision_value #>> '{}'
            )
        THEN
            RETURN false;
        END IF;
    END LOOP;
    FOR effective_limit_key, effective_limit_value IN
        SELECT key, value FROM jsonb_each(p_payload->'effective_limits')
    LOOP
        IF effective_limit_key NOT IN (
            'candidate_limit', 'max_results', 'max_rounds', 'max_tokens',
            'recall_limit', 'skill_limit', 'timeout_ms', 'tool_limit', 'top_k',
            'entity_requested_k', 'entity_effective_k', 'entity_count',
            'preference_requested_k', 'preference_effective_k', 'preference_count',
            'message_requested_k', 'message_effective_k', 'message_count',
            'reasoning_trace_requested_k', 'reasoning_trace_effective_k', 'reasoning_trace_count'
        )
            OR jsonb_typeof(effective_limit_value) <> 'number'
        THEN
            RETURN false;
        END IF;
        effective_limit_number := (effective_limit_value #>> '{}')::numeric;
        IF effective_limit_number <> trunc(effective_limit_number)
            OR effective_limit_number NOT BETWEEN 0 AND 1000000000
        THEN
            RETURN false;
        END IF;
    END LOOP;
    FOR memory_prefix, memory_kind IN VALUES
        ('entity', 'memory_entity'), ('preference', 'memory_preference'),
        ('message', 'memory_message'),
        ('reasoning_trace', 'memory_reasoning_trace')
    LOOP
        SELECT count(*) INTO actual_count
        FROM jsonb_array_elements(p_payload->'result_ids') AS result
        WHERE result->>'kind' = memory_kind;
        IF actual_count > 0
            OR p_payload->'effective_limits' ? (memory_prefix || '_requested_k')
            OR p_payload->'effective_limits' ? (memory_prefix || '_effective_k')
            OR p_payload->'effective_limits' ? (memory_prefix || '_count')
        THEN
            memory_metadata_present := true;
            IF NOT p_payload->'effective_limits' ?& ARRAY[
                memory_prefix || '_requested_k',
                memory_prefix || '_effective_k',
                memory_prefix || '_count'
            ] THEN
                RETURN false;
            END IF;
            memory_requested := (
                p_payload->'effective_limits'->>(memory_prefix || '_requested_k')
            )::numeric;
            memory_effective := (
                p_payload->'effective_limits'->>(memory_prefix || '_effective_k')
            )::numeric;
            memory_count := (
                p_payload->'effective_limits'->>(memory_prefix || '_count')
            )::numeric;
            IF memory_requested < memory_effective
                OR memory_effective < memory_count
                OR memory_count <> actual_count
            THEN
                RETURN false;
            END IF;
            memory_total := memory_total + memory_count;
        END IF;
    END LOOP;
    RETURN NOT memory_metadata_present OR memory_total = result_count;
EXCEPTION
    WHEN data_exception OR invalid_text_representation OR numeric_value_out_of_range THEN
        RETURN false;
END;
$$;

ALTER TABLE aura.adaptive_outbox
    DROP CONSTRAINT adaptive_outbox_delivery_schema_check,
    ADD CONSTRAINT adaptive_outbox_delivery_schema_check CHECK (
        event_kind <> 'delivery'
        OR payload->>'schema_version' IS NOT DISTINCT FROM '2.0'
    ) NOT VALID;

CREATE OR REPLACE FUNCTION aura.enforce_adaptive_schema2_delivery_assignment()
RETURNS trigger
    LANGUAGE plpgsql SET search_path = pg_catalog
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
