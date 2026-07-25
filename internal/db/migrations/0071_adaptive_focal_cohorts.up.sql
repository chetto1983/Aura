CREATE UNIQUE INDEX adaptive_policy_snapshots_cohort_scope_uidx
    ON aura.adaptive_policy_snapshots (
        owner_id, id, sha256, domain, decision_point, provider_id, model_id,
        policy_version
    );

CREATE FUNCTION aura.adaptive_focal_cohort_id_valid(
    p_value text, p_maximum_length integer
) RETURNS boolean LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE
SET search_path = pg_catalog AS $$
DECLARE normalized text; token text;
BEGIN
    IF p_value <> lower(p_value)
       OR NOT aura.adaptive_schema2_ascii_id_valid(p_value, p_maximum_length)
    THEN RETURN false; END IF;
    normalized := replace(replace(p_value, '_', '-'), '.', '-');
    IF normalized LIKE '%api-key%' OR normalized LIKE '%private-summary%' THEN
        RETURN false;
    END IF;
    FOREACH token IN ARRAY regexp_split_to_array(normalized, '-') LOOP
        IF token ~ '^(apikey|secret|password|credential|bearer|token|content)'
        THEN RETURN false; END IF;
    END LOOP;
    RETURN true;
END; $$;

CREATE FUNCTION aura.adaptive_focal_cohort_catalogs_valid(p_document jsonb)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE
SET search_path = pg_catalog AS $$
DECLARE entry jsonb; prior text; arm_total numeric := 0; action_total numeric := 0;
    alpha_total numeric := 0; numeric_value numeric; evaluator jsonb; block text; n integer;
BEGIN
    IF jsonb_typeof(p_document->'arms') <> 'array' OR jsonb_array_length(p_document->'arms') NOT BETWEEN 2 AND 128
       OR jsonb_typeof(p_document->'actions') <> 'array' OR jsonb_array_length(p_document->'actions') NOT BETWEEN 2 AND 128
       OR jsonb_typeof(p_document->'evaluators') <> 'array' OR jsonb_array_length(p_document->'evaluators') NOT BETWEEN 1 AND 128
       OR jsonb_typeof(p_document->'looks') <> 'array' OR jsonb_array_length(p_document->'looks') <> 5 THEN RETURN false; END IF;
    prior := NULL;
    FOR entry IN SELECT value FROM jsonb_array_elements(p_document->'arms') LOOP
        IF jsonb_typeof(entry) <> 'object' OR NOT entry ?& ARRAY['arm_id','probability'] OR (SELECT count(*) FROM jsonb_object_keys(entry)) <> 2
           OR jsonb_typeof(entry->'arm_id') <> 'string' OR jsonb_typeof(entry->'probability') <> 'number' THEN RETURN false; END IF;
        numeric_value := (entry->>'probability')::numeric;
        IF NOT aura.adaptive_focal_cohort_id_valid(entry->>'arm_id', 128) OR numeric_value <= 0 OR numeric_value > 1
           OR (prior IS NOT NULL AND (entry->>'arm_id') COLLATE "C" <= prior COLLATE "C") THEN RETURN false; END IF;
        prior := entry->>'arm_id'; arm_total := arm_total + numeric_value;
    END LOOP;
    IF abs(arm_total - 1) > 0.000000001 THEN RETURN false; END IF;
    prior := NULL;
    FOR entry IN SELECT value FROM jsonb_array_elements(p_document->'actions') LOOP
        IF jsonb_typeof(entry) <> 'object' OR NOT entry ?& ARRAY['action_id','probability'] OR (SELECT count(*) FROM jsonb_object_keys(entry)) <> 2
           OR jsonb_typeof(entry->'action_id') <> 'string' OR jsonb_typeof(entry->'probability') <> 'number' THEN RETURN false; END IF;
        numeric_value := (entry->>'probability')::numeric;
        IF entry->>'action_id' = 'none' OR NOT aura.adaptive_focal_cohort_id_valid(entry->>'action_id', 128) OR numeric_value <= 0 OR numeric_value > 1
           OR (prior IS NOT NULL AND (entry->>'action_id') COLLATE "C" <= prior COLLATE "C") THEN RETURN false; END IF;
        prior := entry->>'action_id'; action_total := action_total + numeric_value;
    END LOOP;
    IF abs(action_total - 1) > 0.000000001 OR NOT (p_document->'actions' @> '[{"action_id":"static"}]') THEN RETURN false; END IF;
    prior := NULL;
    FOR evaluator IN SELECT value FROM jsonb_array_elements(p_document->'evaluators') LOOP
        IF jsonb_typeof(evaluator) <> 'object' OR NOT evaluator ?& ARRAY['kind','id','version','rubric_id','calibration_id','provenance_sha256'] OR (SELECT count(*) FROM jsonb_object_keys(evaluator)) <> 6
           OR jsonb_typeof(evaluator->'kind') <> 'string' OR jsonb_typeof(evaluator->'id') <> 'string' OR jsonb_typeof(evaluator->'version') <> 'string' OR jsonb_typeof(evaluator->'rubric_id') <> 'string' OR jsonb_typeof(evaluator->'calibration_id') <> 'string' OR jsonb_typeof(evaluator->'provenance_sha256') <> 'string'
           OR evaluator->>'kind' NOT IN ('deterministic','human','calibrated_judge') OR NOT aura.adaptive_focal_cohort_id_valid(evaluator->>'id', 128) OR NOT aura.adaptive_focal_cohort_id_valid(evaluator->>'version', 128) OR NOT aura.adaptive_focal_cohort_id_valid(evaluator->>'rubric_id', 128) OR NOT aura.adaptive_focal_cohort_id_valid(evaluator->>'calibration_id', 128) OR evaluator->>'provenance_sha256' !~ '^[0-9a-f]{64}$'
           OR (prior IS NOT NULL AND (((evaluator->>'kind') || '/' || (evaluator->>'id')) COLLATE "C") <= prior COLLATE "C") THEN RETURN false; END IF;
        prior := (evaluator->>'kind') || '/' || (evaluator->>'id');
    END LOOP;
    FOREACH block IN ARRAY ARRAY['primary_quality','primary_harm'] LOOP
        entry := p_document -> block;
        evaluator := entry->'evaluator';
        IF jsonb_typeof(entry) <> 'object' OR NOT entry ?& ARRAY['id','kind','evaluator'] OR (SELECT count(*) FROM jsonb_object_keys(entry)) <> 3
           OR jsonb_typeof(entry->'id') <> 'string' OR jsonb_typeof(entry->'kind') <> 'string' OR jsonb_typeof(entry->'evaluator') <> 'object'
           OR NOT evaluator ?& ARRAY['kind','id','version','rubric_id','calibration_id','provenance_sha256'] OR (SELECT count(*) FROM jsonb_object_keys(evaluator)) <> 6
           OR jsonb_typeof(evaluator->'kind') <> 'string' OR jsonb_typeof(evaluator->'id') <> 'string' OR jsonb_typeof(evaluator->'version') <> 'string' OR jsonb_typeof(evaluator->'rubric_id') <> 'string' OR jsonb_typeof(evaluator->'calibration_id') <> 'string' OR jsonb_typeof(evaluator->'provenance_sha256') <> 'string'
           OR (entry->>'kind') <> (CASE block WHEN 'primary_quality' THEN 'binary_quality' ELSE 'binary_harm' END)
           OR NOT aura.adaptive_focal_cohort_id_valid(entry->>'id', 128) OR (entry->>'id') <> (evaluator->>'rubric_id') OR NOT EXISTS (SELECT 1 FROM jsonb_array_elements(p_document->'evaluators') AS catalog(value) WHERE catalog.value = evaluator) THEN RETURN false; END IF;
    END LOOP;
    entry := p_document->'admission';
    IF jsonb_typeof(entry) <> 'object' OR NOT entry ?& ARRAY['block_keys','time_block_seconds','interference'] OR (SELECT count(*) FROM jsonb_object_keys(entry)) <> 3
       OR jsonb_typeof(entry->'block_keys') <> 'array' OR jsonb_array_length(entry->'block_keys') NOT BETWEEN 1 AND 4 OR jsonb_typeof(entry->'time_block_seconds') <> 'number' OR jsonb_typeof(entry->'interference') <> 'string' OR entry->>'interference' <> 'none_between_units' THEN RETURN false; END IF;
    prior := NULL;
    FOR entry IN SELECT value FROM jsonb_array_elements(p_document->'admission'->'block_keys') LOOP
        block := entry #>> '{}';
        IF jsonb_typeof(entry) <> 'string' OR block NOT IN ('episode','owner','session','time_block')
           OR (prior IS NOT NULL AND block COLLATE "C" <= prior COLLATE "C") THEN RETURN false; END IF;
        prior := block;
    END LOOP;
    numeric_value := (p_document->'admission'->>'time_block_seconds')::numeric;
    IF prior IS NULL OR NOT (p_document->'admission'->'block_keys' @> '["time_block"]') OR numeric_value <= 0 OR numeric_value <> trunc(numeric_value) OR numeric_value > 9223372036854775807 THEN RETURN false; END IF;
    n := 0;
    FOR entry IN SELECT value FROM jsonb_array_elements(p_document->'looks') LOOP
        n := n + 1;
        IF jsonb_typeof(entry) <> 'object' OR NOT entry ?& ARRAY['look','alpha'] OR (SELECT count(*) FROM jsonb_object_keys(entry)) <> 2 OR jsonb_typeof(entry->'look') <> 'number' OR jsonb_typeof(entry->'alpha') <> 'number' THEN RETURN false; END IF;
        numeric_value := (entry->>'alpha')::numeric;
        IF (entry->>'look')::numeric <> n OR numeric_value <= 0 OR numeric_value > 1 THEN RETURN false; END IF;
        alpha_total := alpha_total + numeric_value;
    END LOOP;
    RETURN alpha_total <= 0.050000000001;
EXCEPTION WHEN character_not_in_repertoire OR data_exception OR invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END; $$;

CREATE FUNCTION aura.adaptive_focal_cohort_metrics_valid(p_document jsonb)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE
SET search_path = pg_catalog AS $$
DECLARE power jsonb := p_document->'power'; margins jsonb := p_document->'margins'; censoring jsonb := p_document->'censoring'; q numeric; h numeric; lift numeric; support numeric; expected numeric; minimum numeric; numeric_value numeric;
BEGIN
    IF jsonb_typeof(power) <> 'object' OR NOT power ?& ARRAY['baseline_quality_rate','baseline_harm_rate','minimum_detectable_lift','expected_members','minimum_members','minimum_arm_support','simulation_sha256'] OR (SELECT count(*) FROM jsonb_object_keys(power)) <> 7
       OR jsonb_typeof(margins) <> 'object' OR NOT margins ?& ARRAY['minimum_quality_uplift','maximum_harm_increase'] OR (SELECT count(*) FROM jsonb_object_keys(margins)) <> 2
       OR jsonb_typeof(censoring) <> 'object' OR NOT censoring ?& ARRAY['max_censor_rate','max_censor_imbalance'] OR (SELECT count(*) FROM jsonb_object_keys(censoring)) <> 2
       OR jsonb_typeof(power->'baseline_quality_rate') <> 'number' OR jsonb_typeof(power->'baseline_harm_rate') <> 'number' OR jsonb_typeof(power->'minimum_detectable_lift') <> 'number' OR jsonb_typeof(power->'expected_members') <> 'number' OR jsonb_typeof(power->'minimum_members') <> 'number' OR jsonb_typeof(power->'minimum_arm_support') <> 'number' OR jsonb_typeof(power->'simulation_sha256') <> 'string'
       OR jsonb_typeof(margins->'minimum_quality_uplift') <> 'number' OR jsonb_typeof(margins->'maximum_harm_increase') <> 'number' OR jsonb_typeof(censoring->'max_censor_rate') <> 'number' OR jsonb_typeof(censoring->'max_censor_imbalance') <> 'number' THEN RETURN false; END IF;
    q := (power->>'baseline_quality_rate')::numeric; h := (power->>'baseline_harm_rate')::numeric; lift := (power->>'minimum_detectable_lift')::numeric; support := (power->>'minimum_arm_support')::numeric; expected := (power->>'expected_members')::numeric; minimum := (power->>'minimum_members')::numeric;
    IF q NOT BETWEEN 0 AND 1 OR h NOT BETWEEN 0 AND 1 OR lift <= 0 OR q + lift > 1 OR support <= 0 OR support > 1 OR expected <= 0 OR minimum <= 0 OR minimum > expected OR expected <> trunc(expected) OR minimum <> trunc(minimum) OR expected > 18446744073709551615 OR power->>'simulation_sha256' !~ '^[0-9a-f]{64}$' THEN RETURN false; END IF;
    FOR numeric_value IN SELECT (entry.value->>'probability')::numeric FROM jsonb_array_elements(p_document->'arms') AS entry(value) LOOP IF numeric_value < support THEN RETURN false; END IF; END LOOP;
    q := (margins->>'minimum_quality_uplift')::numeric; h := (margins->>'maximum_harm_increase')::numeric;
    IF q < 0 OR q >= 1 OR (power->>'baseline_quality_rate')::numeric + q >= 1 OR h < 0 OR h >= 1 OR (power->>'baseline_harm_rate')::numeric + h > 1 OR (censoring->>'max_censor_rate')::numeric < 0 OR (censoring->>'max_censor_rate')::numeric >= 1 OR (censoring->>'max_censor_imbalance')::numeric < 0 OR (censoring->>'max_censor_imbalance')::numeric >= 1 THEN RETURN false; END IF;
    RETURN true;
EXCEPTION WHEN data_exception OR invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END; $$;

CREATE FUNCTION aura.adaptive_focal_cohort_artifact_valid(
    p_artifact bytea, p_artifact_json jsonb, p_owner_id uuid, p_provider_id text,
    p_model_id text, p_policy_epoch bigint, p_policy_version text, p_snapshot_id uuid,
    p_snapshot_sha256 bytea, p_environment text, p_domain text, p_decision_point text,
    p_point_ordinal bigint, p_predicate_sha256 bytea, p_experiment_id text, p_cutoff timestamptz
) RETURNS boolean LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE SET search_path = pg_catalog AS $$
DECLARE document jsonb; scope_json jsonb; predicate_json jsonb; expected_predicate bytea;
BEGIN
    IF NOT (convert_from(p_artifact, 'UTF8') IS JSON OBJECT WITH UNIQUE KEYS) THEN RETURN false; END IF;
    document := convert_from(p_artifact, 'UTF8')::jsonb;
    IF document <> p_artifact_json OR jsonb_typeof(document) <> 'object' OR NOT document ?& ARRAY['schema_version','scope','predicate','experiment_id','arms','actions','evaluators','primary_quality','primary_harm','power','margins','censoring','admission','looks','cutoff'] OR (SELECT count(*) FROM jsonb_object_keys(document)) <> 15
       OR jsonb_typeof(document->'schema_version') <> 'string' OR document->>'schema_version' <> '1.0'
       OR jsonb_typeof(document->'experiment_id') <> 'string' OR jsonb_typeof(document->'cutoff') <> 'string'
       OR NOT aura.adaptive_focal_cohort_id_valid(document->>'experiment_id', 128)
       OR NOT aura.adaptive_focal_cohort_catalogs_valid(document)
       OR NOT aura.adaptive_focal_cohort_metrics_valid(document) THEN RETURN false; END IF;
    scope_json := document->'scope'; predicate_json := document->'predicate';
    IF jsonb_typeof(scope_json) <> 'object' OR NOT scope_json ?& ARRAY['owner_id','provider_id','model_id','policy_version','policy_epoch','snapshot_id','snapshot_sha256','environment'] OR (SELECT count(*) FROM jsonb_object_keys(scope_json)) <> 8 OR jsonb_typeof(predicate_json) <> 'object' OR NOT predicate_json ?& ARRAY['domain','point','ordinal'] OR (SELECT count(*) FROM jsonb_object_keys(predicate_json)) <> 3
       OR jsonb_typeof(scope_json->'owner_id') <> 'string' OR jsonb_typeof(scope_json->'provider_id') <> 'string'
       OR jsonb_typeof(scope_json->'model_id') <> 'string' OR jsonb_typeof(scope_json->'policy_version') <> 'string'
       OR jsonb_typeof(scope_json->'policy_epoch') <> 'number' OR jsonb_typeof(scope_json->'snapshot_id') <> 'string'
       OR jsonb_typeof(scope_json->'snapshot_sha256') <> 'string' OR jsonb_typeof(scope_json->'environment') <> 'string'
       OR scope_json->>'owner_id' <> p_owner_id::text OR scope_json->>'provider_id' <> p_provider_id
       OR scope_json->>'model_id' <> p_model_id OR (scope_json->>'policy_epoch')::numeric <> p_policy_epoch
       OR (scope_json->>'policy_epoch')::numeric <> trunc((scope_json->>'policy_epoch')::numeric)
       OR scope_json->>'policy_version' <> p_policy_version OR scope_json->>'snapshot_id' <> p_snapshot_id::text
       OR scope_json->>'snapshot_sha256' !~ '^[0-9a-f]{64}$'
       OR decode(scope_json->>'snapshot_sha256','hex') <> p_snapshot_sha256
       OR scope_json->>'environment' <> p_environment
       OR NOT aura.adaptive_focal_cohort_id_valid(scope_json->>'provider_id',64)
       OR NOT aura.adaptive_focal_cohort_id_valid(scope_json->>'model_id',256)
       OR NOT aura.adaptive_focal_cohort_id_valid(scope_json->>'policy_version',128)
       OR jsonb_typeof(predicate_json->'domain') <> 'string' OR jsonb_typeof(predicate_json->'point') <> 'string'
       OR jsonb_typeof(predicate_json->'ordinal') <> 'number'
       OR predicate_json->>'domain' <> p_domain OR predicate_json->>'point' <> p_decision_point
       OR (predicate_json->>'ordinal')::numeric <> p_point_ordinal
       OR (predicate_json->>'ordinal')::numeric <> trunc((predicate_json->>'ordinal')::numeric)
       OR document->>'experiment_id' <> p_experiment_id OR (document->>'cutoff')::timestamptz <> p_cutoff THEN RETURN false; END IF;
    expected_predicate := sha256(convert_to(format('{"domain":%s,"point":%s,"ordinal":%s}', to_json(p_domain)::text, to_json(p_decision_point)::text, p_point_ordinal), 'UTF8'));
    RETURN expected_predicate = p_predicate_sha256;
EXCEPTION WHEN character_not_in_repertoire OR data_exception OR invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END; $$;

CREATE FUNCTION aura.reject_adaptive_focal_cohort_update()
RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog
AS $$
BEGIN
    RAISE EXCEPTION USING
        ERRCODE = '55000',
        MESSAGE = 'adaptive focal cohorts and claims are immutable';
END;
$$;

CREATE FUNCTION aura.set_adaptive_focal_cohort_created_at()
RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog
AS $$
BEGIN
    NEW.created_at := statement_timestamp();
    IF NEW.created_at >= NEW.cutoff THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'adaptive focal cohort cutoff must be after server creation time';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION aura.enforce_adaptive_focal_claim_cutoff_and_blocks()
RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog AS $$
DECLARE
    cohort_cutoff timestamptz;
    block_keys jsonb;
    block_seconds bigint;
BEGIN
    SELECT cutoff, artifact_json->'admission'->'block_keys',
           (artifact_json->'admission'->>'time_block_seconds')::bigint
    INTO cohort_cutoff, block_keys, block_seconds
    FROM aura.adaptive_focal_cohorts
    WHERE id=NEW.cohort_id AND owner_id=NEW.owner_id;
    IF NOT FOUND THEN RETURN NEW; END IF;
    NEW.claimed_at := statement_timestamp();
    IF NEW.claimed_at >= cohort_cutoff THEN
        RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='adaptive focal claim is after cohort cutoff';
    END IF;
    NEW.time_block_start := to_timestamp(floor(extract(epoch FROM NEW.claimed_at) / block_seconds) * block_seconds);
    IF (block_keys ? 'session') <> (NEW.session_id IS NOT NULL)
       OR (block_keys ? 'episode') <> (NEW.episode_id IS NOT NULL) THEN
        RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='adaptive focal claim block keys do not match cohort admission';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TABLE aura.adaptive_focal_cohorts (
    id                 uuid PRIMARY KEY,
    owner_id           uuid NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE,
    provider_id        text NOT NULL CHECK (aura.adaptive_schema2_ascii_id_valid(provider_id, 64)),
    model_id           text NOT NULL CHECK (aura.adaptive_schema2_ascii_id_valid(model_id, 256)),
    policy_epoch       bigint NOT NULL CHECK (policy_epoch > 0),
    policy_version     text NOT NULL CHECK (aura.adaptive_schema2_ascii_id_valid(policy_version, 128)),
    snapshot_id        uuid NOT NULL,
    snapshot_sha256    bytea NOT NULL CHECK (octet_length(snapshot_sha256) = 32),
    environment        text NOT NULL CHECK (environment = 'production_canary'),
    domain             text NOT NULL CHECK (domain IN ('reasoning', 'tool_discovery', 'skill_routing', 'knowledge_retrieval', 'memory_recall')),
    decision_point     text NOT NULL CHECK (decision_point = domain),
    point_ordinal      bigint NOT NULL CHECK (point_ordinal BETWEEN 0 AND 4294967295),
    predicate_sha256   bytea NOT NULL CHECK (octet_length(predicate_sha256) = 32),
    experiment_id      text NOT NULL CHECK (aura.adaptive_schema2_ascii_id_valid(experiment_id, 128)),
    cutoff             timestamptz NOT NULL,
    sha256             bytea NOT NULL CHECK (octet_length(sha256) = 32),
    artifact           bytea NOT NULL,
    artifact_json      jsonb NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT statement_timestamp(),
    CONSTRAINT adaptive_focal_cohorts_snapshot_scope_fk
        FOREIGN KEY (owner_id, snapshot_id, snapshot_sha256, domain, decision_point, provider_id, model_id, policy_version)
        REFERENCES aura.adaptive_policy_snapshots (owner_id, id, sha256, domain, decision_point, provider_id, model_id, policy_version),
    CONSTRAINT adaptive_focal_cohorts_scope_uidx
        UNIQUE (id, owner_id, domain, decision_point, point_ordinal),
    CONSTRAINT adaptive_focal_cohorts_artifact_digest_check CHECK (
        sha256 = pg_catalog.sha256(artifact)
        AND id = aura.adaptive_uuid_v5_sha256(
            '37e07806-1464-47f3-8d89-f922fe7a1392'::uuid, sha256
        )
    ),
    CONSTRAINT adaptive_focal_cohorts_artifact_scope_check CHECK (
        aura.adaptive_focal_cohort_artifact_valid(
            artifact, artifact_json, owner_id, provider_id, model_id, policy_epoch,
            policy_version, snapshot_id, snapshot_sha256, environment, domain,
            decision_point, point_ordinal, predicate_sha256, experiment_id, cutoff
        )
    ),
    CONSTRAINT adaptive_focal_cohorts_cutoff_check CHECK (created_at < cutoff)
);

CREATE TABLE aura.adaptive_focal_cohort_claims (
    id                         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id                   uuid NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE,
    cohort_id                  uuid NOT NULL,
    request_id                 uuid NOT NULL,
    evaluation_conversation_id uuid NOT NULL,
    assignment_id              uuid NOT NULL,
    domain                     text NOT NULL,
    decision_point             text NOT NULL,
    point_ordinal              bigint NOT NULL CHECK (point_ordinal BETWEEN 0 AND 4294967295),
    session_id                 uuid,
    episode_id                 uuid,
    time_block_start           timestamptz NOT NULL,
    claimed_at                 timestamptz NOT NULL DEFAULT statement_timestamp(),
    CONSTRAINT adaptive_focal_cohort_claims_scope_fk
        FOREIGN KEY (cohort_id, owner_id, domain, decision_point, point_ordinal)
        REFERENCES aura.adaptive_focal_cohorts (
            id, owner_id, domain, decision_point, point_ordinal
        ) ON DELETE CASCADE,
    CONSTRAINT adaptive_focal_cohort_claims_assignment_id_check CHECK (
        assignment_id = aura.adaptive_schema2_assignment_id(
            owner_id, request_id, decision_point, point_ordinal
        )
    ),
    UNIQUE (cohort_id, request_id),
    UNIQUE (cohort_id, evaluation_conversation_id),
    UNIQUE (cohort_id, assignment_id)
);

CREATE INDEX adaptive_focal_cohort_claims_owner_cohort_idx
    ON aura.adaptive_focal_cohort_claims (owner_id, cohort_id);

CREATE TRIGGER adaptive_focal_cohorts_created_at
    BEFORE INSERT ON aura.adaptive_focal_cohorts
    FOR EACH ROW EXECUTE FUNCTION aura.set_adaptive_focal_cohort_created_at();
CREATE TRIGGER adaptive_focal_cohorts_immutable
    BEFORE UPDATE ON aura.adaptive_focal_cohorts
    FOR EACH ROW EXECUTE FUNCTION aura.reject_adaptive_focal_cohort_update();
CREATE TRIGGER adaptive_focal_cohort_claims_immutable
    BEFORE UPDATE ON aura.adaptive_focal_cohort_claims
    FOR EACH ROW EXECUTE FUNCTION aura.reject_adaptive_focal_cohort_update();
CREATE TRIGGER adaptive_focal_cohort_claims_cutoff_and_blocks
    BEFORE INSERT ON aura.adaptive_focal_cohort_claims
    FOR EACH ROW EXECUTE FUNCTION aura.enforce_adaptive_focal_claim_cutoff_and_blocks();

ALTER TABLE aura.adaptive_focal_cohorts ENABLE ROW LEVEL SECURITY;
ALTER TABLE aura.adaptive_focal_cohort_claims ENABLE ROW LEVEL SECURITY;
CREATE POLICY adaptive_focal_cohorts_owner_isolation ON aura.adaptive_focal_cohorts
    USING (owner_id = NULLIF(current_setting('app.current_identity', true), '')::uuid)
    WITH CHECK (owner_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);
CREATE POLICY adaptive_focal_cohort_claims_owner_isolation ON aura.adaptive_focal_cohort_claims
    USING (owner_id = NULLIF(current_setting('app.current_identity', true), '')::uuid)
    WITH CHECK (owner_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);

REVOKE ALL ON aura.adaptive_focal_cohorts FROM aura_app;
REVOKE ALL ON aura.adaptive_focal_cohort_claims FROM aura_app;
GRANT SELECT, INSERT ON aura.adaptive_focal_cohorts TO aura_app;
GRANT SELECT, INSERT ON aura.adaptive_focal_cohort_claims TO aura_app;
REVOKE ALL ON FUNCTION aura.adaptive_focal_cohort_artifact_valid(bytea, jsonb, uuid, text, text, bigint, text, uuid, bytea, text, text, text, bigint, bytea, text, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION aura.adaptive_focal_cohort_artifact_valid(bytea, jsonb, uuid, text, text, bigint, text, uuid, bytea, text, text, text, bigint, bytea, text, timestamptz) TO aura_app;
REVOKE ALL ON FUNCTION aura.adaptive_focal_cohort_id_valid(text, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION aura.adaptive_focal_cohort_id_valid(text, integer) TO aura_app;
REVOKE ALL ON FUNCTION aura.adaptive_focal_cohort_catalogs_valid(jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION aura.adaptive_focal_cohort_catalogs_valid(jsonb) TO aura_app;
REVOKE ALL ON FUNCTION aura.adaptive_focal_cohort_metrics_valid(jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION aura.adaptive_focal_cohort_metrics_valid(jsonb) TO aura_app;
REVOKE ALL ON FUNCTION aura.reject_adaptive_focal_cohort_update() FROM PUBLIC;
REVOKE ALL ON FUNCTION aura.set_adaptive_focal_cohort_created_at() FROM PUBLIC;
REVOKE ALL ON FUNCTION aura.enforce_adaptive_focal_claim_cutoff_and_blocks() FROM PUBLIC;

COMMENT ON TABLE aura.adaptive_focal_cohorts IS 'Authoritative immutable preregistered focal cohort artifacts.';
COMMENT ON TABLE aura.adaptive_focal_cohort_claims IS 'Immutable owner-scoped focal assignment claims made before arm draw.';
