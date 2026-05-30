-- Source: PRD §Slice 1.7 + phase 04-SPEC.md Req#5 + 04-RESEARCH.md L399-410 (seed).
-- Single-user `local` identity scaffolding + capability_grants. Seeds identity
-- `local` with the fixed UUID 00000000-0000-0000-0000-000000000001 (kind system)
-- and a '*' wildcard grant, both ON CONFLICT DO NOTHING (idempotent re-apply).

CREATE TABLE aura.identities (
    id         uuid        PRIMARY KEY,
    name       text        NOT NULL UNIQUE,
    kind       text        NOT NULL CHECK (kind IN ('system', 'user', 'channel', 'service')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE aura.capability_grants (
    identity_id uuid        NOT NULL REFERENCES aura.identities (id) ON DELETE CASCADE,
    capability  text        NOT NULL,
    granted_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (identity_id, capability)
);

-- Belt + suspenders DML grants (NEVER TRUNCATE/DROP/CREATE to aura_app).
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.identities         TO aura_app;
GRANT ALL                            ON aura.identities         TO aura_migrate;
GRANT SELECT, INSERT, UPDATE, DELETE ON aura.capability_grants  TO aura_app;
GRANT ALL                            ON aura.capability_grants  TO aura_migrate;

-- Seed the single-user `local` identity + wildcard grant (idempotent).
INSERT INTO aura.identities (id, name, kind)
    VALUES ('00000000-0000-0000-0000-000000000001', 'local', 'system')
    ON CONFLICT DO NOTHING;
INSERT INTO aura.capability_grants (identity_id, capability)
    VALUES ('00000000-0000-0000-0000-000000000001', '*')
    ON CONFLICT DO NOTHING;

COMMENT ON TABLE aura.identities IS
    'Identity scaffolding (Slice 1.7). Single-user: one seeded `local`/system row with the fixed UUID ...001.';
COMMENT ON TABLE aura.capability_grants IS
    'Per-identity capability grants. Wildcard `*` is system-managed (seeded, never grant/revoke via CLI).';
