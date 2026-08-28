-- Reverse 0105: drop the steer-queue-owner leg before restoring run_id's NOT NULL.
-- Any row inserted with only steer_queue_id set (no run_id) would violate the
-- restored NOT NULL, so it is deleted first -- mirroring the project's own precedent
-- of deleting newly-admitted rows before narrowing a constraint back (0104's down).
DELETE FROM aura.pending_notifications WHERE run_id IS NULL;

ALTER TABLE aura.pending_notifications DROP CONSTRAINT pending_notifications_owner_chk;
ALTER TABLE aura.pending_notifications DROP COLUMN steer_queue_id;
ALTER TABLE aura.pending_notifications ALTER COLUMN run_id SET NOT NULL;
