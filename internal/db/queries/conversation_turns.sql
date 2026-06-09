-- name: InsertConversationTurn :exec
INSERT INTO aura.conversation_turns (
    conversation_id, seq, role, content, content_sidecar_path,
    tool_call_id, tool_calls, input_tokens, output_tokens, cached_tokens
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: LockConversationForTurnAppend :one
SELECT id
FROM aura.conversations
WHERE id = $1
FOR UPDATE;

-- name: NextConversationTurnSeq :one
SELECT (COALESCE(MAX(seq), 0) + 1)::int AS seq
FROM aura.conversation_turns
WHERE conversation_id = $1;

-- name: ListTurnsBySeq :many
SELECT conversation_id, seq, role, content, content_sidecar_path,
       tool_call_id, tool_calls, created_at, input_tokens, output_tokens, cached_tokens
FROM aura.conversation_turns
WHERE conversation_id = $1
ORDER BY seq ASC;

-- name: CountTurns :one
SELECT count(*) AS turn_count
FROM aura.conversation_turns
WHERE conversation_id = $1;

-- name: SearchConversationTurns :many
-- LOCKED cross-slice contract (D-A5-03 / SPEC Req#13). Telegram /search (Phase 13)
-- reuses this EXACT query; only the excerpt rendering differs per channel.
SELECT conversation_id, seq, content, similarity(content, $1) AS sim
FROM aura.conversation_turns
WHERE content % $1
ORDER BY similarity(content, $1) DESC
LIMIT $2;
