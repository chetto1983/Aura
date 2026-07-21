-- name: CreateConversation :one
INSERT INTO aura.conversations (id, identity_id, model, status, metadata)
VALUES ($1, $2, $3, 'active', $4)
RETURNING id, title, identity_id, created_at, last_active_at, status, model,
          total_input_tokens, total_output_tokens, total_cached_tokens,
          total_cost_usd, metadata, snapshot_version, delete_reservation;

-- name: GetConversation :one
SELECT id, title, identity_id, created_at, last_active_at, status, model,
       total_input_tokens, total_output_tokens, total_cached_tokens,
       total_cost_usd, metadata, snapshot_version, delete_reservation
FROM aura.conversations
WHERE id = $1;

-- name: GetConversationLastInputTokens :one
-- The input_tokens of the most recent request-bearing turn = the CURRENT
-- context-window fill (distinct from the cumulative total_input_tokens column,
-- which sums every turn's input and so climbs far past the window on a long
-- chat). The runtime footer's context gauge reads this so a reloaded
-- conversation shows real fill, not the lifetime sum. COALESCE => 0 when the
-- conversation has no request-bearing turn yet.
SELECT COALESCE((
    SELECT input_tokens
    FROM aura.conversation_turns
    WHERE conversation_id = $1 AND input_tokens > 0
    ORDER BY seq DESC
    LIMIT 1
), 0)::int AS last_input_tokens;

-- name: ListConversations :many
SELECT id, title, identity_id, created_at, last_active_at, status, model,
       total_input_tokens, total_output_tokens, total_cached_tokens,
       total_cost_usd, metadata, snapshot_version, delete_reservation
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
WHERE id = $1
  AND delete_reservation IS NULL;

-- name: GetConversationForIdentity :one
-- Owner-scoped single-conversation read (Phase 36 MUSR-01 / D-06): GetConversation with
-- an identity_id owner predicate. A miss is the caller's 404 (read hides existence).
-- Routed through db.WithIdentityTx so the RLS owner policy backstops a forgotten filter.
SELECT id, title, identity_id, created_at, last_active_at, status, model,
       total_input_tokens, total_output_tokens, total_cached_tokens,
       total_cost_usd, metadata, snapshot_version, delete_reservation
FROM aura.conversations
WHERE id = $1
  AND identity_id = $2;

-- name: ListConversationsForIdentity :many
-- Owner-scoped conversation list (Phase 36 MUSR-01): ListConversations restricted to one
-- identity. identity_id is NOT NULL (0005) so every conversation is attributable.
SELECT id, title, identity_id, created_at, last_active_at, status, model,
       total_input_tokens, total_output_tokens, total_cached_tokens,
       total_cost_usd, metadata, snapshot_version, delete_reservation
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
  AND identity_id = $2
  AND delete_reservation IS NULL;

-- name: GetConversationVersionForIdentity :one
SELECT snapshot_version
FROM aura.conversations
WHERE id = $1
  AND identity_id = $2;

-- name: ReserveConversationDeleteForIdentityIfVersion :execrows
-- Cross-process export-delete fence. This must commit before any runtime teardown.
-- Reusing the same deterministic reservation is idempotent after a process retry.
UPDATE aura.conversations
SET delete_reservation = sqlc.arg(reservation)
WHERE id = sqlc.arg(id)
  AND identity_id = sqlc.arg(identity_id)
  AND snapshot_version = sqlc.arg(expected_version)
  AND (delete_reservation IS NULL OR delete_reservation = sqlc.arg(reservation));

-- name: DeleteConversationForIdentityIfReservation :execrows
DELETE FROM aura.conversations
WHERE id = sqlc.arg(id)
  AND identity_id = sqlc.arg(identity_id)
  AND delete_reservation = sqlc.arg(reservation);

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

-- name: UpdateConversationReasoningEffortForIdentity :execrows
-- Owner-scoped reasoning-effort persistence (Phase 37E WEBMODEL-01 / D-06): writes the
-- chosen effort symbol into the EXISTING metadata jsonb — the column exists since
-- 0005_conversations.up.sql, so NO migration is added (numbering unchanged). Mirrors
-- RenameConversationForIdentity: rows-affected==0 = the caller does NOT own the id
-- (foreign OR absent), routed through db.WithIdentityTx so the 0032 RLS owner policy
-- backstops a forgotten predicate. Parameterized jsonb_set (never string concat) — the
-- effort is a symbol validated upstream (plan 06). COALESCE seeds '{}' when metadata is
-- NULL so the first write on a metadata-less row succeeds.
UPDATE aura.conversations
SET metadata = jsonb_set(COALESCE(metadata, '{}'::jsonb), '{reasoning_effort}', to_jsonb(sqlc.arg(effort)::text), true)
WHERE id = sqlc.arg(id)
  AND identity_id = sqlc.arg(identity_id);
