-- Down for 0121_retire_capability_wildcard. Restores a single '*' row for
-- each identity that holds the full six-name explicit set left by the up
-- migration, then deletes those six rows for exactly those identities.
--
-- Irreversible in general (recorded in 02-01-PLAN.md's checkpoint): this
-- only recovers identities the up migration itself rewrote, in the shape it
-- left them — it cannot restore a *different* set an operator held before
-- 0121, because that information was never recorded anywhere.

INSERT INTO aura.capability_grants (identity_id, capability)
    SELECT identity_id, '*'
    FROM aura.capability_grants
    WHERE capability IN (
        'identity.create', 'identity.delete', 'agent.run',
        'governance.read', 'governance.write', 'share.public'
    )
    GROUP BY identity_id
    HAVING count(DISTINCT capability) = 6
    ON CONFLICT (identity_id, capability) DO NOTHING;

DELETE FROM aura.capability_grants
    WHERE capability IN (
        'identity.create', 'identity.delete', 'agent.run',
        'governance.read', 'governance.write', 'share.public'
    )
    AND identity_id IN (
        SELECT identity_id FROM aura.capability_grants WHERE capability = '*'
    );

COMMENT ON TABLE aura.capability_grants IS
    'Per-identity capability grants. Wildcard `*` is system-managed (seeded, never grant/revoke via CLI).';
