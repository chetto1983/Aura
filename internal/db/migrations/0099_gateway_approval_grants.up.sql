-- Durable "always approve" grants for the tool gateway (PRD amendment #127).
--
-- Measured before this table existed: every destructive tool call was withheld every time.
-- GatewayApprovals.Consume DELETES the approval as it reads it, and keys it on the
-- canonical args fingerprint, so the same verb on a different object was a fresh
-- interruption. Three self-targeted actions in one operator session cost three separate
-- approvals, and a fourth call would have cost a fourth. The gateway had exactly one
-- approval scope, `once`, and had it by construction rather than by choice.
--
-- This table is the only one of the three scopes that needs to survive a restart. `once`
-- and `session` stay in the gateway's memory, where losing them to a restart costs the
-- operator one extra approval -- the right failure. `always` is the scope an operator
-- expects to still be there tomorrow.
--
-- WHY NOT aura.capability_grants, which already exists and already has a CLI and a cockpit
-- panel: HasCapability resolves `capability = '*' OR capability = $2`, and the operator
-- identity carries the seeded '*' grant from 0004. A scope permission written there would
-- read as TRUE before anyone approved anything -- the gate would ship open. Reading it with
-- ListCapabilities to dodge the wildcard would work and would be worse: two readers of one
-- table with two different meanings, and the next caller to reach for HasCapability reopens
-- the gate without noticing.
--
-- `action` is the verb of an action-multiplexed tool (calendar, skill_manage, task) and ''
-- for every other tool, so a grant on "calendar delete_event" does NOT cover
-- "calendar send_email". NOT NULL DEFAULT '' rather than nullable: '' is a real coordinate
-- here ("this tool has no verb"), and a NULL in a primary key is not a key at all.

CREATE TABLE aura.gateway_approval_grants (
    identity_id uuid        NOT NULL REFERENCES aura.identities (id) ON DELETE CASCADE,
    tool        text        NOT NULL,
    action      text        NOT NULL DEFAULT '',
    granted_at  timestamptz NOT NULL DEFAULT now(),
    -- The capability-layer principal that granted it (the same attribution aura.settings
    -- and the audit tables use), NOT a raw auth-provider user id. NULL for a grant seeded
    -- outside the cockpit.
    granted_by  text,
    PRIMARY KEY (identity_id, tool, action)
);

GRANT SELECT, INSERT, UPDATE, DELETE ON aura.gateway_approval_grants TO aura_app;
GRANT ALL                            ON aura.gateway_approval_grants TO aura_migrate;

-- Fail-closed RLS, the 0087 pair. A grant is a statement by ONE principal about their own
-- agent; a connection that has not said whose grants it means must see none. The permissive
-- owner policy carries no `IS NULL OR` escape, and the restrictive policy makes the failure
-- mode of any future permissive policy "too few rows", never "all rows".
ALTER TABLE aura.gateway_approval_grants ENABLE ROW LEVEL SECURITY;

CREATE POLICY gateway_approval_grants_owner_isolation ON aura.gateway_approval_grants
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);

CREATE POLICY gateway_approval_grants_require_identity ON aura.gateway_approval_grants
    AS RESTRICTIVE FOR ALL TO aura_app
    USING (current_setting('app.current_identity', true) IS NOT NULL
           AND current_setting('app.current_identity', true) <> '');

COMMENT ON TABLE aura.gateway_approval_grants IS
    'Durable "always approve" grants per identity + tool + multiplexed action (amendment #127). Revoked with `aura gateway grants revoke`.';
