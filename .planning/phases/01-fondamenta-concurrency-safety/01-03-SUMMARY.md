---
phase: 01-fondamenta-concurrency-safety
plan: "03"
subsystem: qdrant
tags: [qdrant, client, interface, vector-search, refactor, search, tools]

requires:
  - phase: 01-fondamenta-concurrency-safety
    plan: "01"
    provides: [internal/qdrant.Client, internal/qdrant.NewClient, internal/qdrant.WaitForReady]

provides:
  - "internal/search/qdrant.go uses qdrant.Client for all Qdrant I/O"
  - "internal/search/compact_qdrant.go uses qdrant.Client for all Qdrant I/O"
  - "internal/tools/registry_search_vector.go uses qdrant.Client for all Qdrant I/O"
  - "CheckQdrantReady removed; callers use qdrant.WaitForReady directly"
  - "Zero duplicate Qdrant HTTP implementations across search and tools packages"

affects: [internal/search, internal/tools, internal/telegram, cmd/debug_qdrant]

tech-stack:
  added: []
  patterns:
    - "Consumer-to-shared-client migration: all Qdrant I/O goes through qdrant.Client interface"
    - "rebuildQdrantWikiDocumentsWithClient: inner function that accepts pre-built client to avoid credential re-extraction"
    - "toolVectorIndex lazy-initializes qclient on construction for fts/qdrant backend switching"

key-files:
  created: []
  modified:
    - internal/search/qdrant.go
    - internal/search/compact_qdrant.go
    - internal/tools/registry_search_vector.go
    - cmd/debug_qdrant/main.go

key-decisions:
  - "Keep QdrantConfig struct as public API in search package; callers (telegram/setup.go, debug_qdrant) unchanged"
  - "rebuildQdrantWikiDocumentsWithClient avoids credential re-extraction — qdrantRepository.IndexWikiPages reuses its pre-built client directly"
  - "cmd/debug_qdrant updated in this plan (not Plan 05) to avoid build failure from CheckQdrantReady removal"
  - "NewToolVectorIndex creates qclient eagerly on construction; nil guard on fts backend prevents wasted allocation"
  - "Search() in toolVectorIndex now uses qdrant.ScoredPoint directly instead of stringly-typed map[string]string"

patterns-established:
  - "Consumer packages create qdrant.Client via qdrant.NewClient(qdrant.Config{...}) — no DIY HTTP"
  - "Inner helper pattern: exported func accepts config, inner func accepts pre-built client — avoids credential leakage through interface"

requirements-completed:
  - QDRANT-01

duration: 15min
completed: "2026-05-10"
---

# Phase 01 Plan 03: Migrate Qdrant Consumers to Shared Client Summary

**Eliminated ~200 lines of duplicated Qdrant HTTP code by migrating all three consumers (search/qdrant.go, search/compact_qdrant.go, tools/registry_search_vector.go) to the shared qdrant.Client interface from Plan 01.**

## Performance

- **Duration:** ~15 min
- **Started:** 2026-05-10T12:51:00Z
- **Completed:** 2026-05-10T13:06:00Z
- **Tasks:** 3
- **Files modified:** 4

## Accomplishments

- Removed `qdrantClient` struct and all 9 HTTP methods from `internal/search/qdrant.go`
- Removed duplicate `*qdrantClient` field and all Qdrant HTTP methods from `compact_qdrant.go`
- Removed 9 duplicate Qdrant HTTP helper methods from `registry_search_vector.go` (qdrantBase, collectionPath, authorizeQdrant, recreateQdrantCollection, deleteQdrantCollection, upsertQdrantPoints, queryQdrantPoints, doQdrantJSON, doQdrantJSONDecode)
- All Qdrant I/O in all three consumers now flows through `qdrant.Client`
- Public API signatures preserved: `QdrantConfig`, `NewQdrantSearcher`, `NewQdrantRepository`, `NewCompactMemoryQdrantIndex(WithBatch)`, `RebuildQdrantWikiDocuments` unchanged
- `go test ./internal/search/ ./internal/tools/` passes (all existing tests)

## Task Commits

Each task was committed atomically:

1. **Task 1: Refactor internal/search/qdrant.go + Task 2: Refactor compact_qdrant.go** - `aa699754` (feat)
   - Combined in one commit since both search package files form a cohesive change unit
   - Also includes cmd/debug_qdrant/main.go fix (Rule 3 deviation — see below)
2. **Task 3: Refactor internal/tools/registry_search_vector.go** - `314cece4` (feat)

## Files Created/Modified

- `internal/search/qdrant.go` — Replaced `qdrantClient` struct + 9 HTTP methods with `qdrant.Client` field; added `newQdrantClientFromConfig` helper and `rebuildQdrantWikiDocumentsWithClient` inner function; `qdrantSearcher` now stores `collection string` field
- `internal/search/compact_qdrant.go` — Changed `client *qdrantClient` to `client qdrant.Client + collection string`; replaced all low-level calls with `qdrant.Client` methods; `isNotFoundErr` helper kept for Upsert retry on missing collection
- `internal/tools/registry_search_vector.go` — Added `qclient qdrant.Client + collection string` to `toolVectorIndex`; `Ready()` uses `qclient.Health(ctx)`; `Build()` uses `DeleteCollection + CreateCollection + Upsert`; `Search()` uses `qclient.Search` returning typed `qdrant.ScoredPoint`
- `cmd/debug_qdrant/main.go` — Updated to use `qdrant.WaitForReady` instead of removed `search.CheckQdrantReady`

## Decisions Made

- `QdrantConfig` struct kept as the public input type for backward compatibility with `internal/telegram/setup.go` and `cmd/debug_qdrant/main.go`
- `rebuildQdrantWikiDocumentsWithClient` inner function pattern: the exported `RebuildQdrantWikiDocuments(cfg)` constructs the client once; `IndexWikiPages` calls the inner function directly using its pre-built client — avoids the credential extraction problem (API key not accessible through `qdrant.Client` interface)
- `NewToolVectorIndex` creates `qclient` eagerly at construction time only when `QdrantURL != ""` and backend is not `"fts"` — keeps zero-cost path for FTS backend
- `Search()` in toolVectorIndex simplified: uses `qdrant.ScoredPoint.Score` directly as `float32` instead of the old `strconv.ParseFloat(pt["score"])` pattern

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Updated cmd/debug_qdrant/main.go to remove dependency on deleted CheckQdrantReady**
- **Found during:** Task 1 (search/qdrant.go refactoring)
- **Issue:** The plan specifies removing `CheckQdrantReady`. However, `cmd/debug_qdrant/main.go` calls `search.CheckQdrantReady`. Removing it without updating the caller would break the build. Plan 05 was mentioned as the future fix, but `go build ./...` must pass after every edit.
- **Fix:** Updated `cmd/debug_qdrant/main.go` to use `qdrant.NewClient` + `qdrant.WaitForReady` directly — the exact replacement the plan prescribes.
- **Files modified:** cmd/debug_qdrant/main.go
- **Verification:** `go build ./cmd/debug_qdrant/` exits 0
- **Committed in:** aa699754 (Task 1+2 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking)
**Impact on plan:** Required to keep build passing. Advances the plan's stated intent (callers use qdrant.WaitForReady directly) ahead of schedule.

## Issues Encountered

- **Worktree path drift (#3099):** Initial file edits were accidentally applied to the main repo (`D:\Aura\`) instead of the worktree (`D:\Aura\.claude\worktrees\agent-aac2667edf6b91c2f\`). Corrected by copying modified files to worktree and reverting main repo before committing. All commits are on the correct worktree branch.
- **`go build ./...` tray error:** Pre-existing build failure in `internal/tray/tray_windows.go` (missing `icon_app.ico`). Unrelated to this plan. The three packages changed in this plan (`internal/search`, `internal/tools`, `cmd/debug_qdrant`) all build and vet cleanly.

## Known Stubs

None — all methods are fully implemented.

## Threat Surface Scan

No new network endpoints, auth paths, or schema changes introduced. The `internal/qdrant` client package was already reviewed in Plan 01. All STRIDE mitigations from the plan's threat register are satisfied:

| Threat ID | Status |
|-----------|--------|
| T-01-12 | Mitigated: `qdrantPoint` → `qdrant.Point` conversion is exact field-by-field; Go compiler verifies type compatibility |
| T-01-13 | Mitigated: `Build()` guard `len(docs)==0` returns early before any Qdrant call |
| T-01-14 | Mitigated: API key passed once to `qdrant.NewClient`; consumers never access it directly |

## Next Phase Readiness

- All three Qdrant consumers use the shared `qdrant.Client` interface
- Wave 2 sibling plan (01-04) wires `internal/concurrency` into Telegram bot — no overlap with files modified here
- Plan 01-05 (Qdrant startup gate) can now use `qdrant.WaitForReady` and `qdrant.Client` directly throughout

## Self-Check: PASSED

- [x] internal/search/qdrant.go: no `qdrantClient` struct, no CheckQdrantReady, uses qdrant.Client
- [x] internal/search/compact_qdrant.go: `client qdrant.Client`, no `*qdrantClient`
- [x] internal/tools/registry_search_vector.go: `qclient qdrant.Client`, 9 duplicate methods removed
- [x] cmd/debug_qdrant/main.go: uses qdrant.WaitForReady
- [x] Commit aa699754 exists (Tasks 1+2)
- [x] Commit 314cece4 exists (Task 3)
- [x] go build ./internal/search/ ./internal/tools/ passes
- [x] go vet ./internal/search/ ./internal/tools/ passes
- [x] go test ./internal/search/ ./internal/tools/ passes (all cached)

---
*Phase: 01-fondamenta-concurrency-safety*
*Completed: 2026-05-10*
