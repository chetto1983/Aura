-- Reversal: revoke default privileges + grants, then drop schema cascade.
-- Roles (aura_app, aura_migrate) are intentionally NOT dropped here. Their
-- lifecycle is owned by EnsureRoles (Go), mirroring the up migration which
-- only handles schema + grants on pre-existing roles. This keeps Reset's
-- Down->Up cycle valid: the up migration re-GRANTs to the still-present roles,
-- and the migrate role (no CREATEROLE) could not DROP ROLE anyway.

ALTER DEFAULT PRIVILEGES FOR ROLE aura_migrate IN SCHEMA aura
    REVOKE SELECT, INSERT, UPDATE, DELETE ON TABLES FROM aura_app;
ALTER DEFAULT PRIVILEGES FOR ROLE aura_migrate IN SCHEMA aura
    REVOKE USAGE, SELECT ON SEQUENCES FROM aura_app;

REVOKE ALL ON SCHEMA aura FROM aura_app;
REVOKE ALL ON SCHEMA aura FROM aura_migrate;

DROP SCHEMA IF EXISTS aura CASCADE;
