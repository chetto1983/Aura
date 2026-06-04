---
phase: 09-swarm-minimal
plan: 04
subsystem: api
tags: [ask_user, paused_states, swarm, pgx, pgtype, hitl, askuser, runner]

# Dependency graph
requires:
  - phase: 09-01
    provides: D-05 decision (proxied_* as optional ask_user args) + the swarm v1 shape
  - phase: 04-paused-states (pre-09)
    provides: aura.paused_states.proxied_from_child_id/proxied_tool_call_id columns + sqlc InsertPausedStateParams (paused_states.sql.go:88-89)
provides:
  - "ask_user Spec/args accept optional proxied_from_child_id + proxied_tool_call_id (model-discretionary)"
  - "The 3-layer proxied plumb: ask_user args -> ErrAwaitingUserInput -> AwaitingInput Event -> askuser.InsertParams/Insert -> persistPause"
  - "A proxied pause persists proxied_* into aura.paused_states; a direct pause persists NULL (back-compat)"
affects: [09-swarm-coordinator, swarm-resume-mapping, child-question-relay]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Optional model-discretionary tool args (proxied_*) not in required[]; absent => empty/NULL"
    - "pgtype boundary conversion for nullable columns: *string child id via parseUUID (Valid:false => NULL); pgtype.Text{Valid: s != \"\"}"

key-files:
  created:
    - internal/runner/runner_persist_test.go
  modified:
    - internal/agent/tools/ask_user.go
    - internal/agent/tools/ask_user_test.go
    - internal/agent/event.go
    - internal/agent/llm_agent_pause.go
    - internal/askuser/store.go
    - internal/askuser/store_test.go
    - internal/runner/runner_persist.go
    - internal/runner/fakes_test.go

key-decisions:
  - "proxied_* stay optional (required stays [question, kind]); direct pauses unchanged"
  - "Invalid proxied_from_child_id rejected at the parseUUID domain boundary before the sqlc layer (T-09-10 mitigation)"
  - "A non-empty child id is forwarded as a non-nil *string; empty => nil => SQL NULL"

patterns-established:
  - "Pattern 4 (3-layer proxied plumb): the SQL/sqlc columns existing is NOT enough — the domain layer (args/Event/InsertParams/persistPause) must each carry the field"
  - "db_integration round-trip (not compile-check) asserts both proxied columns persist AND that direct pauses leave them NULL"

requirements-completed: [CAP-03]

# Metrics
duration: ~30min
completed: 2026-06-04
---

# Phase 9 Plan 04: proxied-id pause relay (D-05) Summary

**The 3-layer proxied-id plumb is complete: ask_user gains optional proxied_from_child_id + proxied_tool_call_id args that flow through ErrAwaitingUserInput -> AwaitingInput Event -> persistPause -> askuser.Insert into aura.paused_states; direct (non-proxied) pauses persist NULL, verified by a live db_integration round-trip.**

## Performance

- **Duration:** ~30 min
- **Started:** 2026-06-04T09:36Z (approx)
- **Completed:** 2026-06-04T10:07Z
- **Tasks:** 2 (both TDD: RED -> GREEN)
- **Files modified:** 8 (1 created)

## Accomplishments
- ask_user Spec/args + ErrAwaitingUserInput carry the optional proxied_* ids; required stays exactly `["question", "kind"]`.
- AwaitingInput Event gains ProxiedFromChildID + ProxiedToolCallID, projected by pauseEvent (the doc-comment already anticipated this).
- askuser.InsertParams gains `ProxiedFromChildID *string` + `ProxiedToolCallID string`; Insert converts the child id via the parseUUID boundary (invalid uuid rejected before sqlc — T-09-10) and sets `pgtype.Text{Valid: tc != ""}`.
- persistPause (the SOLE paused_states writer) reads the proxied ids off the Event and forwards them; nil/empty => SQL NULL.
- db_integration TestInsertProxied round-trips both columns live against Postgres (PASS, not skip) and asserts a direct pause persists NULL.

## Task Commits

Each task committed atomically (both TDD; RED test + GREEN impl folded per task since the RED was a compile failure):

1. **Task 1: ask_user Spec/args + AwaitingInput Event carry proxied_* ids** — `574d2d16` (feat)
2. **Task 2: askuser.Insert + persistPause stamp proxied_* into paused_states** — `4bd0df08` (feat)

_Note: per-task RED was a compile-level failure (fields undefined), confirmed before each GREEN impl; both committed as one feat each._

## Files Created/Modified
- `internal/agent/tools/ask_user.go` — askUserArgs + Spec params + ErrAwaitingUserInput carry the optional proxied ids (set in Execute).
- `internal/agent/tools/ask_user_test.go` — assert proxied fields parse when present, stay empty when absent, and required stays [question, kind].
- `internal/agent/event.go` — AwaitingInput gains ProxiedFromChildID + ProxiedToolCallID (omitempty).
- `internal/agent/llm_agent_pause.go` — pauseEvent projects the proxied ids onto the AwaitingInput Event.
- `internal/agent/llm_agent_pause_internal_test.go` — TestPauseEvent_CarriesProxiedIDs (proxied + direct).
- `internal/askuser/store.go` — InsertParams + Insert: proxied conversion at the pgtype boundary.
- `internal/askuser/store_test.go` — TestInsertProxied (db_integration round-trip + NULL on direct).
- `internal/runner/runner_persist.go` — persistPause forwards the proxied ids into InsertParams.
- `internal/runner/runner_persist_test.go` (created) — fake-store unit tests: forwards proxied ids / direct leaves nil.
- `internal/runner/fakes_test.go` — fakePauseStore captures lastInsert for the forwarding assertion.

## Decisions Made
- None beyond the plan. The `parseUUID` boundary (existing helper) is reused for the proxied child id so an invalid uuid is rejected before the sqlc layer — directly satisfying the T-09-10 mitigation disposition.

## Deviations from Plan

**1. [Rule 3 - Blocking] Captured InsertParams in fakePauseStore for the forwarding assertion**
- **Found during:** Task 2 (persistPause unit test)
- **Issue:** The plan's fake-store unit test needs to assert persistPause forwards the proxied ids into `askuser.InsertParams`, but `fakePauseStore` only retained a `Pending` projection (which has no proxied fields), so the forwarded args were unobservable.
- **Fix:** Added a `lastInsert askuser.InsertParams` capture field set on each `Insert` call. `fakes_test.go` is in the runner package and is NOT a sibling-agent file, so this is in-scope.
- **Files modified:** internal/runner/fakes_test.go
- **Verification:** TestPersistPause_ForwardsProxiedIDs + TestPersistPause_DirectPauseLeavesProxiedNil pass (unit + race).
- **Committed in:** 4bd0df08 (Task 2 commit)

**2. [naming] Plan named the integration file `store_integration_test.go`; the actual db_integration file is `store_test.go`**
- The existing `//go:build db_integration` test file for internal/askuser is `store_test.go`. TestInsertProxied was added there (the canonical location) rather than creating a duplicate file.

---

**Total deviations:** 1 auto-fixed (Rule 3 blocking) + 1 file-naming reconciliation.
**Impact on plan:** No scope creep. The fake capture is the minimal mechanism to make the plan's required assertion observable.

## Issues Encountered
- Loading `POSTGRES_PASSWORD` (`!Davide1983!`) into the WSL shell was flaky due to bash history expansion on `!` and intermittent grep returns across WSL invocations. Resolved by `set +H` + a `sed -n 's/^POSTGRES_PASSWORD=//p'` helper script; the integration test then ran live (PASS, 0.08s — a real round-trip, not a skip).

## Verification
- `go build ./...` + `go vet ./internal/askuser/ ./internal/runner/ ./internal/agent/...` clean.
- Unit + race green on internal/agent/..., internal/askuser/, internal/runner/.
- db_integration `TestInsertProxied` ran live against Postgres (WSL, stack up): RUN -> PASS (0.08s); full askuser integration suite green (1.13s). No-skip-as-green honored (envOrSkip t.Fatals under $CI).
- Acceptance greps: ask_user proxied_from_child_id=2, event ProxiedFromChildID=1, pause=1, store=5, runner=3; `"required": ["question", "kind"]` unchanged.

## Next Phase Readiness
- A proxied pause now durably carries the originating child id + tool_call id, so a future swarm-resume can map the user's answer back to the child goal. Best-effort, model-discretionary (D-04/D-05): no parked live children, no new sentinel.
- This plan touched NO internal/swarm/ file and added no new tool — the swarm coordinator (siblings 09-02/09-03) wires independently.

## Self-Check: PASSED

- Files: runner_persist_test.go, ask_user.go, store.go, runner_persist.go, 09-04-SUMMARY.md — all FOUND.
- Commits: 574d2d16 (Task 1), 4bd0df08 (Task 2) — both FOUND in git log.

---
*Phase: 09-swarm-minimal*
*Completed: 2026-06-04*
