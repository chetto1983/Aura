-- name: InsertTelegramSetupPending :exec
INSERT INTO aura.telegram_setup_pending (onboarding_token, identity_id, generated_by, expires_at)
VALUES ($1, $2, $3, $4);

-- name: GetTelegramSetupPending :one
SELECT onboarding_token, identity_id, generated_by, created_at, expires_at, consumed_at
FROM aura.telegram_setup_pending
WHERE onboarding_token = $1;

-- ConsumeTelegramSetupPending atomically marks an unconsumed, unexpired token as
-- consumed and returns it. The consumed_at IS NULL guard makes the consume
-- single-use: a second consume of the same token matches no row (RETURNING is
-- empty → sql.ErrNoRows / pgx.ErrNoRows → ErrTokenConsumed). The expires_at guard
-- rejects a stale token in the same statement.
-- name: ConsumeTelegramSetupPending :one
UPDATE aura.telegram_setup_pending
SET consumed_at = now()
WHERE onboarding_token = $1
  AND consumed_at IS NULL
  AND expires_at > now()
RETURNING onboarding_token, identity_id, generated_by, created_at, expires_at, consumed_at;

-- name: DeleteExpiredTelegramSetupPending :execrows
DELETE FROM aura.telegram_setup_pending
WHERE consumed_at IS NULL
  AND expires_at <= now();
