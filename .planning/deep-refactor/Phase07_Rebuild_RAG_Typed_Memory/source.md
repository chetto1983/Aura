# Phase07 Source Audit

Status: source-merged for parent planning. Phase07A, Phase07B, and Phase07C
have shipped with local code evidence. Phase07D-F remain planned and need fresh
source mapping before implementation.

| Source | Decision Supported | Adopt | Reject / Avoid | Status |
| --- | --- | --- | --- | --- |
| `D:/Aura/prd.md` lines 668-724 | Memory layer taxonomy and write policy | Conversation archive, experience store, operational memory, wiki, sources, projections, and cache are separate layers | One compact memory bucket for all text | read |
| `D:/Aura/prd.md` lines 806-890 | RAG contract | `RecallRequest -> RetrievalPlan -> RetrievalHits -> cited context capsule`; schema-aware layers; hybrid FTS/vector; graph expansion; freshness | Unbounded context injection and stale vector-only answers | read |
| `D:/Aura/prd.md` lines 1392-1427 | Phase 7 gates | Typed layers, citation handles, freshness, graph recall, golden evals | Tool failures in wiki or memory soup | read |
| `D:/Aura/prd.md` lines 1488-1504 | Phase 9 boundary | Keep Phase 9 for source discipline and wiki artifact inspection | Moving write-governance cleanup into Phase07A | read |
| `D:/Aura/.planning/aura-deep-refactor-decisions.json` ADR-031 | Local-first GraphRAG | Markdown wiki plus GraphIndex, FTS/Qdrant projections, community reports, cited capsules | Neo4j/Text2Cypher/core graph DB dependency | read |
| `D:/Aura/docs/graphrag-local-first-reference-map.md` | GraphRAG source map | Weighted graph signals, community reports as projections, no full graph dump in prompt | Community reports as truth | read |
| `D:/Aura/docs/aura-master-plan.md` lines 80-86, 150-153, 392-405 | Historical disposition | Mine old Wave 2 graph-memory and Context-Eng memory-tool ideas into Phase 7 | Execute old wave plans or stale paths directly | read as evidence |
| `D:/tmp/llm_wiki/README.md` lines 64-70, 192-223 | Wiki/source/retrieval pattern | Raw sources -> wiki -> schema; token search plus optional vector plus graph expansion and budget control | Client-local app state as Aura truth | read |
| `D:/tmp/llm_wiki/src/lib/graph-relevance.ts` | Graph scoring | Direct links, source overlap, common neighbors, type affinity | Raw adjacency dump as retrieval | read |
| `D:/tmp/llm_wiki/src/lib/wiki-graph.ts` | Graph projection | Typed nodes, weighted edges, Louvain community projection | Community projection as canonical knowledge | read |
| `D:/tmp/logseq/CODEBASE_OVERVIEW.md` lines 31, 72 | Queryable graph state | Separate document state from UI/runtime state; query graph through structured data | Blending graph truth with UI/chat state | read |
| `D:/tmp/mem0/openclaw/skills/memory-triage/SKILL.md` | Memory write quality | Store self-contained durable facts; skip tool outputs, transient status, one-time commands | Broad assistant/user transcript extraction as Aura user memory | read |
| `D:/tmp/mem0/openclaw/skills/memory-triage/recall-protocol.md` | Recall discipline | Rewrite queries and use category/time filters when structurally implied | Raw user message as the retrieval query | read |
| `D:/tmp/nanobot/docs/memory.md` lines 11-24, 46-62 | Memory layer pattern | Live session, compressed history, durable files, slower consolidation | Archive summary as final curated memory | read |
| `D:/tmp/nanobot/nanobot/agent/memory.py` | Consolidation guardrail | Cursored consolidation and no cursor advance on incomplete Dream | Raw archive fallback as curated memory | read |
| `D:/Aura/internal/storage/memoryindex/rebuild.go` lines 59-74, 205-224, 308-323 | Current pollution point | Centralize archive compact-index eligibility in `ArchiveDocument` so live append and rebuild share policy | Filtering only at formatter/output layer | read |
| `D:/Aura/internal/agent/tools/registry/memory_search.go` lines 107-123, 147-173, 476-498 | Current broad recall surface | Update descriptions/scopes toward typed layers; stop treating archive as default curated memory | `scope=all` silently mixing archive with wiki/source/proposals | read |
| `D:/Aura/internal/agent/tools/registry/memory_search_test.go` lines 338-351 | Current test expectation | Change tests to prove typed default behavior when implementing Phase07A | Preserving all-scope archive inclusion as a permanent invariant | read |
| `D:/Aura/internal/storage/memoryindex/rebuild_test.go` | Closest archive-index tests | Add role/tool-output exclusion tests while preserving raw `conversations` archive | Changing archive persistence when compact-index policy is enough | read |
| `D:/Aura/scripts/ralph/prd-completed-phase-m.json` | Phase07C closure queue and story contract | Use US-M01..M04 as evidence for the implemented projection freshness slice | Treat Ralph queue state as canonical Aura planning; translate into Phase07C files instead | read |
| `D:/Aura/docs/phase07c-current-state-audit-2026-05-16.md` | Phase07C state inventory | Promote `tool_index_state` tuple into a general projection registry and keep current write paths explicit | Guessing reindex owners from memory | read |
| `D:/Aura/docs/phase07c-academic-research-2026-05-16.md` | Phase07C freshness vocabulary | Use `embedding_model_id`, `index_build_id`, `indexed_at`, and `degraded_read` as interoperable freshness fields | Inventing Aura-only names that future telemetry cannot map | read local research artifact |
| `D:/Aura/docs/phase07c-web-research-frameworks-2026-05-16.md` | Phase07C framework comparison | Keep freshness as application-owned SQLite state, with hashes/versioning at write time | Relying on vector-store health as truth | read local research artifact |
| `D:/Aura/docs/phase07c-mem0-elysia-patterns-2026-05-16.md` | Phase07C design synthesis | Choose one `projection_state` table plus per-document freshness columns | Per-chunk freshness side table, automatic memory TTL, graph DB replacement | read |
| `D:/Aura/internal/storage/freshness/store.go` | Phase07C canonical store | SQLite `projection_state` rows with optimistic concurrency, pending counters, and rebuild completion updates | Volatile in-memory freshness as user-visible truth | read |
| `D:/Aura/internal/db/migrations/migrations.go` versions 12-13 | Phase07C schema | Add `projection_state` and compact document freshness columns without data deletion | Main run/event tables absorbing projection churn | read |
| `D:/Aura/internal/storage/memoryindex/rebuild.go` | Phase07C writer hooks | Stamp compact docs with `content_hash`, `embedding_model_id`, `index_build_id`; bump pending on hash drift; mark rebuild complete | Holding SQLite transactions across vector rebuilds | read |
| `D:/Aura/internal/agent/tools/registry/memory_search.go` | Phase07C retrieval surface | Emit `freshness=` and `degraded_read=true` annotations for compact memory hits | Letting stale/degraded vector state silently shape answers | read |

## Adopted Decisions

- Phase07A was the first slice because it fixed the observed production-style
  failure without schema migration or data deletion.
- `conversations` remains the full archive and may contain tool rows. Compact
  memory/retrieval eligibility is a separate policy.
- Raw tool failures and raw tool outputs belong to trace/experience layers, not
  curated memory, wiki, or default knowledge recall.
- Runtime tool schemas and tool memory bootstrap are operational/config inputs.
  They must be loaded deliberately, not rediscovered from compact archive rows.
- GraphRAG work must stay local-first over Aura's Markdown wiki and rebuildable
  projections.
- Phase07C stores projection freshness in SQLite `projection_state`, not in
  logs, Qdrant-only metadata, or cache.
- Phase07C uses per-document `content_hash`, `embedding_model_id`, and
  `index_build_id` on compact memory rows as the first drift signal.
- Retrieval output may surface freshness/degraded annotations; the annotation
  is user-visible context quality metadata, not proof that the underlying fact
  is true.

## Rejected Or Deferred Decisions

- No direct execution of `.planning/wave*` plans.
- No graph database, Text2Cypher, or full graph prompt dump in Phase 7.
- No Mem0-style ADD-only hosted memory replacement.
- No Nanobot raw archive fallback as Aura curated memory.
- No Phase 9 write-governance rewrite inside Phase07A.
- No separate per-chunk freshness docstore for Phase07C; compact memory rows
  already own the document identity.
- No Qdrant alias swap, embedding drift adapter, or wiki/source invalidation
  cascade in Phase07C; those need their own future slice.

## Missing Source Questions

- Phase07C mapped compact-memory projection freshness. Before Phase07D, map
  typed user/operational writers and recall surfaces against the already
  shipped `recall_user_memory` and `recall_operational` code.
- Before a future wiki/source projection slice, map reindex job ownership,
  delete/rename operations, and projection watermarks.
- Before a future GraphRAG slice, map `internal/wiki/graph_index.go`,
  `internal/wiki/store_writes.go`, and graph document projection tests.
