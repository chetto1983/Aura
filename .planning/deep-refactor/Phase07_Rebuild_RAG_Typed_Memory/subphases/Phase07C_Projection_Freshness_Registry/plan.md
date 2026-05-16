# Phase07C Plan - Projection Freshness Registry

Status: closed by existing implementation evidence and self-audited planning
repair on 2026-05-16. Not independently verified in this turn.

## Phase Goal

Aura retrieval exposes whether compact-memory projections are fresh, stale, or
degraded before the agent trusts retrieved context. Projection freshness is
durable SQLite state, while Qdrant, FTS, graph projections, and embedding cache
remain rebuildable support planes.

## Scope

- Add durable `projection_state` rows for the known projection set.
- Add `content_hash`, `embedding_model_id`, and `index_build_id` to
  `compact_memory_documents`.
- Stamp compact memory rows at write/rebuild time and bump pending counts when
  content hash drift is detected.
- Mark rebuild completion with a new build id and reset pending count.
- Surface `freshness=` and `degraded_read=true` annotations from
  `search_memory` compact-memory hits.
- Seed the five canonical projection rows during app boot.

## Non-Goals

- Do not implement wiki/source delete/rename invalidation in this slice.
- Do not implement Qdrant alias swaps or embedding-model migration policy.
- Do not add a per-chunk freshness side table.
- Do not replace `search_memory` with new task-level recall tools here.
- Do not treat projection freshness as canonical memory truth.

## Deep Module RFC

| Option | Shape | Decision | Reason |
| --- | --- | --- | --- |
| A. Volatile health tracker only | Keep vector freshness in process memory and dashboard health structs | Rejected | Restart loses state and retrieval cannot cite degraded projection status. |
| B. Per-chunk freshness docstore | Add a separate row per indexed chunk with hash and timestamp | Rejected | Duplicates `compact_memory_documents.id` identity and adds a second store to reconcile. |
| C. SQLite projection registry plus compact-row freshness columns | One `projection_state` row per projection plus per-document hash/model/build columns | Chosen | Deep module: small interface, durable local state, clear drift signal, no graph/vector truth dependency. |
| D. Vector-store-native freshness | Store all freshness in Qdrant payloads and collection aliases | Deferred | Useful for future embedding swaps, but insufficient as Aura's local-first source of truth. |

Chosen module shape:

```text
compact memory write/rebuild
  -> compact_memory_documents(content_hash, embedding_model_id, index_build_id)
  -> projection_state pending/completed/status counters
  -> search_memory freshness/degraded annotation
```

## Dependencies

- Phase07A archive hygiene keeps raw tool output out of compact memory.
- Phase07B collection registry and result formatting already expose typed hits,
  score components, follow-up handles, and SourceID filtering.
- Phase01A run/event and Phase06 tool observation work remain separate from
  retrieval freshness.

## Implementation Slices

| Slice | Implemented Evidence | Source Files | Status |
| --- | --- | --- | --- |
| US-M01 | `72eb106d feat(freshness): projection_state table + Store API with optimistic concurrency` | `internal/db/migrations/migrations.go`, `internal/storage/freshness/store.go` | closed |
| US-M02 | `b30aa450 feat(memoryindex): content_hash + embedding_model_id + index_build_id columns` | `internal/storage/memoryindex`, `internal/storage/search/compact_qdrant.go` | closed |
| US-M03 | `21ff02bc feat(memoryindex,freshness): eager write-time invalidate + populate freshness columns` | `internal/storage/memoryindex/rebuild.go`, `cmd/aura` | closed |
| US-M04 | `f4751072 feat(tools): freshness + degraded_read annotations on search_memory results` | `internal/agent/tools/registry/memory_search.go` | closed |

## PRD Coverage Matrix

| PRD Item | Plan Location | Benchmark Location | Source Evidence | Status |
| --- | --- | --- | --- | --- |
| Projection freshness registry for FTS/Qdrant/graph/source/user-memory projections | Scope and US-M01 | `benchmark.md` rows 1-2 | `source.md` rows for PRD, Phase-M, migrations, freshness store | covered for known projection rows; graph/source op handling deferred |
| Durable stale/degraded state visible to retrieval | Scope and US-M04 | `benchmark.md` row 5 | `source.md` rows for `memory_search.go` and research docs | covered for compact-memory hits |
| Per-document drift detection | Scope and US-M02/US-M03 | `benchmark.md` rows 3-4 | `source.md` rows for `memoryindex` and Qdrant payload | covered for compact memory docs |
| Stale vector-only empty results cannot suppress exact/FTS/graph evidence | Non-goals and remaining risk | `benchmark.md` deferred row | parent `benchmark.md` | deferred to future wiki/source/vector slice |
| Delete and rename fixtures prove stale projection records removed or invalidated | Non-goals and remaining risk | `benchmark.md` deferred row | `source.md` missing-source questions | deferred |

## Decisions Required Before Next Implementation

- Decide whether Phase07D should follow the PRD status table
  (`User/Operational Memory Typed Tiers`) or split a narrower follow-up for
  wiki/source projection invalidation first.
- Decide whether task-level recall naming belongs in Phase07D or Phase09 after
  current `recall_user_memory` and `recall_operational` code is mapped.

## Rollback / Deviation Rule

If retrieval freshness starts hiding exact, FTS, or graph evidence, stop the
slice and repair the retrieval contract. Degraded status may warn the agent; it
must not silently suppress stronger canonical evidence.
