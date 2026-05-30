---
phase: 04-hitl-identity-conversations
plan: 05
subsystem: runner
tags: [runner, orchestration, hitl, resume, paused-states, conversations, repl, cli, fts, auto-title, goleak, sc-4]

# Dependency graph
requires:
  - phase: 04-hitl-identity-conversations
    plan: 01
    provides: migrations 0003-0006 + db.WithTx + sqlc surface (paused_states / conversations / context_rot_events) + L2 config inputs + tiktoken-go
  - phase: 04-hitl-identity-conversations
    plan: 02
    provides: internal/identity.Store + the canonical Store pattern + hand-rolled switch CLI shape (runDB/runIdentity)
  - phase: 04-hitl-identity-conversations
    plan: 03
    provides: ask_user tool + Actions.AwaitingInput pause Event + askuser.Store (Insert/ListPending/MarkResumed(Batch)/AutoResolveForConversation/GetByToken)
  - phase: 04-hitl-identity-conversations
    plan: 04
    provides: conversations.Store (Create/Get/List/UpdateStatus/Rename/SetTitleIfNull/CountTurns/AppendTurn/LoadHistory/LoadManagedHistory/SearchConversationTurns/Delete) + L1/L2/L2.5 ladder + ScanOrphans + generateTitle body + offline tiktoken
  - phase: 03-llm-client-toolresult
    provides: LlmAgent iter.Seq2 dispatch loop + agenttest.FakeClient + chat.go streaming/cost-footer/two-stage-Ctrl+C REPL
provides:
  - "internal/runner.Runner — Turn/SubmitAnswer(s)/Stop orchestrator; SOLE writer of paused_states (T-04-19); resume-as-fresh-Run (SC-4); goleak-clean auto-title WaitGroup; NewConversation(WithID)"
  - "consumer-side narrow interfaces ConversationStore/PauseStore/IdentityStore (D-A2-02) with hand-written in-memory fakes (85% floor without DB)"
  - "aura chat {list|new|resume|archive|unarchive|delete|rename|search} Runner-driven REPL + the boot composition root (config.Load -> db.Open -> 3 Stores + ScanOrphans + InitEncoder -> Runner)"
  - "inline kind-specific pause rendering (D-A3-02): clarification=free-text, approval=[y/N] default No, choice=numbered"
  - "aura chat search excerpt rendering (conv_id|seq|similarity|excerpt, app-side ±60-char window over the locked FTS query, D-A5-03)"
  - "aura paused-states {list|purge --before <ISO> --confirm} CLI + askuser.ListRecent projection (shows the auto-terminated answer) + ListRecentPausedStates sqlc query"
  - "conversations.GenerateTitle (exported worker-body entry) + conversations.InitEncoder (boot tiktoken init)"
  - "scripts/microcompact_smoke.sh — L1/SC-1/L2.5 live CI contract (mirrors loop_budget_smoke.sh)"
affects: [swarm-phase-9, telegram-phase-13, scheduler-phase-10, memory-phase-11]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Event-sourced persistence (ADK AppendEvent-per-Event): the Runner observes the agent Event stream and persists exactly the turns a round produces — a final Event -> assistant answer turn + usage; a pause Event -> assistant ask_user tool_call turn + the paused_states row (sole writer)"
    - "resume = a FRESH agent.Run over rehydrated history (D-A1-05); the answer is a RoleTool{ToolCallID:<original>} turn already in the loaded history, so the next request carries the question->answer pair with no duplicate ask_user and no silent re-run (SC-4)"
    - "consumer-side narrow interfaces declared in the runner package (D-A2-02); concrete *conversations.Store / *askuser.Store / *identity.Store satisfy them implicitly; unit tests pass hand-written in-memory fakes (no DB)"
    - "goleak-clean background worker: r.wg.Add(1) + WithoutCancel(turnCtx) + WithTimeout; Stop does a bounded wg.Wait() via a select on a done channel + time.After so a hung worker cannot wedge shutdown"
    - "three-action resume (D-A3-01/AM-02): accept -> RoleTool answer; decline -> 'user declined' RoleTool; cancel -> AutoResolveForConversation (abort the turn, inject nothing)"

key-files:
  created:
    - internal/runner/interfaces.go
    - internal/runner/runner.go
    - internal/runner/runner_persist.go
    - internal/runner/runner_resume.go
    - internal/runner/runner_test.go
    - internal/runner/runner_more_test.go
    - internal/runner/runner_errors_test.go
    - internal/runner/fakes_test.go
    - internal/runner/main_test.go
    - cmd/aura/chat_repl.go
    - cmd/aura/paused_states.go
    - cmd/aura/cmdfakes_test.go
    - scripts/microcompact_smoke.sh
  modified:
    - cmd/aura/chat.go
    - cmd/aura/chat_render.go
    - cmd/aura/chat_test.go
    - cmd/aura/cover_test.go
    - cmd/aura/main.go
    - internal/askuser/store.go
    - internal/conversations/title.go
    - internal/conversations/tiktoken.go
    - internal/conversations/context_test.go
    - internal/db/queries/paused_states.sql
    - internal/db/sqlc/paused_states.sql.go
    - internal/db/sqlc/querier.go

key-decisions:
  - "Event-sourced persistence over a post-run history diff: the Runner persists from the observed Event stream (final Event -> answer turn, pause Event -> assistant ask_user turn + paused_states row), keeping LoadHistory a function of completed turns and the agent DB-free (AM-01)"
  - "assistantAskUserToolCalls reconstructs the ask_user tool_call from the AwaitingInput payload so the persisted assistant turn is wire-valid on resume (D-A1-07); the RoleTool answer is keyed by the original ToolCallID (SC-4 / T-04-20)"
  - "added ListRecentPausedStates sqlc query + askuser.ListRecent projection so `aura paused-states list` can show resolved rows with their resumed_answer (ListPending only returns unresolved rows) — Req#11 acceptance"
  - "exported conversations.GenerateTitle + conversations.InitEncoder so the Runner owns the auto-title WaitGroup/WithoutCancel lifecycle and the boot path eagerly inits the cl100k encoder once (D-A2-06 / D-A5-01)"
  - "cobra -> hand-rolled switch confirmed for chat + paused-states groups too (OQ1, same as identity in 04-02); grep -ri cobra go.mod empty"

requirements-completed: [CORE-02, CORE-04, CORE-05]

# Metrics
duration: ~30min
completed: 2026-05-30
coverage: "86.5% (internal/runner, unit tier with in-memory fakes; floor 85%)"
files-created: 13
files-modified: 12
---

# Phase 4 Plan 05: HITL + Conversation Orchestration (Runner + REPL + FTS CLI) Summary

**One-liner:** The integration wave that ties the 04-01..04-04 substrate together — `runner.Runner` (Turn/SubmitAnswer(s)/Stop, the SOLE writer of paused_states, resume-as-fresh-Run with no silent LLM re-run per SC-4, a goleak-clean auto-title WaitGroup) over consumer-side narrow interfaces, the `aura chat` REPL refactored to drive the Runner with inline kind-specific pause rendering + the boot composition root, the `aura paused-states` + `aura chat search` switch CLIs, and the `microcompact_smoke.sh` L1/SC-1/L2.5 CI contract.

## What Was Built

### Task 1 — runner.Runner (commit `29e96296`, TDD)
`internal/runner/` — the orchestration layer (D-A1-01), NOT an `agent.Agent`, no collision with `workflow.LoopAgent` (AM-03):
- **Consumer-side narrow interfaces** (`interfaces.go`, D-A2-02): `ConversationStore` / `PauseStore` / `IdentityStore` — only the methods the Runner calls; the concrete Stores satisfy them implicitly. Hand-written in-memory fakes (`fakes_test.go`) support the 85% floor without a DB.
- **`Turn(ctx, convID, userMsg *string) iter.Seq2[*agent.Event, error]`** — the sole loop-driver. It persists the user turn, loads the L1/L2/L2.5-managed history, builds a **FRESH** `LlmAgent` per round (Pattern-4, seeded via `LlmAgentConfig.UserTurns`), drives one round, and **Event-sources** persistence: a final Event → the assistant answer turn + usage; a pause Event → the assistant `ask_user` tool_call turn + the `paused_states` row (the SOLE `Insert` caller, T-04-19). `userMsg=nil` is continue-after-resume.
- **`SubmitAnswer` / `SubmitAnswers`** — the three-action model (D-A3-01/AM-02): `accept` injects `RoleTool{ToolCallID:<original>}`; `decline` injects "user declined"; `cancel` routes through `AutoResolveForConversation`. Both return the remaining-pending count.
- **`Stop`** — `AutoResolveForConversation` (zero unresolved after) + a bounded `wg.Wait()` join of the auto-title worker (goleak-clean).
- **Auto-title** (D-A5-01) — after `seq>=3` and while untitled, `wg.Add(1)` + `WithoutCancel(turnCtx)` + `WithTimeout` worker calls `conversations.GenerateTitle` then `SetTitleIfNull`; errors never block chat.
- **SC-4 proven** with `agenttest.FakeClient`: the resume request carries the original `ask_user` question→answer pair with **exactly one** ask_user tool_call (no duplicate) and **no replay LLM call** (CallCount asserted).

### Task 2 — aura chat REPL + paused-states CLI + search + composition root (commit `9054afa9`)
- **`cmd/aura/chat.go`** refactored into a `switch` group `{list|new|resume|archive|unarchive|delete|rename|search}` (NOT cobra — OQ1); bare `aura chat` = a new persisted REPL; `resume` (no id) = most-recent active; `delete --confirm` gated like `dbReset`.
- **Composition root** (`bootChat`, D-A2-05): `config.Load` → `db.Open` → 3 Stores → `ScanOrphans` (Req#12, before serving) → `InitEncoder` (cl100k once) → `runner.Runner`.
- **`cmd/aura/chat_repl.go`** — the Runner-driven REPL loop (`chatLoop`/`runUserTurn`/`driveTurn`) preserving Phase-3 streaming + dim tool-activity + cost footer + two-stage Ctrl+C, now sourced from the Runner's Event stream; **inline kind-specific pause rendering** (D-A3-02): `clarification`=free-text, `approval`=`[y/N]` default No, `choice`=numbered 1..N; `SubmitAnswer` → `Turn(convID,nil)` when `remaining==0`. The `chat search` excerpt rendering (`conv_id|seq|similarity|excerpt`) windows ±60 chars over the locked FTS query (D-A5-03).
- **`cmd/aura/paused_states.go`** — `aura paused-states {list|purge --before <ISO> --confirm}`; `list` shows the resolved rows + auto-terminated answer via the new `askuser.ListRecent`; `purge` gated by `--confirm`.
- **Substrate touches**: `ListRecentPausedStates` sqlc query + `askuser.ListRecent` projection; `conversations.GenerateTitle` + `conversations.InitEncoder` exported; `runner.PendingFor` + `PauseStore.GetByToken`.

### Task 3 — microcompact_smoke.sh (commit `cd8b7923`)
`scripts/microcompact_smoke.sh` mirrors `loop_budget_smoke.sh`: it RUNS the conversations `db_integration` tier live and asserts the ground-truth — L1 evicts an old tool turn to a `read_tool_output(<id>)` pointer, **SC-1** writes ZERO `context_rot_events` when L1 alone suffices, and **L2.5** drops the oldest pair writing exactly one `context_rot_events` row with `action='hard_drop_pairs'` (the integration test was strengthened to assert the action value). No-skip-as-green: the script fails loud if a test was SKIPPED or `[no tests to run]`.

## Verification Evidence

| Gate | Result |
|------|--------|
| `go build ./...` / `go vet ./...` | exit 0 |
| `go test -race ./internal/runner/` (unit, FakeClient) | green; goleak clean (WaitGroup join asserted) |
| `internal/runner` combined coverage | **86.5%** (floor 85%) |
| `go test ./cmd/aura/` (scripted-stdin + fake-client REPL, incl. ask_user pause resume) | green |
| db_integration `-race -p 1` (WSL, live Postgres) | conversations + askuser + identity all green |
| `scripts/microcompact_smoke.sh` (WSL, live) | **PASS** — both L1/SC-1 + L2.5 tests RAN + PASS |
| CLI smoke (WSL, live) | `chat list` shows untitled + `$0.0000`; `chat rename` ok; `chat search "postgres database connection pool"` prints `conv\|seq\|0.492\|excerpt`; `paused-states list` shows the resolved auto-terminated answer; `chat delete --confirm` ok |
| `golangci-lint run` (runner + conversations + askuser + cmd/aura) | **0 issues** |
| `grep -ri cobra go.mod` / `grep -r timed_out internal/` | empty |
| `grep -q "case \"paused-states\"" main.go` / `grep -q "runner\." chat.go` | both present |
| file sizes | every touched file ≤600 LOC (largest chat.go 348) |

## SC-1..SC-4 Disposition

- **SC-1 (L1-first / zero rot rows)** — proven by `microcompact_smoke.sh` running `TestLoadManagedHistory_SC1_NoRotEventOnL1Alone` live (zero `context_rot_events` when L1 alone suffices). ✓
- **SC-2 (crash atomicity)** — owned by 04-04 `AppendTurn` (one `db.WithTx`); the Runner drives `AppendTurn`, inheriting it. ✓ (no new surface here)
- **SC-3 (resume never inherits a broken state)** — `Stop`/boot `ScanOrphans` (Req#11/#12) reconcile pendings + orphan dirs; the REPL boot path calls `ScanOrphans` before serving and `Stop` auto-resolves on exit. ✓
- **SC-4 (pause = no silent LLM re-run)** — unit-asserted in `TestResume_NoSilentReRun_SC4`: exactly one ask_user tool_call in the resume request, the injected `RoleTool{ToolCallID:<original>}` answer present, and exactly one extra LLM call (no replay). ✓

## Full-Phase Acceptance Status (14 SPEC criteria)

All 14 are now verifiable end-to-end across the phase: ask_user pause→CLI resume (Req#1/#2/#3 via the REPL + Runner); no-timeout (Req#4, `grep -r timed_out internal/` empty); identity seed + wildcard + CLI (Req#5/#6, 04-02 + live); conversation persist + byte-identical resume + atomic per-turn (Req#7/#8, 04-04 + Runner-driven); auto-title + cost aggregation (Req#9, Runner WaitGroup + `chat list` non-zero cost); context L1/L2/L2.5 (Req#10, smoke); `Loop.Stop` auto-resolve + `paused-states` CLI (Req#11, live `paused-states list` shows the auto-terminated answer); orphan scan + cleanup (Req#12, `ScanOrphans` at boot); FTS + `chat search` (Req#13, live `chat search` prints conv|seq|similarity|excerpt); migrations 0003-0006 (Req#14, 04-01).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 — missing functionality] `aura paused-states list` needed a list-all query**
- **Found during:** Task 2 — the SPEC Req#11 acceptance requires `paused-states list` to show the auto-resolved rows with their answer, but the 04-01 sqlc surface only had `ListPendingPausedStates` (resumed_at IS NULL).
- **Fix:** Added `ListRecentPausedStates :many` (newest-first, limit) + regenerated sqlc + an `askuser.ListRecent` projection that decodes the `resumed_answer` jsonb content. Live-verified: `paused-states list` shows the `<auto-terminated: conversation ended>` answer on a resolved row.
- **Files:** `internal/db/queries/paused_states.sql`, `internal/db/sqlc/*` (regen), `internal/askuser/store.go`.
- **Commit:** `9054afa9`.

**2. [Rule 3 — blocking] exported entry points the Runner consumes were unexported**
- **Found during:** Tasks 1-2 — `conversations.generateTitle` (the auto-title worker body) and the lazy tiktoken `encoder()` were package-internal, but the Runner owns the WaitGroup lifecycle and the boot path must init the encoder once.
- **Fix:** Added thin exported wrappers `conversations.GenerateTitle` and `conversations.InitEncoder` (idempotent, goleak-safe) — no logic change, just the public seam the orchestrator needs.
- **Files:** `internal/conversations/title.go`, `internal/conversations/tiktoken.go`.
- **Commit:** `29e96296` (title), `9054afa9` (encoder).

**3. [Rule 2 — interface completeness] PauseStore needed GetByToken**
- **Found during:** Task 1 — `SubmitAnswer` must look up the original `tool_call_id` to key the injected RoleTool answer; `ListPending` alone could not resolve a single token's ToolCallID.
- **Fix:** Added `GetByToken` to the `PauseStore` interface (already on `*askuser.Store`) + `runner.PendingFor` for the REPL.
- **Files:** `internal/runner/interfaces.go`, `internal/runner/runner_resume.go`.
- **Commit:** `29e96296` / `9054afa9`.

**Total deviations:** 3 auto-fixed (1 missing query for an acceptance requirement, 1 blocking unexported-seam, 1 interface completeness). No scope creep — all three are required for the plan's own acceptance.

## TDD Note (lefthook-vet constraint)

Task 1 is `tdd=true`. The tests were authored test-first and verified to fail (`undefined: Runner`) before any implementation. However, the repo's lefthook pre-commit gate runs `go vet ./...` on the whole module, which refuses to commit a non-compiling tree — so a standalone RED commit (test files referencing an absent `Runner` type) cannot land. The RED test files therefore committed together with the GREEN implementation in `29e96296`; the test-first discipline (design the API via the tests, verify they fail, then implement to green) was followed, and the commit message records it.

## Known Stubs

None. The orchestration layer is wired end-to-end (Runner → REPL → live DB), verified by the live CLI smoke (chat list/search/rename/delete + paused-states list) and the microcompact smoke.

## Minor Deferred (non-blocking)

`usageFromStateDelta` + `anyInt`/`anyFloat` (the per-turn usage projection off the Event StateDelta) exist in both `cmd/aura/chat_render.go` (transport rendering) and `internal/runner/runner_persist.go` (persistence). The `dupl` linter (the codebase's mechanized CLAUDE.md dedup enforcement, threshold 100) does not trip on them (each ~15 LOC at different layers), and `golangci-lint run` is 0 issues. A future touch could lift the canonical helper into `internal/agent` (which owns the StateDelta keys); left as-is to keep this plan's scope tight.

## Self-Check: PASSED

All created files verified present (`internal/runner/{interfaces,runner,runner_persist,runner_resume}.go` + 5 test files; `cmd/aura/{chat_repl,paused_states,cmdfakes_test}.go`; `scripts/microcompact_smoke.sh`). All three task commits present in git history (`29e96296`, `9054afa9`, `cd8b7923`).
