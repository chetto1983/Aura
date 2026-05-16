# Phase07C Source Audit - Projection Freshness Registry

Status: source-audited from local PRD, local research artifacts, Ralph Phase-M
queue, and current implementation files. No online source was newly audited in
this repair turn.

| Source | Decision Supported | Adopt | Reject / Avoid | Status |
| --- | --- | --- | --- | --- |
| `D:/Aura/prd.md` Phase 7 and status table | Phase07C owns projection freshness registry | Durable projection state, content-hash drift, degraded-read annotations | Treating task-level recall as Phase07C after PRD renamed it | read |
| `D:/Aura/.planning/aura-deep-refactor-decisions.json` ADR-029, ADR-031 | RAG projections are not truth and need freshness | Fresh/stale/rebuilding/degraded/disabled state and local-first GraphRAG posture | Vector result absence as evidence of source absence | read |
| `D:/Aura/scripts/ralph/prd-completed-phase-m.json` | Implementation story contract | US-M01..US-M04 as bounded evidence translated into Aura phase files | Ralph queue as canonical planning state | read |
| `D:/Aura/docs/phase07c-current-state-audit-2026-05-16.md` | Existing state inventory | Promote the `tool_index_state` tuple pattern into a general registry | Guessing projection owners without file evidence | read |
| `D:/Aura/docs/phase07c-academic-research-2026-05-16.md` | Freshness fields and annotation vocabulary | `embedding_model_id`, `index_build_id`, `indexed_at`, `degraded_read` | Aura-only field names that cannot map to broader telemetry | read local research artifact |
| `D:/Aura/docs/phase07c-web-research-frameworks-2026-05-16.md` | Framework comparison | Application-owned freshness in SQLite with explicit version/hash fields | Outsourcing freshness truth to vector-store health only | read local research artifact |
| `D:/Aura/docs/phase07c-mem0-elysia-patterns-2026-05-16.md` | Final scope synthesis | One `projection_state` table and per-document freshness columns | Per-chunk freshness side table, automatic TTL, graph DB replacement | read |
| `D:/Aura/internal/db/migrations/migrations.go` | Schema evidence | Migrations v12-v13 and fresh-schema parity for projection state and compact freshness columns | Runtime-only schema drift | read |
| `D:/Aura/internal/storage/freshness/store.go` | Canonical store interface | `Get`, `Upsert`, `UpdateWithVersion`, `BumpPending`, `MarkRebuildComplete`, `List` | Hidden cache-only projection status | read |
| `D:/Aura/internal/storage/memoryindex/rebuild.go` | Write/rebuild invalidation | Stamp hash/model/build and update pending/completed counts | Long transactions spanning vector rebuilds | read |
| `D:/Aura/internal/storage/search/compact_qdrant.go` | Rebuildable payload mirror | Mirror freshness fields into compact Qdrant payload when populated | Treat Qdrant payload as canonical | read |
| `D:/Aura/internal/agent/tools/registry/memory_search.go` | Retrieval annotation | Show `freshness=` and `degraded_read=true` to the agent | Silent stale/degraded retrieval | read |

## Adopted Decisions

- SQLite `projection_state` is the canonical freshness registry for this slice.
- Compact memory rows carry the per-document drift tuple directly.
- Search output exposes freshness/degraded state only when fields are populated.
- Old compact rows with empty defaults remain backward-compatible.
- Boot seeding creates the closed set of five projection rows idempotently.

## Rejected Or Deferred Decisions

- No per-chunk freshness side table.
- No graph database, Qdrant alias swap, or embedding drift adapter in Phase07C.
- No wiki/source delete/rename invalidation in this slice.
- No task-level recall surface redesign inside Phase07C.

## Missing Source Questions

- Which future slice owns wiki/source delete and rename invalidation against
  `projection_state`?
- Should dashboard/API health expose `projection_state` rows directly or through
  a redacted retrieval-health capsule?
- Should stale compact Qdrant rows trigger a warning only, or also enqueue an
  outbox/reindex job once Phase08 durable work is available?
