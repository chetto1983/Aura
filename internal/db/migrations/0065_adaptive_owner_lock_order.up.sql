CREATE OR REPLACE FUNCTION aura.fence_adaptive_identity(p_owner_id uuid) RETURNS boolean
    LANGUAGE plpgsql
    SECURITY DEFINER
    SET search_path = pg_catalog
AS $$
DECLARE
    locked_owner_id uuid;
BEGIN
    SELECT id INTO locked_owner_id
    FROM aura.identities
    WHERE id = p_owner_id
    FOR UPDATE;
    IF NOT FOUND THEN
        RETURN false;
    END IF;

    INSERT INTO aura.adaptive_identity_tombstones (owner_id, deleted_at)
    VALUES (locked_owner_id, now())
    ON CONFLICT (owner_id) DO UPDATE
        SET deleted_at = GREATEST(
            aura.adaptive_identity_tombstones.deleted_at,
            EXCLUDED.deleted_at
        );
    RETURN true;
END;
$$;
