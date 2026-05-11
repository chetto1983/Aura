---
phase: 02-llm-reliability-tool-intelligence
plan: "05"
subsystem: tools
tags: [tool-retrieval, vector-search, embedding, qdrant]
dependency_graph:
  requires: []
  provides:
    - ToolVectorIndex exported type (consumed by Plan 06 ToolsProvider closure)
    - searchableToolEmbeddingText helper (name+description only, D-24)
    - aura_tool_search_v2 collection name convention (wired by Plan 06 in setup.go)
  affects:
    - internal/tools/registry_search_vector.go
    - internal/tools/registry_search.go
    - internal/tools/registry.go
    - internal/tools/registry_search_test.go
    - internal/tools/registry_search_test.go (new file)
tech_stack:
  added: []
  patterns:
    - Embedding-vs-lex text split (narrow embedding, broad lex)
    - Exported type convention for package-internal-but-cross-plan types
key_files:
  created:
    - internal/tools/registry_search_test.go
  modified:
    - internal/tools/registry_search_vector.go
    - internal/tools/registry_search.go
    - internal/tools/registry.go
    - internal/tools/registry_search_vector_test.go
decisions:
  - ToolVectorIndex exported; fields remain lowercase (BLOCKER 9 - struct fields not renamed)
  - Default collection aura_tool_search -> aura_tool_search_v2 forces fresh embeddings on Phase 2 boot
  - searchableToolEmbeddingText returns name+" "+description lowercased only (D-24)
  - searchableToolText (lex path) unchanged - broader corpus retained
metrics:
  duration: "~15 min"
  completed: "2026-05-11"
  tasks_completed: 2
  tasks_total: 2
  files_changed: 5
---

# Phase 02 Plan 05: Tool Vector Index Refactor Summary

Three coupled changes to the tool-vector index: exported type, embedding text narrowing, and collection name bump.

## What Was Built

### Change 1: Rename toolVectorIndex → ToolVectorIndex (exported)

`internal/tools/registry_search_vector.go` now exports `ToolVectorIndex`. All method receivers updated from `(idx *toolVectorIndex)` to `(idx *ToolVectorIndex)` (5 methods). The constructor `NewToolVectorIndex` now returns `*ToolVectorIndex`. `registry.go`'s `Registry.vectorIndex` field and `SetVectorIndex` method parameter updated accordingly.

Rationale: Plan 06 needs to reference the type directly in the per-turn ToolsProvider closure.

### Change 2: Narrow embedding text (D-24)

`internal/tools/registry_search.go` gains `searchableToolEmbeddingText(def ToolDefinition) string`:
- Returns `strings.ToLower(name + " " + description)` only
- Does NOT include tags, examples JSON, or parameters JSON

`Registry.BuildVectorIndex` in `registry.go` now calls `searchableToolEmbeddingText(def)` instead of `searchableToolText(def, tags)` when constructing `toolVectorDoc` entries for the vector index.

The lex path (`Registry.Search` → `searchableToolText`) is unchanged — it still includes tags, examples, and parameters for BM25-style matching.

### Change 3: Collection name bump (T-02-F mitigation)

The default collection name in `NewToolVectorIndex` bumps from `"aura_tool_search"` to `"aura_tool_search_v2"`. This forces a fresh collection on Phase 2 first boot because the QDRANT-01 warm-cache short-circuit (`points_count > 0`) sees 0 on the new (empty) collection name and triggers a rebuild with the narrowed embedding text.

The old `aura_tool_search` collection is left orphaned — harmless, can be dropped by an operator manually if disk space matters.

## Shape of the New Vector Index

```
ToolVectorIndex (exported)
  cfg.Collection: "aura_tool_search_v2" (default)
  Build() receives toolVectorDoc{name, text} where text = searchableToolEmbeddingText (narrow)
  Search() remains unchanged — queries the Qdrant collection, fuses results with lex
  Health() unchanged
```

## Deferred to Plan 06

- `internal/telegram/setup.go:529`: `Collection: "aura_tool_search"` literal must be updated to `"aura_tool_search_v2"`. This plan provides the new name as the package default; the call-site wiring is Plan 06's responsibility.

## Test Coverage

Three new tests in `internal/tools/registry_search_test.go` (63 LOC):

| Test | What it checks |
|------|---------------|
| `TestSearchableToolEmbeddingText_NameAndDescriptionOnly` | Returns exactly `name+" "+description` lowercased; does NOT include parameters/examples JSON |
| `TestSearchableToolText_LexKeepsBroadCorpus` | Existing lex function still includes tags+examples text |
| `TestToolVectorIndex_ExportedType` | Compile-time assertion that `ToolVectorIndex` is exported |

Existing tests in `registry_search_vector_test.go` updated:
- `TestToolVectorIndexNilReady` (line 51): `var idx *ToolVectorIndex`
- `TestToolVectorIndexNilHealth` (line 87): `var idx *ToolVectorIndex`
- `newToolVectorIndexForTest` helper (line 141): return type `*ToolVectorIndex`
- `TestNewToolVectorIndexDefaults` (line 21): asserts `aura_tool_search_v2`
- `TestToolVectorConfigDefaultCollection` (line 124): asserts `aura_tool_search_v2`

All tests GREEN: `go test -count=1 ./internal/tools/` exits 0.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed false-positive forbidden-token test assertion**
- **Found during:** Task 2 (test execution)
- **Issue:** The plan's test template included `"frob"` in the forbidden tokens list for `TestSearchableToolEmbeddingText_NameAndDescriptionOnly`. However, `strings.Contains("frobnicate frobnicate a thing.", "frob")` is `true` because "frob" is a prefix of "frobnicate" — causing an immediate false-positive failure.
- **Fix:** Removed `"frob"` from the forbidden list. The remaining tokens (`"properties"`, `"required"`, `"arguments"`) are distinct to the parameters/examples JSON blobs and do not appear in the tool name or description.
- **Files modified:** `internal/tools/registry_search_test.go`
- **Commit:** 9a93c816

## Notes for Plan 06

In `internal/telegram/setup.go:529`, update:
```go
Collection: "aura_tool_search",
```
→
```go
Collection: "aura_tool_search_v2",
```

## Notes for Plan 08

`searchableToolEmbeddingText` is now locked to `name+" "+description` (D-24). The precision@5 retrieval fixture test should use this as the ground truth for embedding queries against `aura_tool_search_v2`.

## Known Stubs

None. The narrowed embedding text is fully wired through `BuildVectorIndex`. The collection name update in `setup.go` is intentionally deferred to Plan 06 (not a stub — it's a planned incremental change documented above).

## Threat Flags

None. No new network endpoints, auth paths, or schema changes introduced. The Qdrant collection rename is documented in the plan's threat model as T-02-F mitigation.

## Self-Check

Files created/modified:
- `internal/tools/registry_search_vector.go` — modified (type rename, collection bump)
- `internal/tools/registry_search.go` — modified (searchableToolEmbeddingText added)
- `internal/tools/registry.go` — modified (field type, BuildVectorIndex call site)
- `internal/tools/registry_search_vector_test.go` — modified (3 type refs, 2 collection assertions)
- `internal/tools/registry_search_test.go` — created (63 LOC, 3 tests)

Commits:
- `97161e01` feat(tools): export ToolVectorIndex; narrow embedding text; bump collection name [02-05]
- `9a93c816` test(tools): add embedding narrowing + exported type tests [02-05]

## Self-Check: PASSED
