# GraphRAG Local-First Reference Map

Purpose: keep the sources, examples, adopted patterns, and rejected paths for
Aura's local-first GraphRAG work easy to find during development.

## Decision

Aura implements GraphRAG over the canonical Markdown wiki, not as a Neo4j-backed
core dependency.

```text
raw sources
  -> curated Markdown wiki
  -> GraphIndex + FTS/Qdrant projections
  -> community detection and reports
  -> cited retrieval capsules
```

Neo4j remains a possible future sidecar for very large or exploratory graphs,
but it is not part of the core refactor.

## Local Sources

| Source | Use |
| --- | --- |
| `D:/Aura/docs/llm-wiki.md` | Core pattern: raw sources, persistent wiki, schema, ingest/query/lint loops. |
| `D:/Aura/prd.md` | Target architecture, RAG contract, wiki graph contract, Phase 7 gates. |
| `D:/Aura/AGENTS.md` | Repo-level operating rules for wiki graph, RAG freshness, sources/examples. |
| `D:/Aura/.planning/aura-deep-refactor-decisions.json` | ADR-027 through ADR-031 decision history. |
| `D:/Aura/internal/wiki/graph_index.go` | Existing in-memory graph index to extend. |
| `D:/Aura/internal/wiki/store_writes.go` | Wiki write path that refreshes graph state and submits reindex work. |
| `D:/Aura/internal/storage/search/graph_documents.go` | Existing graph document projection for search. |
| `D:/Aura/internal/storage/search/qdrant.go` | Current Qdrant projection/reindex behavior and known freshness pitfalls. |

## Example Sources

| Example | Adopt |
| --- | --- |
| `D:/tmp/llm_wiki/src/lib/wiki-graph.ts` | Local graph build from Markdown, Louvain community detection, cohesion, top nodes. |
| `D:/tmp/llm_wiki/src/lib/graph-relevance.ts` | Direct links, source overlap, common neighbors, type affinity scoring. |
| `D:/tmp/llm_wiki/src/lib/graph-insights.ts` | Surprising links, isolated pages, sparse communities, bridge nodes. |
| `D:/tmp/llm_wiki/src/lib/ingest-queue.ts` | Persistent ingest/review style queue patterns. |
| `D:/tmp/llm_wiki/src/lib/sweep-reviews.ts` | Review/proposal sweep pattern for stale or uncertain graph state. |
| `D:/tmp/llm_wiki/src/lib/wiki-page-delete.ts` | Delete cascade pattern for wiki/source/media/index cleanup. |

## External References

| Reference | Adopt |
| --- | --- |
| `https://arxiv.org/abs/2404.16130` | Local/global GraphRAG distinction, entity graph, community summaries, map-reduce global answers. |
| `https://microsoft.github.io/graphrag/query/local_search/` | Entity-focused local search using graph plus raw text units. |
| `https://microsoft.github.io/graphrag/examples_notebooks/global_search/` | Community report based global search; use as an offline/budgeted pattern, not default. |
| `https://www.microsoft.com/en-us/research/blog/introducing-drift-search-combining-global-and-local-search-methods-to-improve-quality-and-efficiency/` | DRIFT-style community-primer plus local follow-up loop as a future query mode. |
| `https://neo4j.com/labs/genai-ecosystem/graphrag/` | Useful comparison for graph/vector traversal and explainability; not adopted as core storage. |
| `https://neo4j.com/docs/graph-data-science/current/algorithms/community/` | Confirms mature community detection options; use as benchmark/reference, not dependency. |

## Adopted Patterns

- Markdown wiki remains the canonical graph and human-readable artifact.
- GraphIndex is the low-latency traversal substrate for ordinary recall.
- FTS, Qdrant, graph documents, and community reports are rebuildable
  projections with freshness state.
- `recall_knowledge` seeds with hybrid search, expands through graph neighbors,
  ranks with graph signals, then returns a compact cited capsule.
- Community detection runs offline or as a scheduled maintenance job, not inside
  every user turn.
- Community reports are derived summaries with evidence handles. They may be
  promoted into wiki `synthesis` pages only through review/proposal flow.
- Graph health produces proposals for orphans, sparse clusters, bridge nodes,
  contradictions, and missing pages.

## Rejected Patterns

- No Neo4j or graph database in the core refactor.
- No direct Text2Cypher or LLM-generated graph query execution in the core loop.
- No default global map-reduce over all community reports on every query.
- No hidden second source of truth for graph edges or summaries.
- No full graph dump in prompt context for ordinary recall.
- No LLM whole-page rewrites when a constrained patch can preserve user content.

## First Implementation Slices

1. Extend `GraphIndex` with typed weighted edges, source edges, degree, and
   bounded neighborhood/path queries.
2. Build graph relevance scoring in `internal/rag` or `internal/wiki` using
   direct link, source overlap, common neighbor, and type affinity signals.
3. Add local community detection and graph insight jobs with durable freshness
   state.
4. Generate community reports as derived projections with citations and
   promotion workflow into wiki synthesis pages.
5. Upgrade `recall_knowledge` to use hybrid seed retrieval plus graph expansion
   and community hints.
6. Add evals for local entity questions, global sensemaking questions,
   stale/degraded projections, deletes, renames, and source-backed citations.
