-- The durable record of a tool call parked for the operator's approval: one row per parked
-- call, keyed on its pause token, carrying the exact arguments the accept will run.
-- Slot measured with `ls internal/db/migrations/ | tail -1`: 0159 was the head.
-- Spec: docs/superpowers/specs/2026-11-27-unified-tool-approval-design.md.
--
-- WHY A TABLE, NOT paused_states.resume_context. The context is streamed to the surfaces, the
-- model can write one through ask_user, the record needs compare-and-set states, and
-- CleanupResumedOlderThan deletes resumed pause rows while a record must outlive its pause.
-- So there is no FK on pause_token either.
--
-- The key. assistant_seq is the seq of the parked assistant turn. The same tool_call_id may
-- park again in a later round, so the call id alone is never unique; the turn's slot is.
--
-- The states: pending -> accepted | declined | expired | not_run | cancelled;
-- accepted -> running | not_run | cancelled; running -> done | unknown | not_run.
--
-- The conversation FK is not identity-scoped (aura.conversations has no unique (id, identity_id));
-- the only writer is the runner's CommitPause, in the conversation owner's identity transaction.
CREATE TABLE aura.tool_approvals (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  identity_id uuid NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE,
  conversation_id uuid NOT NULL REFERENCES aura.conversations(id) ON DELETE CASCADE,
  pause_token uuid NOT NULL UNIQUE,
  assistant_seq integer NOT NULL,
  call_index integer NOT NULL CHECK (call_index >= 0),
  tool_call_id text NOT NULL CHECK (length(tool_call_id) BETWEEN 1 AND 128),
  tool_name text NOT NULL CHECK (length(tool_name) BETWEEN 1 AND 128),
  source text NOT NULL CHECK (source IN ('tool', 'gateway')),
  execute_args jsonb NOT NULL CHECK (jsonb_typeof(execute_args) = 'object'),
  execute_fingerprint text NOT NULL CHECK (length(execute_fingerprint) = 64),
  -- review_binding holds a filesystem path (tracked-cwd:<dir>), hence its 4096 bound.
  review_binding text NOT NULL DEFAULT '' CHECK (length(review_binding) <= 4096),
  ran_note text NOT NULL DEFAULT '' CHECK (length(ran_note) <= 4096),
  brief text NOT NULL DEFAULT '' CHECK (length(brief) <= 2048),
  grant_tool text NOT NULL DEFAULT '' CHECK (length(grant_tool) <= 128),
  grant_action text NOT NULL DEFAULT '' CHECK (length(grant_action) <= 128),
  status text NOT NULL DEFAULT 'pending' CHECK (status IN
    ('pending', 'accepted', 'running', 'done', 'unknown', 'declined', 'expired', 'cancelled', 'not_run')),
  approval_scope text CHECK (approval_scope IS NULL OR approval_scope IN ('once', 'session', 'always')),
  operator_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  decided_at timestamptz,
  started_at timestamptz,
  finished_at timestamptz,
  UNIQUE (conversation_id, assistant_seq, call_index),
  CHECK ((status = 'pending') = (decided_at IS NULL)),
  CHECK (status NOT IN ('running', 'done', 'unknown') OR started_at IS NOT NULL)
);

CREATE INDEX tool_approvals_open_idx ON aura.tool_approvals
  (identity_id, conversation_id, assistant_seq, call_index) WHERE status IN ('pending', 'accepted', 'running');

GRANT SELECT, INSERT, UPDATE, DELETE ON aura.tool_approvals TO aura_app;
GRANT ALL ON aura.tool_approvals TO aura_migrate;

ALTER TABLE aura.tool_approvals ENABLE ROW LEVEL SECURITY;
CREATE POLICY tool_approvals_owner_isolation ON aura.tool_approvals
  USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid)
  WITH CHECK (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);
CREATE POLICY tool_approvals_require_identity ON aura.tool_approvals
  AS RESTRICTIVE FOR ALL TO aura_app
  USING (NULLIF(current_setting('app.current_identity', true), '') IS NOT NULL)
  WITH CHECK (NULLIF(current_setting('app.current_identity', true), '') IS NOT NULL);

-- THE REVERT GUARD. CheckMigrationHead (internal/db/migration_head.go) refuses to boot unless
-- the database's version equals the binary's newest embedded migration, so any release that can
-- still boot on a database carrying this table embeds this migration, and with it this trigger.
-- A release that reverts the unified approval Go code keeps 0160 for that reason, and its
-- generic resolve would mark a tool_approval pause resumed and answer the parked call as if it
-- had run. The trigger refuses that resume while the pause's record is still pending: the
-- resolve fails loudly and the record stays pending. The current code decides the record before
-- it marks the pause resumed, in the same transaction, so it passes.
--
-- SECURITY DEFINER, as aura.conversation_owner (0103) is: the function runs as aura_migrate,
-- which owns aura.tool_approvals and so is not filtered by its RLS; search_path is pinned per
-- the PostgreSQL manual, "Writing SECURITY DEFINER Functions Safely".
-- No GRANT EXECUTE: PostgreSQL checks EXECUTE on a trigger function at CREATE TRIGGER, not when it fires (measured: 55000 raised as aura_app).
CREATE FUNCTION aura.refuse_resume_of_pending_tool_approval() RETURNS trigger
    LANGUAGE plpgsql
    SECURITY DEFINER
    SET search_path = pg_catalog, aura
    AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM aura.tool_approvals WHERE pause_token = OLD.token AND status = 'pending') THEN
        RAISE EXCEPTION 'tool approval pause % still has a pending record: decide the record before resuming the pause', OLD.token
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    RETURN NEW;
END;
$$;

REVOKE ALL ON FUNCTION aura.refuse_resume_of_pending_tool_approval() FROM PUBLIC;

CREATE TRIGGER paused_states_refuse_pending_tool_approval_trg
    BEFORE UPDATE OF resumed_at ON aura.paused_states
    FOR EACH ROW
    WHEN (OLD.resumed_at IS NULL AND NEW.resumed_at IS NOT NULL
          AND OLD.resume_context->>'type' = 'tool_approval')
    EXECUTE FUNCTION aura.refuse_resume_of_pending_tool_approval();
