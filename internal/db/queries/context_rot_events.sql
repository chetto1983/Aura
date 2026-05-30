-- name: InsertContextRotEvent :exec
INSERT INTO aura.context_rot_events (
    conversation_id, action, pairs_dropped, tokens_before, tokens_after
) VALUES ($1, $2, $3, $4, $5);

-- name: ListContextRotEvents :many
SELECT ts, conversation_id, action, pairs_dropped, tokens_before, tokens_after
FROM aura.context_rot_events
WHERE conversation_id = $1
ORDER BY ts ASC;
