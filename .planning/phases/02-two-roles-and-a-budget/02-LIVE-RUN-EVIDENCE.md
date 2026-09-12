# Phase 02 — Live run evidence

Driven on 2026-09-12 against the installed appliance in `/opt/aura` (`aura:edge` at
`4f6cf063b`, Postgres 18.4, ArcadeDB 26.9.1, Garage 2.3.0, Caddy in front at
`https://localhost`), through the cockpit, by two Playwright specs:

- `web/e2e/two-role-live.spec.ts` — the two-role witness (new, this plan). 1 passed, 2.9 min.
- `web/e2e/management-key-onboarding-live.spec.ts` — the credit/role half, run the same day.
  2 passed, 42.5 s; its services-cap test alone 8.8 s.

Both drive real identities against a real OpenRouter account. Nothing in this file is read
from a test double: every number below was measured on the deployment, and the queries that
produced them are named where they are not obvious.

## What a member actually did

A member (`e2e-role-1789223338167@example.com`, identity `68f8d057-…`) was created through the
cockpit wizard, credited by the admin, then driven from its OWN browser session:

| Capability | What it did, live | Evidence |
|---|---|---|
| provisioning grant | held exactly `agent.run, governance.read, governance.write, share.public` | `SELECT capability FROM aura.capability_grants` for its id — four rows, no administrative row, no wildcard |
| `governance.write` | wrote a skill into its own root | `aura.skill_audit`: `create · e2e-member-skill-1789223347625 · 14:29:07`, and the board then listed it |
| `agent.run` | ran a shell command in its own box | turn 3 of its conversation: `AURA_E2E_SHELL_OK [aura_shell {"exit_code":0,"cwd":"/workspace…`, with `aura-box-68f8d057-…` and `aura-egress-68f8d057-…` up for the run |
| `identity.create` | refused | `POST /api/onboarding/start` → 403 |
| `identity.delete` | refused | `DELETE /api/admin/identities/{admin}` → 403 |
| RBAC-10 read-back | read BOTH refusals out of its own audit feed | `GET /api/admin/audit?identity=<self>` → `source=capability` rows for `identity.create` and `identity.delete` |

The grant is proven by USING it, not by reading the table that stores it — which is what plan
02-10 asked for. The two refusals are proven from the member's side of the wire, and the
read-back is the member's own query, not an inspection of the database.

## The reverse saga, plane by plane

Measured before the removal (while the member existed) and after the cockpit's Remove:

| Plane | Before | After |
|---|---|---|
| `aura.identities` | 1 row | 0 |
| `aura.capability_grants` | 4 rows | 0 |
| `aura.identity_llm_key` | 1 row | 0 |
| `aura.identity_object_store` | 1 row | 0 |
| `aura.conversations` | 1 | 0 |
| ArcadeDB database | `mem_68f8d057_7407_453a_ae47_2f3e155fa387` | gone |
| Garage bucket | `aura-68f8d057-7407-453a-ae47-2f3e155fa387` | gone |
| Sandbox + egress containers | both up | gone |
| OpenRouter key | active, named after the identity | revoked (management-key spec asserts it) |
| **Authula account** | **1 row** | **1 row — NOT removed** |

## The defect this run found

Every plane was torn down except the identity's Authula account, and it was not a one-off:
`authula.users` held six accounts whose identities the cockpit had removed over 2026-09-11 and
2026-09-12 (`e2e-member-…` ×5, `e2e-role-…` ×1).

Root cause, read from the source rather than guessed: `cmd/aura/serve.go` wires the cron
dispatch (line ~300) and the AG-UI server (line ~365) BEFORE `buildAuthDeps` (line ~388), so
`buildDeprovisioner` had no Authula provider to hand the saga and left `Sessions` and
`AuthulaDelete` nil. The saga nil-skips an unwired leg by design, so the removal reported
success while the account stayed. `serve_provisioning.go`'s own comment had recorded the gap
("`aura identity {deactivate|purge}` … is currently the only path that leaves no Authula
orphan"), and no test caught it because the unit tests inject a fake for that port.

Fixed the same day: `agui.Deprovisioner.SetAuthulaTeardown` supplies both legs after
construction, `buildDeprovisioner` memoizes so the removal route and the grace-window sweep
share one instance, and `serve.go` attaches the legs as soon as the provider answers. Three
unit tests cover it (wired / session-kill / unwired nil-skip), plus one that pins the shared
instance the wiring depends on.

## Where each 02-10 truth is proven

| Truth from 02-10-PLAN.md | Verdict |
|---|---|
| Live run driven as two real identities, A administrative and B ordinary | met — the spec above |
| B installs an MCP server, writes a skill, runs a shell command, approves a destructive tool call | PARTIAL — skill write and shell command driven live; see the exclusions below |
| B attempts create and remove; both refused; both read back from the audit trail by B's own query | met |
| A creates a third identity and removes it through the cockpit; reverse saga observed on every plane | met — and it is what found the Authula leg |
| B at a zero cap is refused before the model; A credits it; B's next turn runs; B's spend is B's | met — management-key spec (member refused, capped at 0, raised to 0.5 on the provider, then the turn ran) |
| Mutation gate extended with `capability_policy` and `credit_policy` | NOT DONE — deviation, below |
| No package-manager install, no new dependency | met — `git diff` on `go.mod`, `go.sum`, `web/package.json`, `web/package-lock.json` is empty for this plan |
| The run records what it does NOT demonstrate | this file's last section |

## Deviations

1. **The mutation-gate extension was deliberately not done** (operator's call, 2026-09-12). The
   gate is real — CI installs `go-mutesting`, runs Stryker, and `critical_mutation_gate.py`
   fails any scope under 70% with no averaging — but it scores one file per scope, and adding
   `capability_policy.go` and `identitykey/policy.go` was judged box-ticking against a green
   gate rather than closing a defect. Recorded here so it is a decision, not an omission.
2. **The closing run is a Playwright spec, not `cmd/aura/two_role_credit_live_test.go`.** The
   standing rule is that end-to-end proof goes through the UI; a Go live test would have
   reached the same routes by another door and would not have exercised the cockpit at all.

## What this run does NOT demonstrate

- **A member installing an MCP server, and approving a destructive tool call.** Both were left
  out on purpose: an MCP install mutates the operator's live governance surface beyond what the
  run cleans up, and a destructive approval means really destroying something on a deployment
  that is in daily use. Both paths are covered by automated tests, and both remain unproven
  live. The capability behind them, `governance.write`, IS proven live by the skill write.
- **That the Authula fix works on the deployment.** It is proven by unit tests and by reading
  the boot order; the live re-check waits for the next edge image.
- **Provider-side cap enforcement beyond what the management-key spec measured** — a raised cap
  reaching OpenRouter in ~25 s is measured; OpenRouter's own refusal at the ceiling is not.
- **What OpenRouter retains about a deleted key's consumption.** Unchanged from phase 01.
- **Anything about a third role.** There are two: an identity holds the administrative pair or
  it does not.

## Score

9.6 / 10. The substance of the phase is proven on a live deployment and the run earned its
keep by finding a real defect in the very leg it was written to observe. It is not a 10: two of
the four member activities were excluded by judgement rather than driven, and the fix the run
produced has not yet been re-observed live.
