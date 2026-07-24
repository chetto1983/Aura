-- Policy state, evidence, and audit mutate only through one capability-checked
-- SECURITY DEFINER boundary. Actor UUIDs are immutable audit snapshots, not
-- lifecycle-blocking foreign keys.
ALTER TABLE aura.adaptive_promotion_evidence
    DROP CONSTRAINT adaptive_promotion_evidence_actor_id_fkey;
ALTER TABLE aura.adaptive_policy_transitions
    DROP CONSTRAINT adaptive_policy_transitions_actor_id_fkey;

COMMENT ON COLUMN aura.adaptive_promotion_evidence.actor_id IS
    'Immutable actor UUID snapshot; intentionally no FK so identity deletion cannot erase or block audit.';
COMMENT ON COLUMN aura.adaptive_policy_transitions.actor_id IS
    'Immutable actor UUID snapshot; intentionally no FK so identity deletion cannot erase or block audit.';

CREATE FUNCTION aura.apply_adaptive_policy_transition(
    p_actor_id uuid,
    p_expected_epoch bigint,
    p_policy_version text,
    p_mode text,
    p_rollout_bps integer,
    p_config jsonb,
    p_evidence_id uuid,
    p_model_id text,
    p_cohort_id text,
    p_evidence_hash bytea,
    p_report jsonb,
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
    current_epoch bigint;
    current_mode text;
    current_rollout integer;
    evidence_required boolean;
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM aura.capability_grants
        WHERE identity_id = p_actor_id
          AND (capability = '*' OR capability = 'adaptive.manage')
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '42501',
            MESSAGE = 'adaptive.manage capability is required';
    END IF;
    IF btrim(COALESCE(p_reason, '')) = '' THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'adaptive policy transition reason is required';
    END IF;

    SELECT s.epoch, s.mode, s.rollout_bps
    INTO current_epoch, current_mode, current_rollout
    FROM aura.adaptive_policy_state AS s
    WHERE s.scope = 'global'
    FOR UPDATE;
    IF current_epoch IS NULL THEN
        RAISE EXCEPTION USING
            ERRCODE = 'P0001',
            MESSAGE = 'adaptive global policy row is missing';
    END IF;
    IF current_epoch <> p_expected_epoch THEN
        RAISE EXCEPTION USING
            ERRCODE = '40001',
            MESSAGE = 'adaptive policy epoch changed';
    END IF;

    evidence_required := p_mode IN ('canary', 'active');
    IF evidence_required THEN
        IF p_evidence_id IS NULL
           OR btrim(COALESCE(p_model_id, '')) = ''
           OR btrim(COALESCE(p_cohort_id, '')) = ''
           OR p_evidence_hash IS NULL
           OR octet_length(p_evidence_hash) <> 32
           OR p_report IS NULL
           OR jsonb_typeof(p_report) <> 'object' THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE = 'canary/active transition requires complete promotion evidence';
        END IF;
    ELSIF p_evidence_id IS NOT NULL
       OR p_model_id IS NOT NULL
       OR p_cohort_id IS NOT NULL
       OR p_evidence_hash IS NOT NULL
       OR p_report IS NOT NULL THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'non-promoting transition cannot attach promotion evidence';
    END IF;

    IF p_mode IN ('off', 'rollback') THEN
        NULL;
    ELSIF p_mode = 'shadow' THEN
        IF current_mode NOT IN ('off', 'rollback', 'shadow') THEN
            RAISE EXCEPTION USING ERRCODE = '23514',
                MESSAGE = 'illegal adaptive policy transition';
        END IF;
    ELSIF p_mode = 'canary' THEN
        IF current_mode NOT IN ('shadow', 'canary')
           OR (current_mode = 'canary' AND p_rollout_bps <= current_rollout) THEN
            RAISE EXCEPTION USING ERRCODE = '23514',
                MESSAGE = 'illegal adaptive policy transition';
        END IF;
    ELSIF p_mode = 'active' THEN
        IF current_mode <> 'canary' THEN
            RAISE EXCEPTION USING ERRCODE = '23514',
                MESSAGE = 'illegal adaptive policy transition';
        END IF;
    ELSE
        RAISE EXCEPTION USING ERRCODE = '23514',
            MESSAGE = 'unknown adaptive policy mode';
    END IF;

    IF evidence_required THEN
        INSERT INTO aura.adaptive_promotion_evidence (
            id, actor_id, policy_version, model_id, cohort_id,
            evidence_hash, report
        ) VALUES (
            p_evidence_id, p_actor_id, p_policy_version, p_model_id,
            p_cohort_id, p_evidence_hash, p_report
        );
    END IF;

    UPDATE aura.adaptive_policy_state AS s
    SET epoch = s.epoch + 1,
        policy_version = p_policy_version,
        mode = p_mode,
        rollout_bps = p_rollout_bps,
        config = COALESCE(p_config, '{}'::jsonb),
        updated_at = now()
    WHERE s.scope = 'global';

    INSERT INTO aura.adaptive_policy_transitions (
        id, actor_id, from_epoch, to_epoch, from_mode, to_mode,
        policy_version, rollout_bps, evidence_id, reason
    ) VALUES (
        gen_random_uuid(), p_actor_id, current_epoch, current_epoch + 1,
        current_mode, p_mode, p_policy_version, p_rollout_bps,
        p_evidence_id, p_reason
    );

    RETURN QUERY
    SELECT s.epoch, s.policy_version, s.mode, s.rollout_bps,
           s.config, s.updated_at
    FROM aura.adaptive_policy_state AS s
    WHERE s.scope = 'global';
END;
$$;

REVOKE INSERT, UPDATE, DELETE ON aura.adaptive_policy_state FROM aura_app;
REVOKE INSERT, UPDATE, DELETE ON aura.adaptive_promotion_evidence FROM aura_app;
REVOKE INSERT, UPDATE, DELETE ON aura.adaptive_policy_transitions FROM aura_app;

REVOKE ALL ON FUNCTION aura.apply_adaptive_policy_transition(
    uuid, bigint, text, text, integer, jsonb,
    uuid, text, text, bytea, jsonb, text
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION aura.apply_adaptive_policy_transition(
    uuid, bigint, text, text, integer, jsonb,
    uuid, text, text, bytea, jsonb, text
) TO aura_app;

COMMENT ON FUNCTION aura.apply_adaptive_policy_transition(
    uuid, bigint, text, text, integer, jsonb,
    uuid, text, text, bytea, jsonb, text
) IS 'Only runtime mutation boundary for adaptive policy/evidence/audit state.';
