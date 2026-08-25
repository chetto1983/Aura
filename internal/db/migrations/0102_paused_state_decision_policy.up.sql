-- Every persisted pause must carry a server-authored decision policy. Existing rows
-- predate that invariant, so normalize their context to an object and backfill the
-- exact decision set they supported when created. Runtime validation can then remain
-- fail-closed without a compatibility branch.
UPDATE aura.paused_states
SET resume_context = CASE
    WHEN jsonb_typeof(resume_context) = 'object' THEN resume_context
    ELSE '{}'::jsonb
END || '{"allowed_decisions":["accept","decline","cancel"]}'::jsonb;
