CREATE UNIQUE INDEX adaptive_focal_cohort_claims_owner_conversation_uidx
    ON aura.adaptive_focal_cohort_claims
    (owner_id, evaluation_conversation_id);
