-- The always-on flag lives in the SKILL.md frontmatter, and the always-on block is rendered
-- from the filesystem loader that reads it. The catalog's denormalised copy and its partial
-- index (0118) served a lookup that never had a production caller, and no code reads the
-- column back (PRD §14, amendment #224).
DROP INDEX IF EXISTS aura.skill_catalog_always_apply_idx;
ALTER TABLE aura.skill_catalog DROP COLUMN IF EXISTS always_apply;
