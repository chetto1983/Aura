---
phase: 25-chat-approval-center
plan: 02
subsystem: api
tags: [agui, approvals, hitl, askuser, runner, resume, postgres, capability]

# Dependency graph
requires:
  - phase: 25-chat-approval-center
    plan: 01
    provides: "conversation REST adapter pattern + RequireAuth/RequireCapability mount + sanitizeErr/SanitizeString"
  - phase: 05-askuser
    provides: "askuser.Store (ListPending/AutoResolveForConversation) + ActionAccept/Decline/Cancel + ErrPauseNotFound"
  - phase: 03-runner
    provides: "Runner.SubmitAnswers three-action model (declinedContent + cancel->AutoResolveForConversation)"
provides:
  - "askuser.Store.ListPendingAll(ctx, limit) — cross-thread pending read (priority DESC, created_at ASC, token ASC)"
  - "sqlc ListAllPendingPausedStates :many (no per-conversation filter)"
  - "GET /api/approvals — sanitized cross-thread pending queue (APRV-01), behind RequireAuth"
  - "POST /api/approvals/{token}/resolve — accept|decline|cancel resume bridge (APRV-02), capability-gated"
  - "Terminal-state projection so an auto-terminated pending surfaces, never silently lost (APRV-03)"
  - "agui ApprovalStore interface + SetApprovalStore optional wiring"
affects: [25-05, approval-center-ui, inline-approval-card, cross-thread-badge]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Three-action resume bridge: HTTP verb -> askuser.Action* -> single-entry map -> Runner.SubmitAnswers (reaches the full model the two-state AG-UI Resume[] cannot)"
    - "Capability-gated mutating route mirrors POST /agent/run (RequireCapability after RequireAuth binds the principal)"
    - "Thin adapter over shipped Store+Runner: uuid.Parse-guard -> verb map -> one call -> status; errors redacted"

key-files:
  created:
    - internal/agui/approvals_api.go
    - internal/agui/approvals_api_test.go
    - internal/agui/approvals_api_unit_test.go
    - internal/runner/runner_resume_test.go
  modified:
    - internal/db/queries/paused_states.sql
    - internal/askuser/store.go
    - internal/askuser/store_test.go
    - internal/agui/server.go
    - cmd/aura/serve_webui.go
    - cmd/aura/serve_webui_test.go
  generated:
    - internal/db/sqlc/paused_states.sql.go
    - internal/db/sqlc/querier.go

key-decisions:
  - "APRV-02 resolve uses Runner.SubmitAnswers directly (RESEARCH OQ1 Option A) — ALL three verbs through one adapter, bypassing the two-state AG-UI Resume[] enum (Pitfall 4)"
  - "Decline maps to ActionDecline -> Runner injects declinedContent ('user declined to answer'), NOT the operator text — the deny != accept footgun (T-25-08)"
  - "The mutating resolve is capability-gated (RequireCapability/agent.run) like POST /agent/run — cross-thread resume/cancel is privileged (V4/T-25-07); the read inherits RequireAuth"
  - "No new index for the cross-thread scan (RESEARCH A4 — premature for a single operator); the mandatory token ASC tiebreaker keeps tx-batched same-created_at rows deterministic"

patterns-established:
  - "Resume-bridge adapter: the HTTP layer maps verbs and reaches the Runner's existing three-action model; it re-implements no decline/cancel logic"
  - "DB-free resolve coverage via a scriptedRunner double (records the answers map) + a fakeApprovalStore, so the verb mapping + 400/404/503 branches run in CI without the live stack"

requirements-completed: [APRV-01, APRV-02, APRV-03]

# Metrics
duration: ~60min (incl. transient-529 crash recovery)
completed: 2026-06-17
---

# Phase 25 Plan 02: Chat + Approval Center — Cross-Thread Approval Backend Summary

**Closed the two backend gaps the approval center needs: a cross-thread pending read (`ListPendingAll`) and a three-action resolve adapter (`/api/approvals/{token}/resolve`) that reaches the Runner's full accept/decline/cancel model — the AG-UI `Resume[]` path is two-state and cannot decline. Mounted behind `RequireAuth` with the mutating resolve capability-gated like `POST /agent/run`. Pure wiring over shipped seams; zero new dependencies.**

## Performance

- **Duration:** ~60 min (including recovery from a transient API-529 executor crash mid-Task-2)
- **Completed:** 2026-06-17
- **Tasks:** 3 of 3
- **Files created/modified:** 10 (+2 sqlc-generated)

## Accomplishments
- **APRV-01:** `askuser.Store.ListPendingAll` aggregates every still-pending `ask_user` pause across ALL conversations in total order (`priority DESC, created_at ASC, token ASC`); `GET /api/approvals` projects it to sanitized JSON behind `RequireAuth`.
- **APRV-02:** `POST /api/approvals/{token}/resolve` maps `accept|decline|cancel` to `askuser.Action*` and calls `Runner.SubmitAnswers` directly — reaching the three-action model end-to-end. Decline injects `declinedContent` (agent continues informed, NOT killed); cancel auto-resolves the conversation.
- **APRV-03:** the read projection carries the terminal marker, and an already-resolved/auto-terminated token surfaces `ErrPauseNotFound → 404` — never a silent false success.
- The mutating resolve is `RequireCapability(agent.run)`-gated like `POST /agent/run`; the read inherits the whole-origin `RequireAuth`. No route shadows the integrations proxy.

## Task Commits

Each task committed atomically:

1. **Task 1: Cross-thread pending query + ListPendingAll (APRV-01 / D-04)** — `11208ad0` (feat)
2. **Task 2: Approvals resolve adapter + decline bridge + cross-thread read (APRV-02/03)** — `dd1072c8` (feat)
3. **Task 3: Mount /api/approvals behind RequireAuth + capability-gate the resolve** — `034d5d8c` (feat)

## Files Created/Modified
- `internal/db/queries/paused_states.sql` — `ListAllPendingPausedStates :many` (copy of `ListPendingPausedStates` with the `conversation_id` filter dropped, `resumed_at IS NULL` kept, `ORDER BY priority DESC, created_at ASC, token ASC`, `LIMIT $1`).
- `internal/askuser/store.go` + `store_test.go` — `ListPendingAll(ctx, limit)` beside `ListPending`, limit≤0→100, reusing the `fromRow` projector; db_integration test covers cross-thread aggregation + the deterministic tiebreaker + the limit default.
- `internal/agui/approvals_api.go` — `handleListApprovals` (read, `SanitizeString` on the question, V7) + `handleResolveApproval` (verb→action map, `uuid.Parse`-guard→404, unknown action→400, `ErrPauseNotFound`→404) + `registerApprovalRoutes`.
- `internal/agui/approvals_api_test.go` — db_integration coverage of the read projection + all three resolve verbs + edges (live DB).
- `internal/agui/approvals_api_unit_test.go` — DB-free verb-mapping + 400/404/503/redaction branches via `scriptedRunner` + `fakeApprovalStore`.
- `internal/runner/runner_resume_test.go` — asserts each verb maps to the correct `ResponseInput.Action` and the decline-content invariant.
- `internal/agui/server.go` — `ApprovalStore` interface + `approvals` field + `SetApprovalStore` + `registerApprovalRoutes(mux)`.
- `cmd/aura/serve_webui.go` + `serve_webui_test.go` — mounted `/api/approvals` (read) + `POST /api/approvals/{token}/resolve` (capability-gated); constants `approvalsListRoute`/`approvalsResolveRoute`.

## Decisions Made
- **Three verbs through one adapter (RESEARCH OQ1 Option A).** The AG-UI `Resume[]` `ResumeStatus` enum is two-state (resolved/cancelled) and cannot express decline (Pitfall 4). The resolve handler reaches `Runner.SubmitAnswers` directly with a single-entry map, using the full three-action model the Runner already implements — the handler re-implements no decline/cancel logic.
- **Decline ≠ accept (T-25-08).** Decline maps to `askuser.ActionDecline`; the Runner injects `declinedContent`, never the operator-supplied text. Pinned by `TestSubmitAnswers_DeclineInjectsDeclinedContent`.
- **Capability gate on the mutating route (V4/T-25-07).** Resuming/cancelling another thread's (possibly background) run is privileged, so resolve is `RequireCapability(agent.run)`-gated exactly like `POST /agent/run`; the read needs only `RequireAuth`.
- **No new index (RESEARCH A4).** The cross-thread scan does not warrant a new index for a single operator; the `token ASC` tiebreaker keeps tx-batched same-`created_at` rows deterministic.

## Deviations from Plan

### Execution anomaly (recovered)
- **Transient API-529 executor crash mid-Task-2.** The first two executor dispatches died on `API Error: 529 Overloaded` (a sustained Anthropic-side outage), the first after ~79 tool-uses with Task 2's files written but **uncommitted and non-compiling** (the unit test referenced an undefined `errDSNLeak`). Per operator decision during the outage, Task 2+3 were finished **inline** (orchestrator main loop) rather than waiting for subagent dispatch to recover. The partial work was preserved, not discarded.

### Auto-fixed Issues
**1. [Rule 2 - Missing critical functionality] Defined the `errDSNLeak` test sentinel**
- **Found during:** Task 2 recovery (the crash point).
- **Issue:** `approvals_api_unit_test.go` referenced `errDSNLeak` (a runner/store error carrying a fake DSN password) at the 500-redaction assertions, but it was never defined — `go vet ./internal/agui/` failed with `undefined: errDSNLeak`.
- **Fix:** Defined `errDSNLeak` as a package-level test var embedding `postgres://aura_app:supersecret@...`; the assertions now prove `sanitizeErr` collapses the DSN to `postgres://[redacted]` so the password never reaches the wire.
- **Commit:** `dd1072c8`

## Threat Model Coverage
- **T-25-06 (question/answer secret leak):** `SanitizeString` applied to the surfaced `question` in the read projection (asserted: an `api_key=`/`sk-` value is redacted on the wire).
- **T-25-07 (privileged cross-thread resume):** resolve behind `RequireAuth` + `RequireCapability(agent.run)`; an authenticated-but-uncapable principal is 403'd before the handler (`TestServeWebuiApprovalsCapabilityGate`).
- **T-25-08 (deny != accept):** decline injects `declinedContent`, not the operator text — pinned by the runner test.
- **T-25-09 (malformed-token 500 leak):** `uuid.Parse` before any round-trip → clean 404; `ErrPauseNotFound` → 404; errors via `sanitizeErr`.
- **T-25-10 (SQL injection):** parameterized sqlc only (`LIMIT $1`); the SELECT is the copied LOCKED contract.
- **T-25-SC (supply chain):** zero new dependencies (pure wiring over shipped stores + Runner).

## Verification Evidence
- `go test -tags db_integration -race -run TestListPendingAll ./internal/askuser/` → ok 1.70s (real DB; cross-thread + tiebreaker + limit default).
- `go test -tags db_integration -race -run TestApprovalsAPI ./internal/agui/` → ok 2.00s (real DB; read + 3 verbs + edges).
- `go test -race -run TestSubmitAnswers ./internal/runner/` → ok (accept/decline/cancel mapping + declinedContent invariant + cancel auto-resolve).
- `go test ./cmd/aura/ -run 'ServeWebui|Approvals' -race` → ok (mount + no-shadow + RequireAuth-inherited + capability gate, incl. uncapable→403).
- `go vet ./...` → clean (whole repo; the untagged sibling test double break that crashed the first executor is fixed).
- Source assertions: `SubmitAnswers`/`ActionDecline`/`SanitizeString` present in approvals_api.go; `/api/approvals` mounted; `RequireCapability` count ≥2 in serve_webui.go (`/agent/run` + resolve); no bare `mux.Handle("/api/",`.

### Integration tiers exercised
- **db_integration:** RUN LIVE on the up `aura-postgres` stack (DSNs derived from `POSTGRES_PASSWORD`). NOT faked.
- **Coverage (live, -race):** `internal/agui` 91.2%, `internal/askuser` 93.0% — both above the 85% floor.
- **Shared-DB note:** the db_integration tiers for `internal/agui` + `internal/askuser` must run **serially (`-p 1`)** on the single shared operator Postgres. Running both packages in parallel contaminates overlapping `aura.*` rows (the cross-thread `ListPendingAll` reads ALL pending rows; `TestConversationsAPI_GetAggregates` asserts exact aggregates) → two spurious failures that vanish under `-p 1` and in isolation. This is a test-harness property of the shared DB, not a code regression — verified both tests pass isolated and serial.
- **neo4j_integration:** not applicable (no graph surface).
- **mutation / make quality:** the operator's WSL/CI gate per CLAUDE.md.

## Self-Check: PASSED
