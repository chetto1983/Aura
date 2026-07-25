DROP TRIGGER IF EXISTS adaptive_randomization_receipts_immutable
    ON aura.adaptive_randomization_receipts;
DROP TRIGGER IF EXISTS adaptive_randomization_receipts_binding
    ON aura.adaptive_randomization_receipts;
DROP TABLE IF EXISTS aura.adaptive_randomization_receipts;
DROP FUNCTION IF EXISTS aura.reject_adaptive_randomization_receipt_mutation();
DROP FUNCTION IF EXISTS aura.enforce_adaptive_randomization_receipt_binding();
DROP FUNCTION IF EXISTS aura.adaptive_randomization_receipt_artifact_valid(
    bytea, jsonb, uuid, uuid, uuid, uuid, uuid, bytea, bytea, bytea
);

ALTER TABLE aura.adaptive_outbox
    DROP CONSTRAINT adaptive_outbox_schema2_assignment_check,
    ADD CONSTRAINT adaptive_outbox_schema2_assignment_check CHECK (
        payload->>'schema_version' <> '2.0'
        OR event_kind <> 'decision'
        OR aura.adaptive_schema2_assignment_payload_valid(
            payload, owner_id, aggregate_id, decision_id
        )
    );

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
    IF NOT aura.adaptive_schema2_assignment_payload_valid(
        p_payload, p_owner_id, p_aggregate_id, p_decision_id
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

DROP FUNCTION IF EXISTS aura.adaptive_schema2_randomized_assignment_payload_valid(
    jsonb, uuid, text, uuid
);

DROP INDEX IF EXISTS aura.adaptive_focal_claims_receipt_scope_uidx;

CREATE OR REPLACE FUNCTION aura.enforce_adaptive_focal_claim_cutoff_and_blocks()
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

ALTER TABLE aura.adaptive_focal_cohort_claims
    DROP CONSTRAINT IF EXISTS adaptive_focal_claims_v2_hashes_check,
    DROP COLUMN IF EXISTS interference_cluster_id,
    DROP COLUMN IF EXISTS interference_cluster_schema_sha256,
    DROP COLUMN IF EXISTS analysis_stratum_id,
    DROP COLUMN IF EXISTS analysis_stratum_schema_sha256;

ALTER TABLE aura.adaptive_focal_cohorts
    DROP CONSTRAINT adaptive_focal_cohorts_artifact_scope_check,
    DROP COLUMN IF EXISTS randomization_plan_artifact_json,
    DROP COLUMN IF EXISTS randomization_plan_artifact,
    ADD CONSTRAINT adaptive_focal_cohorts_artifact_scope_check CHECK (
        aura.adaptive_focal_cohort_artifact_valid(
            artifact, artifact_json, owner_id, provider_id, model_id,
            policy_epoch, policy_version, snapshot_id, snapshot_sha256,
            environment, domain, decision_point, point_ordinal,
            predicate_sha256, experiment_id, cutoff
        )
    );

DROP FUNCTION IF EXISTS aura.adaptive_focal_cohort_v2_artifact_valid(
    bytea, jsonb, bytea, jsonb, uuid, text, text, bigint, text, uuid,
    bytea, text, text, text, bigint, bytea, text, timestamptz
);
DROP FUNCTION IF EXISTS aura.adaptive_child_artifact_ref_valid(
    jsonb, text, text
);
