-- name: UpsertSetting :one
INSERT INTO aura.settings (key, value, is_secret, updated_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (key) DO UPDATE
SET value      = EXCLUDED.value,
    is_secret  = EXCLUDED.is_secret,
    updated_by = EXCLUDED.updated_by,
    updated_at = now()
RETURNING key, value, is_secret, updated_at, updated_by;

-- name: ListSettings :many
SELECT key, value, is_secret, updated_at, updated_by
FROM aura.settings
ORDER BY key;

-- name: GetSetting :one
SELECT key, value, is_secret, updated_at, updated_by
FROM aura.settings
WHERE key = $1;

-- name: DeleteSetting :exec
DELETE FROM aura.settings WHERE key = $1;
