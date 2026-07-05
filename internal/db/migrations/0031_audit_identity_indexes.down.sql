-- Reverse 0031: drop the D-28 per-identity audit indexes.
DROP INDEX IF EXISTS aura.skill_audit_identity_created_idx;
DROP INDEX IF EXISTS aura.mcp_audit_actor_created_idx;
