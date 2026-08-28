-- Source: 51-06a (SWARM-06 guard rails). Checkpoint decision (2026-08-28, resolved
-- `new-columns`): D-12's fencing id and D-13's level identity for a BACKGROUND
-- per-worker pause are NEW columns, not a widening of the shipped
-- proxied_from_child_id/proxied_tool_call_id pair.
--
-- Why not extend the shipped pair: proxied_from_child_id/proxied_tool_call_id describe a
-- SYNCHRONOUS, MODEL-RELAYED pause -- the parent re-offers a child's ask_user through its
-- OWN pause, and internal/agent/tools/ask_user.go's schema marks both fields "Optional,
-- model-discretionary": the MODEL decides what they say. owning_worker_id describes a
-- BACKGROUND pause a worker raises and owns directly -- HOST-written, never model-
-- supplied. Collapsing the two into one column would let a model forge a worker
-- attribution it did not earn (T-51-47); the CHECK constraint below is that mitigation,
-- not a tidiness preference.
ALTER TABLE aura.paused_states
    ADD COLUMN IF NOT EXISTS pending_action_id text,
    ADD COLUMN IF NOT EXISTS owning_worker_id  text;

COMMENT ON COLUMN aura.paused_states.pending_action_id IS
    'Fencing id (D-12), mirroring LibreChat ApprovalLifecycle''s flat pendingActionId: a '
    'resume must supply the SAME value or the conditional UPDATE '
    '(MarkPausedStateResumedFenced) matches zero rows -- a stale decision cannot resume a '
    'pause that has since moved on. A column, never a resume_context jsonb path: a nested '
    'JSON field cannot be compared inside a Postgres conditional UPDATE. NULL for every '
    'pre-migration row and every ordinary operator pause -- the fence is additive, never a '
    'new precondition on the shipped resume path.';

COMMENT ON COLUMN aura.paused_states.owning_worker_id IS
    'Level identity (D-13) for a BACKGROUND per-worker pause the worker itself owns and '
    'can be resumed into directly -- host-written, never model-supplied. AUTHORITATIVE '
    'for a background per-worker pause; proxied_from_child_id/proxied_tool_call_id stay '
    'AUTHORITATIVE for a SYNCHRONOUS model-relayed pause. The two never coexist on one '
    'row (see paused_states_worker_attribution_exclusive below), so a single accessor '
    '(askuser.Pending.WorkerID) can answer "which worker owns this pause" without a '
    'model-controlled field ever outranking a host-written one (T-51-47).';

-- Enforcement, not documentation: a row can be a background per-worker pause OR a
-- synchronous relayed pause, never both. inserting a row with both populated fails
-- closed rather than leaving two competing answers to "which worker owns this pause".
ALTER TABLE aura.paused_states
    ADD CONSTRAINT paused_states_worker_attribution_exclusive
    CHECK (owning_worker_id IS NULL OR proxied_from_child_id IS NULL);
