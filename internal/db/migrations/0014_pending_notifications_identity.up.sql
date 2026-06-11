-- Source: Phase 20 R6/Fork 1. Snapshot the stable identity_id (NO FK, plain text,
-- mirrors scheduler_tasks.identity_id) so a quiet-hours-deferred / failed notification
-- routes back to its origin channel after a sweep. Survives a deleted origin conversation.
ALTER TABLE aura.pending_notifications ADD COLUMN identity_id text;
COMMENT ON COLUMN aura.pending_notifications.identity_id IS
    'Stable owning identity snapshot (Phase 20, Fork 1): the channel-independent delivery key for the deferred/failed sweep route-back. Plain text, no FK — survives a deleted origin conversation. NULL for legacy/CLI rows → falls back to notify_route.';
-- The existing aura_app DML grant (0013:26) already covers the new column — no new GRANT.
