DROP TRIGGER IF EXISTS adaptive_outbox_set_recorded_at ON aura.adaptive_outbox;
DROP FUNCTION IF EXISTS aura.set_adaptive_outbox_recorded_at();

CREATE OR REPLACE FUNCTION aura.reject_adaptive_outbox_fact_update() RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog
AS $$
BEGIN
    IF ROW(
        NEW.id, NEW.owner_id, NEW.aggregate_id, NEW.sequence, NEW.decision_id,
        NEW.event_kind, NEW.payload, NEW.payload_hash, NEW.created_at
    ) IS DISTINCT FROM ROW(
        OLD.id, OLD.owner_id, OLD.aggregate_id, OLD.sequence, OLD.decision_id,
        OLD.event_kind, OLD.payload, OLD.payload_hash, OLD.created_at
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE = 'adaptive event fact columns are immutable';
    END IF;
    RETURN NEW;
END;
$$;

ALTER TABLE aura.adaptive_outbox
    DROP COLUMN recorded_at;
