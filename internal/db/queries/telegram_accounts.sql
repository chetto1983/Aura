-- name: InsertTelegramAccount :one
INSERT INTO aura.telegram_accounts (telegram_user_id, identity_id, username, first_name)
VALUES ($1, $2, $3, $4)
RETURNING telegram_user_id, identity_id, username, first_name, added_at, last_seen_at;

-- name: GetTelegramAccountByTelegramID :one
SELECT telegram_user_id, identity_id, username, first_name, added_at, last_seen_at
FROM aura.telegram_accounts
WHERE telegram_user_id = $1;

-- name: GetTelegramAccountByIdentity :one
-- The account seen most recently, in the order the password reset and the recovery use. With no
-- ORDER BY the first row in the heap came back, and every update moves the writer's row to the
-- end, so the push reached the account used least recently (measured 2026-10-28).
SELECT telegram_user_id, identity_id, username, first_name, added_at, last_seen_at
FROM aura.telegram_accounts
WHERE identity_id = $1
ORDER BY COALESCE(last_seen_at, added_at) DESC, added_at DESC, telegram_user_id DESC
LIMIT 1;

-- name: TouchTelegramLastSeen :exec
UPDATE aura.telegram_accounts
SET last_seen_at = now()
WHERE telegram_user_id = $1;

-- name: CountTelegramAccounts :one
SELECT count(*) FROM aura.telegram_accounts;

-- name: ListTelegramAccountsByIdentity :many
SELECT telegram_user_id, identity_id, username, first_name, added_at, last_seen_at
FROM aura.telegram_accounts
WHERE identity_id = $1
ORDER BY added_at ASC, telegram_user_id ASC;

-- name: DeleteTelegramAccountForIdentity :execrows
DELETE FROM aura.telegram_accounts
WHERE telegram_user_id = $1 AND identity_id = $2;
