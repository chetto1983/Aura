-- name: SetDocumentDigest :one
-- Identity-scoped: this is a WRITE reachable from a tool call, so a document id
-- alone must never be enough to overwrite what someone else's library says about
-- their file. A non-owner gets no rows, which the caller reports as not-found.
UPDATE aura.documents
SET digest = sqlc.arg(digest),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND identity_id = sqlc.arg(identity_id)
  AND deleted_at IS NULL
RETURNING *;

-- name: SetDocumentCard :one
-- The machine's own description, written at ingest from the file. Identity-scoped like
-- the digest above: same table, same reachability, same rule. It deliberately does NOT
-- touch digest — that column belongs to document_describe, and a re-ingest must not
-- delete what the agent learned by opening the file.
UPDATE aura.documents
SET card = sqlc.arg(card),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND identity_id = sqlc.arg(identity_id)
  AND deleted_at IS NULL
RETURNING *;

-- name: SearchDocumentDigests :many
-- Rank a library by what each document IS, to pick WHICH file to open. The
-- ranking is Postgres' own ts_rank over the weighted title/tags/digest/card
-- vector — no embedding, no reranker, no graph. The four legs are weighted in
-- that order (A title, B tags, C the agent's note, D the machine card): the card
-- makes every file findable from the moment it lands, and never outranks what a
-- person or the agent said about it. websearch_to_tsquery is used because the
-- query arrives as a user's sentence, and it degrades a malformed one to terms
-- instead of raising, which plainto_tsquery does not do as gracefully.
--
-- A blank query is not an error: it lists the library newest-first, which is what
-- "the file I just uploaded" means.
--
-- The query is UNACCENTED, not run through aura.searchable_text: the index already
-- holds both the written and the folded form, so folding the query is enough to
-- meet it. Passing the query through searchable_text instead emits every variant
-- as a separate term, and websearch_to_tsquery ANDs them — measured, that dropped
-- an accented query from 0.15 to 0.0002 by requiring both spellings at once.
--
-- KNOWN LIMIT: 'simple' does no stemming, so "venditori" does not match a digest
-- that says "venditore". That is the accepted price of not picking one language's
-- stemmer for a mixed corpus (measured: a single stemmer halved recall). A library
-- is tens of documents and the agent can rephrase; if this starts costing, fold
-- plurals inside aura.searchable_text rather than reaching for a stemmer.
SELECT *, ts_rank(digest_tsv, websearch_to_tsquery('simple', public.unaccent('public.unaccent'::regdictionary, sqlc.arg(query)::text))) AS rank
FROM aura.documents
WHERE identity_id = sqlc.arg(identity_id)
  AND deleted_at IS NULL
  AND status <> 'deleted'
  AND (
    sqlc.arg(query)::text = ''
    OR digest_tsv @@ websearch_to_tsquery('simple', public.unaccent('public.unaccent'::regdictionary, sqlc.arg(query)::text))
  )
ORDER BY rank DESC, updated_at DESC
LIMIT sqlc.arg(row_limit);
