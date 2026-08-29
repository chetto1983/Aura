-- Remove the three runtime compatibility modes superseded by PRD Amendment #176.
-- Existing rows are upgraded before the constraints land; new rows must name their
-- route, durable owner and (for delegation results) fan-out explicitly.

UPDATE aura.scheduler_tasks
SET notify_route = 'none'
WHERE notify_route IS NULL OR btrim(notify_route) = '';

ALTER TABLE aura.scheduler_tasks
    ALTER COLUMN notify_route SET NOT NULL,
    ADD CONSTRAINT scheduler_tasks_notify_route_chk
        CHECK (notify_route IN ('none', 'stdout', 'telegram', 'whatsapp', 'email'));

UPDATE aura.pending_notifications AS n
SET notify_route = COALESCE(NULLIF(btrim(n.notify_route), ''), t.notify_route),
    identity_id = COALESCE(NULLIF(btrim(n.identity_id), ''), t.identity_id)
FROM aura.agent_job_runs AS r
JOIN aura.scheduler_tasks AS t ON t.id = r.task_id
WHERE n.run_id = r.id;

UPDATE aura.pending_notifications AS n
SET notify_route = COALESCE(NULLIF(btrim(n.notify_route), ''), 'stdout'),
    identity_id = COALESCE(NULLIF(btrim(n.identity_id), ''), s.identity_id::text)
FROM aura.steer_queue AS s
WHERE n.steer_queue_id = s.id;

ALTER TABLE aura.pending_notifications
    DROP CONSTRAINT pending_notifications_owner_chk,
    ALTER COLUMN notify_route SET NOT NULL,
    ALTER COLUMN identity_id SET NOT NULL,
    ADD CONSTRAINT pending_notifications_notify_route_chk
        CHECK (notify_route IN ('none', 'stdout', 'telegram', 'whatsapp', 'email')),
    ADD CONSTRAINT pending_notifications_owner_chk
        CHECK (num_nonnulls(run_id, steer_queue_id) = 1);

COMMENT ON COLUMN aura.pending_notifications.notify_route IS
    'Amendment #176: explicit retry destination; never NULL and never inferred from process configuration.';
COMMENT ON COLUMN aura.pending_notifications.identity_id IS
    'Amendment #176: mandatory owning identity snapshot recovered from the row''s exactly-one durable owner.';
COMMENT ON CONSTRAINT pending_notifications_owner_chk ON aura.pending_notifications IS
    'Amendment #176: exactly one cascade-deleting owner, scheduler run XOR steer row.';

UPDATE aura.steer_queue
SET fanout_key = 'f-' || substr(md5(id::text), 1, 16)
WHERE kind = 'delegation_result'
  AND (fanout_key IS NULL OR btrim(fanout_key) = '');

ALTER TABLE aura.steer_queue
    ADD CONSTRAINT steer_queue_fanout_key_chk CHECK (
        (kind = 'delegation_result' AND fanout_key IS NOT NULL AND btrim(fanout_key) <> '')
        OR (kind <> 'delegation_result' AND fanout_key IS NULL)
    );

COMMENT ON CONSTRAINT steer_queue_fanout_key_chk ON aura.steer_queue IS
    'Amendment #176: every delegation result belongs to exactly one explicit fan-out; '
    'all other steer rows carry no fan-out key.';
