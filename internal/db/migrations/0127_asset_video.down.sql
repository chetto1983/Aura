-- A populated video store cannot be downgraded without an explicit data migration: this
-- constraint intentionally fails transactionally while any 'video' row exists, rather than
-- deleting or relabeling user media to force the rollback through.
ALTER TABLE aura.assets DROP CONSTRAINT assets_modality_check;
ALTER TABLE aura.assets ADD CONSTRAINT assets_modality_check
  CHECK (modality IN ('document', 'image', 'audio', 'unknown'));
