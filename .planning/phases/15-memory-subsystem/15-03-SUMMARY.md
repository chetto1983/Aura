---
phase: 15-memory-subsystem
plan: 03
subsystem: api
tags: [mcp, agent-memory, cli, operator-tooling, streamable-http, neo4j-agent-memory]

# Dependency graph
requires:
  - phase: 15-01
    provides: "PRD amendment #62 re-scope (UX-08 advisory snapshot, agent-memory MCP adoption contract)"
provides:
  - "`aura memory <verb>` operator CLI: a verb router that opens the managed memory sidecar over streamable-HTTP and calls RAW memory_* MCP tools directly, bypassing the agent loop"
  - "Verb→RAW-tool mapping over the spike-032 16-tool surface (search/context/sessions/conversation/add-entity/add-fact/add-preference/store-message/get-entity/relationship/export/trace {start|step|complete|observations}/query)"
  - "Unit-tier verb-mapping proof over a recording fake streamable-HTTP MCP transport (no live sidecar)"
affects:
  - "15-04 (memory_integration tier): the recall verb is the hook the advisory recall@5/p95 snapshot measures; live seed/read + trace round-trip exercise this CLI"
  - "15-05 (reproducible compose build): the CLI is the operator verification surface against the rebuilt image"

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Operator CLI verb router over a managed MCP sidecar: reuse effectiveManagedMCPServer + mcp.OpenServer + Transport.CallTool with a 20s timeout, RAW wire tool names (same shape as cmd/aura/mcp_tools.go)"
    - "Recording streamable-HTTP MCP fake (extends managed_mount_test.go's httpMCPServer with a tools/call recorder) for unit-tier verb→tool assertions"

key-files:
  created:
    - cmd/aura/memory.go
    - cmd/aura/memory_test.go
  modified:
    - cmd/aura/main.go

key-decisions:
  - "Verb router holds a table-driven memoryVerbToTool mapping returning the RAW tool name + args, instead of a per-verb literal CallTool call — cleaner, keeps the single CallTool site, and the RAW-name contract is unit-proven by TestMemoryVerbMapping rather than by a source grep"
  - "facts-read has no standalone memory_get_facts on the live surface (Open Q4); the `query` verb exposes read-only graph_query and `search` covers entity/preference recall"
  - "Write verbs are deliberate operator-invoked calls (D-01/D-02); recall verbs are pull-on-demand (D-03) — no passive extraction surface added"

patterns-established:
  - "Pattern: aura <domain> CLI = top-level run<Domain>(args) → run<Domain>Command(ctx, args, out) switch, usage-on-error + os.Exit(1), wired into main.go switch + usage()"
  - "Pattern: unit-test a managed-MCP CLI by writing a streamable_http ManagedServer into a temp AURA_MCP_CONFIG so effectiveManagedMCPServer resolves to an httptest fake"

requirements-completed: [UX-08]

# Metrics
duration: 6min
completed: 2026-06-12
---

# Phase 15 Plan 03: `aura memory` Operator CLI Summary

**`aura memory <verb>` verb router that opens the managed agent-memory sidecar over streamable-HTTP and calls the RAW `memory_*` MCP tools directly (bypassing the agent loop), unit-proven over a recording fake transport.**

## Performance

- **Duration:** 6 min
- **Started:** 2026-06-12T06:53:33Z
- **Completed:** 2026-06-12T06:59:xxZ
- **Tasks:** 2
- **Files modified:** 3 (2 created, 1 modified)

## Accomplishments

- `cmd/aura/memory.go` (243 LOC): `runMemory`/`runMemoryCommand` dispatch each verb to its RAW `memory_*` (or read-only `graph_query`) wire tool via `effectiveManagedMCPServer("memory")` → `mcp.OpenServer` → `CallTool`, with a 20s timeout that fails fast on a dead sidecar (T-15-03-03).
- All 15 `memory_*` tools + `graph_query` mapped: search/context/sessions/conversation/add-entity/add-fact/add-preference/store-message/get-entity/relationship/export, plus the reasoning-trace surface `trace {start|step|complete|observations}` and read-only `query`.
- Wired `case "memory": runMemory(os.Args[2:])` into the `main.go` top-level switch and added `memory <sub>` to `usage()`.
- `cmd/aura/memory_test.go` (215 LOC): `TestMemoryVerbMapping` — a 16-row table asserting each verb dispatches the expected RAW name (no `memory__` prefix) over a recording streamable-HTTP MCP fake, the canned result reaches `out`, and a representative arg is marshaled; `TestMemoryVerbMappingNegativeCases` (6 rows: no-args/unknown verb/missing args/unknown trace subverb); `TestMemoryNotConfigured`.

## Task Commits

Each task was committed atomically:

1. **Task 1: `cmd/aura/memory.go` verb router → direct memory_* CallTool** - `cde90530` (feat)
2. **Task 2: unit-test the verb→tool mapping over a fake streamable-HTTP MCP transport** - `0f446b4e` (test)

_Note: this plan's two tasks are both `tdd="true"`; because the test file (Task 2) and router (Task 1) are split across plan tasks, the cycle was router-first (Task 1, build/vet green) then the mapping test (Task 2, GREEN-confirming the router + RAW-name contract). The lint auto-fix was folded into the Task 2 commit before it landed._

## Files Created/Modified

- `cmd/aura/memory.go` (created) - verb router + `memoryVerbToTool` mapping + `callMemoryTool` (managed-server resolve → OpenServer → RAW CallTool, 20s timeout) + small arg helpers.
- `cmd/aura/memory_test.go` (created) - recording streamable-HTTP MCP fake + verb→tool table + negative + not-configured tests.
- `cmd/aura/main.go` (modified) - `case "memory"` in the top-level switch; `memory <sub>` added to `usage()`.

## Decisions Made

- **Table-driven router over per-verb CallTool literals.** The plan's `key_links` pattern hinted at a `CallTool(ctx, "memory_..."` source literal per verb; instead `memoryVerbToTool` returns the RAW name from a switch and there is a single `CallTool(callCtx, tool, args)` site. This keeps the open/timeout/close logic DRY and makes the RAW-wire-name contract a unit-tested invariant (`TestMemoryVerbMapping` asserts each dispatched name and that none carry the `memory__` prefix) rather than a brittle grep. The 15 `return "memory_*"` literals + `graph_query` are all present and greppable.
- **No `memory_get_facts`** (Open Q4): fact reads route through `query` (read-only `graph_query`) or `search`; matches the spike-032 live 16-tool surface and spike-033's read-back path.
- **Operator path = deliberate writes + pull-on-demand recall** (D-01/D-02/D-03): write verbs only fire when invoked; the CLI adds no passive/every-turn extraction surface. A file-level comment cites D-01/D-03.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed staticcheck QF1008 on the test fake's cleanup selector**
- **Found during:** Task 2 (verb-mapping test)
- **Issue:** `t.Cleanup(rec.Server.Close)` selected through the embedded `*httptest.Server` field explicitly; `golangci-lint`/staticcheck flagged QF1008 (the promoted `Close` should be used). The project gate (`golangci-lint run ./cmd/aura/`) is part of Gate 2.
- **Fix:** Changed to `t.Cleanup(rec.Close)` (promoted method).
- **Files modified:** cmd/aura/memory_test.go
- **Verification:** `golangci-lint run ./cmd/aura/` → 0 issues; tests still pass.
- **Committed in:** `0f446b4e` (Task 2 commit, before it landed)

---

**Total deviations:** 1 auto-fixed (1 lint/quality bug).
**Impact on plan:** Lint cleanliness is a Gate-2 requirement; no scope change, no behavior change.

## Issues Encountered

- **Worktree-path gotcha (no code impact):** the plan's verify commands literally prefix `cd /d/Aura && go test ...`, which runs against the **main repo** (`D:\Aura\cmd\aura`), not this worktree — `go test ... -run Memory` reported `[no tests to run]` (a classic false-green) because the new files live only in the worktree. Resolved by running the verify commands from the worktree root (no `cd /d/Aura`). All tests pass in the worktree: `go test ./cmd/aura/ -run Memory` and `go test -race ./cmd/aura/ -run Memory` (race via `BASH_ENV=~/.aura-toolchain.sh`, the Windows w64devkit fix) are both green.

## User Setup Required

None - no external service configuration required. (The live sidecar seed/read + reasoning-trace round-trip are owned by plan 15-04's `memory_integration` tier; this plan is unit-tier only.)

## Next Phase Readiness

- The `aura memory` CLI is ready as the operator verification + recall hook for 15-04's `memory_integration` live tier (seed via the write verbs, read via `search`/`context`/`query`, trace round-trip via `trace ...`).
- No blockers. The default-on managed mount of the `memory` recipe (so `effectiveManagedMCPServer("memory")` resolves at runtime without `aura mcp install`) is owned by a sibling Wave-1 plan (15-02); the CLI errors clearly ("memory MCP server is not configured or is disabled") until that recipe is mounted.

## Self-Check: PASSED

- `cmd/aura/memory.go`, `cmd/aura/memory_test.go`, `15-03-SUMMARY.md` all exist.
- Commits `cde90530` (feat) and `0f446b4e` (test) present in `git log`.
- `go build`/`go vet`/`go test ./cmd/aura/ -run Memory` (incl. `-race`) and `golangci-lint run ./cmd/aura/` all green in the worktree.

---
*Phase: 15-memory-subsystem*
*Completed: 2026-06-12*
