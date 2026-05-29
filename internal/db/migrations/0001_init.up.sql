-- Source: PRD §Slice 0.5 amendment #17 + Postgres 17 GRANT/CREATE ROLE docs.
-- Lands in Slice 0.5 commit; gates every later Postgres migration.
--
-- ROLE CREATION HAPPENS IN GO BEFORE THIS MIGRATION RUNS.
-- golang-migrate/iofs does NOT support psql variable substitution (`:'var'`),
-- so role creation with parametrized passwords is performed by
-- internal/db/migrate.go::EnsureRoles using pgxpool parametrized DO blocks
-- BEFORE m.Up() runs. This .sql file only handles schema + grants on roles
-- that the EnsureRoles bootstrap has already created.

-- 1. Schema
CREATE SCHEMA IF NOT EXISTS aura;

-- 2. Schema-level grants (roles aura_app + aura_migrate were created in
--    EnsureRoles bootstrap before this migration ran).
GRANT USAGE         ON SCHEMA aura TO aura_app;
GRANT CREATE, USAGE ON SCHEMA aura TO aura_migrate;

-- 3. Default privileges — apply to tables/sequences created by aura_migrate
--    in future migrations (Pitfall #1 mitigation — without this, every new
--    table in a later migration would silently lack aura_app grants).
ALTER DEFAULT PRIVILEGES FOR ROLE aura_migrate IN SCHEMA aura
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO aura_app;
ALTER DEFAULT PRIVILEGES FOR ROLE aura_migrate IN SCHEMA aura
    GRANT USAGE, SELECT ON SEQUENCES TO aura_app;

-- 4. Search path documentation (runtime sets via connection string).
COMMENT ON SCHEMA aura IS 'Aura application schema — set search_path TO aura, public on every session';

-- 5. Audit guardrail: aura_app is explicitly NOT granted TRUNCATE / DROP /
--    CREATE on schema aura. The role separation is enforced by NOT granting
--    these to aura_app. T-1.05-02 mitigation — verified by
--    TestRoleSeparation_AppDenied (integration suite).
