-- name: EnsureBoard :one
-- Creates the identity's board on first use and returns it either way. The no-op update makes
-- RETURNING yield the existing row on conflict.
INSERT INTO aura.boards (identity_id, name, columns)
VALUES ($1, $2, $3)
ON CONFLICT (identity_id, name) DO UPDATE SET name = EXCLUDED.name
RETURNING *;

-- name: UpdateBoardColumns :one
UPDATE aura.boards
SET columns = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListBoardCards :many
SELECT * FROM aura.board_cards
WHERE board_id = $1
ORDER BY column_id, position;

-- name: ListBoardColumnCards :many
SELECT * FROM aura.board_cards
WHERE board_id = $1 AND column_id = $2
ORDER BY position;

-- name: GetBoardCard :one
SELECT * FROM aura.board_cards WHERE id = $1;

-- name: InsertBoardCard :one
INSERT INTO aura.board_cards (
    board_id, identity_id, column_id, position, label, description, priority, tags, due_at,
    conversation_id, task_id, source, updated_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING *;

-- name: UpdateBoardCard :one
UPDATE aura.board_cards
SET label = $2, description = $3, priority = $4, tags = $5, due_at = $6, task_id = $7,
    updated_by = $8, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: MoveBoardCard :one
UPDATE aura.board_cards
SET column_id = $2, position = $3, updated_by = $4, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetBoardCardPosition :exec
UPDATE aura.board_cards SET position = $2 WHERE id = $1;

-- name: DeleteBoardCard :execrows
DELETE FROM aura.board_cards WHERE id = $1;

-- name: SearchBoardCards :many
SELECT * FROM aura.board_cards
WHERE board_id = $1
  AND (label ILIKE '%' || sqlc.arg(query)::text || '%'
       OR description ILIKE '%' || sqlc.arg(query)::text || '%'
       OR sqlc.arg(query)::text = ANY (tags))
ORDER BY updated_at DESC
LIMIT sqlc.arg(row_limit);

-- name: ListBoardViews :many
SELECT * FROM aura.board_views
WHERE identity_id = $1
ORDER BY pinned DESC, name;

-- name: UpsertBoardView :one
INSERT INTO aura.board_views (identity_id, name, filters, pinned)
VALUES ($1, $2, $3, $4)
ON CONFLICT (identity_id, name) DO UPDATE SET filters = EXCLUDED.filters, pinned = EXCLUDED.pinned
RETURNING *;

-- name: DeleteBoardView :execrows
DELETE FROM aura.board_views WHERE id = $1;
