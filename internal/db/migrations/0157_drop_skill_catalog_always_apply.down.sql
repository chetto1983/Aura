-- Restores 0118's shape only: every row comes back false, and a skill's true value returns
-- when the writer next upserts its catalog row from the frontmatter.
ALTER TABLE aura.skill_catalog ADD COLUMN IF NOT EXISTS always_apply boolean NOT NULL DEFAULT false;
CREATE INDEX IF NOT EXISTS skill_catalog_always_apply_idx
    ON aura.skill_catalog (owner_identity_id)
    WHERE always_apply;
