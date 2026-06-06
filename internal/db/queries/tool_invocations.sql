-- name: InsertToolInvocation :exec
INSERT INTO aura.tool_invocations (
    conversation_id, request_id, tool_call_id, tool_name, event_kind, seq, ts,
    started_at, ended_at, duration_ms,
    args_raw, args_bytes,
    status, error, result_preview, preview_bytes, result_bytes, result_truncated,
    result_sidecar_path, exit_code, meta
) VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    $8, $9, $10,
    $11, $12,
    $13, $14, $15, $16, $17, $18,
    $19, $20, $21
)
ON CONFLICT (conversation_id, request_id, tool_call_id, event_kind) DO NOTHING;

-- name: ListToolInvocationsByConversation :many
SELECT id, conversation_id, request_id, tool_call_id, tool_name, event_kind, seq, ts,
       started_at, ended_at, duration_ms,
       args_raw, args_bytes,
       status, error, result_preview, preview_bytes, result_bytes, result_truncated,
       result_sidecar_path, exit_code, meta
FROM aura.tool_invocations
WHERE conversation_id = $1
ORDER BY seq ASC, ts ASC, id ASC;
