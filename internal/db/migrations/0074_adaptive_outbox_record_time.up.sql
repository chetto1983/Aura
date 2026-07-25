ALTER TABLE aura.adaptive_outbox
    ADD COLUMN recorded_at timestamptz;

CREATE FUNCTION aura.set_adaptive_outbox_recorded_at() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog
AS $$
BEGIN
    NEW.recorded_at := statement_timestamp();
    RETURN NEW;
END;
$$;

CREATE TRIGGER adaptive_outbox_set_recorded_at
    BEFORE INSERT ON aura.adaptive_outbox
    FOR EACH ROW EXECUTE FUNCTION aura.set_adaptive_outbox_recorded_at();

REVOKE ALL ON FUNCTION aura.set_adaptive_outbox_recorded_at() FROM PUBLIC;

CREATE OR REPLACE FUNCTION aura.reject_adaptive_outbox_fact_update() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog
AS $$
BEGIN
    IF ROW(
        NEW.id, NEW.owner_id, NEW.aggregate_id, NEW.sequence, NEW.decision_id,
        NEW.event_kind, NEW.payload, NEW.payload_hash, NEW.created_at, NEW.recorded_at
    ) IS DISTINCT FROM ROW(
        OLD.id, OLD.owner_id, OLD.aggregate_id, OLD.sequence, OLD.decision_id,
        OLD.event_kind, OLD.payload, OLD.payload_hash, OLD.created_at, OLD.recorded_at
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'adaptive event fact columns are immutable';
    END IF;
    RETURN NEW;
END;
$$;
