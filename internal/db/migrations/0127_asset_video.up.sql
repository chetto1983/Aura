ALTER TABLE aura.assets DROP CONSTRAINT assets_modality_check;
ALTER TABLE aura.assets ADD CONSTRAINT assets_modality_check
  CHECK (modality IN ('document', 'image', 'audio', 'video', 'unknown'));
