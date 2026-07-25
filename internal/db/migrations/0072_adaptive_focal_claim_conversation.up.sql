CREATE FUNCTION aura.enforce_adaptive_focal_claim_conversation()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog
AS $$
BEGIN
    PERFORM 1
    FROM aura.conversations
    WHERE id = NEW.evaluation_conversation_id
      AND identity_id = NEW.owner_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING
            ERRCODE = '23503',
            MESSAGE = 'adaptive focal claim conversation does not belong to owner';
    END IF;
    RETURN NEW;
END;
$$;

REVOKE ALL ON FUNCTION aura.enforce_adaptive_focal_claim_conversation() FROM PUBLIC;

CREATE TRIGGER adaptive_focal_cohort_claims_conversation
    BEFORE INSERT ON aura.adaptive_focal_cohort_claims
    FOR EACH ROW
    EXECUTE FUNCTION aura.enforce_adaptive_focal_claim_conversation();
