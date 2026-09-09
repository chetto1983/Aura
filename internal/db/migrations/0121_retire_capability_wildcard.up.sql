-- Source: PRD Phase 45+ Slice 3 (RBAC-01, D-04) — retire the capability
-- wildcard. Migration floor before this is 0120 (worker_steer_scopes); this
-- lands at 0121, measured from `ls internal/db/migrations/ | tail -1` at
-- landing time, never copied from planning docs.
--
-- One transaction (golang-migrate wraps each .sql file): insert the six
-- explicit rows for every identity currently holding '*' (ON CONFLICT DO
-- NOTHING, so the `local` identity's already-explicit rows from 0026 are not
-- duplicated), THEN delete the '*' rows. Insert-before-delete so an operator
-- can never observe a state where an identity holds neither the wildcard nor
-- the explicit set (T-02-12 DoS mitigation).
--
-- Irreversible half (recorded in 02-01-PLAN.md's checkpoint, approved before
-- this file was written): the rewrite cannot know which of the six an
-- operator was MEANT to hold, because the wildcard never recorded that.
-- Every rewritten identity gets all six.

INSERT INTO aura.capability_grants (identity_id, capability)
    SELECT g.identity_id, caps.cap
    FROM aura.capability_grants g
    CROSS JOIN (VALUES
        ('identity.create'),
        ('identity.delete'),
        ('agent.run'),
        ('governance.read'),
        ('governance.write'),
        ('share.public')
    ) AS caps(cap)
    WHERE g.capability = '*'
    ON CONFLICT (identity_id, capability) DO NOTHING;

DELETE FROM aura.capability_grants WHERE capability = '*';

COMMENT ON TABLE aura.capability_grants IS
    'Per-identity capability grants. The `*` wildcard is retired as of 0121 (RBAC-01) — HasCapability matches only the exact capability name.';
