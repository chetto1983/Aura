---
plan: 02-06
phase: 02
status: complete
commits:
  - 8427f15e
  - b7d9c813
  - 41726065
---

# Plan 02-06 — Integration (package-local)

## Scope

First half of the integration wave. Package-local artifacts only — system-wide wiring and the deletion sweep belong to 02-06b.

## Tasks delivered

**Task 1 — `LatestUserMessageText()` accessor (commit `8427f15e`)**
- Added method on `*conversation.Context` that reverse-walks `Messages()` and returns the content of the most recent `role=="user"` message, or `""` on cold start.
- 3 tests in `internal/conversation/context_test.go`.

**Task 2 — `wiki.Store` ↔ reindex submitter wiring (commit `b7d9c813`)**
- Added `SetReindexSubmitter(reindex.Submitter)` setter on `*wiki.Store`.
- `WritePage` enqueues `OpUpsert`, `DeletePage` enqueues `OpDelete` after file ops succeed regardless of git commit outcome (D-14).
- nil-safe — calling `Submit` on a nil submitter is a no-op.
- 3 tests in `internal/wiki/store_writes_test.go`.

**Task 3 — Per-turn `ToolsProvider` + conversation.go cleanup (commit `41726065`)**
- New file `internal/telegram/tools_provider.go` with `alwaysOnCore` (6 names) and `makeToolsProvider` using function-typed deps.
- Closure semantics: cold-start → core only; Qdrant down or empty → FULL tool set; normal → core ∪ top-K=5 retrieval (single batched `defsForFn` call).
- All 3 "tool_search discoveries" prompt sites in `conversation.go` replaced with "Qdrant top-K=5 retrieval" wording.
- `ToolsProvider: currentToolDefs` flipped to `toolsProvider`.
- `maxCallsPerTool`: removed `tool_search:2`, added `write_wiki_page:3`.
- Deviation Rule 3: fixed pre-existing `turnRetrievalCapsule` undefined type so the file compiles in the worktree.
- 4 tests in `internal/telegram/tools_provider_test.go`.

## Validation

- `go vet ./internal/conversation/ ./internal/wiki/ ./internal/telegram/` clean
- `go build ./...` clean
- 10 new tests pass; pre-existing `internal/conversation/` and `internal/wiki/` suites pass with no regressions
- Known failure carried forward to 02-06b: `TestRunToolCallingLoopAddsToolSearchDiscoveries` in `internal/telegram/` — the test still asserts the legacy "tool_search discoveries" prompt copy. 02-06b will update or delete it as part of the tool_search removal sweep.

## Notes for 02-06b

1. Wire `wikiStore.SetReindexSubmitter(reindexWorker)` in `internal/telegram/setup.go` after constructing the reindex worker.
2. Delete `internal/tools/tool_search.go` and `tool_search_test.go`; update or remove `TestRunToolCallingLoopAddsToolSearchDiscoveries` to match the new "Qdrant top-K=5 retrieval" prompt.
3. Bump Qdrant collection in `internal/telegram/setup.go:529` from `"aura_tool_search"` to `"aura_tool_search_v2"` (matches the default already set in `NewToolVectorIndex`).
