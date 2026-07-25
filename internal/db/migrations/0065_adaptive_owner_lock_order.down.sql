CREATE OR REPLACE FUNCTION aura.fence_adaptive_identity(p_owner_id uuid) RETURNS boolean
    LANGUAGE plpgsql
    SECURITY DEFINER
    SET search_path = pg_catalog
AS $$
DECLARE
    fenced boolean := false;
BEGIN
    INSERT INTO aura.adaptive_identity_tombstones (owner_id, deleted_at)
    SELECT id, now()
    FROM aura.identities
    WHERE id = p_owner_id
    ON CONFLICT (owner_id) DO UPDATE
        SET deleted_at = GREATEST(
            aura.adaptive_identity_tombstones.deleted_at,
            EXCLUDED.deleted_at
        )
    RETURNING true INTO fenced;
    RETURN COALESCE(fenced, false);
END;
$$;
