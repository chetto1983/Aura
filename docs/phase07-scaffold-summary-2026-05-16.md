# Phase 7 Scaffold Summary — 2026-05-16

Synthesised from `.planning/deep-refactor/Phase07_Rebuild_RAG_Typed_Memory/`
parent + Phase07A subfolder + `prd.md` §7.

## A. PRD §7 (lines 1394-1430) verbatim

**Goal:** retrieval stops being one broad memory soup.

**Steps:** define memory layer IDs + citation handles; collection metadata
registry (wiki/sources/user/archive/operational); split recall by task intent;
hybrid FTS/vector with RRF fusion; preserve chunk-to-parent expansion;
projection freshness registry; durable idempotent op-aware reindex jobs;
per-slug wiki upsert/delete reindex + force full-rebuild; GraphIndex typed
weighted edges + source edges + bounded queries; community detection +
graph insight jobs; community reports as derived projections with evidence
handles; community reports as hints (not hidden truth); structured retrieval
hits with score components + follow-up handles; freshness/degraded warnings;
retrieval errors as recoverable learning events; golden RAG evals.

**Gate (10 criteria):** user facts not in wiki unless promoted; tool failures
not in wiki; source hits cite artifact; wiki hits cite `[[slug]]`; hybrid
beats vector-only/keyword-only; stale projections visible; stale vector
empty cannot suppress exact/FTS/graph; delete/rename fixtures prove cleanup;
GraphRAG evals local+global; community reports carry evidence; Neo4j NOT
required; repeated bad searches self-heal.

## B. Parent plan: Deep Module RFC

| Option | Decision | Reason |
|---|---|---|
| A. Keep `search_memory(scope=all)` broad | Rejected | Preserves memory soup |
| B. Filter only at output | Rejected | Bad data pollutes rankers/rebuilds |
| **C. Typed recall + archive hygiene first** | **Chosen** | Small slice, high leverage, compatible |
| D. New memory platform/graph DB | Rejected | Second truth source |

**Chosen module shape:**
```
RecallRequest → typed collection registry → retrieval plan by layer/intent
            → FTS/vector/graph/proposal/archive adapters
            → cited context capsule with score and freshness
```

## C. Sub-phases

| Sub-Phase | Goal | Primary Files | Status |
|---|---|---|---|
| **Phase07A** Compact Archive Hygiene | Raw tool output stays in `conversations` but does not enter compact memory or default recall | `rebuild.go`, `memory_search.go`, tests | **planned next** |
| Phase07B Typed Collection Registry | Layer metadata + citation handles + filterable + freshness fields | `memoryindex`, future `internal/rag`, adapters | planned |
| Phase07C Task-Level Recall Surface | Move from broad `search_memory` to `recall_user/knowledge/operational` | `agent/tools/registry`, prompt/tool descriptions | planned |
| Phase07D Projection Freshness + Reindex Jobs | Explicit durable stale-aware op-aware projections | `storage/search`, reindex code | planned |
| Phase07E Wiki GraphRAG | Weighted typed edges + bounded queries + community reports | `internal/wiki`, graph/rag code | planned |
| Phase07F Golden RAG Evals | Fixtures prove layer separation, citations, freshness, delete/rename, GraphRAG local/global | eval/probe harness | planned |

## D. Phase Boundary

- **Phase 7** owns: retrieval shape, typed layers, archive hygiene, projection freshness, graph-aware recall, RAG evals.
- **Phase 9** owns: promotion discipline, source immutability, wiki/source inspection, write governance.
- **Phase 10** owns (closed): runtime config source-of-truth.

## E. Phase07A planned-next primary files

1. `internal/storage/memoryindex/rebuild.go` — centralise `ArchiveDocument` eligibility policy
2. `internal/storage/memoryindex/rebuild_test.go` — role exclusion + live-vs-rebuild parity
3. `internal/agent/tools/registry/memory_search.go` — description/scope updates if required
4. `internal/agent/tools/registry/memory_search_test.go` — prove tool-output excluded from default recall
5. `internal/conversation/archive_turns_test.go` — guard raw persistence

## F. Outstanding Questions

- Phase07A: NONE (bounded scope, single-seam fix at `rebuild.go:308-324`)
- Phase07B (later): map current collection schemas + citation handles + vector payload fields
- Phase07D (later): map reindex job ownership + projection watermarks
- Phase07E (later): map `graph_index.go` + `store_writes.go` + graph projection tests

## G. Stop Signals (from Phase07A plan §"Stop Signals")

- A required fix needs data deletion or migration → escalate
- Excluding tool rows breaks archive deletion/purge invariants → escalate
- Final assistant rows cannot be distinguished from intermediate scaffolding without schema → close Phase07A with `role=tool`-only exclusion + plan schema-backed Phase07B/C follow-up
