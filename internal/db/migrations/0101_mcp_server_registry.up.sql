-- The MCP server registry moves off the filesystem and into the database. The floor before
-- this is 0100 (identity_mcp_oauth); the registry lands at 0101.
--
-- WHY. Until now the registry lived in ONE root-owned JSON file, and the board did not read
-- it alone: handleMCPList served `file + the config snapshot taken at boot`, while every
-- write (install, env edit, trust, enable/disable, remove) read and wrote the file only.
-- Two read paths over one logical thing is a split brain, and on 2026-08-24 it produced
-- exactly the failure that shape predicts: the file was truncated to `"mcpServers": {}`
-- while the board went on listing Slack and linear from the boot snapshot, so Remove
-- answered "mcp server not found" for a server the operator could see, and the profile
-- column read empty for servers that had been in a profile minutes earlier.
--
-- A file is also the wrong durability for something a deployment cannot be rebuilt without.
-- Aura already keeps the MCP AUDIT trail (0022) and the per-identity OAuth GRANTS (0100)
-- in Postgres; the registry was the last piece still on a file, and it is the piece whose
-- loss silently disables every server at once.
--
-- SHAPE. `config` is the ManagedServer as jsonb MINUS its env, which is the shape LibreChat
-- stores too (packages/data-schemas/src/schema/mcpServer.ts keeps `config` as a Mixed
-- document per (serverName, tenantId)). Storing it whole avoids a column per field and a
-- migration every time ManagedServer grows; the fields anything actually filters on --
-- name, source, enabled -- are promoted to columns.
--
-- WHY env IS SEALED AND config IS NOT. ManagedServer.Env carries the operator's OWN
-- credentials for a remote server (MCP_OAUTH_CLIENT_SECRET, MCP_HEADER_*, API keys). On
-- disk those sat in a 0600 root-owned file; in a table they would otherwise be readable by
-- anything holding an aura_app connection, which is a strictly wider audience. The rest of
-- ManagedServer -- command, url, transport, trust, runtime -- is configuration, not
-- credential, and keeping it queryable is what lets the board render without a key.
--
-- NOT IDENTITY-SCOPED, deliberately, and this is the line between 0100 and 0101. A grant
-- identifies a PERSON and must never be visible to another identity, so 0100 carries both
-- RLS layers. Which servers a deployment has configured is not personal: every operator
-- with governance.read sees the same board, the agent mounts the same set, and the CLI
-- administers it from a shell. Scoping these rows per identity would mean a server
-- installed by one operator vanished for their colleague and for the daemon itself.
-- created_by is recorded for the audit trail, never used as a visibility predicate.

CREATE TABLE aura.mcp_server (
    name       text        PRIMARY KEY,
    -- 'custom' for an operator install, 'recipe:<name>' for a catalog entry that has been
    -- materialised. Recipes that are merely DECLARED in code are not rows here: they are
    -- overlaid read-only at read time, so upgrading Aura still updates them.
    source     text        NOT NULL DEFAULT 'custom',
    -- NULL means "enabled", matching ManagedServer.Enabled's *bool: a config imported from
    -- a Claude-style file says nothing about enablement and must not read as disabled.
    enabled    boolean,
    config     jsonb       NOT NULL DEFAULT '{}'::jsonb,
    -- AES-256-GCM over the JSON-encoded []string of KEY=VALUE entries. NULL when a server
    -- carries no env at all, which is the common case for a URL-only remote server.
    env_enc    bytea,
    -- Profile membership, denormalised onto the row it belongs to. It was a separate map in
    -- the file, which is how an install could write the server and forget the profile --
    -- leaving it listed, mountable by nothing, and inert.
    profiles   text[]      NOT NULL DEFAULT '{}',
    created_by uuid        REFERENCES aura.identities (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX mcp_server_profiles_idx ON aura.mcp_server USING gin (profiles);

GRANT SELECT, INSERT, UPDATE, DELETE ON aura.mcp_server TO aura_app;
GRANT ALL                           ON aura.mcp_server TO aura_migrate;

COMMENT ON TABLE aura.mcp_server IS
    'The MCP server registry (migration 0101), previously one root-owned JSON file. config is the ManagedServer as jsonb minus env; env_enc is AES-256-GCM ciphertext over its KEY=VALUE list (KEK derived from AURA_AUTHULA_SECRET, the same trust boundary as aura.identity_mcp_oauth). Deployment-scoped on purpose, NOT per identity: which servers exist is configuration every operator and the daemon itself must see, unlike aura.identity_mcp_oauth, where a row identifies one person. created_by is audit, never a visibility predicate.';

COMMENT ON COLUMN aura.mcp_server.enabled IS
    'NULL means enabled (ManagedServer.Enabled is a *bool): a config that says nothing about enablement must not read as disabled.';
