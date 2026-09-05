-- Drop the per-identity skill catalog and the generic resource ACL.
--
-- Rolling back costs exactly the OWNERSHIP, never a skill: the bodies live on the
-- filesystem under $AURA_SKILLS_IDENTITY_DIR/<identity>/<name> and are untouched here. What
-- is lost is who owns what and who it was shared with, so a re-apply starts from "every
-- personal skill is unshared" -- fail-closed, which is the right direction for a boundary.
--
-- The triggers are dropped before their functions because a function still referenced by a
-- trigger cannot be dropped, and identities_acl_principal_cascade lives on aura.identities,
-- which this migration does not own and must therefore leave clean.

DROP TRIGGER IF EXISTS identities_acl_principal_cascade ON aura.identities;
DROP FUNCTION IF EXISTS aura.delete_acl_for_principal();

-- The two tables' RLS policies reference EACH OTHER, so neither DROP TABLE order works on
-- its own: skill_catalog_shared_read reads aura.resource_acl, and the two write policies that
-- make a grant require ownership read aura.skill_catalog. Postgres records those as real
-- dependencies and refuses the first DROP with 2BP01. Measured 2026-09-05 in CI run 1789: the
-- Reset helper runs this down against the shared `aura` database, so the failure did not stay
-- in a disposable drill — it left the database DIRTY at 117 and took every migrating test in
-- the suite with it.
--
-- Dropping the policies first breaks the cycle explicitly. It is also the honest order: a
-- policy that spans two tables belongs to neither, so it is removed before either.
DROP POLICY IF EXISTS skill_catalog_shared_read       ON aura.skill_catalog;
DROP POLICY IF EXISTS resource_acl_grant_what_you_own ON aura.resource_acl;
DROP POLICY IF EXISTS resource_acl_regrant_what_you_own ON aura.resource_acl;

DROP TABLE IF EXISTS aura.resource_acl;
-- skill_catalog_acl_cascade goes with its table; its function does not.
DROP TABLE IF EXISTS aura.skill_catalog;
DROP FUNCTION IF EXISTS aura.delete_acl_for_skill();
