-- Acquire this in a separate statement before the guarded insert. A lock inside
-- that insert would retain a snapshot taken before waiting and race the cap.
-- https://www.postgresql.org/docs/18/functions-admin.html#FUNCTIONS-ADVISORY-LOCKS
-- name: LockSteerConversation :exec
SELECT pg_advisory_xact_lock(hashtextextended('aura.steer:' || sqlc.arg(conversation_id)::text, 0));

-- name: ListWorkerSteers :many
SELECT * FROM aura.steer_queue
WHERE identity_id = sqlc.arg(identity_id)::uuid
  AND conversation_id = sqlc.arg(conversation_id)
  AND target_worker_id = sqlc.arg(target_worker_id)
ORDER BY created_at DESC, id DESC
LIMIT 100;

-- name: ExpireWorkerRunSteers :execrows
UPDATE aura.steer_queue
SET expired_at = now(), expiry_reason = 'worker_run_ended'
WHERE identity_id = sqlc.arg(identity_id)::uuid
  AND conversation_id = sqlc.arg(conversation_id)
  AND target_run_id = sqlc.arg(target_run_id)
  AND drained_at IS NULL AND expired_at IS NULL;
