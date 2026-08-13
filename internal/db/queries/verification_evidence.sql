-- Coding verification evidence ledger (migration 0094).
--
-- Ported from NousResearch/hermes-agent `agent/verification_evidence.py` (MIT), whose
-- store is SQLite. The statements below are the same operations against Postgres; the
-- invariant they maintain is the module's whole point:
--
--   a passing verification event CLEARS last_edit_at;
--   an edit SETS it;
--   so "was this workspace edited after its last passing verification" is one column.

-- name: InsertVerificationEvent :one
INSERT INTO aura.verification_events (
    identity_id, session_id, cwd, root, command, canonical_command,
    kind, scope, status, exit_code, output_summary
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING id, created_at;

-- name: UpsertVerificationStateVerified :exec
-- The clearing half of the invariant: recording an event makes this workspace the
-- verified one and drops the edit marker with the paths it carried.
INSERT INTO aura.verification_state (
    identity_id, session_id, root, last_event_id, last_edit_at, changed_paths
) VALUES ($1, $2, $3, $4, NULL, '[]'::jsonb)
ON CONFLICT (identity_id, session_id, root) DO UPDATE SET
    last_event_id = excluded.last_event_id,
    last_edit_at  = NULL,
    changed_paths = '[]'::jsonb;

-- name: UpsertVerificationStateEdited :exec
-- The setting half: an edit stales whatever evidence existed. last_event_id is kept so
-- the status read can still name the command that last passed, which is what makes the
-- nudge say "last command X" instead of a bare "unverified".
INSERT INTO aura.verification_state (
    identity_id, session_id, root, last_event_id, last_edit_at, changed_paths
) VALUES ($1, $2, $3, NULL, now(), $4)
ON CONFLICT (identity_id, session_id, root) DO UPDATE SET
    last_edit_at  = now(),
    changed_paths = excluded.changed_paths;

-- name: GetVerificationState :one
SELECT s.last_event_id, s.last_edit_at, s.changed_paths,
       e.command, e.canonical_command, e.kind, e.scope, e.status,
       e.exit_code, e.output_summary, e.created_at
FROM aura.verification_state s
LEFT JOIN aura.verification_events e ON e.id = s.last_event_id
WHERE s.identity_id = $1 AND s.session_id = $2 AND s.root = $3;

-- name: DeleteSupersededVerificationEvents :exec
-- Bound ledger growth per session+root, keeping the newest $4 events. The row the
-- state points at survives because it is always among the newest.
DELETE FROM aura.verification_events AS victim
WHERE victim.identity_id = $1 AND victim.session_id = $2 AND victim.root = $3
  AND victim.id NOT IN (
      SELECT keep.id FROM aura.verification_events AS keep
      WHERE keep.identity_id = $1 AND keep.session_id = $2 AND keep.root = $3
      ORDER BY keep.id DESC
      LIMIT $4
  );

-- name: DeleteExpiredVerificationState :exec
-- Retention, mirroring the original's two-branch prune: state that was edited before
-- the cutoff, and state whose only anchor is an event older than the cutoff.
DELETE FROM aura.verification_state
WHERE (last_edit_at IS NOT NULL AND last_edit_at < $1)
   OR (last_edit_at IS NULL AND last_event_id IN (
        SELECT id FROM aura.verification_events WHERE created_at < $1
   ));

-- name: DeleteExpiredVerificationEvents :exec
DELETE FROM aura.verification_events
WHERE created_at < $1
  AND id NOT IN (
      SELECT last_event_id FROM aura.verification_state WHERE last_event_id IS NOT NULL
  );
