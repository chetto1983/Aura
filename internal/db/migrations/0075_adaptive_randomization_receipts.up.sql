ALTER TABLE aura.adaptive_focal_cohorts
    ADD COLUMN randomization_plan_artifact bytea,
    ADD COLUMN randomization_plan_artifact_json jsonb;

CREATE FUNCTION aura.adaptive_child_artifact_ref_valid(
    p_ref jsonb,
    p_kind text,
    p_artifact_schema_id text
) RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
STRICT
PARALLEL SAFE
SET search_path = pg_catalog
AS $$
BEGIN
    RETURN jsonb_typeof(p_ref) = 'object'
       AND p_ref ?& ARRAY[
           'schema_id','revision','kind','artifact_schema_id',
           'artifact_revision','artifact_id','artifact_sha256'
       ]
       AND (SELECT count(*) FROM jsonb_object_keys(p_ref)) = 7
       AND p_ref->>'schema_id' = 'aura.adaptive.child-artifact-ref/v1'
       AND p_ref->'revision' = '1'::jsonb
       AND p_ref->>'kind' = p_kind
       AND p_ref->>'artifact_schema_id' = p_artifact_schema_id
       AND p_ref->'artifact_revision' = '1'::jsonb
       AND (p_ref->>'artifact_id')::uuid::text = p_ref->>'artifact_id'
       AND p_ref->>'artifact_sha256' ~ '^[0-9a-f]{64}$';
EXCEPTION
    WHEN data_exception OR invalid_text_representation THEN
        RETURN false;
END;
$$;

CREATE FUNCTION aura.adaptive_focal_cohort_v2_artifact_valid(
    p_artifact bytea,
    p_artifact_json jsonb,
    p_randomization_plan_artifact bytea,
    p_randomization_plan_artifact_json jsonb,
    p_owner_id uuid,
    p_provider_id text,
    p_model_id text,
    p_policy_epoch bigint,
    p_policy_version text,
    p_snapshot_id uuid,
    p_snapshot_sha256 bytea,
    p_environment text,
    p_domain text,
    p_decision_point text,
    p_point_ordinal bigint,
    p_predicate_sha256 bytea,
    p_experiment_id text,
    p_cutoff timestamptz
) RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
STRICT
PARALLEL SAFE
SET search_path = pg_catalog
AS $$
DECLARE
    document jsonb;
    plan jsonb;
    predicate_json jsonb;
    expected_predicate bytea;
    prior text;
    entry jsonb;
    baseline jsonb;
    challenger jsonb;
BEGIN
    IF NOT (convert_from(p_artifact, 'UTF8') IS JSON OBJECT WITH UNIQUE KEYS)
       OR NOT (
           convert_from(p_randomization_plan_artifact, 'UTF8')
           IS JSON OBJECT WITH UNIQUE KEYS
       )
    THEN
        RETURN false;
    END IF;
    document := convert_from(p_artifact, 'UTF8')::jsonb;
    plan := convert_from(p_randomization_plan_artifact, 'UTF8')::jsonb;
    IF document <> p_artifact_json
       OR plan <> p_randomization_plan_artifact_json
       OR jsonb_typeof(document) <> 'object'
       OR NOT document ?& ARRAY[
           'schema_id','revision','owner_id','domain','provider_id','model_id',
           'policy_epoch','policy_version','snapshot_id','snapshot_sha256',
           'environment','experiment_id','predicate','predicate_sha256',
           'supported_actions','evaluators','primary_quality','primary_harm',
           'power','margins','censoring','randomization_plan_ref',
           'interference_plan_ref','look_plan_ref','quality_outcome_model_ref',
           'harm_outcome_model_ref','ope_plan_ref','cutoff'
       ]
       OR (SELECT count(*) FROM jsonb_object_keys(document)) <> 28
       OR document->>'schema_id' <> 'aura.adaptive.cohort/v2'
       OR document->'revision' <> '1'::jsonb
       OR document->>'owner_id' <> p_owner_id::text
       OR document->>'domain' <> p_domain
       OR document->>'provider_id' <> p_provider_id
       OR document->>'model_id' <> p_model_id
       OR (document->>'policy_epoch')::numeric <> p_policy_epoch
       OR (document->>'policy_epoch')::numeric
          <> trunc((document->>'policy_epoch')::numeric)
       OR document->>'policy_version' <> p_policy_version
       OR document->>'snapshot_id' <> p_snapshot_id::text
       OR document->>'snapshot_sha256' <> encode(p_snapshot_sha256, 'hex')
       OR document->>'environment' <> p_environment
       OR document->>'experiment_id' <> p_experiment_id
       OR (document->>'cutoff')::timestamptz <> p_cutoff
       OR p_decision_point <> p_domain
       OR NOT aura.adaptive_focal_cohort_id_valid(document->>'provider_id', 64)
       OR NOT aura.adaptive_focal_cohort_id_valid(document->>'model_id', 256)
       OR NOT aura.adaptive_focal_cohort_id_valid(document->>'policy_version', 128)
       OR NOT aura.adaptive_focal_cohort_id_valid(document->>'experiment_id', 128)
    THEN
        RETURN false;
    END IF;

    predicate_json := document->'predicate';
    IF jsonb_typeof(predicate_json) <> 'object'
       OR NOT predicate_json ?& ARRAY['domain','point','ordinal']
       OR (SELECT count(*) FROM jsonb_object_keys(predicate_json)) <> 3
       OR predicate_json->>'domain' <> p_domain
       OR predicate_json->>'point' <> p_decision_point
       OR (predicate_json->>'ordinal')::numeric <> p_point_ordinal
       OR (predicate_json->>'ordinal')::numeric
          <> trunc((predicate_json->>'ordinal')::numeric)
    THEN
        RETURN false;
    END IF;
    expected_predicate := sha256(convert_to(format(
        '{"domain":%s,"point":%s,"ordinal":%s}',
        to_json(p_domain)::text, to_json(p_decision_point)::text, p_point_ordinal
    ), 'UTF8'));
    IF expected_predicate <> p_predicate_sha256
       OR document->>'predicate_sha256' <> encode(expected_predicate, 'hex')
    THEN
        RETURN false;
    END IF;

    IF jsonb_typeof(document->'supported_actions') <> 'array'
       OR jsonb_array_length(document->'supported_actions') NOT BETWEEN 2 AND 128
       OR jsonb_typeof(document->'evaluators') <> 'array'
       OR jsonb_array_length(document->'evaluators') NOT BETWEEN 1 AND 128
    THEN
        RETURN false;
    END IF;
    prior := NULL;
    FOR entry IN SELECT value FROM jsonb_array_elements(document->'supported_actions') LOOP
        IF jsonb_typeof(entry) <> 'string'
           OR NOT aura.adaptive_focal_cohort_id_valid(entry #>> '{}', 128)
           OR (prior IS NOT NULL AND (entry #>> '{}') COLLATE "C" <= prior COLLATE "C")
        THEN
            RETURN false;
        END IF;
        prior := entry #>> '{}';
    END LOOP;
    IF NOT document->'supported_actions' @> '["static"]'::jsonb THEN
        RETURN false;
    END IF;

    IF NOT aura.adaptive_child_artifact_ref_valid(
           document->'randomization_plan_ref',
           'randomization_plan', 'aura.adaptive.randomization-plan/v1'
       )
       OR NOT aura.adaptive_child_artifact_ref_valid(
           document->'interference_plan_ref',
           'interference_plan', 'aura.adaptive.interference-plan/v1'
       )
       OR NOT aura.adaptive_child_artifact_ref_valid(
           document->'look_plan_ref',
           'look_plan', 'aura.adaptive.look-plan/utc-cutoff/v1'
       )
       OR NOT aura.adaptive_child_artifact_ref_valid(
           document->'quality_outcome_model_ref',
           'quality_outcome_model', 'aura.adaptive.outcome-model/action-mean/v1'
       )
       OR NOT aura.adaptive_child_artifact_ref_valid(
           document->'harm_outcome_model_ref',
           'harm_outcome_model', 'aura.adaptive.outcome-model/action-mean/v1'
       )
       OR NOT aura.adaptive_child_artifact_ref_valid(
           document->'ope_plan_ref',
           'ope_plan', 'aura.adaptive.ope-plan/v1'
       )
       OR document->'randomization_plan_ref'->>'artifact_sha256'
          <> encode(sha256(p_randomization_plan_artifact), 'hex')
    THEN
        RETURN false;
    END IF;

    IF jsonb_typeof(plan) <> 'object'
       OR NOT plan ?& ARRAY[
           'schema_id','revision','design_id','mechanism_id',
           'mechanism_revision','entropy_source_id','draw_byte_count',
           'bit_index','common_denominator','analysis_stratum_schema_id',
           'analysis_stratum_revision','analysis_stratum_schema_sha256',
           'block_origin','block_width_seconds','arms'
       ]
       OR (SELECT count(*) FROM jsonb_object_keys(plan)) <> 15
       OR plan->>'schema_id' <> 'aura.adaptive.randomization-plan/v1'
       OR plan->'revision' <> '1'::jsonb
       OR plan->>'design_id' <> 'independent-bernoulli'
       OR plan->>'mechanism_id' <> 'aura/go-crypto-rand-bit'
       OR plan->'mechanism_revision' <> '1'::jsonb
       OR plan->>'entropy_source_id' <> 'go-crypto-rand/os-csprng'
       OR plan->'draw_byte_count' <> '1'::jsonb
       OR plan->'bit_index' <> '0'::jsonb
       OR plan->'common_denominator' <> '2'::jsonb
       OR plan->>'analysis_stratum_schema_id'
          <> 'aura.adaptive.analysis-stratum/owner-claim-time-block/v1'
       OR plan->'analysis_stratum_revision' <> '1'::jsonb
       OR plan->>'analysis_stratum_schema_sha256' !~ '^[0-9a-f]{64}$'
       OR plan->>'block_origin' <> '1970-01-01T00:00:00Z'
       OR (plan->>'block_width_seconds')::numeric <= 0
       OR (plan->>'block_width_seconds')::numeric
          <> trunc((plan->>'block_width_seconds')::numeric)
       OR jsonb_typeof(plan->'arms') <> 'array'
       OR jsonb_array_length(plan->'arms') <> 2
    THEN
        RETURN false;
    END IF;
    baseline := plan->'arms'->0;
    challenger := plan->'arms'->1;
    IF baseline->>'schema_id' <> 'aura.adaptive.arm-policy-ref/v1'
       OR baseline->'revision' <> '1'::jsonb
       OR baseline->>'arm_id' <> 'baseline'
       OR baseline->>'arm_role' <> 'control'
       OR baseline->'weight' <> '{"numerator":1,"denominator":1}'::jsonb
       OR baseline->>'policy_id' <> 'aura/static-champion'
       OR baseline->'policy_revision' <> '1'::jsonb
       OR challenger->>'schema_id' <> 'aura.adaptive.arm-policy-ref/v1'
       OR challenger->'revision' <> '1'::jsonb
       OR challenger->>'arm_id' <> 'challenger'
       OR challenger->>'arm_role' <> 'treatment'
       OR challenger->'weight' <> '{"numerator":1,"denominator":1}'::jsonb
       OR challenger->>'policy_id' <> 'aura/graph-knn-snapshot'
       OR challenger->'policy_revision' <> '1'::jsonb
       OR baseline->>'snapshot_id' <> p_snapshot_id::text
       OR challenger->>'snapshot_id' <> p_snapshot_id::text
       OR baseline->>'snapshot_sha256' <> encode(p_snapshot_sha256, 'hex')
       OR challenger->>'snapshot_sha256' <> encode(p_snapshot_sha256, 'hex')
       OR baseline->>'action_schema_sha256'
          <> challenger->>'action_schema_sha256'
       OR baseline->>'feature_schema_sha256'
          <> challenger->>'feature_schema_sha256'
       OR baseline->>'feature_schema_sha256' !~ '^[0-9a-f]{64}$'
    THEN
        RETURN false;
    END IF;
    RETURN true;
EXCEPTION
    WHEN character_not_in_repertoire
       OR data_exception
       OR invalid_text_representation
       OR numeric_value_out_of_range
    THEN
        RETURN false;
END;
$$;

ALTER TABLE aura.adaptive_focal_cohorts
    DROP CONSTRAINT adaptive_focal_cohorts_artifact_scope_check,
    ADD CONSTRAINT adaptive_focal_cohorts_artifact_scope_check CHECK (
        COALESCE(
            artifact_json->>'schema_version' = '1.0'
            AND randomization_plan_artifact IS NULL
            AND randomization_plan_artifact_json IS NULL
            AND aura.adaptive_focal_cohort_artifact_valid(
                artifact, artifact_json, owner_id, provider_id, model_id,
                policy_epoch, policy_version, snapshot_id, snapshot_sha256,
                environment, domain, decision_point, point_ordinal,
                predicate_sha256, experiment_id, cutoff
            ),
            false
        )
        OR COALESCE(
            artifact_json->>'schema_id' = 'aura.adaptive.cohort/v2'
            AND randomization_plan_artifact IS NOT NULL
            AND randomization_plan_artifact_json IS NOT NULL
            AND aura.adaptive_focal_cohort_v2_artifact_valid(
                artifact, artifact_json, randomization_plan_artifact,
                randomization_plan_artifact_json, owner_id, provider_id,
                model_id, policy_epoch, policy_version, snapshot_id,
                snapshot_sha256, environment, domain, decision_point,
                point_ordinal, predicate_sha256, experiment_id, cutoff
            ),
            false
        )
    );

REVOKE ALL ON FUNCTION aura.adaptive_child_artifact_ref_valid(
    jsonb, text, text
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION aura.adaptive_child_artifact_ref_valid(
    jsonb, text, text
) TO aura_app;
REVOKE ALL ON FUNCTION aura.adaptive_focal_cohort_v2_artifact_valid(
    bytea, jsonb, bytea, jsonb, uuid, text, text, bigint, text, uuid,
    bytea, text, text, text, bigint, bytea, text, timestamptz
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION aura.adaptive_focal_cohort_v2_artifact_valid(
    bytea, jsonb, bytea, jsonb, uuid, text, text, bigint, text, uuid,
    bytea, text, text, text, bigint, bytea, text, timestamptz
) TO aura_app;

ALTER TABLE aura.adaptive_focal_cohort_claims
    ADD COLUMN analysis_stratum_schema_sha256 bytea,
    ADD COLUMN analysis_stratum_id bytea,
    ADD COLUMN interference_cluster_schema_sha256 bytea,
    ADD COLUMN interference_cluster_id bytea,
    ADD CONSTRAINT adaptive_focal_claims_v2_hashes_check CHECK (
        (
            analysis_stratum_schema_sha256 IS NULL
            AND analysis_stratum_id IS NULL
            AND interference_cluster_schema_sha256 IS NULL
            AND interference_cluster_id IS NULL
        )
        OR (
            octet_length(analysis_stratum_schema_sha256) = 32
            AND octet_length(analysis_stratum_id) = 32
            AND octet_length(interference_cluster_schema_sha256) = 32
            AND octet_length(interference_cluster_id) = 32
        )
    );

CREATE OR REPLACE FUNCTION aura.enforce_adaptive_focal_claim_cutoff_and_blocks()
RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog AS $$
DECLARE
    cohort_cutoff timestamptz;
    block_keys jsonb;
    block_seconds bigint;
    time_block_id bigint;
    cohort_schema_id text;
BEGIN
    SELECT cutoff,
           CASE
               WHEN artifact_json->>'schema_id' = 'aura.adaptive.cohort/v2'
               THEN '[]'::jsonb
               ELSE artifact_json->'admission'->'block_keys'
           END,
           CASE
               WHEN artifact_json->>'schema_id' = 'aura.adaptive.cohort/v2'
               THEN (randomization_plan_artifact_json->>'block_width_seconds')::bigint
               ELSE (artifact_json->'admission'->>'time_block_seconds')::bigint
           END,
           artifact_json->>'schema_id'
    INTO cohort_cutoff, block_keys, block_seconds, cohort_schema_id
    FROM aura.adaptive_focal_cohorts
    WHERE id=NEW.cohort_id AND owner_id=NEW.owner_id;
    IF NOT FOUND THEN RETURN NEW; END IF;
    NEW.claimed_at := statement_timestamp();
    IF NEW.claimed_at >= cohort_cutoff THEN
        RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='adaptive focal claim is after cohort cutoff';
    END IF;
    time_block_id := floor(extract(epoch FROM NEW.claimed_at) / block_seconds);
    NEW.time_block_start := to_timestamp(time_block_id * block_seconds);
    IF cohort_schema_id IS DISTINCT FROM 'aura.adaptive.cohort/v2'
       AND (
           (block_keys ? 'session') <> (NEW.session_id IS NOT NULL)
           OR (block_keys ? 'episode') <> (NEW.episode_id IS NOT NULL)
       ) THEN
        RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='adaptive focal claim block keys do not match cohort admission';
    END IF;
    NEW.analysis_stratum_schema_sha256 := sha256(convert_to(format(
        '{"schema_id":"aura.adaptive.analysis-stratum/owner-claim-time-block/v1","revision":1,"field_order":["schema_id","revision","owner_id","time_block_id"],"block_origin":"1970-01-01T00:00:00Z","block_width_seconds":%s}',
        block_seconds
    ), 'UTF8'));
    NEW.analysis_stratum_id := sha256(convert_to(format(
        '{"schema_id":"aura.adaptive.analysis-stratum/owner-claim-time-block/v1","revision":1,"owner_id":"%s","time_block_id":%s}',
        NEW.owner_id, time_block_id
    ), 'UTF8'));
    NEW.interference_cluster_schema_sha256 := sha256(convert_to(
        '{"schema_id":"aura.adaptive.interference-cluster/conversation/v1","revision":1,"cluster_keys":["owner_id","evaluation_conversation_id"]}',
        'UTF8'
    ));
    NEW.interference_cluster_id := sha256(convert_to(format(
        '{"schema_id":"aura.adaptive.interference-cluster/conversation/v1","revision":1,"owner_id":"%s","evaluation_conversation_id":"%s"}',
        NEW.owner_id, NEW.evaluation_conversation_id
    ), 'UTF8'));
    RETURN NEW;
END;
$$;

CREATE FUNCTION aura.adaptive_schema2_randomized_assignment_payload_valid(
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
    normalized jsonb := p_payload;
    entry jsonb;
    action_index integer := 0;
    zero_count integer := 0;
    nonzero_count integer := 0;
    other_half_count integer := 0;
    static_index integer := -1;
    static_probability numeric := 0;
    intended_probability numeric := 0;
    recommended_probability numeric := 0;
    probability numeric;
    epsilon numeric := 0.000000000001;
BEGIN
    IF p_payload->>'policy_mode' <> 'canary'
       OR p_payload->>'selection_reason' <> 'canary_assignment'
       OR p_payload->>'arm_id' NOT IN ('baseline', 'challenger')
       OR (p_payload->>'arm_probability')::numeric <> 0.5
       OR jsonb_typeof(p_payload->'cohort_id') <> 'string'
       OR jsonb_typeof(p_payload->'action_probabilities') <> 'array'
    THEN
        RETURN false;
    END IF;
    FOR entry IN SELECT value FROM jsonb_array_elements(p_payload->'action_probabilities') LOOP
        probability := (entry->>'probability')::numeric;
        IF probability < 0 OR probability > 1 THEN
            RETURN false;
        END IF;
        IF entry->>'action_id' = 'static' THEN
            static_index := action_index;
            static_probability := probability;
        END IF;
        IF entry->>'action_id' = p_payload->>'intended_action_id' THEN
            intended_probability := probability;
        END IF;
        IF entry->>'action_id' = p_payload->>'recommended_action_id' THEN
            recommended_probability := probability;
        END IF;
        IF probability = 0 THEN
            zero_count := zero_count + 1;
        ELSE
            nonzero_count := nonzero_count + 1;
            IF entry->>'action_id' <> 'static' AND probability = 0.5 THEN
                other_half_count := other_half_count + 1;
            END IF;
        END IF;
        action_index := action_index + 1;
    END LOOP;
    IF static_index < 0 OR intended_probability <= 0 OR recommended_probability <= 0
       OR (
           p_payload->>'arm_id' = 'baseline'
           AND p_payload->>'intended_action_id' <> 'static'
       )
       OR (
           p_payload->>'arm_id' = 'challenger'
           AND p_payload->>'intended_action_id' <> p_payload->>'recommended_action_id'
       )
       OR NOT (
           (nonzero_count = 1 AND static_probability = 1)
           OR (
               nonzero_count = 2
               AND static_probability = 0.5
               AND other_half_count = 1
           )
       )
    THEN
        RETURN false;
    END IF;
    IF zero_count = 0 THEN
        RETURN aura.adaptive_schema2_assignment_payload_valid(
            p_payload, p_owner_id, p_aggregate_id, p_decision_id
        );
    END IF;
    FOR action_index IN 0..jsonb_array_length(p_payload->'action_probabilities') - 1 LOOP
        IF (p_payload->'action_probabilities'->action_index->>'probability')::numeric = 0 THEN
            normalized := jsonb_set(
                normalized,
                ARRAY['action_probabilities', action_index::text, 'probability'],
                to_jsonb(epsilon)
            );
        END IF;
    END LOOP;
    normalized := jsonb_set(
        normalized,
        ARRAY['action_probabilities', static_index::text, 'probability'],
        to_jsonb(static_probability - zero_count * epsilon)
    );
    RETURN aura.adaptive_schema2_assignment_payload_valid(
        normalized, p_owner_id, p_aggregate_id, p_decision_id
    );
EXCEPTION
    WHEN data_exception OR invalid_text_representation OR numeric_value_out_of_range THEN
        RETURN false;
END;
$$;

CREATE OR REPLACE FUNCTION aura.adaptive_schema2_assignment_row_valid(
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
    IF NOT (
        aura.adaptive_schema2_assignment_payload_valid(
            p_payload, p_owner_id, p_aggregate_id, p_decision_id
        )
        OR aura.adaptive_schema2_randomized_assignment_payload_valid(
            p_payload, p_owner_id, p_aggregate_id, p_decision_id
        )
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

ALTER TABLE aura.adaptive_outbox
    DROP CONSTRAINT adaptive_outbox_schema2_assignment_check,
    ADD CONSTRAINT adaptive_outbox_schema2_assignment_check CHECK (
        payload->>'schema_version' <> '2.0'
        OR event_kind <> 'decision'
        OR aura.adaptive_schema2_assignment_payload_valid(
            payload, owner_id, aggregate_id, decision_id
        )
        OR aura.adaptive_schema2_randomized_assignment_payload_valid(
            payload, owner_id, aggregate_id, decision_id
        )
    );

REVOKE ALL ON FUNCTION aura.adaptive_schema2_randomized_assignment_payload_valid(
    jsonb, uuid, text, uuid
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION aura.adaptive_schema2_randomized_assignment_payload_valid(
    jsonb, uuid, text, uuid
) TO aura_app;

CREATE UNIQUE INDEX adaptive_focal_claims_receipt_scope_uidx
    ON aura.adaptive_focal_cohort_claims
    (id, owner_id, cohort_id, request_id, assignment_id);

CREATE FUNCTION aura.adaptive_randomization_receipt_artifact_valid(
    p_artifact bytea,
    p_artifact_json jsonb,
    p_owner_id uuid,
    p_cohort_id uuid,
    p_request_id uuid,
    p_claim_id uuid,
    p_assignment_id uuid,
    p_randomization_plan_sha256 bytea,
    p_analysis_stratum_schema_sha256 bytea,
    p_analysis_stratum_id bytea
) RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
STRICT
PARALLEL SAFE
SET search_path = pg_catalog
AS $$
DECLARE
    document jsonb;
    baseline jsonb;
    challenger jsonb;
    entry jsonb;
    prior text := NULL;
    total numeric := 0;
    numerator numeric;
    denominator numeric;
    nonzero integer := 0;
    static_probability numeric := 0;
    other_half integer := 0;
BEGIN
    IF NOT (convert_from(p_artifact, 'UTF8') IS JSON OBJECT WITH UNIQUE KEYS) THEN
        RETURN false;
    END IF;
    document := convert_from(p_artifact, 'UTF8')::jsonb;
    IF document <> p_artifact_json
       OR jsonb_typeof(document) <> 'object'
       OR NOT document ?& ARRAY[
           'schema_id','revision','assignment_id','owner_id','cohort_id',
           'request_id','claim_id','randomization_plan_sha256','mechanism_id',
           'mechanism_revision','entropy_source_id','draw_byte','mapped_bit',
           'selected_arm_id','selected_arm_role','arm_probability',
           'analysis_stratum_id','analysis_stratum_schema_sha256',
           'arm_policy_refs','action_probabilities'
       ]
       OR (SELECT count(*) FROM jsonb_object_keys(document)) <> 20
       OR document->>'schema_id' <> 'aura.adaptive.randomization-receipt/v1'
       OR document->'revision' <> '1'::jsonb
       OR document->>'assignment_id' <> p_assignment_id::text
       OR document->>'owner_id' <> p_owner_id::text
       OR document->>'cohort_id' <> p_cohort_id::text
       OR document->>'request_id' <> p_request_id::text
       OR document->>'claim_id' <> p_claim_id::text
       OR document->>'randomization_plan_sha256' <> encode(p_randomization_plan_sha256, 'hex')
       OR document->>'analysis_stratum_schema_sha256' <> encode(p_analysis_stratum_schema_sha256, 'hex')
       OR document->>'analysis_stratum_id' <> encode(p_analysis_stratum_id, 'hex')
       OR document->>'mechanism_id' <> 'aura/go-crypto-rand-bit'
       OR document->'mechanism_revision' <> '1'::jsonb
       OR document->>'entropy_source_id' <> 'go-crypto-rand/os-csprng'
       OR document->>'draw_byte' !~ '^[0-9a-f]{2}$'
       OR document->'mapped_bit' NOT IN ('0'::jsonb, '1'::jsonb)
       OR (get_byte(decode(document->>'draw_byte', 'hex'), 0) & 1) <> (document->>'mapped_bit')::integer
       OR document->>'selected_arm_id' <> (
           CASE document->>'mapped_bit' WHEN '0' THEN 'baseline' ELSE 'challenger' END
       )
       OR document->>'selected_arm_role' <> (
           CASE document->>'mapped_bit' WHEN '0' THEN 'control' ELSE 'treatment' END
       )
       OR document->'arm_probability' <> '{"numerator":1,"denominator":2}'::jsonb
       OR jsonb_typeof(document->'arm_policy_refs') <> 'array'
       OR jsonb_array_length(document->'arm_policy_refs') <> 2
       OR jsonb_typeof(document->'action_probabilities') <> 'array'
       OR jsonb_array_length(document->'action_probabilities') NOT BETWEEN 1 AND 128
    THEN
        RETURN false;
    END IF;

    baseline := document->'arm_policy_refs'->0;
    challenger := document->'arm_policy_refs'->1;
    IF baseline->>'schema_id' <> 'aura.adaptive.arm-policy-ref/v1'
       OR baseline->'revision' <> '1'::jsonb
       OR baseline->>'arm_id' <> 'baseline'
       OR baseline->>'arm_role' <> 'control'
       OR baseline->'weight' <> '{"numerator":1,"denominator":1}'::jsonb
       OR baseline->>'policy_id' <> 'aura/static-champion'
       OR baseline->'policy_revision' <> '1'::jsonb
       OR challenger->>'schema_id' <> 'aura.adaptive.arm-policy-ref/v1'
       OR challenger->'revision' <> '1'::jsonb
       OR challenger->>'arm_id' <> 'challenger'
       OR challenger->>'arm_role' <> 'treatment'
       OR challenger->'weight' <> '{"numerator":1,"denominator":1}'::jsonb
       OR challenger->>'policy_id' <> 'aura/graph-knn-snapshot'
       OR challenger->'policy_revision' <> '1'::jsonb
       OR baseline->>'snapshot_id' <> challenger->>'snapshot_id'
       OR baseline->>'snapshot_sha256' <> challenger->>'snapshot_sha256'
       OR baseline->>'action_schema_sha256' <> challenger->>'action_schema_sha256'
       OR baseline->>'feature_schema_sha256' <> challenger->>'feature_schema_sha256'
       OR baseline->>'feature_schema_sha256' = '9d3da8a268742cc8871fa33993732e4cf6515b2acb6d52070b292fda3f2620c0'
    THEN
        RETURN false;
    END IF;

    FOR entry IN SELECT value FROM jsonb_array_elements(document->'action_probabilities') LOOP
        IF jsonb_typeof(entry) <> 'object'
           OR NOT entry ?& ARRAY['action_id','probability']
           OR (SELECT count(*) FROM jsonb_object_keys(entry)) <> 2
           OR jsonb_typeof(entry->'action_id') <> 'string'
           OR jsonb_typeof(entry->'probability') <> 'object'
           OR NOT entry->'probability' ?& ARRAY['numerator','denominator']
           OR (SELECT count(*) FROM jsonb_object_keys(entry->'probability')) <> 2
           OR (prior IS NOT NULL AND (entry->>'action_id') COLLATE "C" <= prior COLLATE "C")
        THEN
            RETURN false;
        END IF;
        numerator := (entry->'probability'->>'numerator')::numeric;
        denominator := (entry->'probability'->>'denominator')::numeric;
        IF denominator <= 0 OR numerator < 0 OR numerator > denominator
           OR numerator <> trunc(numerator) OR denominator <> trunc(denominator)
        THEN
            RETURN false;
        END IF;
        total := total + numerator / denominator;
        IF numerator <> 0 THEN
            nonzero := nonzero + 1;
            IF entry->>'action_id' = 'static' THEN
                static_probability := numerator / denominator;
            ELSIF numerator / denominator = 0.5 THEN
                other_half := other_half + 1;
            END IF;
        END IF;
        prior := entry->>'action_id';
    END LOOP;
    RETURN total = 1
       AND (
           (nonzero = 1 AND static_probability = 1)
           OR (nonzero = 2 AND static_probability = 0.5 AND other_half = 1)
       );
EXCEPTION
    WHEN character_not_in_repertoire
       OR data_exception
       OR invalid_text_representation
       OR numeric_value_out_of_range
    THEN
        RETURN false;
END;
$$;

CREATE TABLE aura.adaptive_randomization_receipts (
    id                                 uuid PRIMARY KEY,
    owner_id                           uuid NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE,
    cohort_id                          uuid NOT NULL,
    request_id                         uuid NOT NULL,
    claim_id                           uuid NOT NULL,
    assignment_id                      uuid NOT NULL,
    randomization_plan_sha256          bytea NOT NULL CHECK (octet_length(randomization_plan_sha256) = 32),
    analysis_stratum_schema_sha256     bytea NOT NULL CHECK (octet_length(analysis_stratum_schema_sha256) = 32),
    analysis_stratum_id                bytea NOT NULL CHECK (octet_length(analysis_stratum_id) = 32),
    sha256                             bytea NOT NULL CHECK (octet_length(sha256) = 32),
    artifact                           bytea NOT NULL,
    artifact_json                      jsonb NOT NULL,
    created_at                         timestamptz NOT NULL DEFAULT statement_timestamp(),
    CONSTRAINT adaptive_randomization_receipts_claim_fk
        FOREIGN KEY (claim_id, owner_id, cohort_id, request_id, assignment_id)
        REFERENCES aura.adaptive_focal_cohort_claims
        (id, owner_id, cohort_id, request_id, assignment_id)
        ON DELETE CASCADE,
    CONSTRAINT adaptive_randomization_receipts_assignment_uidx
        UNIQUE (owner_id, assignment_id),
    CONSTRAINT adaptive_randomization_receipts_claim_uidx
        UNIQUE (claim_id),
    CONSTRAINT adaptive_randomization_receipts_digest_check
        CHECK (sha256 = pg_catalog.sha256(artifact)),
    CONSTRAINT adaptive_randomization_receipts_artifact_check
        CHECK (aura.adaptive_randomization_receipt_artifact_valid(
            artifact, artifact_json, owner_id, cohort_id, request_id, claim_id,
            assignment_id, randomization_plan_sha256,
            analysis_stratum_schema_sha256, analysis_stratum_id
        ))
);

CREATE FUNCTION aura.enforce_adaptive_randomization_receipt_binding()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM aura.adaptive_focal_cohort_claims AS claim
        WHERE claim.id = NEW.claim_id
          AND claim.owner_id = NEW.owner_id
          AND claim.cohort_id = NEW.cohort_id
          AND claim.request_id = NEW.request_id
          AND claim.assignment_id = NEW.assignment_id
          AND claim.analysis_stratum_schema_sha256 = NEW.analysis_stratum_schema_sha256
          AND claim.analysis_stratum_id = NEW.analysis_stratum_id
          AND claim.interference_cluster_schema_sha256 IS NOT NULL
          AND claim.interference_cluster_id IS NOT NULL
    ) OR NOT EXISTS (
        SELECT 1
        FROM aura.adaptive_outbox AS assignment
        WHERE assignment.owner_id = NEW.owner_id
          AND assignment.decision_id = NEW.assignment_id
          AND assignment.event_kind = 'decision'
          AND assignment.payload->>'schema_version' = '2.0'
          AND assignment.payload->>'assignment_id' = NEW.assignment_id::text
          AND assignment.payload->>'request_id' = NEW.request_id::text
          AND assignment.payload->>'cohort_id' = NEW.cohort_id::text
          AND assignment.payload->>'arm_id' = NEW.artifact_json->>'selected_arm_id'
          AND (assignment.payload->>'arm_probability')::numeric = 0.5
          AND assignment.payload->>'intended_action_id' = CASE
              WHEN NEW.artifact_json->>'selected_arm_id' = 'baseline' THEN 'static'
              ELSE assignment.payload->>'recommended_action_id'
          END
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'adaptive randomization receipt does not bind its claim and assignment';
    END IF;
    NEW.created_at := statement_timestamp();
    RETURN NEW;
END;
$$;

CREATE FUNCTION aura.reject_adaptive_randomization_receipt_mutation()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog
AS $$
BEGIN
    RAISE EXCEPTION USING
        ERRCODE = '55000',
        MESSAGE = 'adaptive randomization receipts are immutable';
END;
$$;

CREATE TRIGGER adaptive_randomization_receipts_binding
    BEFORE INSERT ON aura.adaptive_randomization_receipts
    FOR EACH ROW EXECUTE FUNCTION aura.enforce_adaptive_randomization_receipt_binding();

CREATE TRIGGER adaptive_randomization_receipts_immutable
    BEFORE UPDATE OR DELETE ON aura.adaptive_randomization_receipts
    FOR EACH ROW EXECUTE FUNCTION aura.reject_adaptive_randomization_receipt_mutation();

ALTER TABLE aura.adaptive_randomization_receipts ENABLE ROW LEVEL SECURITY;

CREATE POLICY adaptive_randomization_receipts_owner_isolation
    ON aura.adaptive_randomization_receipts
    USING (owner_id = NULLIF(current_setting('app.current_identity', true), '')::uuid)
    WITH CHECK (owner_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);

REVOKE ALL ON aura.adaptive_randomization_receipts FROM aura_app;
GRANT SELECT, INSERT ON aura.adaptive_randomization_receipts TO aura_app;

REVOKE ALL ON FUNCTION aura.adaptive_randomization_receipt_artifact_valid(
    bytea, jsonb, uuid, uuid, uuid, uuid, uuid, bytea, bytea, bytea
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION aura.adaptive_randomization_receipt_artifact_valid(
    bytea, jsonb, uuid, uuid, uuid, uuid, uuid, bytea, bytea, bytea
) TO aura_app;
REVOKE ALL ON FUNCTION aura.enforce_adaptive_randomization_receipt_binding() FROM PUBLIC;
REVOKE ALL ON FUNCTION aura.reject_adaptive_randomization_receipt_mutation() FROM PUBLIC;

COMMENT ON TABLE aura.adaptive_randomization_receipts IS
    'Immutable canonical proof of the store-owned one-byte focal assignment draw.';
