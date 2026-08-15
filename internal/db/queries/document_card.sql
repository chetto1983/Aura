-- name: SetDocumentCard :one
-- The machine's own description, written at ingest from the file. Identity-scoped:
-- this is a write reachable from an ingest a tool call can start, so a document id alone
-- must never be enough to overwrite what someone else's library says about their file.
-- A non-owner gets no rows, which the caller reports as not-found.
UPDATE aura.documents
SET card = sqlc.arg(card),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND identity_id = sqlc.arg(identity_id)
  AND deleted_at IS NULL
RETURNING *;
