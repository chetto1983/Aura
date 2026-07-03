-- name: InsertToolInvocation :execrows
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

-- name: GetToolInvocationEnd :one
SELECT id, conversation_id, request_id, tool_call_id, tool_name, event_kind, seq, ts,
       started_at, ended_at, duration_ms,
       args_raw, args_bytes,
       status, error, result_preview, preview_bytes, result_bytes, result_truncated,
       result_sidecar_path, exit_code, meta
FROM aura.tool_invocations
WHERE conversation_id = $1 AND request_id = $2 AND tool_call_id = $3 AND event_kind = 'end';

-- name: ListInFlightToolInvocationsBefore :many
SELECT s.id, s.conversation_id, s.request_id, s.tool_call_id, s.tool_name, s.event_kind, s.seq, s.ts,
       s.started_at, s.ended_at, s.duration_ms,
       s.args_raw, s.args_bytes,
       s.status, s.error, s.result_preview, s.preview_bytes, s.result_bytes, s.result_truncated,
       s.result_sidecar_path, s.exit_code, s.meta
FROM aura.tool_invocations s
WHERE s.event_kind = 'start'
  AND s.started_at < $1
  AND NOT EXISTS (
      SELECT 1 FROM aura.tool_invocations e
      WHERE e.conversation_id = s.conversation_id
        AND e.request_id = s.request_id
        AND e.tool_call_id = s.tool_call_id
        AND e.event_kind = 'end'
  )
ORDER BY s.started_at ASC, s.id ASC;
