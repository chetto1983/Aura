---
phase: 01-fondamenta-concurrency-safety
plan: "06"
subsystem: search
tags: [qdrant, warm-cache, vector-search, tdd, gap-closure]
dependency_graph:
  requires:
    - 01-05
  provides:
    - QDRANT-01 warm-cache short-circuit (all three rebuild call sites)
  affects:
    - internal/search/qdrant.go
    - internal/tools/registry_search_vector.go
    - internal/search/compact_qdrant.go
tech_stack:
  added: []
  patterns:
    - CollectionInfo probe before DeleteCollection (defensive warm-cache skip)
    - TDD: RED/GREEN per call site
key_files:
  created: []
  modified:
    - internal/search/qdrant.go
    - internal/search/qdrant_test.go
    - internal/tools/registry_search_vector.go
    - internal/tools/registry_search_vector_test.go
    - internal/search/compact_qdrant.go
    - internal/search/compact_qdrant_test.go
decisions:
  - Warm-cache DocsIndexed reports live PointsCount (not 0) so maintenance UI shows correct count after a warm restart
  - CollectionInfo error causes warn-level log + defensive full rebuild; warm-cache miss is never forced by a probe error
  - len(docs)==0 early-return in Recreate is deliberately excluded from warm-cache logic (explicit drop-collection call)
  - qclient nil-guard moved before embed call in toolVectorIndex.Build so warm-cache probe runs before costly embedding
metrics:
  duration: "~15 minutes"
  completed: "2026-05-10T13:56:09Z"
  tasks_completed: 4
  files_modified: 6
---

# Phase 01 Plan 06: QDRANT-01 Warm-Cache Short-Circuit Summary

Wire `qdrant.Client.CollectionInfo` + `PointsCount > 0` short-circuits into all three rebuild call sites, preventing destructive Delete/Create/re-embed on every Aura restart when the Qdrant collection is already populated.

## What Was Built

Three production files each received the same warm-cache pattern:

1. **`rebuildQdrantWikiDocumentsWithClient`** (`internal/search/qdrant.go`) — checks `CollectionInfo` after `Health` and before `loadWikiDocuments`. On warm hit: returns `QdrantRebuildReport{DocsIndexed: int(info.PointsCount)}` (W1: live count for maintenance UI). The `loadWikiDocuments` error in the warm-cache branch is surfaced at warn level rather than swallowed (W2).

2. **`toolVectorIndex.Build`** (`internal/tools/registry_search_vector.go`) — checks `CollectionInfo` after the nil-guard and before the embed call. On warm hit: sets `idx.docCount = len(docs)`, `idx.lastRebuild = time.Now()`, returns nil. The `qclient == nil` guard was moved before the embed call to enable the warm-cache probe to run first.

3. **`CompactMemoryQdrantIndex.Recreate`** (`internal/search/compact_qdrant.go`) — checks `CollectionInfo` after the `len(docs)==0` early-return (explicit drop path preserved) and before `pointsForDocuments`. On warm hit: returns `VectorReport{Collection: i.collection}`.

All three sites use the same defensive pattern: `CollectionInfo` error logs a warn and falls back to full rebuild; 404 (zero-value `CollectionInfo`) also falls back to full rebuild.

## Tests Added

| File | New Tests | Strategy |
|------|-----------|----------|
| `internal/search/qdrant_test.go` | 4 | TDD RED/GREEN; mock httptest server |
| `internal/tools/registry_search_vector_test.go` | 4 + 2 helpers | TDD RED/GREEN; mock httptest server |
| `internal/search/compact_qdrant_test.go` | 4 | TDD RED/GREEN; mock httptest server |

Each file covers: warm-cache hit (no Delete/Create/Upsert/embed), cold path (PointsCount==0), not-found (404), CollectionInfo error fallback.

## Commits

| Hash | Message |
|------|---------|
| `231443b7` | feat(01-06): add warm-cache short-circuit to rebuildQdrantWikiDocumentsWithClient |
| `8d24df0d` | feat(01-06): add warm-cache short-circuit to toolVectorIndex.Build |
| `cca63bde` | feat(01-06): add warm-cache short-circuit to CompactMemoryQdrantIndex.Recreate |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Existing mock servers rejected new GET /collections requests**

- **Found during:** Task 1 GREEN, Task 3 GREEN validation
- **Issue:** The existing tests (`TestRebuildQdrantWikiDocumentsCreatesCollectionAndUpsertsDocs`, `TestCompactMemoryQdrantIndexRecreatesCollectionAndUpsertsPayloads`, `TestCompactMemoryQdrantIndexUsesBatchEmbeddings`) had strict `default: t.Fatalf("unexpected qdrant request...")` handlers. After the new production code began calling `GET /collections/...`, these tests fatally failed.
- **Fix:** Added a `GET /collections/{name}: w.WriteHeader(http.StatusNotFound)` case to each affected test's switch statement. This correctly simulates a cold-start (no existing collection) and allows the existing assertions (Delete + Create + Upsert all called) to remain correct.
- **Files modified:** `internal/search/qdrant_test.go`, `internal/search/compact_qdrant_test.go`
- **Commit:** Included in Task 1 and Task 3 commits respectively.

## Task 4: Whole-Tree Validation

- `go vet ./internal/search/ ./internal/tools/ ./internal/qdrant/` — PASS
- `go build ./...` — FAIL on `internal/tray` only (`icon_app.ico: no matching files found`) — **pre-existing issue, confirmed via git stash test; unrelated to this plan**
- `go test -count=1 ./internal/search/ ./internal/tools/ ./internal/qdrant/` — PASS (all 3 packages)

The `internal/tray` build failure exists on the base commit `e1c35f4b` and is outside this plan's scope. It has been noted in `deferred-items.md` below.

## Known Stubs

None. All three warm-cache short-circuits return populated reports or set meaningful index state.

## Threat Flags

No new security-relevant surface introduced beyond what the plan's threat model documents. The `CollectionInfo` response is parsed and only `PointsCount` (a `uint64`) gates behavior — no new trust boundary introduced. See threat register in `01-06-PLAN.md` (T-01-24 through T-01-26).

## Self-Check

Files exist:
- `internal/search/qdrant.go` — CollectionInfo call present
- `internal/tools/registry_search_vector.go` — CollectionInfo call present
- `internal/search/compact_qdrant.go` — CollectionInfo call present

Commits exist: `231443b7`, `8d24df0d`, `cca63bde` — all on branch `worktree-agent-a093a545a8c066ff8`.

## Self-Check: PASSED
