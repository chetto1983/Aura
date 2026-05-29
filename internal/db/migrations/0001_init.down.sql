-- Reversal: revoke default privileges, drop schema cascade, drop roles.
-- EnsureRoles bootstrap will recreate roles on the next migrate.

ALTER DEFAULT PRIVILEGES FOR ROLE aura_migrate IN SCHEMA aura
    REVOKE SELECT, INSERT, UPDATE, DELETE ON TABLES FROM aura_app;
ALTER DEFAULT PRIVILEGES FOR ROLE aura_migrate IN SCHEMA aura
    REVOKE USAGE, SELECT ON SEQUENCES FROM aura_app;

REVOKE ALL ON SCHEMA aura FROM aura_app;
REVOKE ALL ON SCHEMA aura FROM aura_migrate;

DROP SCHEMA IF EXISTS aura CASCADE;

DROP ROLE IF EXISTS aura_app;
DROP ROLE IF EXISTS aura_migrate;
