-- name: SetDocumentDigest :one
UPDATE aura.documents
SET digest = sqlc.arg(digest),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND deleted_at IS NULL
RETURNING *;

-- name: SearchDocumentDigests :many
-- Rank a library by what each document IS, to pick WHICH file to open. The
-- ranking is Postgres' own ts_rank over the weighted title/tags/digest vector —
-- no embedding, no reranker, no graph. websearch_to_tsquery is used because the
-- query arrives as a user's sentence, and it degrades a malformed one to terms
-- instead of raising, which plainto_tsquery does not do as gracefully.
--
-- A blank query is not an error: it lists the library newest-first, which is what
-- "the file I just uploaded" means.
SELECT *, ts_rank(digest_tsv, websearch_to_tsquery('simple', sqlc.arg(query)::text)) AS rank
FROM aura.documents
WHERE identity_id = sqlc.arg(identity_id)
  AND deleted_at IS NULL
  AND status <> 'deleted'
  AND (
    sqlc.arg(query)::text = ''
    OR digest_tsv @@ websearch_to_tsquery('simple', sqlc.arg(query)::text)
  )
ORDER BY rank DESC, updated_at DESC
LIMIT sqlc.arg(row_limit);
