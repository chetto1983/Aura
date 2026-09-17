DROP INDEX IF EXISTS aura.media_job_studio_idx;
ALTER TABLE aura.media_job DROP CONSTRAINT IF EXISTS media_job_image_is_finished;
ALTER TABLE aura.media_job DROP CONSTRAINT IF EXISTS media_job_surface_scope;
ALTER TABLE aura.media_job DROP COLUMN IF EXISTS kind;
ALTER TABLE aura.media_job DROP COLUMN IF EXISTS surface;
