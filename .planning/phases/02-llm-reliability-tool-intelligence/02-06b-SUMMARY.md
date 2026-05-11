---
phase: 02-llm-reliability-tool-intelligence
plan: 06b
subsystem: telegram/api/tools
tags: [reindex, wiring, deletion-sweep, health-api, write_wiki_page]
dependency_graph:
  requires: [02-06]
  provides: [system-wide-wiring, tool_search-deletion, /api/health-reindex]
  affects: [internal/telegram/setup.go, internal/telegram/bot.go, internal/api/health.go, internal/api/types.go, internal/api/router.go]
tech_stack:
  added: [reindex.Worker in Bot, ReindexHealthResponse in /api/health]
  patterns: [BLOCKER 6 Option B (searchEngine captured before Bot), BLOCKER 3 (local *wiki.Store not b.wiki), nil-safe accessors]
key_files:
  created:
    - internal/api/health_test.go
  modified:
    - internal/telegram/setup.go
    - internal/telegram/bot.go
    - internal/telegram/conversation_tool_exec.go
    - internal/telegram/debug_smoke.go
    - internal/telegram/debug_smoke_test.go
    - internal/tools/exec.go
    - internal/tools/registry_test.go
    - internal/api/health.go
    - internal/api/router.go
    - internal/api/types.go
    - cmd/debug_telegram_sandbox/main.go
    - scripts/test-runtime-answer-discipline-smokes.ps1
  deleted:
    - internal/tools/tool_search.go
    - scripts/test-agent-tool-search-smoke.ps1
decisions:
  - "searchEngine (search.Repository) captured before Bot construction (BLOCKER 6 Option B) — avoids widening Bot.search from search.Searcher"
  - "wikiStore.SetReindexSubmitter called on local *wiki.Store, never b.wiki (BLOCKER 3 — b.wiki is wiki.Repository interface without the setter)"
  - "reindex.Worker nil-guarded throughout; graceful degradation when Qdrant unconfigured"
  - "tool_search.go deleted; TestRunToolCallingLoopAddsToolSearchDiscoveries and TestModelToolNamesExposesEntireRegistry deleted (subsumed by Qdrant top-K retrieval)"
  - "ReindexHealthResponse always present in /api/health JSON (not omitempty) for TypeScript type stability"
metrics:
  duration: "~45 minutes"
  completed: "2026-05-11"
  tasks_completed: 3
  files_modified: 12
  files_deleted: 2
  files_created: 1
---

# Phase 02 Plan 06b: System-wide wiring + deletion sweep Summary

Second half of the integration wave. Finishes INDEX-01, WIKI-01, GIT-01, TOOL-01, TOOL-02, and WARNING 12 (D-16 health surface without Phase 3 deferral).

## What Was Built

**Reindex worker end-to-end wiring and tool_search deletion sweep.**

### Task 1: setup.go + bot.go wiring

`internal/telegram/setup.go`:
- Line 188: `reindexWorker = reindex.NewWorker(searchEngine, reindex.DefaultConfig())` — BLOCKER 6 Option B: the `searchEngine` LOCAL variable (type `search.Repository`, which includes `ReindexWikiPage`) is captured BEFORE Bot is built. `Bot.search` stays as `search.Searcher`.
- Line 195: `wikiStore.SetReindexSubmitter(reindexWorker)` — BLOCKER 3 of 2026-05-11 revision 2: uses the local `*wiki.Store` concrete variable, NOT `b.wiki` (which is typed `wiki.Repository` at bot.go:45 and lacks the setter).
- Line 795: `tools.NewWriteWikiPageTool(wikiStore, reindexWorker)` registered — same local `*wiki.Store`.
- Removed `tools.NewToolSearchTool(toolRegistry)` registration (3 lines).
- Line 554: `Collection: "aura_tool_search_v2"` — T-02-F mitigation; matches Plan 05 production default.
- Lines 851-855: `NewRetryClient` now populates `MaxContentRetries: 3`, `ContentTemperatures: []float64{0.0, 0.3, 0.7}`, `JitterRatio: 0.5` (D-07 CONTENT retry staircase).
- Line 713: `ReindexHealth: b.ReindexHealth` wired into `api.Deps` (WARNING 12 closed).

`internal/telegram/bot.go`:
- Line 66: `reindex *reindex.Worker` field added to Bot struct.
- Lines 94-102: `ReindexHealth() reindex.Health` accessor — nil-safe, returns zero value when worker absent.
- Lines 247-249: `b.reindex.Stop()` called in `Stop()` AFTER `b.archiver.Close()` — T-02-D threat mitigation (archiver flushes first so final reindex enqueues drain before worker cancels).

`internal/api/router.go`, `internal/api/health.go`, `internal/api/types.go`:
- `ReindexHealthResponse` struct added to `types.go` mirroring `reindex.Health`.
- `Reindex ReindexHealthResponse` field added to `HealthRollup` (always present, no `omitempty`).
- `ReindexHealth func() reindex.Health` callback field added to `Deps`.
- `handleHealth` populates `rollup.Reindex` from callback; zero value when nil.

### Task 2: tool_search deletion sweep (BLOCKER 1 of 2026-05-10 revision)

All sites enumerated in BLOCKER 1 cleaned:

| File | Sites |
|------|-------|
| `internal/tools/tool_search.go` | DELETED |
| `scripts/test-agent-tool-search-smoke.ps1` | DELETED |
| `internal/tools/exec.go` | 5 sites: description (line 97), tools_allowed param (line 126), blockedInternalToolCalls map entry (line 216), 2 error messages (lines 269+297) |
| `internal/tools/registry_test.go` | fixture renamed 'tool_search' → 'alpha' (line 162); TestToolSearchToolReturnsJSONResults deleted; TestToolSearchToolRequiresQuery deleted; unused json import removed |
| `internal/telegram/conversation_tool_exec.go` | `tool_search` branch in results loop removed; `toolNamesFromToolSearchResult` helper deleted; unused json import removed |
| `internal/telegram/debug_smoke.go` | `case "tool_search"` in switch removed |
| `internal/telegram/debug_smoke_test.go` | `TestModelToolNamesExposesEntireRegistry` deleted (used `NewToolSearchTool`); `TestRunToolCallingLoopAddsToolSearchDiscoveries` deleted; `hasLLMTool` helper deleted; `categorizedCountingTelegramTool` type deleted |
| `cmd/debug_telegram_sandbox/main.go` | `--expect-tool-search-calls-max` flag removed; `MaxToolSearchCalls` field from `debugExpectations` removed; `tool_search_calls` printf removed; assertion removed |
| `scripts/test-runtime-answer-discipline-smokes.ps1` | `tool_search` stripped from `forbid-final-fragments`; explicit-raw-command smoke updated to not use `tool_search` |
| `internal/agentloop/loop.go` | verified clean (no `tierSearch` references) |

**Full-repo sweep result:** `grep -RIn "tool_search\|ToolSearchTool\|tierSearch\|toolNamesFromToolSearchResult" internal/ cmd/ scripts/` returns ZERO matches in production AND test files. Remaining `aura_tool_search` (without `_v2`) matches in `internal/tools/registry_search_vector_test.go` are Qdrant collection name test fixtures (not the tool name) — correctly excluded.

### Task 3: Worker.Health() passthrough (WARNING 12)

`internal/api/health_test.go` (new):
- `TestHealth_ReindexFieldPresent`: verifies `queue_depth=7, dropped=3, dropped_after_stop=1` flow from callback to JSON.
- `TestHealth_ReindexFieldZero_WhenCallbackNil`: verifies the `reindex` key is present with zero values when callback is nil.
- WARNING 7 of 2026-05-11 plan revision 2: `int64(rx["dropped"].(float64))` composition confirmed compile-clean by running tests.

## Deviations from Plan

**1. [Rule 2 - Missing File] tool_search_test.go did not exist in the worktree**

- **Found during:** Task 2 Step 1
- **Issue:** `internal/tools/tool_search_test.go` listed in plan for deletion but was absent from the worktree (possibly never committed to this branch or already deleted upstream).
- **Fix:** Proceeded without the git rm — the file was already absent. The tool tests in `registry_test.go` (lines 177-208) were deleted as planned.
- **Impact:** None — the file didn't exist so nothing to delete.

**2. [Deviation - Pattern] Used existing test infrastructure (`newTestEnv`) for Task 3 tests**

- **Found during:** Task 3
- **Issue:** The plan specified `newTestDeps(t)` but no such function exists in the api test package. The existing pattern uses `newTestEnv(t)` and `e.router = NewRouter(Deps{...})`.
- **Fix:** Adapted tests to match existing test infrastructure. Functional contract is identical.

## Known Stubs

None — all wiring is complete end-to-end. The `discoveredTools` field in `toolExecutionSummary` remains (not a stub — it's kept for potential future use; it's just never populated from `tool_search` anymore, which is correct).

## Threat Flags

None. No new network endpoints, auth paths, or schema changes introduced. Changes are all internal wiring/cleanup.

## Success Criteria Verification

- [x] INDEX-01 wired end-to-end: `searchEngine` → `reindex.NewWorker` → `wikiStore.SetReindexSubmitter`
- [x] Worker.Stop() in Bot.Stop() AFTER archiver.Close (T-02-D)
- [x] WIKI-01 reachable: `write_wiki_page` registered with submitter wired through local `*wiki.Store`
- [x] GIT-01 reachable: Plan 03's Unversioned set/clear is reachable from real LLM-driven writes
- [x] LLM-01/02 wired: NewRetryClient gets MaxContentRetries/ContentTemperatures/JitterRatio
- [x] TOOL-01 + TOOL-02 fully closed: `aura_tool_search_v2` in setup.go; deletion sweep COMPLETE
- [x] WARNING 12 closed: ReindexHealth surfaces under /api/health (D-16 lock honored without Phase 3 deferral)
- [x] BLOCKER 1 closed: all 9 sweep sites cleaned
- [x] BLOCKER 6 closed: Option B used; Bot.search not widened
- [x] BLOCKER 3 closed: SetReindexSubmitter on local `wikiStore *wiki.Store`, NOT `b.wiki`
- [x] WARNING 7 closed: test compiles and passes (confirmed by running tests)
- [x] `TestRunToolCallingLoopAddsToolSearchDiscoveries`: DELETED (replaced by Qdrant top-K retrieval)
- [x] `go vet ./...` clean
- [x] `go build ./...` clean (excluding tray — pre-existing icon_app.ico issue unrelated to this plan)
- [x] `go test -count=1 ./internal/telegram/ ./internal/tools/ ./internal/api/` all PASS

## Commits

| Hash | Description |
|------|-------------|
| 40a9c0eb | feat(telegram,api): wire reindex.Worker, write_wiki_page, health endpoint [02-06b] |
| 1093f21c | chore(tools): delete tool_search (replaced by Qdrant retrieval) [02-06b] |
| 353a1f46 | feat(api): add TestHealth_Reindex tests for /api/health reindex field [02-06b] |

## Notes for Plan 08

- The CI grep guard MUST NOT exclude `_test.go` files (BLOCKER 1 of 2026-05-10 revision — test files were a prior hiding spot for tool_search references).
- The godclass test must continue to pass (`internal/wiki/godclass_test.go` is out of scope for this plan).
- The precision@5 retrieval fixture references actual registered tool names — `tool_search` is now absent; ensure test fixtures use current registry surface.

## Self-Check: PASSED

All key files exist, deleted files are gone, commits are present:
- FOUND: internal/api/health_test.go
- FOUND: internal/api/types.go
- FOUND: internal/telegram/bot.go
- FOUND: internal/telegram/setup.go
- CONFIRMED DELETED: internal/tools/tool_search.go
- CONFIRMED DELETED: scripts/test-agent-tool-search-smoke.ps1
- Commit 40a9c0eb: Task 1 wiring
- Commit 1093f21c: Task 2 deletion sweep
- Commit 353a1f46: Task 3 health tests
- `go vet ./...` clean
- `go test -count=1 ./internal/telegram/ ./internal/tools/ ./internal/api/` all PASS
