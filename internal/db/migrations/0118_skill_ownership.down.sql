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

DROP TABLE IF EXISTS aura.resource_acl;
-- skill_catalog_acl_cascade goes with its table; its function does not.
DROP TABLE IF EXISTS aura.skill_catalog;
DROP FUNCTION IF EXISTS aura.delete_acl_for_skill();
