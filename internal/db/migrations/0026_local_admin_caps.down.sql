-- Reverse ONLY the three explicit admin caps seeded for `local` by 0026.up.
-- The system-managed `*` wildcard (seeded by 0004) is intentionally left intact —
-- it is never CLI-managed and predates this migration.

DELETE FROM aura.capability_grants
WHERE identity_id = '00000000-0000-0000-0000-000000000001'
  AND capability IN ('governance.write', 'identity.create', 'agent.run');
