-- name: ApplianceWorkInFlight :one
-- What the host updater waits on before it restarts Aura on its own, system-wide half: the
-- newest tool event and job heartbeat, and the work in flight right now. Neither table
-- carries row security (the gateway reconciler reads tool_invocations the same way).
-- Measured on 1M synthetic tool rows (2026-09-24): both tool legs stay under 1 ms through the
-- (tool_name, ts DESC) index by Postgres 18 skip scan, so they need no index of their own.
-- The start-without-end shape is ListInFlightToolInvocationsBefore's; the reconciler closes
-- orphaned starts, and the hour bound keeps one it missed from pinning live_runs above zero
-- forever. A job run counts only while its heartbeat is fresh.
SELECT
    GREATEST(
        (SELECT max(t.ts) FROM aura.tool_invocations t WHERE t.ts > now() - interval '1 hour'),
        (SELECT max(j.last_heartbeat_at) FROM aura.agent_job_runs j WHERE j.status = 'running')
    )::timestamptz AS last_work_at,
    ((SELECT count(*)
      FROM aura.tool_invocations s
      WHERE s.event_kind = 'start'
        AND s.ts > now() - interval '1 hour'
        AND NOT EXISTS (
            SELECT 1 FROM aura.tool_invocations e
            WHERE e.conversation_id = s.conversation_id
              AND e.request_id = s.request_id
              AND e.tool_call_id = s.tool_call_id
              AND e.event_kind = 'end'))
     + (SELECT count(*)
        FROM aura.agent_job_runs j
        WHERE j.status = 'running'
          AND j.last_heartbeat_at > now() - interval '5 minutes'))::int AS live_runs;

-- name: LatestConversationActivity :one
-- The per-identity half, run inside WithIdentityTx: conversations are owner-only (migration
-- 0089), so a table-wide max from the daemon pool reads NULL. last_active_at moves with every
-- turn (equal to the newest conversation_turns.created_at on the lab VM, 2026-09-24), so a
-- chat that calls no tool still counts and the turns table is never scanned.
SELECT max(c.last_active_at)::timestamptz AS last_active_at
FROM aura.conversations c;
