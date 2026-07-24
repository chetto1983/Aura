-- Make UUID-only adaptive identity tombstones permanent for the runtime role.
--
-- aura_app may read the deny-list and request a fence through the narrow
-- SECURITY DEFINER function, but it cannot insert, rewrite, move, delete, or
-- truncate tombstone rows directly.
CREATE OR REPLACE FUNCTION aura.tombstone_adaptive_identity() RETURNS trigger
    LANGUAGE plpgsql
    SECURITY DEFINER
    SET search_path = pg_catalog
AS $$
BEGIN
    INSERT INTO aura.adaptive_identity_tombstones (owner_id, deleted_at)
    VALUES (OLD.id, now())
    ON CONFLICT (owner_id) DO UPDATE
        SET deleted_at = GREATEST(
            aura.adaptive_identity_tombstones.deleted_at,
            EXCLUDED.deleted_at
        );
    RETURN OLD;
END;
$$;

-- The UUID is the deny-list key. Retaining a name/email forever is unnecessary
-- personal data and is not used by any fence check.
ALTER TABLE aura.adaptive_identity_tombstones DROP COLUMN identity_name;

CREATE FUNCTION aura.fence_adaptive_identity(p_owner_id uuid) RETURNS boolean
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

REVOKE INSERT, UPDATE, DELETE, TRUNCATE
    ON aura.adaptive_identity_tombstones FROM aura_app;
REVOKE ALL ON FUNCTION aura.fence_adaptive_identity(uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION aura.tombstone_adaptive_identity() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION aura.fence_adaptive_identity(uuid) TO aura_app;

COMMENT ON FUNCTION aura.fence_adaptive_identity(uuid) IS
    'Narrow runtime boundary that permanently fences an existing identity before graph purge.';
COMMENT ON TABLE aura.adaptive_identity_tombstones IS
    'Permanent UUID-only deny-list for deleted identities; contains no retained identity name.';
