# Tool approval policy: `ask` and `deny` per identity

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** An identity can narrow what its agent may do, tool by tool: `deny` refuses the call at the gateway with a reason the model reads, `ask` routes the call to approval whatever its tier and offers no `always`. Precedence deny > ask > grant > tier. Visible in the cockpit's standing-approvals panel, settable from the CLI, recorded in every reservation it touched.

**Architecture:** One table beside the grants (`aura.gateway_tool_policies`, the 0087 RLS pair), one sibling store package (`internal/approvalpolicies`), one seam in the gateway (`policyStore`) read in `Decide` after classification and before the read-only early return, the approval prompt's label table trimmed under `ask`, three routes under `/api/approvals/policies`, a `policy` branch of `aura gateway`, and a block in `StandingApprovalsPanel`.

**Tech Stack:** Go 1.27, PostgreSQL 16 + sqlc 1.31.1 + golang-migrate, React 19 + TanStack Query + vitest, build tag `db_integration`.

**Spec:** `docs/superpowers/specs/2026-10-09-tool-approval-policy-design.md`, answered questions included (2026-10-09). PRD §5, "A tool policy per identity".

## Global Constraints

- Every Go file stays at or under 600 lines (`make file-size`); split on touch. `approve.go` is at 265, `decide.go` at 119, `approvals_api.go` at 356: the policy handlers go in a new `approval_policies_api.go`.
- After every Go edit: `go vet ./internal/<pkg>/ ./cmd/aura/`, `go build ./...`, `go test -race -count=1 ./internal/<pkg>/`. The `db_integration` tier runs against the disposable cluster (recipe below), never a live `aura` database.
- Disposable Postgres on this host (2026-10-09): cluster `16/aura` on port 5433, roles `aura` (superuser), `aura_app`, `aura_migrate`, password `aura`, database `aura_cov` owned by `aura_migrate`. Env for the tier: `POSTGRES_PASSWORD=aura PGHOST=127.0.0.1 PGPORT=5433 AURA_DB_URL=postgres://aura_app:aura@127.0.0.1:5433/aura_cov?sslmode=disable AURA_DB_MIGRATE_URL=postgres://aura_migrate:aura@127.0.0.1:5433/aura_cov?sslmode=disable AURA_DB_BOOTSTRAP_URL=postgres://aura:aura@127.0.0.1:5433/aura_cov?sslmode=disable CI=true`.
- The migration number is the next free slot when the task runs: `ls internal/db/migrations/ | tail -1` printed `0139_scheduler_task_transient_retries.up.sql` on 2026-10-09, so the slot is `0140`. Re-run first.
- After editing `internal/db/migrations/` or `internal/db/queries/`, run `sqlc generate` (1.31.1) and commit `internal/db/sqlc/` with the change (CI job `sqlc-golden`).
- Commit per task, pathspecs only, message with the why. Never `--no-verify`.
- No agent tool writes a policy. The cockpit route is own-identity; only the CLI resolves another identity, by name, as the grants CLI does.
- Comments only where the why is not obvious; code, comments and messages in English.

## Review Focus

1. **A `deny` subject never executes and never reserves.** `Decide` returns `Deny` with reason `policy` before the read-only early return, so a read-only denied tool is refused too. Pinned by `TestPolicyDenyRefusesBeforeExecution` and `TestPolicyDenyCoversReadOnlyTools` (Task 3).
2. **An `always` grant does not bypass `ask`.** `routeApprove` skips `alwaysGranted` and the session grant under `ask`, and the label table has no `always`. Pinned by `TestPolicyAskOutranksAStandingGrant` and `TestPolicyAskOffersNoAlways` (Task 3).
3. **A failing policy read prompts, never runs.** `policyFor` returns `PolicyAsk` on a store error with one warning. Pinned by `TestPolicyLookupErrorFallsToThePrompt` (Task 3).
4. **RLS fails closed.** A connection with no identity bound sees no policy; another identity sees none. Pinned by `TestPoliciesAreInvisibleWithoutAnIdentity` and `TestPoliciesAreIdentityScoped` (Task 2).
5. **Dev and local_trusted ignore policies**, as `Decide` is a no-op there. Pinned by `TestPolicyIsInertUnderDevProfile` (Task 3).

## Decisions taken while writing this plan

- **`PolicyAsk` is the fail-closed value of `policyFor`**, not `PolicyDeny`: a store blip then costs one prompt, which the operator can decline, rather than a refusal of every tool while the database is unreachable. The spec says so; the plan keeps it.
- **The policy read happens once per `Decide`**, before classification's early return, and its value travels into `routeApprove` as a parameter rather than being re-read there.
- **`gateway_policy` rides the reservation Meta and the deny decision fact**, beside `approval_scope`; no new ledger column (D-01 zero-migration for Meta).
- **The counter is a boundary**, `obs.NewGlobalBoundary` with `Operation: "gateway_policy"` and `State: ask|deny`, `Count: obs.GatewayPolicyDecisionsID`: the obs package has no bare counter helper and every other package counts through a boundary.
- **Setting `ask` revokes the `always` grant in the API handler**, after the policy is written, through the grants store's existing `Revoke`; the store packages stay independent and precedence covers the window between the two writes.
- **Taken during execution (2026-10-09):** under `ask` the session grant is honoured and only the `always` grant is skipped, as the spec says; the plan's Task 3 text said both. The operator who picks "for this conversation" on an `ask` prompt has answered for this conversation.
- **Taken during execution (2026-10-09):** the two new unsafe routes are inventoried in `idempotency_http.go` (`approval_policy_set`, `approval_policy_clear`); the mutation-route test found them missing. The grant revoke and the policy clear share one handler body, which the CI linter's `dupl` asked for.
- **The cockpit types the tool name.** The subject vocabulary is what `tool_search` and `aura mcp tools` print; a picker from the manifest is a follow-up named in the spec.

## File structure

| File | Task | Responsibility |
|---|---|---|
| `internal/db/migrations/0140_gateway_tool_policies.{up,down}.sql` | 1 | Table, CHECK, RLS pair, grants |
| `internal/db/queries/gateway_tool_policies.sql` | 1 | Set (upsert), Get, List, Clear |
| `internal/db/sqlc/` | 1 | Regenerated |
| `internal/db/migrate_0140_integration_test.go` | 1 | Up, down, CHECK, RLS |
| `internal/approvalpolicies/store.go` (new) | 2 | `Policy`, `Store`, `Set`, `Get`, `List`, `Clear` |
| `internal/approvalpolicies/store_integration_test.go` (new) | 2 | RLS, upsert, cascade |
| `internal/gateway/policy.go` (new) | 3 | `Policy`, `policyStore`, `SetPolicyStore`, `policyFor`, the boundaries |
| `internal/gateway/decide.go`, `approve.go`, `scope.go`, `reserve.go` | 3 | Deny/ask branches, label table, Meta |
| `internal/gateway/policy_test.go` (new) | 3 | The five review-focus tests and the rest |
| `internal/obs/catalog.go` | 3 | `GatewayPolicyDecisionsID` |
| `internal/agui/approval_policies_api.go` (new) | 4 | Three routes, seam, grant revoke on `ask` |
| `internal/agui/approval_policies_api_test.go` (new) | 4 | Own-identity, 400s, revoke on `ask` |
| `cmd/aura/serve_webui_routes.go`, `serve_webui.go`, `serve_agui.go`, `chat_boot.go` | 4 | Mounts and wiring |
| `cmd/aura/gateway_policy.go` (new), `gateway_grants.go` | 5 | `aura gateway policy {list,set,clear}` |
| `cmd/aura/gateway_policy_test.go` (new) | 5 | Against the fake identity store |
| `web/src/approvals/useApprovalPolicies.ts` (new) | 6 | Query and mutations |
| `web/src/settings/ToolPoliciesPanel.tsx` (new), `StandingApprovalsPanel.tsx` | 6 | The block under the grants |
| `web/src/i18n/resources.settings.ts` | 6 | Keys, en and it |
| `web/src/settings/__tests__/ToolPoliciesPanel.test.tsx` (new) | 6 | List, set, clear, errors |

---

### Task 1: Table and queries

**Files:** the four under `internal/db/` above.

- [x] **Step 1: Confirm the slot.** `ls internal/db/migrations/ | tail -1` → `0139_...`; the slot is `0140`.
- [x] **Step 2: Write the failing migration test** `internal/db/migrate_0140_integration_test.go` (tag `db_integration`): fresh database to 0139, table absent; up: table present, `policy` CHECK admits `ask` and `deny` and refuses `maybe`, RLS enabled with the two policies, `aura_app` without `app.current_identity` sees zero rows after an insert as `aura`; down: table gone; up again.
- [x] **Step 3: Write the migration** by copying 0099 line for line and renaming: `aura.gateway_tool_policies (identity_id, tool, action DEFAULT '', policy CHECK IN ('ask','deny'), set_at, set_by, PRIMARY KEY (identity_id, tool, action))`.
- [x] **Step 4: Write the queries** `SetGatewayToolPolicy` (`INSERT ... ON CONFLICT (identity_id, tool, action) DO UPDATE SET policy = EXCLUDED.policy, set_at = now(), set_by = EXCLUDED.set_by`), `GetGatewayToolPolicy :one`, `ListGatewayToolPolicies :many ORDER BY tool, action`, `ClearGatewayToolPolicy :execrows`.
- [x] **Step 5: `sqlc generate`**, run the test with the tier env, commit: `feat(db): add per-identity tool policies beside the approval grants`.

### Task 2: The store

- [x] **Step 1: Failing tests** `internal/approvalpolicies/store_integration_test.go`: set then get; upsert `ask` to `deny`; list order; clear reports removed or not; no identity bound sees nothing (`db.WithIdentityTx` with another identity); cascade on identity delete.
- [x] **Step 2: Implement** `store.go` in `approvalgrants`'s shape: `Policy` string type with `PolicyAsk`, `PolicyDeny`, `ParsePolicy`; `Row{Tool, Action, Policy, SetAt, SetBy}` with `Subject()` through `approvalgrants.Subject`; `Set` refuses an empty tool and an unknown policy; `Get` returns `(Policy, bool, error)`.
- [x] **Step 3: Gates, commit** `feat(approvalpolicies): durable ask and deny policies per identity`.

### Task 3: The gateway

- [x] **Step 1: Failing tests** `internal/gateway/policy_test.go` with a `fakePolicies` beside `fakeGrants`: the five review-focus tests, plus `TestPolicyAskReservesAReadOnlyTool` (an `ask` on a read-only spec goes through `routeApprove` and, after accept, reserves with `gateway_policy=ask` in Meta), `TestPolicyDenyRecordsADecisionFact` (an `end` row with `gateway_policy=deny`, `reason=policy`), `TestPolicyAskUnderHeadlessDeniesWithGuidance`.
- [x] **Step 2: Implement** `policy.go`: `type Policy = approvalpolicies.Policy`? No: the gateway declares its own `Policy string` with the two values so it imports no store package (the same line `grantStore` draws); `policyStore interface{ Get(ctx, identityID, tool, action string) (string, bool, error) }`; `SetPolicyStore`; `policyFor(ctx, identityID, subject) Policy` with the warn-and-ask fallback; two boundaries.
- [x] **Step 3: `decide.go`:** after `tier := classify(...)`, `policy := g.policyFor(...)`; `deny` → `recordPolicyDeny` (an `end` decision fact like `recordDegradedDeny`, reason `policy`) and `Verdict{Deny, Reason: "policy"}`; `ask` → `spec.Mutating = true`; `if gated(tier) || policy == PolicyAsk { routeApprove(..., policy) }`. `ErrDenied` message for `policy`: `tool "<subject>" is disabled for this identity by policy: do not retry it, tell the operator` (built where `execTool` maps the verdict; check `internal/agent/llm_agent_retry.go:156`).
- [x] **Step 4: `approve.go`:** `routeApprove` takes `policy`; under `ask` skip `SessionGrant` and `alwaysGranted`; pass `policy` to `gatewayApprovalRequiredResult` → `scopeOptions(subject, policy)`; `scope.go`: `scopeLabels(s, policy)` drops the `always` entry under `ask` and `scopeForAnswer` resolves against the same table (an `always` answer under `ask` is `ScopeOnce`).
- [x] **Step 5: `reserve.go`:** `reservationStart` gains `policy` and writes `meta["gateway_policy"]` when set; thread through `reserve`.
- [x] **Step 6: Gates on `internal/gateway` (unit, race) and the two integration suites it has under the tier; commit** `feat(gateway): ask and deny policies outrank grants and tiers`.

### Task 4: The API

- [x] **Step 1: Failing tests** `approval_policies_api_test.go` with fakes: `GET` lists own identity only; `PUT` with `deny`, with `ask` (revokes the grant), with `maybe` (400 `invalid_policy`), with an empty tool (400); `POST /clear` reports removed; 503 when unwired.
- [x] **Step 2: Implement** `approval_policies_api.go`: `approvalPolicyStore` seam (Set, Get not needed, List, Clear), `SetApprovalPolicyStore`, `registerApprovalPolicyRoutes` called from `registerApprovalRoutes`, items with `subject`, `policy`, `set_at`, `set_by`.
- [x] **Step 3: Mounts** in `serve_webui_routes.go` (`approvalPoliciesRoute`, `approvalPoliciesClearRoute`) and `serve_webui.go`, wiring in `serve_agui.go` (`SetApprovalPolicyStore(approvalpolicies.New(chat.pool))`) and `chat_boot.go` (`gw.SetPolicyStore(approvalpolicies.New(pool))`).
- [x] **Step 4: Gates, commit** `feat(agui): own-identity tool policies at /api/approvals/policies`.

### Task 5: The CLI

- [x] **Step 1: Failing tests** `gateway_policy_test.go`: `set <identity> <tool> [action] ask|deny`, `clear`, `list`, unknown policy word refused, usage on bad arity.
- [x] **Step 2: Implement** `gateway_policy.go`; `runGateway` grows `case "policy"`; usage text covers both verbs.
- [x] **Step 3: Gates, commit** `feat(cli): aura gateway policy sets ask and deny per identity`.

### Task 6: The cockpit

- [x] **Step 1: Failing test** `ToolPoliciesPanel.test.tsx` (the `CreditPanel` test's stubbed-fetch shape): renders the list with subjects and policies, submits a `PUT`, clears, shows the error alert.
- [x] **Step 2: Implement** `useApprovalPolicies.ts` (mirrors `useApprovalGrants.ts`), `ToolPoliciesPanel.tsx` (list + form: tool, optional action, ask/deny), mounted under the grants in `StandingApprovalsPanel`; keys `settings.toolPolicies.*` in en and it.
- [x] **Step 3:** `npm run typecheck`, `npm run lint`, `npm test -- ToolPoliciesPanel`, i18n key parity test; commit `feat(web): tool policies block under standing approvals`.

### Task 7: Closing

- [x] `make file-size`, `go vet ./...`, `go build ./...`, `go test -race ./internal/gateway/ ./internal/approvalpolicies/ ./internal/agui/ ./cmd/aura/`, the `db_integration` tier for the touched packages, `golangci-lint run ./internal/gateway/... ./internal/approvalpolicies/... ./internal/agui/... ./cmd/aura/...`.
- [ ] Lab-VM acceptance (spec, Testing, items 1-4): open; each run leaves its file in `docs/superpowers/verification/`.
