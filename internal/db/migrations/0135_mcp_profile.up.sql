-- MCP profiles and the active one become registry state. Slot measured with
-- `ls internal/db/migrations/ | tail -1`: 0134 was the head.
--
-- WHY. 0101 moved the registry into aura.mcp_server and put profile membership on each
-- server's row, which left two things with nowhere to live: a profile with no servers, and
-- which profile is active. Measured 2026-10-04 with the release binary: `aura mcp profile
-- create work` and `aura mcp profile use work` both answered ok and wrote an mcp_audit row,
-- and neither took effect -- the ledger recorded two changes that never happened.
--
-- Membership stays on aura.mcp_server.profiles; a name there that has no row here is still
-- a profile. This table adds the profiles that exist on their own and the single active one.
--
-- NOT IDENTITY-SCOPED, for the reason 0101 gives: which servers a deployment mounts is
-- configuration the daemon and every operator share. No created_by either: mcp_audit
-- already records who created or switched a profile.

CREATE TABLE aura.mcp_profile (
    name       text        PRIMARY KEY CHECK (btrim(name) <> ''),
    active     boolean     NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- At most one active profile, enforced by the server rather than by every writer.
CREATE UNIQUE INDEX mcp_profile_one_active_idx ON aura.mcp_profile (active) WHERE active;

GRANT SELECT, INSERT, UPDATE, DELETE ON aura.mcp_profile TO aura_app;
GRANT ALL                           ON aura.mcp_profile TO aura_migrate;

COMMENT ON TABLE aura.mcp_profile IS
    'MCP profiles that exist on their own, and the single active one (migration 0135). Membership stays on aura.mcp_server.profiles. Deployment-scoped like aura.mcp_server.';
