-- Typed, append-only adaptive evidence. Legacy promotion rows remain readable,
-- but only these content-addressed artifacts are transition-eligible.
CREATE TABLE aura.adaptive_evidence_artifacts (
    id              uuid PRIMARY KEY,
    owner_id        uuid NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE,
    cohort_id       uuid NOT NULL REFERENCES aura.adaptive_focal_cohorts(id) ON DELETE CASCADE,
    kind            text NOT NULL CHECK (kind IN (
        'randomization_plan', 'interference_plan', 'look_plan', 'snapshot',
        'quality_outcome_model', 'harm_outcome_model', 'ope_plan',
        'operator_approval', 'quality_report', 'harm_report',
        'quality_ope_report', 'harm_ope_report', 'censor_diagnostics'
    )),
    schema_id       text NOT NULL CHECK (btrim(schema_id) <> ''),
    revision        integer NOT NULL CHECK (revision = 1),
    sha256          bytea NOT NULL CHECK (octet_length(sha256) = 32),
    artifact        bytea NOT NULL,
    artifact_json   jsonb NOT NULL CHECK (jsonb_typeof(artifact_json) = 'object'),
    created_at      timestamptz NOT NULL DEFAULT statement_timestamp(),
    UNIQUE (owner_id, cohort_id, kind, id),
    CHECK (sha256 = pg_catalog.sha256(artifact)),
    CHECK (artifact IS JSON OBJECT WITH UNIQUE KEYS),
    CHECK (artifact_json->>'schema_id' = schema_id),
    CHECK ((artifact_json->>'revision')::integer = revision)
);

CREATE TABLE aura.adaptive_sealed_evidence (
    id                       uuid PRIMARY KEY,
    kind                     text NOT NULL CHECK (
        kind IN ('canary_admission', 'canary_outcome')
    ),
    owner_id                 uuid NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE,
    cohort_id                uuid NOT NULL REFERENCES aura.adaptive_focal_cohorts(id) ON DELETE CASCADE,
    cohort_sha256            bytea NOT NULL CHECK (octet_length(cohort_sha256) = 32),
    domain                   text NOT NULL CHECK (domain IN (
        'reasoning', 'tool_discovery', 'skill_routing',
        'knowledge_retrieval', 'memory_recall'
    )),
    provider_id              text NOT NULL,
    model_id                 text NOT NULL,
    policy_epoch             bigint NOT NULL CHECK (policy_epoch > 0),
    policy_version           text NOT NULL,
    snapshot_id              uuid NOT NULL,
    snapshot_sha256          bytea NOT NULL CHECK (octet_length(snapshot_sha256) = 32),
    look_number              integer CHECK (look_number BETWEEN 1 AND 5),
    look_cutoff              timestamptz,
    predecessor_evidence_id  uuid,
    predecessor_sha256       bytea CHECK (
        predecessor_sha256 IS NULL OR octet_length(predecessor_sha256) = 32
    ),
    disposition              text CHECK (disposition IN (
        'continue', 'promote', 'retain_canary', 'ineligible'
    )),
    eligible                 boolean NOT NULL,
    body_sha256              bytea NOT NULL CHECK (octet_length(body_sha256) = 32),
    sha256                   bytea NOT NULL CHECK (octet_length(sha256) = 32),
    artifact                 bytea NOT NULL,
    artifact_json            jsonb NOT NULL CHECK (jsonb_typeof(artifact_json) = 'object'),
    sealer_actor_id          uuid NOT NULL,
    sealer_capability        text NOT NULL CHECK (btrim(sealer_capability) <> ''),
    created_at               timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CHECK (sha256 = pg_catalog.sha256(artifact)),
    CHECK (artifact IS JSON OBJECT WITH UNIQUE KEYS),
    CHECK (
        (kind = 'canary_admission'
         AND look_number IS NULL AND look_cutoff IS NULL
         AND predecessor_evidence_id IS NULL AND predecessor_sha256 IS NULL
         AND disposition IS NULL AND eligible)
        OR
        (kind = 'canary_outcome'
         AND look_number IS NOT NULL AND look_cutoff IS NOT NULL
         AND disposition IS NOT NULL)
    )
);

CREATE UNIQUE INDEX adaptive_sealed_evidence_admission_uidx
    ON aura.adaptive_sealed_evidence (owner_id, cohort_id)
    WHERE kind = 'canary_admission';
CREATE UNIQUE INDEX adaptive_sealed_evidence_outcome_look_uidx
    ON aura.adaptive_sealed_evidence (owner_id, cohort_id, look_number)
    WHERE kind = 'canary_outcome';

ALTER TABLE aura.adaptive_policy_transitions
    DROP CONSTRAINT IF EXISTS adaptive_policy_transitions_evidence_id_fkey,
    ADD COLUMN evidence_kind text,
    ADD COLUMN evidence_hash bytea,
        ADD COLUMN transition_binding bytea,
        ADD COLUMN transition_binding_json jsonb,
        ADD CONSTRAINT adaptive_policy_transitions_typed_evidence_check CHECK (
            (evidence_kind IS NULL AND evidence_hash IS NULL
             AND transition_binding IS NULL AND transition_binding_json IS NULL)
        OR
        (evidence_id IS NOT NULL
         AND evidence_kind IN ('canary_admission', 'canary_outcome')
         AND octet_length(evidence_hash) = 32
         AND transition_binding IS NOT NULL
         AND transition_binding IS JSON OBJECT WITH UNIQUE KEYS
         AND jsonb_typeof(transition_binding_json) = 'object'
         AND evidence_hash IS NOT NULL)
    );

CREATE FUNCTION aura.reject_adaptive_sealed_evidence_mutation() RETURNS trigger
    LANGUAGE plpgsql
    SECURITY DEFINER
    SET search_path = pg_catalog
AS $$
BEGIN
    RAISE EXCEPTION 'adaptive sealed evidence is append-only: % is not permitted', TG_OP;
END;
$$;

CREATE TRIGGER adaptive_evidence_artifacts_immutable
    BEFORE UPDATE OR DELETE ON aura.adaptive_evidence_artifacts
    FOR EACH ROW EXECUTE FUNCTION aura.reject_adaptive_sealed_evidence_mutation();
CREATE TRIGGER adaptive_evidence_artifacts_no_truncate
    BEFORE TRUNCATE ON aura.adaptive_evidence_artifacts
    FOR EACH STATEMENT EXECUTE FUNCTION aura.reject_adaptive_sealed_evidence_mutation();
CREATE TRIGGER adaptive_sealed_evidence_immutable
    BEFORE UPDATE OR DELETE ON aura.adaptive_sealed_evidence
    FOR EACH ROW EXECUTE FUNCTION aura.reject_adaptive_sealed_evidence_mutation();
CREATE TRIGGER adaptive_sealed_evidence_no_truncate
    BEFORE TRUNCATE ON aura.adaptive_sealed_evidence
    FOR EACH STATEMENT EXECUTE FUNCTION aura.reject_adaptive_sealed_evidence_mutation();

ALTER TABLE aura.adaptive_evidence_artifacts ENABLE ROW LEVEL SECURITY;
ALTER TABLE aura.adaptive_sealed_evidence ENABLE ROW LEVEL SECURITY;
CREATE POLICY adaptive_evidence_artifacts_owner_isolation
    ON aura.adaptive_evidence_artifacts
    USING (owner_id = NULLIF(current_setting('app.current_identity', true), '')::uuid)
    WITH CHECK (owner_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);
CREATE POLICY adaptive_sealed_evidence_owner_isolation
    ON aura.adaptive_sealed_evidence
    USING (owner_id = NULLIF(current_setting('app.current_identity', true), '')::uuid)
    WITH CHECK (owner_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);

CREATE FUNCTION aura.store_adaptive_evidence_artifact(
    p_actor_id uuid,
    p_owner_id uuid,
    p_cohort_id uuid,
    p_kind text,
    p_artifact_id uuid,
    p_schema_id text,
    p_revision integer,
    p_sha256 bytea,
    p_artifact bytea,
    p_artifact_json jsonb
) RETURNS aura.adaptive_evidence_artifacts
    LANGUAGE plpgsql
    SECURITY DEFINER
    SET search_path = pg_catalog
AS $$
DECLARE
    stored aura.adaptive_evidence_artifacts;
BEGIN
    IF p_actor_id <> p_owner_id OR NOT EXISTS (
        SELECT 1 FROM aura.capability_grants
        WHERE identity_id = p_actor_id
          AND (capability = '*' OR capability = 'adaptive.evidence.seal')
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '42501',
            MESSAGE = 'adaptive.evidence.seal capability is required';
    END IF;
    IF NULLIF(current_setting('app.current_identity', true), '')::uuid <> p_owner_id THEN
        RAISE EXCEPTION USING ERRCODE = '42501',
            MESSAGE = 'adaptive evidence owner context differs';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM aura.adaptive_focal_cohorts
        WHERE id = p_cohort_id AND owner_id = p_owner_id
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '23503',
            MESSAGE = 'adaptive evidence cohort is missing';
    END IF;

    INSERT INTO aura.adaptive_evidence_artifacts (
        id, owner_id, cohort_id, kind, schema_id, revision,
        sha256, artifact, artifact_json
    ) VALUES (
        p_artifact_id, p_owner_id, p_cohort_id, p_kind, p_schema_id, p_revision,
        p_sha256, p_artifact, p_artifact_json
    )
    ON CONFLICT (id) DO NOTHING;

    SELECT * INTO stored
    FROM aura.adaptive_evidence_artifacts
    WHERE id = p_artifact_id AND owner_id = p_owner_id AND cohort_id = p_cohort_id;
    IF stored.id IS NULL OR stored.kind <> p_kind OR stored.schema_id <> p_schema_id
       OR stored.revision <> p_revision OR stored.sha256 <> p_sha256
       OR stored.artifact <> p_artifact OR stored.artifact_json <> p_artifact_json THEN
        RAISE EXCEPTION USING ERRCODE = '23505',
            MESSAGE = 'adaptive evidence artifact conflicts with persisted content';
    END IF;
    RETURN stored;
END;
$$;

CREATE FUNCTION aura.seal_adaptive_evidence(
    p_actor_id uuid,
    p_kind text,
    p_evidence_id uuid,
    p_owner_id uuid,
    p_cohort_id uuid,
    p_cohort_sha256 bytea,
    p_domain text,
    p_provider_id text,
    p_model_id text,
    p_policy_epoch bigint,
    p_policy_version text,
    p_snapshot_id uuid,
    p_snapshot_sha256 bytea,
    p_look_number integer,
    p_look_cutoff timestamptz,
    p_predecessor_evidence_id uuid,
    p_predecessor_sha256 bytea,
    p_disposition text,
    p_eligible boolean,
    p_body_sha256 bytea,
    p_sha256 bytea,
    p_artifact bytea,
    p_artifact_json jsonb,
    p_sealer_capability text
) RETURNS aura.adaptive_sealed_evidence
    LANGUAGE plpgsql
    SECURITY DEFINER
    SET search_path = pg_catalog
AS $$
DECLARE
    cohort aura.adaptive_focal_cohorts;
    predecessor aura.adaptive_sealed_evidence;
    stored aura.adaptive_sealed_evidence;
BEGIN
    IF p_actor_id <> p_owner_id OR p_sealer_capability <> 'adaptive.evidence.seal'
       OR NOT EXISTS (
            SELECT 1 FROM aura.capability_grants
            WHERE identity_id = p_actor_id
              AND (capability = '*' OR capability = p_sealer_capability)
       ) THEN
        RAISE EXCEPTION USING ERRCODE = '42501',
            MESSAGE = 'adaptive.evidence.seal capability is required';
    END IF;
    IF NULLIF(current_setting('app.current_identity', true), '')::uuid <> p_owner_id THEN
        RAISE EXCEPTION USING ERRCODE = '42501',
            MESSAGE = 'adaptive evidence owner context differs';
    END IF;

    SELECT * INTO cohort FROM aura.adaptive_focal_cohorts
    WHERE id = p_cohort_id AND owner_id = p_owner_id
    FOR UPDATE;
    IF cohort.id IS NULL OR cohort.sha256 <> p_cohort_sha256
       OR cohort.domain <> p_domain OR cohort.provider_id <> p_provider_id
       OR cohort.model_id <> p_model_id OR cohort.policy_epoch <> p_policy_epoch
       OR cohort.policy_version <> p_policy_version
       OR cohort.snapshot_id <> p_snapshot_id
       OR cohort.snapshot_sha256 <> p_snapshot_sha256
       OR convert_from(p_artifact, 'UTF8')::jsonb <> p_artifact_json
       OR pg_catalog.sha256(p_artifact) <> p_sha256
       OR p_artifact_json->>'evidence_id' <> p_evidence_id::text
       OR p_artifact_json->>'kind' <> p_kind
       OR p_artifact_json->>'owner_id' <> p_owner_id::text
       OR p_artifact_json->>'cohort_id' <> p_cohort_id::text
       OR decode(p_artifact_json->>'cohort_sha256', 'hex') <> p_cohort_sha256
       OR p_artifact_json->>'domain' <> p_domain
       OR p_artifact_json->>'provider_id' <> p_provider_id
       OR p_artifact_json->>'model_id' <> p_model_id
       OR (p_artifact_json->>'policy_epoch')::bigint <> p_policy_epoch
       OR p_artifact_json->>'policy_version' <> p_policy_version
       OR p_artifact_json->>'snapshot_id' <> p_snapshot_id::text
       OR decode(p_artifact_json->>'snapshot_sha256', 'hex') <> p_snapshot_sha256
       OR (p_artifact_json->>'created_at')::timestamptz <> transaction_timestamp()
       OR p_artifact_json->>'sealer_actor_id' <> p_actor_id::text
       OR p_artifact_json->>'sealer_capability' <> p_sealer_capability
       OR decode(p_artifact_json->>'body_sha256', 'hex') <> p_body_sha256 THEN
        RAISE EXCEPTION USING ERRCODE = '23514',
            MESSAGE = 'adaptive sealed evidence scope or content is invalid';
    END IF;

    IF p_kind = 'canary_admission' THEN
        IF p_look_number IS NOT NULL OR p_look_cutoff IS NOT NULL
           OR p_predecessor_evidence_id IS NOT NULL OR p_predecessor_sha256 IS NOT NULL
           OR p_disposition IS NOT NULL OR NOT p_eligible
           OR p_artifact_json->'body'->>'target_state' <> 'canary'
           OR p_artifact_json#>>'{body,snapshot_ref,artifact_id}' <> p_snapshot_id::text
           OR decode(p_artifact_json#>>'{body,snapshot_ref,artifact_sha256}', 'hex') <> p_snapshot_sha256
           OR p_artifact_json#>'{body,randomization_plan_ref}' <> cohort.artifact_json->'randomization_plan_ref'
           OR p_artifact_json#>'{body,interference_plan_ref}' <> cohort.artifact_json->'interference_plan_ref'
           OR p_artifact_json#>'{body,look_plan_ref}' <> cohort.artifact_json->'look_plan_ref'
           OR p_artifact_json#>'{body,quality_outcome_model_ref}' <> cohort.artifact_json->'quality_outcome_model_ref'
           OR p_artifact_json#>'{body,harm_outcome_model_ref}' <> cohort.artifact_json->'harm_outcome_model_ref'
           OR p_artifact_json#>'{body,ope_plan_ref}' <> cohort.artifact_json->'ope_plan_ref'
           OR (SELECT count(*) FROM aura.adaptive_evidence_artifacts AS child
               WHERE child.owner_id = p_owner_id AND child.cohort_id = p_cohort_id
                 AND child.kind || ':' || child.id::text || ':' || encode(child.sha256, 'hex') = ANY (ARRAY[
                    'randomization_plan:' || (p_artifact_json#>>'{body,randomization_plan_ref,artifact_id}') || ':' || (p_artifact_json#>>'{body,randomization_plan_ref,artifact_sha256}'),
                    'interference_plan:' || (p_artifact_json#>>'{body,interference_plan_ref,artifact_id}') || ':' || (p_artifact_json#>>'{body,interference_plan_ref,artifact_sha256}'),
                    'look_plan:' || (p_artifact_json#>>'{body,look_plan_ref,artifact_id}') || ':' || (p_artifact_json#>>'{body,look_plan_ref,artifact_sha256}'),
                    'quality_outcome_model:' || (p_artifact_json#>>'{body,quality_outcome_model_ref,artifact_id}') || ':' || (p_artifact_json#>>'{body,quality_outcome_model_ref,artifact_sha256}'),
                    'harm_outcome_model:' || (p_artifact_json#>>'{body,harm_outcome_model_ref,artifact_id}') || ':' || (p_artifact_json#>>'{body,harm_outcome_model_ref,artifact_sha256}'),
                    'ope_plan:' || (p_artifact_json#>>'{body,ope_plan_ref,artifact_id}') || ':' || (p_artifact_json#>>'{body,ope_plan_ref,artifact_sha256}'),
                    'operator_approval:' || (p_artifact_json#>>'{body,operator_approval_ref,artifact_id}') || ':' || (p_artifact_json#>>'{body,operator_approval_ref,artifact_sha256}')
                 ])) <> 7 THEN
            RAISE EXCEPTION USING ERRCODE = '23514',
                MESSAGE = 'canary admission evidence binding is invalid';
        END IF;
    ELSIF p_kind = 'canary_outcome' THEN
        IF p_look_number IS NULL OR p_look_cutoff IS NULL OR p_disposition IS NULL
           OR p_artifact_json->'body'->>'target_state' <> 'active'
           OR (p_artifact_json#>>'{body,look_number}')::integer IS DISTINCT FROM p_look_number
           OR (p_artifact_json#>>'{body,look_cutoff}')::timestamptz IS DISTINCT FROM p_look_cutoff
           OR p_artifact_json#>>'{body,disposition}' IS DISTINCT FROM p_disposition
           OR (p_artifact_json#>>'{body,eligible}')::boolean IS DISTINCT FROM p_eligible
           OR p_artifact_json#>>'{body,randomization_plan_sha256}'
              IS DISTINCT FROM cohort.artifact_json#>>'{randomization_plan_ref,artifact_sha256}'
           OR (p_disposition = 'continue' AND p_look_number = 5)
           OR (p_disposition = 'ineligible') = p_eligible THEN
            RAISE EXCEPTION USING ERRCODE = '23514',
                MESSAGE = 'canary outcome evidence binding is invalid';
        END IF;
        IF (SELECT count(*) FROM aura.adaptive_evidence_artifacts AS report
            WHERE report.owner_id = p_owner_id AND report.cohort_id = p_cohort_id
              AND report.kind || ':' || report.id::text || ':' || encode(report.sha256, 'hex') = ANY (ARRAY[
                'quality_report:' || (p_artifact_json#>>'{body,quality_report_id}') || ':' || (p_artifact_json#>>'{body,quality_report_sha256}'),
                'harm_report:' || (p_artifact_json#>>'{body,harm_report_id}') || ':' || (p_artifact_json#>>'{body,harm_report_sha256}'),
                'quality_ope_report:' || (p_artifact_json#>>'{body,quality_ope_report_id}') || ':' || (p_artifact_json#>>'{body,quality_ope_report_sha256}'),
                'harm_ope_report:' || (p_artifact_json#>>'{body,harm_ope_report_id}') || ':' || (p_artifact_json#>>'{body,harm_ope_report_sha256}'),
                'censor_diagnostics:' || (p_artifact_json#>>'{body,censor_diagnostics_id}') || ':' || (p_artifact_json#>>'{body,censor_diagnostics_sha256}')
              ])) <> 5 THEN
            RAISE EXCEPTION USING ERRCODE = '23514',
                MESSAGE = 'canary outcome report binding is invalid';
        END IF;
        IF p_look_number = 1 THEN
            IF p_predecessor_evidence_id IS NOT NULL OR p_predecessor_sha256 IS NOT NULL
               OR p_artifact_json#>'{body,predecessor_evidence_id}' <> 'null'::jsonb
               OR p_artifact_json#>'{body,predecessor_evidence_sha256}' <> 'null'::jsonb THEN
                RAISE EXCEPTION USING ERRCODE = '23514',
                    MESSAGE = 'look one cannot bind predecessor evidence';
            END IF;
        ELSE
            SELECT * INTO predecessor FROM aura.adaptive_sealed_evidence
            WHERE id = p_predecessor_evidence_id
              AND owner_id = p_owner_id AND cohort_id = p_cohort_id
              AND kind = 'canary_outcome' AND look_number = p_look_number - 1
            FOR UPDATE;
            IF predecessor.id IS NULL OR predecessor.sha256 <> p_predecessor_sha256
               OR predecessor.disposition <> 'continue' OR NOT predecessor.eligible
               OR p_artifact_json#>>'{body,predecessor_evidence_id}' <> p_predecessor_evidence_id::text
               OR decode(p_artifact_json#>>'{body,predecessor_evidence_sha256}', 'hex') <> p_predecessor_sha256 THEN
                RAISE EXCEPTION USING ERRCODE = '23514',
                    MESSAGE = 'canary outcome predecessor is invalid';
            END IF;
        END IF;
    ELSE
        RAISE EXCEPTION USING ERRCODE = '23514',
            MESSAGE = 'adaptive evidence kind is invalid';
    END IF;

    INSERT INTO aura.adaptive_sealed_evidence (
        id, kind, owner_id, cohort_id, cohort_sha256, domain, provider_id,
        model_id, policy_epoch, policy_version, snapshot_id, snapshot_sha256,
        look_number, look_cutoff, predecessor_evidence_id, predecessor_sha256,
        disposition, eligible, body_sha256, sha256, artifact, artifact_json,
        sealer_actor_id, sealer_capability
    ) VALUES (
        p_evidence_id, p_kind, p_owner_id, p_cohort_id, p_cohort_sha256, p_domain,
        p_provider_id, p_model_id, p_policy_epoch, p_policy_version, p_snapshot_id,
        p_snapshot_sha256, p_look_number, p_look_cutoff,
        p_predecessor_evidence_id, p_predecessor_sha256, p_disposition, p_eligible,
        p_body_sha256, p_sha256, p_artifact, p_artifact_json,
        p_actor_id, p_sealer_capability
    )
    ON CONFLICT (id) DO NOTHING;

    SELECT * INTO stored FROM aura.adaptive_sealed_evidence
    WHERE id = p_evidence_id AND owner_id = p_owner_id;
    IF stored.id IS NULL OR stored.sha256 <> p_sha256 OR stored.artifact <> p_artifact
       OR stored.kind <> p_kind OR stored.body_sha256 <> p_body_sha256 THEN
        RAISE EXCEPTION USING ERRCODE = '23505',
            MESSAGE = 'adaptive sealed evidence conflicts with persisted content';
    END IF;
    RETURN stored;
END;
$$;

DROP FUNCTION IF EXISTS aura.apply_adaptive_policy_transition(
    uuid, bigint, text, text, integer, jsonb,
    uuid, text, text, bytea, jsonb, text
);

CREATE FUNCTION aura.apply_adaptive_policy_transition(
    p_actor_id uuid,
    p_expected_epoch bigint,
    p_evidence_id uuid,
    p_rollout_bps integer,
    p_reason text
) RETURNS TABLE (
    epoch bigint,
    policy_version text,
    mode text,
    rollout_bps integer,
    config jsonb,
    updated_at timestamptz
)
    LANGUAGE plpgsql
    SECURITY DEFINER
    SET search_path = pg_catalog
AS $$
DECLARE
    state aura.adaptive_policy_state;
    evidence aura.adaptive_sealed_evidence;
    cohort aura.adaptive_focal_cohorts;
    target_mode text;
    binding jsonb;
    binding_bytes bytea;
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM aura.capability_grants
        WHERE identity_id = p_actor_id
          AND (capability = '*' OR capability = 'adaptive.manage')
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '42501',
            MESSAGE = 'adaptive.manage capability is required';
    END IF;
    IF btrim(COALESCE(p_reason, '')) = '' THEN
        RAISE EXCEPTION USING ERRCODE = '23514',
            MESSAGE = 'adaptive policy transition reason is required';
    END IF;

    SELECT * INTO state FROM aura.adaptive_policy_state
    WHERE scope = 'global' FOR UPDATE;
    IF state.epoch IS NULL OR state.epoch <> p_expected_epoch THEN
        RAISE EXCEPTION USING ERRCODE = '40001',
            MESSAGE = 'adaptive policy epoch changed';
    END IF;
    SELECT * INTO evidence FROM aura.adaptive_sealed_evidence
    WHERE id = p_evidence_id FOR UPDATE;
    IF evidence.id IS NULL OR NOT evidence.eligible
       OR evidence.sha256 <> pg_catalog.sha256(evidence.artifact)
       OR convert_from(evidence.artifact, 'UTF8')::jsonb <> evidence.artifact_json
       OR evidence.artifact_json->>'body_sha256' <> encode(evidence.body_sha256, 'hex')
       OR NOT EXISTS (
            SELECT 1 FROM aura.capability_grants
            WHERE identity_id = evidence.sealer_actor_id
              AND (capability = '*' OR capability = evidence.sealer_capability)
       ) THEN
        RAISE EXCEPTION USING ERRCODE = '23514',
            MESSAGE = 'adaptive sealed evidence is not transition-eligible';
    END IF;
    SELECT * INTO cohort FROM aura.adaptive_focal_cohorts
    WHERE id = evidence.cohort_id AND owner_id = evidence.owner_id FOR UPDATE;
    IF cohort.id IS NULL OR cohort.sha256 <> evidence.cohort_sha256
       OR cohort.domain <> evidence.domain OR cohort.provider_id <> evidence.provider_id
       OR cohort.model_id <> evidence.model_id OR cohort.policy_epoch <> evidence.policy_epoch
       OR cohort.policy_version <> evidence.policy_version
       OR cohort.snapshot_id <> evidence.snapshot_id
       OR cohort.snapshot_sha256 <> evidence.snapshot_sha256 THEN
        RAISE EXCEPTION USING ERRCODE = '23514',
            MESSAGE = 'adaptive sealed evidence scope is stale';
    END IF;

    IF evidence.kind = 'canary_admission' THEN
        target_mode := 'canary';
        IF state.mode <> 'shadow' OR state.epoch <> evidence.policy_epoch
           OR state.policy_version <> evidence.policy_version
           OR evidence.disposition IS NOT NULL
           OR evidence.look_number IS NOT NULL OR p_rollout_bps NOT BETWEEN 1 AND 9999
           OR evidence.artifact_json#>>'{body,target_state}' <> target_mode
           OR evidence.artifact_json#>>'{body,snapshot_ref,artifact_id}' <> evidence.snapshot_id::text
           OR evidence.artifact_json#>'{body,randomization_plan_ref}' <> cohort.artifact_json->'randomization_plan_ref'
           OR evidence.artifact_json#>'{body,interference_plan_ref}' <> cohort.artifact_json->'interference_plan_ref'
           OR evidence.artifact_json#>'{body,look_plan_ref}' <> cohort.artifact_json->'look_plan_ref'
           OR evidence.artifact_json#>'{body,quality_outcome_model_ref}' <> cohort.artifact_json->'quality_outcome_model_ref'
           OR evidence.artifact_json#>'{body,harm_outcome_model_ref}' <> cohort.artifact_json->'harm_outcome_model_ref'
           OR evidence.artifact_json#>'{body,ope_plan_ref}' <> cohort.artifact_json->'ope_plan_ref' THEN
            RAISE EXCEPTION USING ERRCODE = '23514',
                MESSAGE = 'admission evidence cannot authorize this transition';
        END IF;
    ELSIF evidence.kind = 'canary_outcome' THEN
        target_mode := 'active';
        IF state.mode <> 'canary' OR state.epoch <> evidence.policy_epoch + 1
           OR state.policy_version <> evidence.policy_version
           OR evidence.disposition <> 'promote'
           OR evidence.look_number IS NULL OR p_rollout_bps <> 10000
           OR (evidence.artifact_json#>>'{body,look_number}')::integer IS DISTINCT FROM evidence.look_number
           OR (evidence.artifact_json#>>'{body,look_cutoff}')::timestamptz IS DISTINCT FROM evidence.look_cutoff
           OR evidence.artifact_json#>>'{body,disposition}' IS DISTINCT FROM evidence.disposition
           OR (evidence.artifact_json#>>'{body,eligible}')::boolean IS DISTINCT FROM evidence.eligible
           OR (SELECT count(*) FROM aura.adaptive_evidence_artifacts AS report
               WHERE report.owner_id = evidence.owner_id AND report.cohort_id = evidence.cohort_id
                 AND report.id::text || ':' || encode(report.sha256, 'hex') = ANY (ARRAY[
                    (evidence.artifact_json#>>'{body,quality_report_id}') || ':' || (evidence.artifact_json#>>'{body,quality_report_sha256}'),
                    (evidence.artifact_json#>>'{body,harm_report_id}') || ':' || (evidence.artifact_json#>>'{body,harm_report_sha256}'),
                    (evidence.artifact_json#>>'{body,quality_ope_report_id}') || ':' || (evidence.artifact_json#>>'{body,quality_ope_report_sha256}'),
                    (evidence.artifact_json#>>'{body,harm_ope_report_id}') || ':' || (evidence.artifact_json#>>'{body,harm_ope_report_sha256}'),
                    (evidence.artifact_json#>>'{body,censor_diagnostics_id}') || ':' || (evidence.artifact_json#>>'{body,censor_diagnostics_sha256}')
                 ])) <> 5 THEN
            RAISE EXCEPTION USING ERRCODE = '23514',
                MESSAGE = 'outcome evidence cannot authorize this transition';
        END IF;
    ELSE
        RAISE EXCEPTION USING ERRCODE = '23514',
            MESSAGE = 'legacy evidence is transition-ineligible';
    END IF;

    binding := jsonb_build_object(
        'schema_id', 'aura.adaptive.transition-binding/v1',
        'revision', 1,
        'from_state', state.mode,
        'target_state', target_mode,
        'evidence_kind', evidence.kind,
        'evidence_id', evidence.id,
        'evidence_sha256', encode(evidence.sha256, 'hex'),
        'owner_id', evidence.owner_id,
        'domain', evidence.domain,
        'provider_id', evidence.provider_id,
        'model_id', evidence.model_id,
        'policy_epoch', evidence.policy_epoch,
        'policy_version', evidence.policy_version,
        'snapshot_id', evidence.snapshot_id,
        'snapshot_sha256', encode(evidence.snapshot_sha256, 'hex'),
        'cohort_id', evidence.cohort_id,
        'cohort_sha256', encode(evidence.cohort_sha256, 'hex'),
        'look_number', evidence.look_number,
        'look_cutoff', evidence.look_cutoff,
        'predecessor_evidence_id', evidence.predecessor_evidence_id,
        'predecessor_sha256', encode(evidence.predecessor_sha256, 'hex'),
        'disposition', evidence.disposition,
        'eligible', evidence.eligible,
        'quality_report_sha256', evidence.artifact_json#>>'{body,quality_report_sha256}',
        'harm_report_sha256', evidence.artifact_json#>>'{body,harm_report_sha256}',
        'quality_ope_report_sha256', evidence.artifact_json#>>'{body,quality_ope_report_sha256}',
        'harm_ope_report_sha256', evidence.artifact_json#>>'{body,harm_ope_report_sha256}',
        'censor_diagnostics_sha256', evidence.artifact_json#>>'{body,censor_diagnostics_sha256}',
        'actor_id', p_actor_id,
        'capability', 'adaptive.manage'
    );
    binding_bytes := convert_to(binding::text, 'UTF8');

    UPDATE aura.adaptive_policy_state AS policy
    SET epoch = policy.epoch + 1,
        policy_version = evidence.policy_version,
        mode = target_mode,
        rollout_bps = p_rollout_bps,
        config = jsonb_build_object(
            'owner_id', evidence.owner_id,
            'domain', evidence.domain,
            'provider_id', evidence.provider_id,
            'model_id', evidence.model_id,
            'snapshot_id', evidence.snapshot_id,
            'snapshot_sha256', encode(evidence.snapshot_sha256, 'hex'),
            'cohort_id', evidence.cohort_id,
            'cohort_sha256', encode(evidence.cohort_sha256, 'hex')
        ),
        updated_at = statement_timestamp()
    WHERE scope = 'global';

    INSERT INTO aura.adaptive_policy_transitions (
        id, actor_id, from_epoch, to_epoch, from_mode, to_mode,
        policy_version, rollout_bps, evidence_id, evidence_kind, evidence_hash,
        transition_binding, transition_binding_json, reason
    ) VALUES (
        gen_random_uuid(), p_actor_id, state.epoch, state.epoch + 1,
        state.mode, target_mode, evidence.policy_version, p_rollout_bps,
        evidence.id, evidence.kind, evidence.sha256,
        binding_bytes, binding, p_reason
    );

    RETURN QUERY SELECT s.epoch, s.policy_version, s.mode, s.rollout_bps,
        s.config, s.updated_at
    FROM aura.adaptive_policy_state AS s WHERE s.scope = 'global';
END;
$$;

REVOKE ALL ON aura.adaptive_evidence_artifacts FROM aura_app;
REVOKE ALL ON aura.adaptive_sealed_evidence FROM aura_app;
GRANT SELECT ON aura.adaptive_evidence_artifacts TO aura_app;
GRANT SELECT ON aura.adaptive_sealed_evidence TO aura_app;

REVOKE ALL ON FUNCTION aura.reject_adaptive_sealed_evidence_mutation() FROM PUBLIC;
REVOKE ALL ON FUNCTION aura.store_adaptive_evidence_artifact(
    uuid, uuid, uuid, text, uuid, text, integer, bytea, bytea, jsonb
) FROM PUBLIC;
REVOKE ALL ON FUNCTION aura.seal_adaptive_evidence(
    uuid, text, uuid, uuid, uuid, bytea, text, text, text, bigint, text,
    uuid, bytea, integer, timestamptz, uuid, bytea, text, boolean,
    bytea, bytea, bytea, jsonb, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION aura.apply_adaptive_policy_transition(
    uuid, bigint, uuid, integer, text
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION aura.store_adaptive_evidence_artifact(
    uuid, uuid, uuid, text, uuid, text, integer, bytea, bytea, jsonb
) TO aura_app;
GRANT EXECUTE ON FUNCTION aura.seal_adaptive_evidence(
    uuid, text, uuid, uuid, uuid, bytea, text, text, text, bigint, text,
    uuid, bytea, integer, timestamptz, uuid, bytea, text, boolean,
    bytea, bytea, bytea, jsonb, text
) TO aura_app;
GRANT EXECUTE ON FUNCTION aura.apply_adaptive_policy_transition(
    uuid, bigint, uuid, integer, text
) TO aura_app;
