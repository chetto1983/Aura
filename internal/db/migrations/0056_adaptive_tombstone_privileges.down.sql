REVOKE EXECUTE ON FUNCTION aura.fence_adaptive_identity(uuid) FROM aura_app;
DROP FUNCTION IF EXISTS aura.fence_adaptive_identity(uuid);

ALTER TABLE aura.adaptive_identity_tombstones ADD COLUMN identity_name text;
UPDATE aura.adaptive_identity_tombstones SET identity_name = '<redacted>';
ALTER TABLE aura.adaptive_identity_tombstones ALTER COLUMN identity_name SET NOT NULL;

CREATE OR REPLACE FUNCTION aura.tombstone_adaptive_identity() RETURNS trigger
    LANGUAGE plpgsql
    SECURITY INVOKER
AS $$
BEGIN
    INSERT INTO aura.adaptive_identity_tombstones (owner_id, identity_name, deleted_at)
    VALUES (OLD.id, OLD.name, now())
    ON CONFLICT (owner_id) DO UPDATE
        SET identity_name = EXCLUDED.identity_name,
            deleted_at = GREATEST(
                aura.adaptive_identity_tombstones.deleted_at,
                EXCLUDED.deleted_at
            );
    RETURN OLD;
END;
$$;
GRANT EXECUTE ON FUNCTION aura.tombstone_adaptive_identity() TO PUBLIC;

GRANT SELECT, INSERT, UPDATE, DELETE
    ON aura.adaptive_identity_tombstones TO aura_app;
