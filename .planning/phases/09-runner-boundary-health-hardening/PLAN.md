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

- [ ] Capture current runtime boundaries before editing.
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
- [ ] Record the live god-class responsibilities in this plan under `Implementation Notes` before changing code:
  - session active-state tracking;
  - conversation context load/store;
  - user/assistant message append;
  - prompt/retrieval/toolset preparation;
  - event emission and debug snapshot population;
  - terminal tool no-tool finalization;
  - archive append and context enforcement;
  - user-facing delivery.
- [ ] Define the cut:
  - Telegram keeps Telegram I/O, chat IDs, message placeholders, and delivery.
  - `agentruntime` owns generic invocation events, finalization decisions, session lifecycle hooks, and per-turn result metadata.
- [ ] Verification:
  - `go test ./internal/agentruntime ./internal/telegram -count=1`

## Task 2: Runner-Owned Session Boundary

- [ ] Add a small session abstraction in `internal/agentruntime`.
  - Create `internal/agentruntime/session.go`.
  - Add a minimal interface for turn-scoped state:
    - `Begin(ctx, userID, config)`;
    - `Conversation()`;
    - `Finish(result)`;
    - `Abort(err)`.
  - Back it with the existing `conversation.Context` type and current in-memory maps.
  - Keep the abstraction boring: no database writes, no summarizer, no routing policy.
- [ ] Move active-session bookkeeping out of `internal/telegram/conversation.go`.
  - Replace direct `b.active.Store/Delete` and raw `b.ctxMap.LoadOrStore/Store` usage with the new `agentruntime` session boundary.
  - Leave Telegram-specific chat/user resolution in Telegram.
- [ ] Add tests.
  - `internal/agentruntime/session_test.go` covers:
    - first turn creates context;
    - next turn reuses context;
    - active marker is cleared on finish;
    - active marker is cleared on error/abort;
    - existing context message order is preserved.
  - Update Telegram tests only where they currently assert raw map behavior.
- [ ] Delete any now-unused Telegram helper code for active/context map lifecycle.
- [ ] Verification:
  - `go test ./internal/agentruntime ./internal/telegram -count=1`

## Task 3: Runtime Turn Result And Event Contract

- [ ] Extend `internal/agentruntime/runner.go` without turning it into another god class.
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
- [ ] Move debug snapshot population to consume runner events instead of Telegram-local side effects.
  - Touch:
    - `internal/telegram/conversation.go`
    - `internal/telegram/conversation_snapshot.go`
    - `internal/telegram/debug_smoke.go`
  - The debug smoke should read result metadata from the runtime event/result path, not reconstruct state from scattered Telegram fields.
- [ ] Add tests.
  - `internal/agentruntime/runner_test.go` verifies event order and `TurnResult` population.
  - `internal/telegram/debug_smoke_test.go` verifies loop/tool/LLM counters still appear.
- [ ] Delete stale duplicated stats wiring after the runtime result is canonical.
- [ ] Verification:
  - `go test ./internal/agentruntime ./internal/telegram ./cmd/debug_telegram_sandbox -count=1`

## Task 4: Terminal Tool Finalization Behind Runtime Boundary

- [ ] Extract generic terminal finalization out of Telegram.
  - Create `internal/agentruntime/terminal.go`.
  - Move generic decisions from `internal/telegram/conversation_terminal.go`:
    - terminal tool can finish without another LLM pass;
    - typed document result can become a concise final response;
    - raw tool JSON must not leak to the user;
    - delivered artifacts mark the result as delivered.
  - Keep Telegram-only send/edit behavior in Telegram as callbacks.
- [ ] Keep route-specific document behavior intact.
  - `create_docx`, `create_xlsx`, and `create_pdf` remain terminal document tools.
  - `execute_code` and other terminal tools still support compact finalization.
  - Hidden-tool errors remain capability-boundary messages, not raw registry errors.
- [ ] Add tests.
  - `internal/agentruntime/terminal_test.go` covers:
    - document artifact finalization;
    - malformed tool result fallback;
    - no raw JSON final answer;
    - delivered artifact state;
    - optional no-tool LLM finalization when needed.
  - Update `internal/telegram` tests to assert Telegram delivery callbacks, not finalization logic.
- [ ] Delete leftover duplicated finalization branches in `internal/telegram/conversation_terminal.go` once covered by runtime tests.
- [ ] Verification:
  - `go test ./internal/agentruntime ./internal/agentloop ./internal/telegram ./cmd/debug_telegram_sandbox -count=1`

## Task 5: Compact Memory Mirror Health

- [ ] Add explicit compact-memory vector sync state.
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
- [ ] Wire tracker updates into `internal/telegram/setup.go`.
  - Mark sync started before `memoryStore.SyncVector`.
  - Mark success with `VectorReport`.
  - Mark failure with error text.
  - Do not block startup on Qdrant mirror sync.
- [ ] Expose health in API.
  - Update `internal/api/types.go`:
    - add `CompactMemory CompactMemoryHealth`.
  - Update `internal/api/router.go` dependencies:
    - add compact memory health reader.
  - Update `internal/api/health.go`:
    - include compact memory mirror state in `/api/health`.
  - If `/status` is a separate Telegram or HTTP path, include a compact text summary there too.
- [ ] Add tests.
  - `internal/memoryindex/vector_health_test.go`
  - `internal/api/router_test.go` for health payload.
- [ ] Verification:
  - `go test ./internal/memoryindex ./internal/api ./internal/telegram -count=1`

## Task 6: Docker-First Debug Smoke Defaults

- [ ] Make `cmd/debug_telegram_sandbox` prefer runtime paths and container-owned behavior.
  - Default workspace root should match Docker runtime intent when running from the repo.
  - Legacy local `./wiki` and `./skills` paths should be mapped to `runtime-workspace` or container paths only for compatibility.
  - The smoke must never mutate live `data/aura.db` from the host while Compose `aura` is running.
- [ ] Add explicit performance/tool gates.
  - `-expect-llm-calls-max`
  - `-expect-tool-calls-max`
  - `-expect-loop-steps-max` already exists; keep it canonical.
  - Keep `-max-elapsed-ms`.
- [ ] Add broad prompt fixtures.
  - Project/status prompt: expects default toolset, low loop count, no document tool.
  - Memory prompt: expects retrieval path only when memory is requested.
  - Document prompt: expects `document` toolset and terminal document tool.
- [ ] Add tests in `cmd/debug_telegram_sandbox`.
  - Flag parsing.
  - Expectation failures return non-zero.
  - Docker-first path normalization does not point at deleted `D:\Aura\wiki` or repo `skills`.
- [ ] Verification:
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

Fill this section while executing Task 1. Keep it factual and tied to files/lines. Do not turn it into a design diary.

## Verification Log

Fill this section while executing Tasks 1-8. Each entry should include the exact command, result, and any relevant latency/tool-call counters.

## Commit Plan

- [ ] Commit 1: `agentruntime: own session boundary`
- [ ] Commit 2: `agentruntime: finalize terminal tools`
- [ ] Commit 3: `health: expose compact memory mirror`
- [ ] Commit 4: `debug smoke: enforce broad runtime budgets`
- [ ] Commit 5: `docs: close runner boundary phase`

Keep commits small enough to review. If a task reveals obsolete code that no longer has callers, delete it in the same commit that removes the caller.
