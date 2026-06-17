-- Source: docs/cockpit-overhaul/05-authula-auth-SPEC.md §6 (H1 schema isolation) +
-- §5 (identity mapping). Lands in the Authula web-auth integration (Option A2).
--
-- Authula (the embedded auth framework) has NO table-prefix/schema config: it creates
-- bare users/sessions/accounts/verifications + its plugin tables via bun in the
-- connection's default schema. To stop those generic names colliding with aura.* or
-- public, Authula is pointed at a DEDICATED `authula` schema via a DSN search_path=authula.
-- This migration only CREATES that schema (Aura's golang-migrate ledger owns the schema
-- *existence*); Authula's own bun migrator fills the schema CONTENTS at New().
--
-- Authula connects with the runtime role aura_app and auto-creates its tables, so aura_app
-- needs CREATE+USAGE on the `authula` schema. This does NOT weaken the aura.* role
-- separation (aura_app still lacks CREATE/DROP/TRUNCATE on schema aura, 0001 §5) — the
-- authula schema is Authula's own isolated sandbox.

-- 0. Authula's core migration runs `CREATE EXTENSION IF NOT EXISTS pgcrypto` (for
--    gen_random_uuid()) as the runtime role aura_app, which lacks the privilege to
--    create an extension. Pre-create it here (aura_migrate has database CREATE) so that
--    aura_app's IF-NOT-EXISTS becomes a privilege-safe no-op. The extension is
--    database-global (not schema-scoped); it does not pollute aura.* with objects.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- 1. Isolated schema for all Authula tables (owner aura_migrate; runtime role aura_app
--    gets CREATE+USAGE so the embedded framework can run its own migrations).
CREATE SCHEMA IF NOT EXISTS authula AUTHORIZATION aura_migrate;
GRANT CREATE, USAGE ON SCHEMA authula TO aura_app;

-- aura_app owns the tables it creates in `authula`, so it already has full DML on them;
-- no ALTER DEFAULT PRIVILEGES is needed for the authula schema.

COMMENT ON SCHEMA authula IS
    'Isolated schema owned by the embedded Authula auth framework (H1). Disjoint from aura.* and public; Authula''s bun migrator manages its contents.';

-- 2. The operator-user <-> Aura-identity binding (spec §5). One row pins an Authula
--    users.id (text, lives in authula.users) to an aura.identities.id. UNIQUE on
--    authula_user_id (NOT identity_id) so it generalizes 1:N for multi-user (OQ-8).
CREATE TABLE aura.identity_auth_links (
    identity_id     uuid        NOT NULL REFERENCES aura.identities (id) ON DELETE CASCADE,
    authula_user_id text        NOT NULL UNIQUE,
    created_at      timestamptz NOT NULL DEFAULT now()
);

GRANT SELECT, INSERT, UPDATE, DELETE ON aura.identity_auth_links TO aura_app;
GRANT ALL                            ON aura.identity_auth_links TO aura_migrate;

COMMENT ON TABLE aura.identity_auth_links IS
    'Maps an Authula user-id to a seeded aura.identities row (spec §5). Single-operator: the operator user pins to `local` (…0001). The A2 session-validate seam maps authula user-id -> this identity_id -> principalKey{}.';
