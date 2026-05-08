# Runner Boundary & Health Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` to execute this plan. Use `test-driven-development` for each code slice and `verification-before-completion` before claiming completion.

**Goal:** make Telegram a thin adapter by moving remaining session/finalization responsibilities behind `internal/agentruntime`, expose compact-memory/Qdrant mirror health, and make broad debug smokes catch slow or wasteful turns before Docker deployment.

**Architecture:** keep the Phase 08 Runtime Diet shape. Do not reintroduce profile/preflight taxonomy, always-on summarization, swarm routing, legacy wiki/skill wrappers, or prompt-era policy objects. `internal/agentruntime` owns invocation/session/event boundaries; `internal/telegram` prepares Telegram-specific inputs and delivers Telegram-specific outputs; `internal/api` reports health from runtime-owned state; `cmd/debug_telegram_sandbox` validates the live runtime shape.

**Tech Stack:** Go, existing `internal/agentloop`, `internal/agentruntime`, `internal/orchestration`, `internal/telegram`, SQLite, Qdrant, Docker Compose, embedded React dashboard health API.

---

## Non-Negotiables

- [ ] Preserve the Runtime Diet hot path: ordinary text turns must not trigger broad source/archive scans, skill scans, swarm, summarizer, or graph rebuilds.
- [ ] Delete or move code that exists only to compensate for old orchestration complexity; do not disable it behind another setting.
- [ ] Keep skills as file-backed procedures in runtime workspace storage, not source-repo defaults.
- [ ] Keep compact memory graph retrieval powerful: exact + FTS + vector + graph-expanded facts stay available through `search_memory`.
- [ ] Do not mutate `data/aura.db` from the host while Docker Compose `aura` is running.
- [ ] Do not make Telegram own generic runtime decisions that belong in `internal/agentruntime`.

## Task 1: Baseline And Cut Lines

- [x] Capture current runtime boundaries before editing.
  - Read:
    - `internal/agentruntime/runner.go`
    - `internal/agentruntime/runner_test.go`
    - `internal/telegram/conversation.go`
    - `internal/telegram/conversation_terminal.go`
    - `internal/telegram/debug_smoke.go`
    - `internal/api/health.go`
    - `internal/api/types.go`
    - `internal/telegram/setup.go`
    - `cmd/debug_telegram_sandbox/main.go`
  - Confirm `git status --short` is clean or contains only intentional user changes.
- [x] Record the live god-class responsibilities in this plan under `Implementation Notes` before changing code:
  - session active-state tracking;
  - conversation context load/store;
  - user/assistant message append;
  - prompt/retrieval/toolset preparation;
  - event emission and debug snapshot population;
  - terminal tool no-tool finalization;
  - archive append and context enforcement;
  - user-facing delivery.
- [x] Define the cut:
  - Telegram keeps Telegram I/O, chat IDs, message placeholders, and delivery.
  - `agentruntime` owns generic invocation events, finalization decisions, session lifecycle hooks, and per-turn result metadata.
- [x] Verification:
  - `go test ./internal/agentruntime ./internal/telegram -count=1`

## Task 2: Runner-Owned Session Boundary

- [x] Add a small session abstraction in `internal/agentruntime`.
  - Create `internal/agentruntime/session.go`.
  - Add a minimal interface for turn-scoped state:
    - `Begin(ctx, userID, config)`;
    - `Conversation()`;
    - `Finish(result)`;
    - `Abort(err)`.
  - Back it with the existing `conversation.Context` type and current in-memory maps.
  - Keep the abstraction boring: no database writes, no summarizer, no routing policy.
- [x] Move active-session bookkeeping out of `internal/telegram/conversation.go`.
  - Replace direct `b.active.Store/Delete` and raw `b.ctxMap.LoadOrStore/Store` usage with the new `agentruntime` session boundary.
  - Leave Telegram-specific chat/user resolution in Telegram.
- [x] Add tests.
  - `internal/agentruntime/session_test.go` covers:
    - first turn creates context;
    - next turn reuses context;
    - active marker is cleared on finish;
    - active marker is cleared on error/abort;
    - existing context message order is preserved.
  - Update Telegram tests only where they currently assert raw map behavior.
- [x] Delete any now-unused Telegram helper code for active/context map lifecycle.
- [x] Verification:
  - `go test ./internal/agentruntime ./internal/telegram -count=1`

## Task 3: Runtime Turn Result And Event Contract

- [x] Extend `internal/agentruntime/runner.go` without turning it into another god class.
  - Add `TurnResult` with:
    - final text;
    - delivered flag;
    - terminal tool name;
    - loop steps;
    - LLM call count;
    - tool call count;
    - exposed tools;
    - prompt hash/version;
    - retrieval capsule present flag.
  - Keep existing `EventToolsExposed`, `EventStats`, and `EventFinal`.
  - Add only events that the runtime actually emits and Telegram/debug smokes consume.
- [x] Move debug snapshot population to consume runner events instead of Telegram-local side effects.
  - Touch:
    - `internal/telegram/conversation.go`
    - `internal/telegram/conversation_snapshot.go`
    - `internal/telegram/debug_smoke.go`
  - The debug smoke should read result metadata from the runtime event/result path, not reconstruct state from scattered Telegram fields.
- [x] Add tests.
  - `internal/agentruntime/runner_test.go` verifies event order and `TurnResult` population.
  - `internal/telegram/debug_smoke_test.go` verifies loop/tool/LLM counters still appear.
- [x] Delete stale duplicated stats wiring after the runtime result is canonical.
- [x] Verification:
  - `go test ./internal/agentruntime ./internal/telegram ./cmd/debug_telegram_sandbox -count=1`

## Task 4: Terminal Tool Finalization Behind Runtime Boundary

- [x] Extract generic terminal finalization out of Telegram.
  - Create `internal/agentruntime/terminal.go`.
  - Move generic decisions from `internal/telegram/conversation_terminal.go`:
    - terminal tool can finish without another LLM pass;
    - typed document result can become a concise final response;
    - raw tool JSON must not leak to the user;
    - delivered artifacts mark the result as delivered.
  - Keep Telegram-only send/edit behavior in Telegram as callbacks.
- [x] Keep route-specific document behavior intact.
  - `create_docx`, `create_xlsx`, and `create_pdf` remain terminal document tools.
  - `execute_code` and other terminal tools still support compact finalization.
  - Hidden-tool errors remain capability-boundary messages, not raw registry errors.
- [x] Add tests.
  - `internal/agentruntime/terminal_test.go` covers:
    - document artifact finalization;
    - malformed tool result fallback;
    - no raw JSON final answer;
    - delivered artifact state;
    - optional no-tool LLM finalization when needed.
  - Update `internal/telegram` tests to assert Telegram delivery callbacks, not finalization logic.
- [x] Delete leftover duplicated finalization branches in `internal/telegram/conversation_terminal.go` once covered by runtime tests.
- [x] Verification:
  - `go test ./internal/agentruntime ./internal/agentloop ./internal/telegram ./cmd/debug_telegram_sandbox -count=1`

## Task 5: Compact Memory Mirror Health

- [x] Add explicit compact-memory vector sync state.
  - Create `internal/memoryindex/vector_health.go`.
  - Add a thread-safe tracker with:
    - enabled;
    - running;
    - collection;
    - vector size;
    - last started time;
    - last finished time;
    - last indexed document count;
    - last error.
- [x] Wire tracker updates into `internal/telegram/setup.go`.
  - Mark sync started before `memoryStore.SyncVector`.
  - Mark success with `VectorReport`.
  - Mark failure with error text.
  - Do not block startup on Qdrant mirror sync.
- [x] Expose health in API.
  - Update `internal/api/types.go`:
    - add `CompactMemory CompactMemoryHealth`.
  - Update `internal/api/router.go` dependencies:
    - add compact memory health reader.
  - Update `internal/api/health.go`:
    - include compact memory mirror state in `/api/health`.
  - If `/status` is a separate Telegram or HTTP path, include a compact text summary there too.
- [x] Add tests.
  - `internal/memoryindex/vector_health_test.go`
  - `internal/api/router_test.go` for health payload.
- [x] Verification:
  - `go test ./internal/memoryindex ./internal/api ./internal/telegram -count=1`

## Task 6: Docker-First Debug Smoke Defaults

- [x] Make `cmd/debug_telegram_sandbox` prefer runtime paths and container-owned behavior.
  - Default workspace root should match Docker runtime intent when running from the repo.
  - Legacy local `./wiki` and `./skills` paths should be mapped to `runtime-workspace` or container paths only for compatibility.
  - The smoke must never mutate live `data/aura.db` from the host while Compose `aura` is running.
- [x] Add explicit performance/tool gates.
  - `-expect-llm-calls-max`
  - `-expect-tool-calls-max`
  - `-expect-loop-steps-max` already exists; keep it canonical.
  - Keep `-max-elapsed-ms`.
- [ ] Add broad prompt fixtures.
  - Project/status prompt: expects default toolset, low loop count, no document tool.
  - Memory prompt: expects retrieval path only when memory is requested.
  - Document prompt: expects `document` toolset and terminal document tool.
- [x] Add tests in `cmd/debug_telegram_sandbox`.
  - Flag parsing.
  - Expectation failures return non-zero.
  - Docker-first path normalization does not point at deleted `D:\Aura\wiki` or repo `skills`.
- [x] Verification:
  - `go test ./cmd/debug_telegram_sandbox -count=1`

## Task 7: Broad Runtime Smokes

- [ ] Rebuild and run the container.
  - `docker compose config --quiet`
  - `docker compose up -d --build aura`
- [ ] Verify health.
  - `/status` returns `ok`.
  - `/api/health` includes compact memory mirror state.
  - Logs show compact mirror sync started and either synced or degraded clearly.
- [ ] Run broad debug smokes.
  - Broad status prompt:
    - max elapsed: 30000 ms;
    - max loop steps: 2;
    - max LLM calls: 2;
    - max tool calls: 2.
    - 2026-05-08 local smoke passed after removing default swarm exposure:
      - `elapsed_ms=10588`
      - `llm_calls=2`
      - `tool_calls_count=2`
      - `loop_steps=2`
      - `tools_called=daily_briefing,search_memory`
      - `swarm_used=false`
  - Memory prompt:
    - uses `search_memory` when the prompt asks for memory/wiki/source/archive context;
    - max elapsed: 30000 ms.
  - Document prompt:
    - toolset `document`;
    - terminal tool `create_docx` or requested typed file tool;
    - max loop steps: 1 when retrieval capsule is sufficient;
    - max elapsed: 30000 ms.
- [ ] Save the exact smoke commands and outputs into this plan under `Verification Log`.

## Task 8: Documentation And Closure

- [ ] Update `.planning/STATE.md`.
  - Active milestone becomes `v3.3 Runner Boundary & Health Hardening` while work is active.
  - Closure evidence includes tests, Docker rebuild, health payload, and broad smokes once complete.
- [ ] Update `.planning/ROADMAP.md`.
  - Add v3.3 as active milestone before v4.0.
  - Keep v4.0 MCP Marketplace as next after v3.3.
- [ ] Update `docs/implementation-tracker.md`.
  - Add a short entry for each implemented slice.
- [ ] When complete, mark this plan closed.
  - Include test commands.
  - Include Docker status.
  - Include smoke outputs.
  - Include deleted-code summary.
- [ ] Final verification before merge:
  - `go fmt ./...`
  - `go test ./internal/agentruntime ./internal/agentloop ./internal/orchestration ./internal/telegram ./internal/memoryindex ./internal/api ./cmd/debug_telegram_sandbox -count=1`
  - `go test ./...`
  - `go build ./...`
  - `docker compose config --quiet`
  - `docker compose up -d --build aura`
  - live `/status`
  - broad debug smokes from Task 7

## Implementation Notes

Task 1 baseline:

- `internal/telegram/conversation.go` owned active-session marking, context creation/reuse, user/system/search message mutation, prompt/retrieval/toolset preparation, tool-loop invocation, archive append, async context enforcement, and final telemetry.
- `internal/telegram/conversation.go` still owns prompt/retrieval/toolset preparation after this slice; that remains intentionally outside the first session-boundary cut.
- `internal/telegram/conversation_snapshot.go` now aliases `agentruntime.Snapshot` and stores snapshots through `agentruntime.SessionStore`; Telegram no longer owns `orchMap`.
- `internal/telegram/conversation_terminal.go` still owns Telegram delivery, but the no-tool terminal LLM request/fallback/usage/cost path now goes through `agentruntime.FinalizeTerminalTool`.
- `internal/telegram/debug_smoke.go` and `internal/telegram/status.go` read conversation state through `agentruntime.SessionStore` instead of raw Telegram maps.
- `internal/telegram/setup.go` starts compact-memory vector mirror sync in the background; health state is now recorded by `memoryindex.VectorHealthTracker` and exposed through `api.HealthRollup.CompactMemory` plus Telegram `/status`.
- 2026-05-08 no-regex routing cut: removed user-text keyword routing from the hot path. `AURA_TOOLSET_MODE=auto` now resolves to the default toolset instead of trying to infer compute/document/admin from substrings. Telegram no longer gates skill manifests, swarm exposure, or speculative retrieval capsules with `strings.Contains` lists. Skills remain available as the cached progressive-disclosure manifest; compact memory remains available through `search_memory`; specialized toolsets are explicit runtime settings rather than hidden text classifiers.

## Verification Log

- `go test ./internal/agentruntime -run TestSession -count=1` failed before `SessionStore` existed, then passed after `internal/agentruntime/session.go`.
- `go test ./internal/agentruntime ./internal/telegram -count=1` passed after moving Telegram active/context lifecycle behind `agentruntime.SessionStore`.
- `go test ./cmd/debug_telegram_sandbox -run "TestValidateDebugExpectations(RejectsLLMCallOverBudget|RejectsToolCallOverBudget|AcceptsMatchedOrchestrationSignals)" -count=1` failed before `LLMCalls`, `ToolCallsCount`, `MaxLLMCalls`, and `MaxToolCalls` existed, then passed after adding the debug smoke gates.
- `go test ./internal/agentruntime -run TestTerminal -count=1` failed before terminal finalization helpers existed in `agentruntime`, then passed after moving generic terminal formatting/safety logic.
- `go test ./internal/memoryindex ./internal/api -run "TestVectorHealthTrackerTransitions|TestHealthRollup_IncludesCompactMemoryHealth" -count=1` failed before vector health/API wiring existed, then passed after adding `VectorHealthTracker` and `/health` compact-memory state.
- `go test ./internal/agentruntime ./internal/agentloop ./internal/orchestration ./internal/telegram ./internal/memoryindex ./internal/api ./cmd/debug_telegram_sandbox -count=1` passed.
- `go test ./...` passed.
- `go build ./...` passed.
- `go test ./internal/agentruntime -run TestSessionStoreStoresAndPrunesSnapshots -count=1` failed before runtime-owned snapshots existed, then passed after moving snapshot storage into `agentruntime.SessionStore`.
- `go test ./internal/agentruntime -run TestFinalizeTerminalTool -count=1` failed before no-tool terminal LLM finalization existed in `agentruntime`, then passed after adding `FinalizeTerminalTool`.
- `go test ./internal/telegram -run TestCompactMemoryStatusSummaryReportsMirrorState -count=1` failed before `/status` compact-memory summary existed, then passed after wiring the health reader into `Bot`.
- `go test ./internal/agentruntime ./internal/agentloop ./internal/orchestration ./internal/telegram ./internal/memoryindex ./internal/api ./cmd/debug_telegram_sandbox -count=1; go test ./...; go build ./...` passed after the review fixes.
- `go test ./internal/orchestration ./internal/telegram ./cmd/debug_telegram_sandbox -count=1` passed after deleting the keyword-based toolset/skill/swarm/retrieval routing and updating tests to assert explicit toolset selection.
- `go run ./cmd/debug_telegram_sandbox -no-validate -prompt "fammi il punto sintetico sullo stato di Aura e non creare file" -expect-toolset default -expect-loop-steps-max 2 -expect-llm-calls-max 2 -expect-tool-calls-max 2 -max-elapsed-ms 30000 -expect-workspace-root /workspace` passed with `elapsed_ms=10588`, `llm_calls=2`, `tool_calls_count=2`, `loop_steps=2`, `tools_called=daily_briefing,search_memory`, `swarm_used=false`.

## Commit Plan

- [x] Commit 1: `agentruntime: own session boundary`
- [x] Commit 2: `agentruntime: finalize terminal tools`
- [x] Commit 3: `health: expose compact memory mirror`
- [x] Commit 4: `debug smoke: enforce broad runtime budgets`
- [ ] Commit 5: `docs: close runner boundary phase`

Keep commits small enough to review. If a task reveals obsolete code that no longer has callers, delete it in the same commit that removes the caller.
