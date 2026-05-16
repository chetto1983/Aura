# Phase07C Benchmark - Projection Freshness Registry

Status: closed by local targeted rerun on 2026-05-16. No independent verifier
was run in this turn, so readiness label is self-audited.

| Check | Command / Method | Fixture / Data Source | Expected Ground Truth | Pass / Fail Threshold | PRD Gate | Actual Result |
| --- | --- | --- | --- | --- | --- | --- |
| Projection registry schema and store | `go test ./internal/storage/freshness ./internal/db/migrations -count=1` | Fresh SQLite DB and migration tests | `projection_state` exists in fresh and migrated schemas; `freshness.Store` roundtrips rows, lists rows, and returns `ErrVersionConflict` for stale optimistic writes | All tests pass; no schema column missing | Projection freshness registry is durable | Passed 2026-05-16 by Codex |
| App boot seeds projection rows | `go test ./cmd/aura -run TestSeedProjectionState -count=1` | Fresh app DB after migrations | Exactly five canonical rows exist: `wiki_documents_fts5`, `aura_memory_v1`, `compact_memory_documents`, `aura_memory_v1_compact`, `embedding_cache`; second seed is no-op | All expected ids present once | Projection state available before retrieval work | Passed inside broader `go test ./cmd/aura -count=1` on 2026-05-16 |
| Compact document freshness columns | `go test ./internal/storage/memoryindex -run "TestDocumentRoundTripWithFreshnessColumns|TestContentHash" -count=1` | Compact memory document fixture with hash/model/build fields | Stored document returns identical `content_hash`, `embedding_model_id`, and `index_build_id`; hash is role/model/body sensitive | Exact field equality and hash sensitivity assertions pass | Per-document drift detection | Passed inside broader `go test ./internal/storage/memoryindex -count=1` on 2026-05-16 |
| Write-time pending and rebuild completion | `go test ./internal/storage/memoryindex -run "TestAppendBumpsPendingOnHashMismatch|TestRebuildResetsPendingAndUpdatesBuildID" -count=1` | Existing compact row with hash A, append/rebuild with hash/build changes | Hash mismatch increments `projection_state.pending_count`; rebuild resets pending to zero and records a new build id/model | SQLite counters match expected values | Stale/degraded projection state is observable | Passed inside broader `go test ./internal/storage/memoryindex -count=1` on 2026-05-16 |
| Retrieval freshness annotation | `go test ./internal/agent/tools/registry -run "TestFreshnessTokenPresent|TestDegradedReadFlag|TestEmptyFreshnessOmitted" -count=1` | `search_memory` compact-result fixtures for fresh, degraded, and empty-default docs | Fresh hit emits `freshness=` and not `degraded_read`; degraded projection emits `degraded_read=true`; old empty docs emit neither | Output string contains or omits exact markers as expected | Stale/degraded reads visible to agent | Passed inside broader `go test ./internal/agent/tools/registry -count=1` on 2026-05-16 |
| Phase07C targeted package gate | `go test ./internal/storage/freshness ./internal/db/migrations ./internal/storage/memoryindex ./internal/agent/tools/registry ./cmd/aura -count=1` | All Phase07C affected packages | Freshness, migration, memoryindex, registry, and boot-seeding packages compile and pass together | Command exits 0 | Phase07C code path is locally coherent | Passed 2026-05-16 by Codex |
| Wiki/source delete and rename invalidation | Future test after owner slice is selected | Wiki/source delete and rename fixture | Old projection records are removed or marked invalid; new citation handles resolve; exact/FTS/graph evidence is not suppressed by stale vector state | Not applicable in Phase07C | Phase 7 delete/rename freshness gate | Not run; deferred |

## Residue Gate

- No runtime data, Qdrant collection, wiki page, SQLite user data, or source
  artifact was mutated by this planning repair.
- The worktree had unrelated dirty Go/Docker/Ralph files before this repair;
  they were preserved.
- No branch, commit, push, or PR was created.
