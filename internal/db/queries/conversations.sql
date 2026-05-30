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
