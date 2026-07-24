-- Durable, append-only evidence and audit records for every adaptive policy
-- transition. Application code evaluates the closed cohort before entering the
-- transaction; the transition, evidence, and actor audit commit together.
CREATE TABLE aura.adaptive_promotion_evidence (
    id              uuid PRIMARY KEY,
    actor_id        uuid NOT NULL REFERENCES aura.identities(id) ON DELETE RESTRICT,
    policy_version  text NOT NULL CHECK (btrim(policy_version) <> ''),
    model_id        text NOT NULL CHECK (btrim(model_id) <> ''),
    cohort_id       text NOT NULL CHECK (btrim(cohort_id) <> ''),
    evidence_hash   bytea NOT NULL CHECK (octet_length(evidence_hash) = 32),
    report          jsonb NOT NULL CHECK (jsonb_typeof(report) = 'object'),
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (policy_version, cohort_id, evidence_hash)
);

CREATE TABLE aura.adaptive_policy_transitions (
    id              uuid PRIMARY KEY,
    actor_id        uuid NOT NULL REFERENCES aura.identities(id) ON DELETE RESTRICT,
    from_epoch      bigint NOT NULL,
    to_epoch        bigint NOT NULL CHECK (to_epoch = from_epoch + 1),
    from_mode       text NOT NULL,
    to_mode         text NOT NULL,
    policy_version  text NOT NULL,
    rollout_bps     integer NOT NULL,
    evidence_id     uuid REFERENCES aura.adaptive_promotion_evidence(id) ON DELETE RESTRICT,
    reason          text NOT NULL CHECK (btrim(reason) <> ''),
    created_at      timestamptz NOT NULL DEFAULT now(),
    CHECK (
        (to_mode IN ('canary', 'active') AND evidence_id IS NOT NULL)
        OR (to_mode NOT IN ('canary', 'active') AND evidence_id IS NULL)
    )
);

CREATE FUNCTION aura.reject_adaptive_policy_audit_mutation() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'adaptive policy evidence/audit is append-only: % is not permitted', TG_OP;
END;
$$;

CREATE TRIGGER adaptive_promotion_evidence_no_update_delete
    BEFORE UPDATE OR DELETE ON aura.adaptive_promotion_evidence
    FOR EACH ROW EXECUTE FUNCTION aura.reject_adaptive_policy_audit_mutation();
CREATE TRIGGER adaptive_policy_transitions_no_update_delete
    BEFORE UPDATE OR DELETE ON aura.adaptive_policy_transitions
    FOR EACH ROW EXECUTE FUNCTION aura.reject_adaptive_policy_audit_mutation();
CREATE TRIGGER adaptive_promotion_evidence_no_truncate
    BEFORE TRUNCATE ON aura.adaptive_promotion_evidence
    FOR EACH STATEMENT EXECUTE FUNCTION aura.reject_adaptive_policy_audit_mutation();
CREATE TRIGGER adaptive_policy_transitions_no_truncate
    BEFORE TRUNCATE ON aura.adaptive_policy_transitions
    FOR EACH STATEMENT EXECUTE FUNCTION aura.reject_adaptive_policy_audit_mutation();

GRANT SELECT, INSERT ON aura.adaptive_promotion_evidence TO aura_app;
GRANT SELECT, INSERT ON aura.adaptive_policy_transitions TO aura_app;

COMMENT ON TABLE aura.adaptive_promotion_evidence IS
    'Immutable calibrated closed-cohort report bound transactionally to a canary/active transition.';
COMMENT ON TABLE aura.adaptive_policy_transitions IS
    'Append-only actor/capability-audited adaptive policy state transition ledger.';
