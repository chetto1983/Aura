-- name: InsertTelegramAccount :one
INSERT INTO aura.telegram_accounts (telegram_user_id, identity_id, username, first_name)
VALUES ($1, $2, $3, $4)
RETURNING telegram_user_id, identity_id, username, first_name, added_at, last_seen_at;

-- name: GetTelegramAccountByTelegramID :one
SELECT telegram_user_id, identity_id, username, first_name, added_at, last_seen_at
FROM aura.telegram_accounts
WHERE telegram_user_id = $1;

-- name: GetTelegramAccountByIdentity :one
SELECT telegram_user_id, identity_id, username, first_name, added_at, last_seen_at
FROM aura.telegram_accounts
WHERE identity_id = $1;

-- name: TouchTelegramLastSeen :exec
UPDATE aura.telegram_accounts
SET last_seen_at = now()
WHERE telegram_user_id = $1;

-- name: CountTelegramAccounts :one
SELECT count(*) FROM aura.telegram_accounts;

-- name: ListTelegramAccounts :many
SELECT telegram_user_id, identity_id, username, first_name, added_at, last_seen_at
FROM aura.telegram_accounts
ORDER BY added_at ASC, telegram_user_id ASC;
