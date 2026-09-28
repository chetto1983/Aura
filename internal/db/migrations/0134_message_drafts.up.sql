-- Owner-scoped, one-shot review for the two trusted outbound text actions.
-- Slot measured with `ls internal/db/migrations/ | tail -1`: 0133 was the head.
-- Full arguments stay server-side; only an owner-only review API may project them.
CREATE TABLE aura.message_drafts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  identity_id uuid NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE,
  conversation_id text NOT NULL,
  tool_call_id text NOT NULL,
  recipe text NOT NULL CHECK (recipe IN ('recipe:calendar', 'recipe:whatsapp')),
  tool_name text NOT NULL,
  registered_tool_name text NOT NULL,
  action text NOT NULL,
  original_args jsonb NOT NULL CHECK (jsonb_typeof(original_args) = 'object'),
  original_fingerprint text NOT NULL CHECK (length(original_fingerprint) = 64),
  effective_args jsonb CHECK (effective_args IS NULL OR jsonb_typeof(effective_args) = 'object'),
  effective_fingerprint text CHECK (effective_fingerprint IS NULL OR length(effective_fingerprint) = 64),
  status text NOT NULL DEFAULT 'pending' CHECK (status IN
    ('pending', 'dispatching', 'sent', 'failed', 'declined', 'uncertain', 'expired')),
  outcome_code text CHECK (outcome_code IS NULL OR length(outcome_code) <= 80),
  expires_at timestamptz NOT NULL,
  dispatch_started_at timestamptz,
  resolved_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (identity_id, conversation_id, tool_call_id),
  CHECK ((recipe = 'recipe:calendar' AND tool_name = 'calendar' AND action = 'send_email') OR
         (recipe = 'recipe:whatsapp' AND tool_name = 'send_message' AND action = '')),
  CHECK (length(registered_tool_name) BETWEEN 4 AND 64 AND
         position('__' in registered_tool_name) > 1 AND
         registered_tool_name ~ '^[A-Za-z0-9_-]+$'),
  CHECK ((status NOT IN ('dispatching', 'sent', 'failed', 'uncertain')) OR
         (effective_args IS NOT NULL AND effective_fingerprint IS NOT NULL AND dispatch_started_at IS NOT NULL)),
  CHECK ((status NOT IN ('sent', 'failed', 'declined', 'uncertain', 'expired')) OR resolved_at IS NOT NULL)
);

CREATE INDEX message_drafts_pending_idx ON aura.message_drafts
  (identity_id, conversation_id, created_at, id) WHERE status = 'pending';
CREATE INDEX message_drafts_recovery_idx ON aura.message_drafts
  (dispatch_started_at, id) WHERE status = 'dispatching';

GRANT SELECT, INSERT, UPDATE, DELETE ON aura.message_drafts TO aura_app;
GRANT ALL ON aura.message_drafts TO aura_migrate;

ALTER TABLE aura.message_drafts ENABLE ROW LEVEL SECURITY;
CREATE POLICY message_drafts_owner_isolation ON aura.message_drafts
  USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid)
  WITH CHECK (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);
CREATE POLICY message_drafts_require_identity ON aura.message_drafts
  AS RESTRICTIVE FOR ALL TO aura_app
  USING (NULLIF(current_setting('app.current_identity', true), '') IS NOT NULL)
  WITH CHECK (NULLIF(current_setting('app.current_identity', true), '') IS NOT NULL);
