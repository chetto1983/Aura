CREATE OR REPLACE FUNCTION aura.adaptive_policy_snapshot_artifact_valid(
    p_artifact bytea,
    p_owner_id uuid,
    p_domain text,
    p_decision_point text,
    p_provider_id text,
    p_model_id text,
    p_policy_version text,
    p_training_cutoff timestamptz,
    p_feature_schema jsonb,
    p_action_catalog jsonb,
    p_champion_action_id text
) RETURNS boolean
    LANGUAGE plpgsql
    IMMUTABLE
    STRICT
    PARALLEL SAFE
    SET search_path = pg_catalog
AS $$
DECLARE
    artifact_json jsonb;
    scope_json jsonb;
    feature_definition jsonb;
    action_definition jsonb;
    example_definition jsonb;
    feature_value jsonb;
    feature_keys text[] := ARRAY[]::text[];
    feature_centers double precision[] := ARRAY[]::double precision[];
    feature_scales double precision[] := ARRAY[]::double precision[];
    normalized_values double precision[] := ARRAY[]::double precision[];
    prior_feature_key text;
    prior_action_id text;
    prior_outcome_id uuid;
    feature_key text;
    action_id text;
    outcome_id uuid;
    feature_index integer;
    numeric_value double precision;
    center_value double precision;
    scale_value double precision;
    normalized_value double precision;
    vector_maximum double precision;
    vector_scaled_squares double precision;
BEGIN
    artifact_json := convert_from(p_artifact, 'UTF8')::jsonb;
    IF jsonb_typeof(artifact_json) <> 'object'
        OR NOT artifact_json ?& ARRAY[
            'schema_version', 'scope', 'champion_action_id',
            'feature_schema', 'actions'
        ]
        OR (SELECT count(*) FROM jsonb_object_keys(artifact_json)) <> 5
        OR artifact_json->>'schema_version' <> '1.0'
        OR artifact_json->>'champion_action_id' <> p_champion_action_id
        OR artifact_json->'feature_schema' <> p_feature_schema
        OR artifact_json->'actions' <> p_action_catalog
    THEN
        RETURN false;
    END IF;

    scope_json := artifact_json->'scope';
    IF jsonb_typeof(scope_json) <> 'object'
        OR NOT scope_json ?& ARRAY[
            'owner_id', 'domain', 'point', 'provider_id',
            'model_id', 'policy_version', 'training_cutoff'
        ]
        OR (SELECT count(*) FROM jsonb_object_keys(scope_json)) <> 7
        OR (scope_json->>'owner_id')::uuid <> p_owner_id
        OR (scope_json->>'owner_id')::uuid::text <> scope_json->>'owner_id'
        OR scope_json->>'domain' <> p_domain
        OR scope_json->>'point' <> p_decision_point
        OR scope_json->>'provider_id' <> p_provider_id
        OR scope_json->>'model_id' <> p_model_id
        OR scope_json->>'policy_version' <> p_policy_version
        OR (scope_json->>'training_cutoff')::timestamptz <> p_training_cutoff
        OR scope_json->>'training_cutoff' !~
            '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,9})?Z$'
    THEN
        RETURN false;
    END IF;

    IF jsonb_typeof(p_feature_schema) <> 'array'
        OR jsonb_array_length(p_feature_schema) NOT BETWEEN 1 AND 128
        OR jsonb_typeof(p_action_catalog) <> 'array'
        OR jsonb_array_length(p_action_catalog) NOT BETWEEN 1 AND 128
        OR p_champion_action_id <> 'static'
    THEN
        RETURN false;
    END IF;

    FOR feature_definition IN
        SELECT value FROM jsonb_array_elements(p_feature_schema)
    LOOP
        IF jsonb_typeof(feature_definition) <> 'object'
            OR NOT feature_definition ?& ARRAY['key', 'center', 'scale']
            OR (SELECT count(*) FROM jsonb_object_keys(feature_definition)) <> 3
            OR jsonb_typeof(feature_definition->'key') <> 'string'
            OR jsonb_typeof(feature_definition->'center') <> 'number'
            OR jsonb_typeof(feature_definition->'scale') <> 'number'
        THEN
            RETURN false;
        END IF;
        feature_key := feature_definition->>'key';
        center_value := (feature_definition->>'center')::double precision;
        scale_value := (feature_definition->>'scale')::double precision;
        IF feature_key NOT IN (
            'available_skill_count', 'available_tool_count', 'candidate_count',
            'context_length', 'context_token_count', 'deferred_tool_count',
            'document_count', 'input_token_count', 'knn_similarity',
            'matched_skill_count', 'memory_count', 'message_count',
            'prompt_length', 'query_length', 'recall_limit', 'request_length',
            'result_count', 'retrieval_limit', 'retrieved_result_count',
            'retry_count', 'skill_candidate_count', 'tool_argument_count',
            'tool_result_count'
        )
            OR (prior_feature_key IS NOT NULL AND feature_key <= prior_feature_key)
            OR scale_value <= 0
            OR scale_value > 1000000000
            OR center_value < 0
            OR (
                feature_key = 'knn_similarity'
                AND center_value > 1
            )
            OR (
                feature_key <> 'knn_similarity'
                AND (
                    center_value > 1000000000
                    OR center_value <> trunc(center_value)
                )
            )
            OR abs((0 - center_value) / scale_value) = 'Infinity'::double precision
            OR abs((
                CASE
                    WHEN feature_key = 'knn_similarity' THEN 1
                    ELSE 1000000000
                END - center_value
            ) / scale_value) = 'Infinity'::double precision
        THEN
            RETURN false;
        END IF;
        feature_keys := array_append(feature_keys, feature_key);
        feature_centers := array_append(feature_centers, center_value);
        feature_scales := array_append(feature_scales, scale_value);
        prior_feature_key := feature_key;
    END LOOP;

    IF EXISTS (
        SELECT 1
        FROM jsonb_array_elements(p_action_catalog)
            AS catalog_action(value)
        CROSS JOIN LATERAL jsonb_array_elements(
            catalog_action.value->'examples'
        ) AS catalog_example(value)
        GROUP BY catalog_example.value->>'outcome_id'
        HAVING count(*) > 1
    ) THEN
        RETURN false;
    END IF;

    FOR action_definition IN
        SELECT value FROM jsonb_array_elements(p_action_catalog)
    LOOP
        IF jsonb_typeof(action_definition) <> 'object'
            OR NOT action_definition ?& ARRAY['action_id', 'examples']
            OR (SELECT count(*) FROM jsonb_object_keys(action_definition)) <> 2
            OR jsonb_typeof(action_definition->'action_id') <> 'string'
            OR jsonb_typeof(action_definition->'examples') <> 'array'
            OR jsonb_array_length(action_definition->'examples') > 256
        THEN
            RETURN false;
        END IF;
        action_id := action_definition->>'action_id';
        IF NOT aura.adaptive_schema2_safe_slug_valid(action_id, 128)
            OR action_id = 'none'
            OR lower(action_id) ~
                '(fingerprint|digest|checksum|_hash|_sha)'
            OR (prior_action_id IS NOT NULL AND action_id <= prior_action_id)
        THEN
            RETURN false;
        END IF;
        prior_action_id := action_id;
        prior_outcome_id := NULL;

        FOR example_definition IN
            SELECT value FROM jsonb_array_elements(action_definition->'examples')
        LOOP
            IF jsonb_typeof(example_definition) <> 'object'
                OR NOT example_definition ?& ARRAY[
                    'outcome_id', 'owner_id', 'domain', 'point', 'provider_id',
                    'model_id', 'observed_at', 'eligible', 'utility', 'features'
                ]
                OR (SELECT count(*) FROM jsonb_object_keys(example_definition)) <> 10
                OR jsonb_typeof(example_definition->'outcome_id') <> 'string'
                OR jsonb_typeof(example_definition->'owner_id') <> 'string'
                OR jsonb_typeof(example_definition->'eligible') <> 'boolean'
                OR jsonb_typeof(example_definition->'utility') <> 'number'
                OR jsonb_typeof(example_definition->'features') <> 'array'
                OR jsonb_array_length(example_definition->'features') <>
                    cardinality(feature_keys)
            THEN
                RETURN false;
            END IF;
            outcome_id := (example_definition->>'outcome_id')::uuid;
            numeric_value := (example_definition->>'utility')::double precision;
            IF outcome_id = '00000000-0000-0000-0000-000000000000'::uuid
                OR outcome_id::text <> example_definition->>'outcome_id'
                OR (
                    prior_outcome_id IS NOT NULL
                    AND uuid_send(outcome_id) <= uuid_send(prior_outcome_id)
                )
                OR (example_definition->>'owner_id')::uuid <> p_owner_id
                OR (example_definition->>'owner_id')::uuid::text <>
                    example_definition->>'owner_id'
                OR example_definition->>'domain' <> p_domain
                OR example_definition->>'point' <> p_decision_point
                OR example_definition->>'provider_id' <> p_provider_id
                OR example_definition->>'model_id' <> p_model_id
                OR (example_definition->>'observed_at')::timestamptz >
                    p_training_cutoff
                OR (example_definition->>'observed_at')::timestamptz =
                    '0001-01-01T00:00:00Z'::timestamptz
                OR example_definition->>'observed_at' !~
                    '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,9})?Z$'
                OR (example_definition->>'eligible')::boolean IS NOT TRUE
                OR numeric_value NOT BETWEEN 0 AND 1
            THEN
                RETURN false;
            END IF;
            prior_outcome_id := outcome_id;

            feature_index := 0;
            normalized_values := ARRAY[]::double precision[];
            vector_maximum := 0;
            FOR feature_value IN
                SELECT value FROM jsonb_array_elements(example_definition->'features')
                WITH ORDINALITY AS feature(value, position)
                ORDER BY position
            LOOP
                feature_index := feature_index + 1;
                IF jsonb_typeof(feature_value) <> 'object'
                    OR NOT feature_value ?& ARRAY['key', 'value']
                    OR (SELECT count(*) FROM jsonb_object_keys(feature_value)) <> 2
                    OR jsonb_typeof(feature_value->'key') <> 'string'
                    OR jsonb_typeof(feature_value->'value') <> 'number'
                THEN
                    RETURN false;
                END IF;
                feature_key := feature_value->>'key';
                numeric_value := (feature_value->>'value')::double precision;
                IF feature_keys[feature_index] IS DISTINCT FROM feature_key
                    OR feature_key = 'knn_similarity' AND numeric_value NOT BETWEEN 0 AND 1
                    OR feature_key <> 'knn_similarity' AND (
                        numeric_value NOT BETWEEN 0 AND 1000000000
                        OR numeric_value <> trunc(numeric_value)
                    )
                THEN
                    RETURN false;
                END IF;
                normalized_value := (
                    numeric_value - feature_centers[feature_index]
                ) / feature_scales[feature_index];
                normalized_values := array_append(
                    normalized_values,
                    normalized_value
                );
                vector_maximum := greatest(
                    vector_maximum,
                    abs(normalized_value)
                );
            END LOOP;
            IF vector_maximum = 0
                OR vector_maximum = 'Infinity'::double precision
                OR vector_maximum = 'NaN'::double precision
            THEN
                RETURN false;
            END IF;
            vector_scaled_squares := 0;
            FOREACH normalized_value IN ARRAY normalized_values
            LOOP
                vector_scaled_squares := vector_scaled_squares + power(
                    normalized_value / vector_maximum,
                    2
                );
            END LOOP;
            IF vector_scaled_squares <= 0
                OR vector_scaled_squares = 'Infinity'::double precision
                OR vector_scaled_squares = 'NaN'::double precision
            THEN
                RETURN false;
            END IF;
        END LOOP;
    END LOOP;

    RETURN prior_action_id IS NOT NULL
        AND EXISTS (
            SELECT 1
            FROM jsonb_array_elements(p_action_catalog) AS action
            WHERE action->>'action_id' = 'static'
        );
EXCEPTION
    WHEN data_exception OR invalid_text_representation
        OR numeric_value_out_of_range THEN
        RETURN false;
END;
$$;
