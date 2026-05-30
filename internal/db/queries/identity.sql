-- name: CreateIdentity :one
INSERT INTO aura.identities (id, name, kind)
VALUES ($1, $2, $3)
RETURNING id, name, kind, created_at;

-- name: GetIdentityByName :one
SELECT id, name, kind, created_at
FROM aura.identities
WHERE name = $1;

-- name: GetIdentityByID :one
SELECT id, name, kind, created_at
FROM aura.identities
WHERE id = $1;

-- name: ListIdentities :many
SELECT id, name, kind, created_at
FROM aura.identities
ORDER BY created_at ASC, name ASC;

-- name: DeleteIdentity :exec
DELETE FROM aura.identities
WHERE name = $1;
