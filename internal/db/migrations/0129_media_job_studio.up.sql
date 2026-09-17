-- Studio generations (Studio plan, Task 1). Number measured with
-- `ls internal/db/migrations/ | tail -1` at landing time (0128_media_job was the head).
--
-- The cockpit Studio generates without an agent turn, so its rows belong to no conversation
-- and no tool call, and it generates images as well as video. surface says which surface
-- asked, kind says what was made, and the two CHECKs keep the shapes apart: a chat job
-- without its conversation could never be delivered, a Studio row with one would be offered
-- to that conversation's recovery, and an image is generated synchronously, so its row is
-- only ever written finished — the watcher, which reads active and completed-undelivered
-- rows, must never find one to poll.
--
-- The column is surface, not origin: a job's origin is already the submission base URL its
-- request records (mediagen.JobAudit.Origin). An image has no provider job, so its
-- provider_job_id is a locally minted image-<uuid>; the UNIQUE index then still stops a
-- replayed insert from recording one paid generation twice.
--
-- Measured before landing on 2026-09-17: 6 live rows, none with an empty conversation or
-- tool call.

ALTER TABLE aura.media_job
  ADD COLUMN surface text NOT NULL DEFAULT 'chat' CHECK (surface IN ('chat', 'studio')),
  ADD COLUMN kind    text NOT NULL DEFAULT 'video' CHECK (kind IN ('image', 'video'));

ALTER TABLE aura.media_job ADD CONSTRAINT media_job_surface_scope CHECK (
  (surface = 'chat'   AND conversation_id <> '' AND tool_call_id <> '') OR
  (surface = 'studio' AND conversation_id =  '' AND tool_call_id =  ''));

ALTER TABLE aura.media_job ADD CONSTRAINT media_job_image_is_finished CHECK (
  kind = 'video' OR (status = 'completed' AND delivered_at IS NOT NULL));

-- Backs the Studio history (ListStudioMediaJobs), newest first.
CREATE INDEX media_job_studio_idx ON aura.media_job (identity_id, created_at DESC, id DESC)
  WHERE surface = 'studio';

COMMENT ON COLUMN aura.media_job.surface IS
    'Which surface asked for the generation (migration 0129): chat (an agent tool call in a conversation) or studio (the cockpit Studio, no conversation, delivered on completion).';
COMMENT ON COLUMN aura.media_job.kind IS
    'What was generated (migration 0129): video (a provider job the watcher supervises) or image (a synchronous generation, recorded already completed and delivered).';
