-- name: ResolveChatTaskApproval :one
-- The answer and the task change share the caller's resume transaction. Return the reviewed
-- fields under lock so a mismatched question rolls back the transition and the pause.
WITH locked AS MATERIALIZED (
    -- A decision may wait here past the deadline. now() is fixed at transaction start.
    -- https://www.postgresql.org/docs/current/functions-datetime.html#FUNCTIONS-DATETIME-CURRENT
    SELECT t.id, t.status, t.kind, t.schedule_kind, t.cron_expr, t.every_minutes, t.tz,
        t.next_run_at, t.run_at, t.payload, t.origin_tool_call_id FROM aura.scheduler_tasks AS t
    WHERE t.id = sqlc.arg(task_id) AND t.identity_id = sqlc.arg(identity_id)
        AND t.origin_conversation_id = sqlc.arg(conversation_id)
        AND t.status IN ('pending_approval', sqlc.arg(new_status)::text) AND t.kind <> 'message'
    FOR UPDATE
), changed AS (
    UPDATE aura.scheduler_tasks AS t SET status = sqlc.arg(new_status)::text, updated_at = now()
    FROM locked
    WHERE t.id = locked.id AND locked.status = 'pending_approval'
        AND (sqlc.arg(new_status)::text = 'cancelled' OR locked.schedule_kind <> 'at' OR locked.next_run_at > clock_timestamp())
    RETURNING t.id
)
SELECT locked.kind, locked.schedule_kind, locked.cron_expr, locked.every_minutes, locked.tz,
    locked.next_run_at, locked.run_at, locked.payload, locked.origin_tool_call_id,
    (locked.status = sqlc.arg(new_status)::text)::boolean AS already_decided
FROM locked
WHERE locked.status = sqlc.arg(new_status)::text OR EXISTS (SELECT FROM changed);
