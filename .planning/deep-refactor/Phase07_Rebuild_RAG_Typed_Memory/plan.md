# Phase07 Plan - Rebuild RAG On Typed Memory Layers

Status: parent plan with Phase07A, Phase07B, and Phase07C closed by local code
evidence. Phase07D-F remain planned/scaffolded and need fresh source mapping
before implementation.

## Goal

Retrieval stops being one broad memory soup. Aura must distinguish runtime
continuity, conversation archive, user/project knowledge, source corpus,
knowledge wiki, operational memory, experience store, projections, and cache
before any result becomes user-visible context.

## Current Trigger

Live container/database evidence from 2026-05-15 showed compact memory search
returning raw tool errors, tool schema dumps, `AGENT.md` dumps, stdout snippets,
and assistant/tool loop noise from `compact_memory_documents` rows whose only
real source was the conversation archive. That violates PRD Phase 7 gates:
tool failures must not appear as memory, and retrieval hits must keep their
typed layer labels and citation handles.

## Deep Module RFC

| Option | Shape | Decision | Reason |
| --- | --- | --- | --- |
| A. Keep `search_memory(scope=all)` broad | Keep wiki, sources, archive, proposals in one tool default | Rejected | Preserves the current memory soup and lets tool output outrank curated knowledge. |
| B. Keep archive indexing and filter only at output | Continue indexing every archive turn, hide noisy rows in formatting | Rejected | The bad data still pollutes FTS/vector rankers and rebuilds. |
| C. Typed recall module with archive hygiene first | Centralize layer eligibility, then move toward task-level recall and cited capsules | Chosen | Small first slice, high leverage, compatible with existing code and PRD Phase 7. |
| D. Adopt a new memory platform or graph DB | Mem0/Zep/Neo4j-style separate memory core | Rejected for Phase 7 | Adds a second truth source and violates local-first/rebuildable projection constraints. |

Chosen module shape:

```text
RecallRequest
  -> typed collection registry
  -> retrieval plan by layer and task intent
  -> FTS/vector/graph/proposal/archive adapters
  -> cited context capsule with score and freshness metadata
```

Phase07A intentionally starts smaller: keep `conversations` as the full raw
archive, but stop compact memory and `search_memory` from treating raw tool
results and intermediate loop scaffolding as durable memory.

## Scope

- Define memory layer IDs and citation handles.
- Create a collection metadata registry for wiki, sources, user memory,
  archive, and operational memory.
- Split recall by task intent instead of one polymorphic memory tool.
- Implement hybrid FTS/vector retrieval with RRF where available.
- Add projection freshness registry and durable op-aware reindex jobs.
- Extend GraphIndex with typed weighted edges, source edges, degree, bounded
  neighborhood queries, and path queries.
- Add community reports as derived projections with evidence handles.
- Return score components, follow-up handles, and freshness/degraded warnings.
- Add golden RAG evals for user facts, wiki/source answers, operational
  lessons, stale vectors, deletes, renames, and embedding-model changes.

## Sub-Phases

| Sub-Phase | Goal | Primary Files | Benchmark Anchor | Status |
| --- | --- | --- | --- | --- |
| Phase07A - Compact Archive Hygiene | Raw tool output and intermediate loop noise remain in `conversations` but do not enter compact memory or default recall. | `internal/storage/memoryindex/rebuild.go`, `internal/agent/tools/registry/memory_search.go`, closest tests | `subphases/Phase07A_Compact_Archive_Hygiene/benchmark.md` | closed |
| Phase07B - Typed Collection Registry | Add layer metadata, citation handles, filterable fields, score components, follow-up handles, and SourceID filtering. | `internal/storage/memoryindex`, `internal/storage/search`, `internal/agent/tools/registry/memory_search.go` | `subphases/Phase07B_Typed_Collection_Registry/benchmark.md` | closed |
| Phase07C - Projection Freshness Registry | Add durable projection state, per-document freshness columns, write-time pending/rebuild accounting, and retrieval-time freshness/degraded annotations. | `internal/storage/freshness`, `internal/storage/memoryindex`, `internal/storage/search`, `internal/agent/tools/registry/memory_search.go`, `cmd/aura` boot seeding | `subphases/Phase07C_Projection_Freshness_Registry/benchmark.md` | closed, self-audited |
| Phase07D - User/Operational Memory Typed Tiers | Wire `user_memory` and `operational` collection writers and recall surfaces as first-class typed memory tiers. | `internal/learning`, `internal/storage/memoryindex`, `internal/agent/tools/registry` | parent `benchmark.md` | planned |
| Phase07E - Source Span And Byte Offsets | Preserve source/page/span or stable artifact offsets on retrieved source hits and follow-up reads. | `internal/source`, `internal/storage/memoryindex`, `internal/agent/tools/registry` | parent `benchmark.md` | planned |
| Phase07F - Wiki Frontmatter Schema And Prompt-Version Promotion | Promote wiki schema/control metadata and prompt-version handling so wiki GraphRAG can trust page type, sources, and freshness. | `internal/wiki`, wiki schema docs, retrieval fixtures | parent `benchmark.md` | planned |

## Phase Boundary

- Phase 7 owns retrieval shape, typed layers, archive search hygiene, projection
  freshness, graph-aware recall, and RAG evals.
- Phase 9 owns promotion discipline, raw source immutability, wiki/source
  artifact inspection, write governance, and memory/source cleanup after typed
  recall exists.
- Phase 10 owns runtime configuration source-of-truth. Tool schemas and tool
  memory bootstrap are operational memory/config inputs, not compact memory
  rows.

## Non-Goals

- Do not delete or mutate user data, raw source files, wiki pages, Qdrant data,
  SQLite archive rows, logs, or runtime payloads in Phase07A.
- Do not require Neo4j or another graph database.
- Do not let stale vector emptiness suppress exact, FTS, or graph evidence.
- Do not promote user facts, raw tool failures, assistant scratchpad text, or
  raw conversation archive rows into wiki by accident.
- Do not replace the full memory architecture with Mem0, Nanobot Dream, or
  Logseq DataScript. Use them only as source patterns.

## PRD Coverage

| PRD Item | Plan Location | Benchmark Location | Source Evidence | Status |
| --- | --- | --- | --- | --- |
| Typed memory layers | this file and Phase07B | `benchmark.md` | `source.md` | partially met by Phase07B; user/operational wiring deferred |
| Conversation archive separated from curated memory | Phase07A | `subphases/Phase07A_Compact_Archive_Hygiene/benchmark.md` | `source.md` | met |
| Hybrid retrieval and RRF | Phase07B plus future golden eval slice | `benchmark.md` | `source.md` | partially met for score components; intent-split and golden retrieval evals remain planned |
| Projection freshness | Phase07C | `subphases/Phase07C_Projection_Freshness_Registry/benchmark.md` | `source.md` and Phase07C `source.md` | met for compact memory projection; wiki/source/Qdrant op-aware invalidation deferred |
| GraphIndex typed edges | future Phase07 graph slice | `benchmark.md` | `source.md` | planned |
| Community reports as projections | future Phase07 graph slice | `benchmark.md` | `source.md` | planned |
| Golden RAG evals | future Phase07 eval slice | `benchmark.md` | `source.md` | planned |

## Implementation Gate

A Phase 7 sub-phase is not ready until its `benchmark.md` proves durable
ground truth with SQLite rows, wiki/source artifacts, retrieval capsules, or
provider/tool response fields. Smoke checks and "it returned results" are not
completion evidence.
