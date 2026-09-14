-- Durable video generation jobs (image/video generation plan, Task 6). Number measured with
-- `ls internal/db/migrations/ | tail -1` at landing time (0127_asset_video was the head).
--
-- A video job is paid for when OpenRouter accepts it, and it finishes minutes later, often
-- after the turn that asked for it and sometimes after a restart. The row is what lets the
-- daemon resume polling from provider_job_id and deliver the clip exactly once. The recovery
-- guarantee starts after this row is inserted; the submission interval before it is excluded
-- (spec 2026-09-14-image-video-generation-design.md §3, Recovery boundary).
--
-- provider_job_id is UNIQUE across identities: one provider job is one row, so a replayed
-- insert can never make a second watcher poll and ingest the same paid job.
--
-- asset_id has no ON DELETE action: assets are soft-deleted (deleted_at), and a completed job
-- must never silently lose the pointer to the clip it paid for. Deleting an identity cascades
-- its jobs and its assets in the same statement, so a job pointing at its OWN identity's asset
-- does not block deprovisioning. The FK does not enforce that the asset shares the job's
-- identity: referential-integrity checks bypass row-level security, so a row pointing at
-- ANOTHER identity's asset would block deleting that identity (23503) until the row is gone.
-- mediagen.Store never writes one, because CompleteMediaJob only sets an asset of the job's own
-- identity; only a raw write outside the store can create it.
--
-- RLS is the 0090/0122 shape: ENABLE, not FORCE, because aura_migrate owns the table and
-- a table owner bypasses row security; aura_app is a non-owner and gets both policies. The
-- owner policy has no IS NULL escape, and the RESTRICTIVE floor AND-combines with every
-- permissive policy, so a caller with no app.current_identity sees and writes nothing.
-- WITH CHECK is spelled out on both, so the write side never depends on the USING default.

CREATE TABLE aura.media_job (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  identity_id uuid NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE,
  conversation_id text NOT NULL,
  tool_call_id text NOT NULL,
  provider_job_id text NOT NULL UNIQUE,
  model text NOT NULL,
  request jsonb NOT NULL,
  status text NOT NULL CHECK (status IN
    ('pending','in_progress','completed','failed','expired','cancelled')),
  error jsonb,
  asset_id uuid REFERENCES aura.assets(id),
  cost_usd numeric,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz,
  delivered_at timestamptz,
  CHECK (cost_usd IS NULL OR cost_usd >= 0),
  CHECK (status <> 'completed' OR asset_id IS NOT NULL),
  CHECK (delivered_at IS NULL OR status = 'completed')
);

-- Backs the per-owner boot resume (ListRecoverableMediaJobs): only active and completed but
-- undelivered rows are ever scanned there.
CREATE INDEX media_job_recovery_idx ON aura.media_job(identity_id, created_at)
  WHERE status IN ('pending','in_progress') OR
    (status = 'completed' AND delivered_at IS NULL);

GRANT SELECT, INSERT, UPDATE, DELETE ON aura.media_job TO aura_app;
GRANT ALL                           ON aura.media_job TO aura_migrate;

ALTER TABLE aura.media_job ENABLE ROW LEVEL SECURITY;
CREATE POLICY media_job_owner_isolation ON aura.media_job
  USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid)
  WITH CHECK (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);
CREATE POLICY media_job_requires_identity ON aura.media_job
  AS RESTRICTIVE FOR ALL TO aura_app
  USING (NULLIF(current_setting('app.current_identity', true), '') IS NOT NULL)
  WITH CHECK (NULLIF(current_setting('app.current_identity', true), '') IS NOT NULL);

COMMENT ON POLICY media_job_requires_identity ON aura.media_job IS
    'Fail-closed floor (migration 0087 pattern): AND-combined with every permissive policy, so no later permissive policy can restore visibility to a caller with no app.current_identity. Set it via internal/db.WithIdentityTx / WithIdentityTxRaw.';

COMMENT ON TABLE aura.media_job IS
    'Durable OpenRouter video generation jobs (migration 0128). One row per provider job; resumed from provider_job_id after a restart, delivered once through delivered_at. request is the clamped body without image data plus a reserved _aura audit object (reference asset ids, submission origin); it never holds a key.';
