-- Per-identity tool policies, the narrowing counterpart of the approval grants (prd.md §5,
-- "A tool policy per identity", 2026-10-09).
--
-- Measured before this table existed: the gateway's three scopes (once, session, always)
-- all WIDEN what an agent may do. An operator could stop being asked, and could not ask to
-- be asked more, nor forbid one tool. The tier of an MCP tool comes from the server's own
-- annotations or a recipe table, so a server that declares a write read-only ran it
-- ungated and unreserved, and nothing let the operator correct that one tool.
--
-- Two values. `ask` routes the subject to approval whatever its tier and takes it through
-- the reservation funnel; `deny` refuses the call at Decide with a reason the model reads.
-- Precedence in the gateway: deny, then ask, then grant, then tier.
--
-- Keyed exactly like aura.gateway_approval_grants (0099): identity + tool + the verb of an
-- action-multiplexed tool, '' for every other tool, so a policy on "calendar send_email"
-- does not touch "calendar delete_event". NOT NULL DEFAULT '' rather than nullable for the
-- same reason as there: '' is a real coordinate and a NULL in a primary key is not a key.

CREATE TABLE aura.gateway_tool_policies (
    identity_id uuid        NOT NULL REFERENCES aura.identities (id) ON DELETE CASCADE,
    tool        text        NOT NULL,
    action      text        NOT NULL DEFAULT '',
    policy      text        NOT NULL CHECK (policy IN ('ask', 'deny')),
    set_at      timestamptz NOT NULL DEFAULT now(),
    -- The capability-layer principal that set it (the same attribution aura.settings and
    -- the audit tables use), NOT a raw auth-provider user id. NULL for a row seeded outside
    -- the cockpit and the CLI.
    set_by      text,
    PRIMARY KEY (identity_id, tool, action)
);

GRANT SELECT, INSERT, UPDATE, DELETE ON aura.gateway_tool_policies TO aura_app;
GRANT ALL                            ON aura.gateway_tool_policies TO aura_migrate;

-- Fail-closed RLS, the 0087 pair. A policy is a statement by ONE principal about their own
-- agent; a connection that has not said whose policies it means must see none. The
-- permissive owner policy carries no `IS NULL OR` escape, and the restrictive policy makes
-- the failure mode of any future permissive policy "too few rows", never "all rows".
ALTER TABLE aura.gateway_tool_policies ENABLE ROW LEVEL SECURITY;

CREATE POLICY gateway_tool_policies_owner_isolation ON aura.gateway_tool_policies
    USING (identity_id = NULLIF(current_setting('app.current_identity', true), '')::uuid);

CREATE POLICY gateway_tool_policies_require_identity ON aura.gateway_tool_policies
    AS RESTRICTIVE FOR ALL TO aura_app
    USING (current_setting('app.current_identity', true) IS NOT NULL
           AND current_setting('app.current_identity', true) <> '');

COMMENT ON TABLE aura.gateway_tool_policies IS
    'Per-identity ask/deny policy per tool + multiplexed action (prd.md §5, 2026-10-09). Outranks grants and tiers; set with `aura gateway policy` or /api/approvals/policies.';
