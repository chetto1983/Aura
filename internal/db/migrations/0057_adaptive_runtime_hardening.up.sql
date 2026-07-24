-- Adaptive facts are insert-only. Delivery metadata has a narrow update grant,
-- and aggregate sequence allocation crosses a scoped SECURITY DEFINER boundary.
CREATE FUNCTION aura.next_adaptive_aggregate_sequence(
    p_owner_id uuid,
    p_aggregate_id text
) RETURNS bigint
    LANGUAGE plpgsql
    SECURITY DEFINER
    SET search_path = pg_catalog
AS $$
DECLARE
    next_value bigint;
    scoped_identity text;
BEGIN
    scoped_identity := NULLIF(current_setting('app.current_identity', true), '');
    IF scoped_identity IS NULL OR scoped_identity::uuid <> p_owner_id THEN
        RAISE EXCEPTION USING
            ERRCODE = '42501',
            MESSAGE = 'adaptive sequence owner does not match transaction identity';
    END IF;
    INSERT INTO aura.adaptive_aggregate_sequences (
        owner_id, aggregate_id, next_sequence
    ) VALUES (
        p_owner_id, p_aggregate_id, 2
    )
    ON CONFLICT (owner_id, aggregate_id)
    DO UPDATE SET next_sequence =
        aura.adaptive_aggregate_sequences.next_sequence + 1
    RETURNING next_sequence - 1 INTO next_value;
    RETURN next_value;
END;
$$;

CREATE FUNCTION aura.reject_adaptive_outbox_fact_update() RETURNS trigger
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

CREATE TRIGGER adaptive_outbox_fact_columns_immutable
    BEFORE UPDATE ON aura.adaptive_outbox
    FOR EACH ROW EXECUTE FUNCTION aura.reject_adaptive_outbox_fact_update();

REVOKE INSERT, UPDATE, DELETE ON aura.adaptive_aggregate_sequences FROM aura_app;
REVOKE DELETE ON aura.adaptive_outbox FROM aura_app;
REVOKE UPDATE ON aura.adaptive_outbox FROM aura_app;
GRANT UPDATE (
    status, attempts, available_at, lease_owner, lease_expires_at,
    projected_at, dead_letter_at, last_error_class
) ON aura.adaptive_outbox TO aura_app;

REVOKE ALL ON FUNCTION aura.next_adaptive_aggregate_sequence(uuid, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION aura.next_adaptive_aggregate_sequence(uuid, text) TO aura_app;
