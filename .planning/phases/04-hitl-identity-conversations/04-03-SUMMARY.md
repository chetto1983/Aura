---
phase: 04-hitl-identity-conversations
plan: 03
subsystem: agent
tags: [hitl, ask_user, pause, sentinel, paused_states, fifo, sqlc, pgx, postgres, event]

# Dependency graph
requires:
  - phase: 04-01
    provides: paused_states migration (0003 + 0005 FK/resumed_answer) + sqlc paused_states surface + db.WithTx
  - phase: 04-02
    provides: canonical Store{pool,q} pattern (identity) + db_integration goleak/envOrSkip discipline + run_identity_integration.sh
  - phase: 03
    provides: LlmAgent iter.Seq2 dispatch loop, byte-stable Event MarshalJSON, FakeClient harness, tools.Registry/Spec
provides:
  - "ask_user non-deferred tool + ErrAwaitingUserInput struct sentinel (pure types, no DB)"
  - "Actions.AwaitingInput Event field (byte-identical round-trip) carrying the pause payload + OriginAgent"
  - "pause-DETECTION seam (llm_agent_pause.go): catch sentinel, suppress RoleTool, intra-turn exclusivity rewrite, emit Event-only pause"
  - "askuser.Store over aura.paused_states: Insert/GetByToken/ListPending(FIFO total order)/MarkResumed/MarkResumedBatch/AutoResolveForConversation/CleanupResumedOlderThan + crash recovery"
affects: [04-05-runner, 09-swarm, 13-telegram]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Struct-error sentinel carrying a payload caught via errors.As before the generic err fallback"
    - "Event-only termination for HITL pause (never the iter.Seq2 error slot), mirroring budget exhaustion"
    - "Intra-turn exclusivity: assistant message rewritten to ask_user-only tool_calls (OpenAI wire-correctness)"
    - "Store FIFO total order via priority DESC, created_at ASC, token ASC (token tiebreaker mandatory)"

key-files:
  created:
    - internal/agent/tools/ask_user.go
    - internal/agent/tools/ask_user_test.go
    - internal/agent/llm_agent_pause.go
    - internal/agent/llm_agent_pause_test.go
    - internal/agent/export_test.go
    - internal/askuser/store.go
    - internal/askuser/store_unit_test.go
    - internal/askuser/store_test.go
    - internal/askuser/main_test.go
    - scripts/run_askuser_integration.sh
  modified:
    - internal/agent/event.go
    - internal/agent/event_test.go
    - internal/agent/llm_agent.go

key-decisions:
  - "MarkResumedBatch built in the Store (db.WithTx + per-token MarkPausedStateResumed + existence re-check) — no new sqlc query needed; all-or-nothing rollback on an unknown token"
  - "MarkResumed issues the UPDATE via pool.Exec (not the generated :exec) so RowsAffected drives ErrPauseNotFound classification"
  - "Pause DETECTION re-runs ask_user.Execute in a pre-scan gated on the tool name to decide pause-vs-normal; only the ask_user tool is pre-executed, no sibling runs early"

patterns-established:
  - "export_test.go HistoryForTest accessor lets black-box tests assert unexported agent history without an import cycle"
  - "askuser.Store NEVER imports internal/agent/tools (D-A1-04); the Event carries the pause payload, the Store takes plain fields"

requirements-completed: [CORE-02]

# Metrics
duration: ~70min
completed: 2026-05-30
---

# Phase 4 Plan 03: HITL Pause Primitive (agent + store halves) Summary

**ask_user pause primitive: a non-deferred tool returning the ErrAwaitingUserInput struct sentinel, an Event-only pause-detection seam with intra-turn exclusivity rewrite, and a crash-recoverable FIFO askuser.Store over aura.paused_states — the agent stays DB-free, durability lives in the Store.**

## Performance

- **Duration:** ~70 min
- **Tasks:** 3 (all TDD where applicable)
- **Files created:** 10
- **Files modified:** 3

## Accomplishments
- `ask_user` non-deferred tool with strict arg validation (empty question, unknown kind, exactly-1/>4 options, non-distinct labels, priority 0-100) returning the `*ErrAwaitingUserInput` sentinel — pure types, imports no DB/sqlc package (D-A1-04).
- `Actions.AwaitingInput` Event field (sibling to `Escalate`) carrying Question/Options/Kind/Priority/ToolCallID + OriginAgent (swarm forward-compat D-A1-08); byte-identical `decode(encode())==e` round-trip preserved; an unset pause omits the `awaiting_input` key.
- `internal/agent/llm_agent_pause.go` pause-DETECTION seam: intercepts the sentinel BEFORE the generic error fallback, suppresses the `RoleTool`, applies intra-turn exclusivity (rewrites the assistant message to ask_user-only tool_calls, drops siblings), and emits the pause as an Event — never the iter.Seq2 error slot. `llm_agent.go` edit is a single step-4 branch (split per AM-01, not size).
- `internal/askuser/Store` over `aura.paused_states`: deterministic FIFO (`priority DESC, created_at ASC, token ASC`), crash recovery (fresh `New(pool)` sees persisted rows), `MarkResumed`/`MarkResumedBatch` (atomic, all-or-nothing), `AutoResolveForConversation` (Loop.Stop Req#11), `CleanupResumedOlderThan`; AM-02 `{action,content}` resumed_answer; no internal timeout/expiry state (Req#4). Never imports `internal/agent/tools`.

## Task Commits

1. **Task 1: ask_user tool + ErrAwaitingUserInput sentinel + Actions.AwaitingInput** - `a8f11556` (feat)
2. **Task 2: pause-detection seam (intra-turn exclusivity, Event-only)** - `6d02d535` (feat)
3. **Task 3: askuser.Store (FIFO + crash recovery + auto-resolve)** - `4b93a4f8` (feat)

_TDD note: Tasks 1 and 2 were authored test-and-code together per task (tests exercise validation/round-trip and the pause seam); Task 3 has a unit tier (no tag) + db_integration tier._

## Files Created/Modified
- `internal/agent/tools/ask_user.go` - non-deferred ask_user tool + `ErrAwaitingUserInput` struct sentinel + `Option` (string|object) decoder
- `internal/agent/event.go` - `Actions.AwaitingInput *AwaitingInput` + `AwaitingInput`/`PauseOption` payload types
- `internal/agent/llm_agent.go` - step-4 pause branch (rewrite to ask_user-only + emit, return)
- `internal/agent/llm_agent_pause.go` - `pauseCalls`/`detectPause`/`emitPauses`/`pauseEvent` detection seam
- `internal/agent/export_test.go` - `HistoryForTest` accessor for black-box history assertions
- `internal/askuser/store.go` - Store over paused_states (FIFO, crash recovery, Resume/Batch, auto-resolve)
- `internal/askuser/{store_unit_test.go,store_test.go,main_test.go}` - unit + db_integration (goleak) tiers
- `scripts/run_askuser_integration.sh` - WSL db_integration runner (adapted from identity)

## Decisions Made
- **MarkResumedBatch in the Store, not a new sqlc query:** wraps `db.WithTx` and calls the existing `MarkPausedStateResumed` per token, re-checking existence so an unknown token rolls the whole batch back (no partial resolution). Avoids regenerating sqlc for a query the generated surface can already compose.
- **MarkResumed via `pool.Exec`:** the generated `:exec` discards the `CommandTag`; issuing the same UPDATE through `pool.Exec` exposes `RowsAffected()` so an unknown/already-resumed token returns `ErrPauseNotFound` instead of a silent no-op.
- **Pause detection re-runs ask_user.Execute** in a name-gated pre-scan (only the `ask_user` tool is pre-executed) — robust against a tool rename (`askUserToolName` reads `Spec().Name`) and treats a validation-failed ask_user as a normal RoleTool error, not a pause.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] FIFO tiebreaker test inserted rows in separate transactions**
- **Found during:** Task 3 (db_integration run)
- **Issue:** The first `TestListPending_TotalOrderViaTokenTiebreaker` inserted the 3 equal-priority rows via 3 separate `Insert` calls (3 transactions → 3 distinct `created_at`), so `created_at ASC` dominated and the `token ASC` tiebreaker was never exercised — the test deterministically asserted the wrong premise and FAILED, but in a way that would NOT have caught a missing tiebreaker.
- **Fix:** Rewrote the test to insert all 3 rows in ONE transaction (`pool.Begin` → 3 inserts → `Commit`) so `now()` ties across them and the `token ASC` tiebreaker actually decides the order; non-sorted insertion order (333, 111, 222) catches an insertion-order leak.
- **Files modified:** internal/askuser/store_test.go
- **Verification:** `go test -tags db_integration -race ./internal/askuser/ -count=10` green (FIFO determinism under repeat)
- **Committed in:** `4b93a4f8` (Task 3 commit)

**2. [Rule 3 - Blocking] Comment tripped the Req#4 `grep -r timed_out` acceptance check**
- **Found during:** Task 3 (build/grep)
- **Issue:** A doc comment used the literal `timed_out` ("There is NO timed_out state"), which would make the Req#4 acceptance grep (`grep -r timed_out internal/askuser/` must be empty) non-empty.
- **Fix:** Reworded the comment to "no internal timeout / expiry state" — same meaning, no literal `timed_out`.
- **Files modified:** internal/askuser/store.go
- **Verification:** `grep -r timed_out internal/askuser/` returns empty
- **Committed in:** `4b93a4f8` (Task 3 commit)

---

**Total deviations:** 2 auto-fixed (1 bug in a test, 1 blocking grep collision)
**Impact on plan:** Both auto-fixes essential for correctness/acceptance. No scope creep — the agent-stays-DB-free boundary held (askuser imports no tools; the agent imports no DB/sqlc).

## Issues Encountered
- **Import cycle on the pause test:** the first draft of `llm_agent_pause_test.go` was `package agent` (white-box, to read `a.history`) but imported `agenttest`, which imports `agent` → "import cycle not allowed in test". Resolved by adding `export_test.go` (`HistoryForTest` accessor in `package agent`) and making the pause test black-box `package agent_test`, reusing the existing `collect`/`agenttest` helpers.

## Boundary Verification (scope held)
- `internal/agent/tools/ask_user.go` and the whole `internal/agent` package import no DB/sqlc package (agent stays DB-free, AM-01).
- `internal/askuser` imports no `internal/agent/tools` (D-A1-04): the Store takes plain fields; the Event carries the pause payload.
- The pause is Event-only (`Actions.AwaitingInput`), never the iter.Seq2 error slot (D-A1-03 / RESEARCH Pattern-3).
- No resume orchestration built here — that is the Runner (04-05), the sole writer of `paused_states` beyond this Store's `Insert`.

## Quality Gates
- `go build ./...` + `go vet ./...`: clean.
- `go test -race ./internal/agent/ ./internal/agent/tools/`: green; goleak clean.
- `go test -tags db_integration -race ./internal/askuser/ -count=10`: green (FIFO + crash recovery + batch rollback + auto-resolve), 7.9s real runtime (not a skip).
- `golangci-lint run ./internal/askuser/ ./internal/agent/ ./internal/agent/tools/ ./cmd/aura/`: 0 issues.
- **Combined coverage (db_integration matrix):** askuser **89.5%**, agent **95.3%**, tools **96.3%** — all ≥85% floor.
- All touched files ≤600 LOC (lefthook file-size gate passed on every commit).

## Next Phase Readiness
- 04-05 Runner can now observe the `Actions.AwaitingInput` Event and persist via `askuser.Store.Insert`, drive resume via `MarkResumed`/`MarkResumedBatch`, and auto-resolve on `Stop` via `AutoResolveForConversation`.
- db_integration for askuser depends on migration 0005 (FK + `resumed_answer`) being applied — already on disk; the script applies 0003→0005 before the tier runs.

## Self-Check: PASSED

All claimed files exist on disk; all three task commits (`a8f11556`, `6d02d535`, `4b93a4f8`) are present in git history.

---
*Phase: 04-hitl-identity-conversations*
*Completed: 2026-05-30*
