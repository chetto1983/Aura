-- Source: PRD Phase 36 MUSR-06 + 36-CONTEXT.md D-25 (local seeded with admin
-- caps) + 36-RESEARCH.md OQ3 (reuse the EXISTING `governance.write` for the
-- D-02/D-03 model-settings capability rather than a net-new `settings.model.write`).
-- The migration floor before this is 0025 (document_control_plane); the local
-- admin-caps seed lands at 0026.
--
-- Seeds the `local` identity (fixed UUID 00000000-0000-0000-0000-000000000001,
-- seeded by 0004) with its EXPLICIT admin capability grants so the admin-gated
-- routes resolve on named capabilities, not solely on the system-managed `*`
-- wildcard. `local` already holds `*` (0004); these explicit rows make the admin
-- contract legible and survive any future narrowing of the wildcard.
--
-- Seed only — NO schema change, GRANTs unchanged. Every row is idempotent
-- (ON CONFLICT DO NOTHING), mirroring the 0004_identity.up.sql seed shape exactly.
--
-- RETIRE-SAFE (guarded by WHERE EXISTS): the Phase 36 authula cutover
-- (cmd/aura/serve_auth.go "retire legacy local identity") migrates the local
-- identity's refs to the first real user and DELETES the local row. If this
-- migration is applied to a DB that already retired `local`, a bare INSERT would
-- FK-violate on capability_grants.identity_id and dirty the migration tracker,
-- blocking every later migration (the exact break this guard prevents). When
-- `local` is gone its caps have already been migrated onto the real user by the
-- retire flow, so seeding here is correctly a no-op — never an error.

INSERT INTO aura.capability_grants (identity_id, capability)
    SELECT '00000000-0000-0000-0000-000000000001'::uuid, cap
    FROM (VALUES ('governance.write'), ('identity.create'), ('agent.run')) AS caps(cap)
    WHERE EXISTS (
        SELECT 1 FROM aura.identities WHERE id = '00000000-0000-0000-0000-000000000001'
    )
    ON CONFLICT DO NOTHING;
