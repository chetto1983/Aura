-- name: CreateConversation :one
INSERT INTO aura.conversations (id, identity_id, model, status, metadata)
VALUES ($1, $2, $3, 'active', $4)
RETURNING id, title, identity_id, created_at, last_active_at, status, model,
          total_input_tokens, total_output_tokens, total_cached_tokens,
          total_cost_usd, metadata;

-- name: GetConversation :one
SELECT id, title, identity_id, created_at, last_active_at, status, model,
       total_input_tokens, total_output_tokens, total_cached_tokens,
       total_cost_usd, metadata
FROM aura.conversations
WHERE id = $1;

-- name: ListConversations :many
SELECT id, title, identity_id, created_at, last_active_at, status, model,
       total_input_tokens, total_output_tokens, total_cached_tokens,
       total_cost_usd, metadata
FROM aura.conversations
WHERE status <> 'deleted'
  AND (sqlc.arg(include_archived)::boolean OR status = 'active')
ORDER BY last_active_at DESC;

-- name: UpdateConversationStatus :exec
UPDATE aura.conversations
SET status = $2,
    last_active_at = now()
WHERE id = $1;

-- name: RenameConversation :exec
UPDATE aura.conversations
SET title = $2
WHERE id = $1;

-- name: SetConversationTitleIfNull :exec
UPDATE aura.conversations
SET title = $2
WHERE id = $1
  AND title IS NULL;

-- name: UpdateConversationAggregates :exec
UPDATE aura.conversations
SET last_active_at = now(),
    total_input_tokens = total_input_tokens + sqlc.arg(input_tokens),
    total_output_tokens = total_output_tokens + sqlc.arg(output_tokens),
    total_cached_tokens = total_cached_tokens + sqlc.arg(cached_tokens),
    total_cost_usd = total_cost_usd + sqlc.arg(cost_usd)
WHERE id = sqlc.arg(id);

-- name: DeleteConversation :exec
DELETE FROM aura.conversations
WHERE id = $1;

-- name: GetConversationForIdentity :one
-- Owner-scoped single-conversation read (Phase 36 MUSR-01 / D-06): GetConversation with
-- an identity_id owner predicate. A miss is the caller's 404 (read hides existence).
-- Routed through db.WithIdentityTx so the RLS owner policy backstops a forgotten filter.
SELECT id, title, identity_id, created_at, last_active_at, status, model,
       total_input_tokens, total_output_tokens, total_cached_tokens,
       total_cost_usd, metadata
FROM aura.conversations
WHERE id = $1
  AND identity_id = $2;

-- name: ListConversationsForIdentity :many
-- Owner-scoped conversation list (Phase 36 MUSR-01): ListConversations restricted to one
-- identity. identity_id is NOT NULL (0005) so every conversation is attributable.
SELECT id, title, identity_id, created_at, last_active_at, status, model,
       total_input_tokens, total_output_tokens, total_cached_tokens,
       total_cost_usd, metadata
FROM aura.conversations
WHERE identity_id = sqlc.arg(identity_id)
  AND status <> 'deleted'
  AND (sqlc.arg(include_archived)::boolean OR status = 'active')
ORDER BY last_active_at DESC;

-- name: DeleteConversationForIdentity :execrows
-- Owner-scoped hard delete (Phase 36 MUSR-01 / D-06): affects a row ONLY when the caller
-- owns it. rows-affected==0 lets the handler split 403 (a known-foreign id) from 404.
DELETE FROM aura.conversations
WHERE id = $1
  AND identity_id = $2;

-- name: UpdateConversationStatusForIdentity :execrows
-- Owner-scoped status transition (archive/unarchive, Phase 36 MUSR-01 / D-06). Serves the
-- /archive + /unarchive routes; rows-affected==0 drives the handler's 403-vs-404 split.
UPDATE aura.conversations
SET status = sqlc.arg(status),
    last_active_at = now()
WHERE id = sqlc.arg(id)
  AND identity_id = sqlc.arg(identity_id);

-- name: RenameConversationForIdentity :execrows
-- Owner-scoped rename (Phase 36 MUSR-01 / D-06). rows-affected==0 drives the 403-vs-404 split.
UPDATE aura.conversations
SET title = sqlc.arg(title)
WHERE id = sqlc.arg(id)
  AND identity_id = sqlc.arg(identity_id);
