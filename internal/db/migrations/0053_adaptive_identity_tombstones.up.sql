-- A deleted identity UUID must never silently regain its historical adaptive
-- policy if an external identity provider or restore later reuses that UUID.
-- Tombstones intentionally have no FK and therefore survive identity deletion.
CREATE TABLE aura.adaptive_identity_tombstones (
    owner_id       uuid PRIMARY KEY,
    identity_name  text NOT NULL,
    deleted_at     timestamptz NOT NULL DEFAULT now()
);

CREATE FUNCTION aura.tombstone_adaptive_identity() RETURNS trigger
    LANGUAGE plpgsql AS $$
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

CREATE TRIGGER identities_adaptive_tombstone_trg
    AFTER DELETE ON aura.identities
    FOR EACH ROW EXECUTE FUNCTION aura.tombstone_adaptive_identity();

GRANT SELECT, INSERT, UPDATE, DELETE ON aura.adaptive_identity_tombstones TO aura_app;
GRANT EXECUTE ON FUNCTION aura.tombstone_adaptive_identity() TO aura_app;

COMMENT ON TABLE aura.adaptive_identity_tombstones IS
    'Permanent deny-list for deleted identity UUIDs; prevents adaptive-policy resurrection after UUID reuse.';
