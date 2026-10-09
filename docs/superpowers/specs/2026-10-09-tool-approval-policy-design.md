# Tool approval policy per identity: `ask` and `deny`

Proposed on 2026-10-09 at `1c872a1`, after reading OpenDots' per-Dot tool toggles (CopilotKit,
MIT, released 2026-10-01) against Aura's tool gateway (prd.md §5, "A tool policy per identity").
Not yet agreed with the operator: every decision below is a proposal for review, and the PRD
paragraph records a reading of the tree, not a live run.

OpenDots lets the owner turn any MCP tool off for one Dot and set "Ask first" on any tool; a
tool the server does not mark read-only starts with "Ask first" on, and the docs say plainly
that the hint comes from the server and is only a hint. Aura's gateway sees every tool, built-in
and MCP, and classifies each call. What it lacks is the operator's side of the same decision.

## The problem, measured in the tree

- `gated` (`internal/gateway/decide.go`) stops a turn for the Destructive tier only, by a
  measured decision: prompts on the expected case trained the answer "yes".
- A read-only call under a strict profile is recorded as a decision fact and never reserved
  (`Decide`, the `!spec.Mutating` branch).
- The three scopes, `once`, `session`, `always` (`internal/gateway/scope.go`, amendment #127),
  all widen. The only durable per-identity row the gateway reads is the `always` grant
  (`aura.gateway_approval_grants`, migration 0099, RLS pair from 0087).
- An MCP tool's tier comes from the server's annotations or a recipe table
  (`internal/agent/mcptools/bridge_risk.go`): a server that declares a write read-only runs it
  ungated and unreserved. The recipe tables correct Aura's own sidecars; nothing corrects a
  stranger's server, and nothing is per identity.
- The registry toggles whole servers and profiles (`aura mcp profile`,
  `POST /api/governance/mcp/{name}/enable|disable`), never one tool. `aura mcp tools <name>`
  lists tools and sets nothing.
- No file in `internal/gateway`, `internal/mcpregistry`, `cmd/aura` or `web/src` names a
  per-tool policy (`grep -rli "tool_polic\|toolpolic"` hits only the reasoning-disclosure
  table in `internal/runner/runner_reasoning_graph.go`, unrelated).

Not measured: how a model behaves when a tool it was offered answers "disabled by policy", and
the cost in prompts of `ask` on a busy read-only tool. Both are acceptance items below.

## Decisions (proposed)

- **Two values, one table.** `ask` and `deny`, keyed exactly like a grant: identity, tool,
  multiplexed action. The subject is `subjectFor(spec, rawArgs)`, the same function grants use,
  so "calendar send_email" and "calendar" are different subjects here too.
- **Every tool, not only MCP.** The gateway classifies built-ins as well; a policy on
  `shell_exec` or `write_file` costs nothing extra. OpenDots is MCP-only because its chat tools
  are not gated at all.
- **`deny` refuses at `Decide`.** The verdict is `Deny` with reason `policy`; the model receives
  the existing `*ErrDenied` with a message that names the subject and says not to retry. No
  reservation, one decision fact with `gateway_policy: deny`.
- **`ask` forces the approval funnel.** Whatever the tier, the subject is routed to
  `routeApprove`; the call is marked mutating so it is reserved and recorded like any gated
  call; the prompt's label table omits `always`, because the operator asked to be asked.
- **Precedence: deny, then ask, then grant, then tier.** A standing `always` grant does not
  bypass `ask`. Setting `ask` on a subject also revokes its `always` grant, so one state is
  stored, not two that contradict each other.
- **A policy read that fails falls to the prompt**, as `alwaysGranted` does: the operator sees
  the call rather than the gateway deciding on a store error. A `deny` subject under a failed
  read is therefore prompted, not run, which is the fail-closed direction.
- **Strict profiles only.** `Decide` is a no-op under dev and local_trusted and stays so.
- **Binds at the next decision.** A running call is not interrupted; OpenDots stops the active
  turn on a settings change and Aura does not need to.
- **Headless runs inherit the headless posture.** An `ask` subject in a cron job or a swarm
  child meets `routeApprove`'s no-responder deny-with-guidance (D-03a), and the scheduled-task
  approval-on-channel flow where it is wired. Nothing new.
- **The identity writes its own policies.** Same authority as grants: a statement by a principal
  about their own agent, RLS-bound. An admin imposing a policy on another identity is a
  capability-layer question and is out of scope. No agent tool writes a policy.
- **Refuse at `Decide`, do not hide from the manifest, in this first release.** Hiding a denied
  tool from `tool_search` would save the model a round; it is also a per-identity manifest, which
  the registry does not have. Measured first on the lab VM (acceptance 4), decided after.

## Shape

### Migration and queries

The slot is the next free number when the task runs: `ls internal/db/migrations/ | tail -1`
printed `0139_scheduler_task_transient_retries.up.sql` on 2026-10-09, so the slot is `0140`
today. Re-run the command first.

```sql
CREATE TABLE aura.gateway_tool_policies (
    identity_id uuid        NOT NULL REFERENCES aura.identities (id) ON DELETE CASCADE,
    tool        text        NOT NULL,
    action      text        NOT NULL DEFAULT '',
    policy      text        NOT NULL CHECK (policy IN ('ask', 'deny')),
    set_at      timestamptz NOT NULL DEFAULT now(),
    set_by      text,
    PRIMARY KEY (identity_id, tool, action)
);
```

Grants, the RLS pair and the table comment copy migration 0099 line for line, with the same
reasoning: a connection that has not said whose policies it means must see none. Queries in
`internal/db/queries/gateway_tool_policies.sql`: `SetGatewayToolPolicy` (upsert),
`GetGatewayToolPolicy`, `ListGatewayToolPolicies`, `ClearGatewayToolPolicy` (`:execrows`).
`sqlc generate` and commit `internal/db/sqlc/` with it.

### Store, `internal/approvalpolicies`

A sibling of `internal/approvalgrants` (174 lines) with the same shape: `Store{pool, q}`,
`withIdentity` binding `app.current_identity`, `Set`, `Get`, `List`, `Clear`, and
`approvalgrants.Subject` for the rendered label so a policy reads exactly like the grant it
overrides. A new package rather than a second table in `approvalgrants`, because that package
says "the durable half of the approval scopes" and a `deny` is not a scope.

### Gateway

- `grants.go` gains the sibling seam:

```go
type policyStore interface {
    Get(ctx context.Context, identityID, tool, action string) (Policy, bool, error)
}
func (g *Gateway) SetPolicyStore(s policyStore)
```

- `decide.go` (119 lines), after `classify` and before the read-only early return:

```go
policy := g.policyFor(ctx, identityctx.IdentityID(ctx), subjectFor(spec, rawArgs))
switch policy {
case PolicyDeny:
    g.recordDecisionFact(..., Deny, "policy")
    return Verdict{Decision: Deny, Tier: tier, Reason: "policy"}, nil
case PolicyAsk:
    spec.Mutating = true
}
...
if gated(tier) || policy == PolicyAsk { v, err := g.routeApprove(ctx, spec, tier, rawArgs, key, policy) ... }
```

- `routeApprove` (`approve.go`, 265 lines) takes the policy: with `PolicyAsk` it skips
  `alwaysGranted` and builds the label table without `always`. The reservation's `Meta` carries
  `gateway_policy` beside `approval_scope`, so the audit view shows which rule did it.
- `policyFor` mirrors `alwaysGranted`: nil store, empty identity or a store error returns
  `PolicyAsk` with one `slog.Warn`, never `""`.

`approve.go` is at 265 lines and `decide.go` at 119; neither needs a split. If `approve.go`
crosses 300 on touch, the label table moves to `scope.go`.

### Surfaces

- API, in `internal/agui/approvals_api.go` (356 lines; the policy handlers go in a new
  `approval_policies_api.go`): `GET /api/approvals/policies`, `PUT /api/approvals/policies`
  with `{tool, action, policy}`, `POST /api/approvals/policies/clear` with `{tool, action}`,
  all own-identity through `scopedIdentityID`. A `PUT` of `ask` also calls the grants store's
  revoke for the subject, after the policy is written: a failure between the two leaves a
  policy and a grant, and precedence makes the policy win.
- CLI, `cmd/aura/gateway_policy.go`, mirroring `gateway_grants.go`:
  `aura gateway policy {list <identity>|set <identity> <tool> [action] ask|deny|clear <identity> <tool> [action]}`.
  `runGateway` grows a `policy` branch.
- Cockpit: `web/src/settings/StandingApprovalsPanel.tsx` gains a "Policies" block under the
  grants, with a list and a form (tool, optional action, ask/deny). The tool name is typed in
  this release; it is what `tool_search` and `aura mcp tools` print. A per-server tool list
  with toggles in `McpServerDetail` is the OpenDots surface and a follow-up: the probe reports
  `tool_count` today and would have to return names.

## Errors

| Where | Condition | Answer |
|---|---|---|
| `Decide` | subject has `deny` | `*ErrDenied{Reason: "policy"}`; message: `tool "<subject>" is disabled for this identity by policy: do not retry it, tell the operator` |
| `Decide` | policy read fails | one warning, the call is prompted as `ask` |
| API `PUT` | policy not in `ask`, `deny` | `400 invalid_policy` |
| API `PUT` | unknown tool name | accepted: a policy may precede the server's mount, as a grant may |
| CLI | identity name unknown | the grants command's existing message |

## Observability

- Decision facts and reservations carry `gateway_policy` in `Meta`.
- `set_by` on every row, as `granted_by` on a grant. The admin audit feed's UNION legs
  (`internal/agui/audit_store.go`) are not extended in this release; the reservation ledger
  already shows every call a policy affected.
- One counter in the obs catalog, `gateway_policy_decisions`, attribute `policy` in
  `ask | deny`.

## Testing

Per prd.md §18 and CLAUDE.md: realistic fixtures, race detector, `db_integration` tier that
`t.Fatal`s under `$CI` when its env is unset, coverage floor 85% across the tag matrix,
mutation ≥70% on the critical files.

- `internal/gateway` unit: `deny` returns `ErrDenied` and writes no reservation; `ask` on a
  read-only tool reaches `routeApprove` and reserves on accept; the label table under `ask`
  has no `always`; an `always` grant does not bypass `ask`; a failing policy read prompts; the
  dev profile ignores policies. Mutation on `decide.go` and the new `policyFor`.
- `internal/approvalpolicies` `db_integration`: RLS fail-closed (no identity bound sees no
  rows), upsert replaces `ask` with `deny`, cascade on identity delete, copied from the
  `approvalgrants` tests.
- `internal/agui`: the three routes, own-identity only, `PUT ask` revokes the grant.
- `cmd/aura`: the `policy` subcommands against the fake store the grants tests use.
- `web`: vitest for the panel's list, form and clear.
- Lab-VM acceptance, the Definition of Done (score > 9.8 on the real scenario):
  1. `ask` on `browser__agent_browser_open`; "open example.com" produces an approval card in
     the chat, with `once` and `session` only; accept runs the call, three runs out of three;
  2. `deny` on `calendar send_email`; "email Bob the summary" ends with the model telling the
     operator the tool is disabled, with no retry loop, on two models of the release's matrix;
  3. `always` granted on `shell_exec`, then `ask` set: the next `shell_exec` prompts and the
     grant row is gone;
  4. count the extra rounds a denied tool costs each model over ten turns, to decide whether
     a denied tool must be hidden from `tool_search` in the next release.

## Open questions for the operator

1. Should `deny` also hide the tool from `tool_search` now, before acceptance 4 measures the
   cost? Proposed no: measure first.
2. Should an admin be able to set a policy on another identity from the CLI? The grants CLI
   resolves any identity by name already, and the root operator runs it; proposed the same,
   with `set_by` recording who.
3. Is `ask` on a read-only tool worth its reservation row? Proposed yes: an `ask` is a
   statement that the operator wants a record.

## Out of scope

OpenDots' one-hour expiry and "runs the stored request, not the approval's arguments": Aura's
approvals already bind identity, operation and the arguments' fingerprint (prd.md §5), and
`ErrPauseExpired` bounds their lifetime. Its per-connection tool refresh that keeps existing
choices: Aura's bridge already reconciles tool lists on `tools/list_changed` and warns when a
tool's mutating flag changes (`bridge.go`); a policy keyed by name survives a refresh untouched.
