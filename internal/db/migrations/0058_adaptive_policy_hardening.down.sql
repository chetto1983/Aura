REVOKE EXECUTE ON FUNCTION aura.apply_adaptive_policy_transition(
    uuid, bigint, text, text, integer, jsonb,
    uuid, text, text, bytea, jsonb, text
) FROM aura_app;
DROP FUNCTION IF EXISTS aura.apply_adaptive_policy_transition(
    uuid, bigint, text, text, integer, jsonb,
    uuid, text, text, bytea, jsonb, text
);

GRANT SELECT, UPDATE ON aura.adaptive_policy_state TO aura_app;
GRANT SELECT, INSERT ON aura.adaptive_promotion_evidence TO aura_app;
GRANT SELECT, INSERT ON aura.adaptive_policy_transitions TO aura_app;

ALTER TABLE aura.adaptive_promotion_evidence
    ADD CONSTRAINT adaptive_promotion_evidence_actor_id_fkey
    FOREIGN KEY (actor_id) REFERENCES aura.identities(id) ON DELETE RESTRICT
    NOT VALID;
ALTER TABLE aura.adaptive_policy_transitions
    ADD CONSTRAINT adaptive_policy_transitions_actor_id_fkey
    FOREIGN KEY (actor_id) REFERENCES aura.identities(id) ON DELETE RESTRICT
    NOT VALID;
