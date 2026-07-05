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

INSERT INTO aura.capability_grants (identity_id, capability)
    VALUES
        ('00000000-0000-0000-0000-000000000001', 'governance.write'),
        ('00000000-0000-0000-0000-000000000001', 'identity.create'),
        ('00000000-0000-0000-0000-000000000001', 'agent.run')
    ON CONFLICT DO NOTHING;
