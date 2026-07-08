# Spike 071 — plan validation (full-codebase change inventory)

**Date:** 2026-07-08 · **Method:** 3 parallel read-only audits (Go / Python-agent-memory / infra), file:line-precise.
**Verdict:** the adopt-strategy plan is **directionally correct but materially incomplete**. The 5 planned
actions cover ~40% of the real surface. Corrected effort: **multi-week (≈4–8 wks for a validated E2E),
NOT "giorni netti."** The GDS-drop decision is **confirmed safe** but removes only *one* of ~8 hard parts.

---

## What the plan got right (validated)

- **Drop in-DB GDS is safe.** Both audits confirm `gds.*` is isolated to `integrations/microsoft_agent/`
  (lazy/`TYPE_CHECKING` imports; no `core`/`memory`/`graph`/`retrieval` dependency). Go side has **zero** GDS.
  Deleting it costs nothing in the live path. ✅
- **Most data-plane Cypher (MERGE/MATCH/UNWIND) ports over Bolt unmodified** (spike 068 cleared 7/9 constructs).
- **Repoint-by-env is clean** — `AURA_NEO4J_BOLT_URL` (Go) + `NEO4J_URI`/`Neo4jConfig` (Python) are single points.
- **ArcadeDB built-in MCP is a real adopt target** (native `get_schema`).

## What the plan MISSED (the reason it's weeks, not days)

| # | Missed surface | Where | Why it's not covered by the 5 actions |
|---|---|---|---|
| **G1** | **agent-memory engine is Neo4j-ONLY** | `docker/agent-memory` (vendored `neo4j-agent-memory`) | Emits Cypher + **creates its own vector/point/schema indexes at runtime**; no ArcadeDB backend exists. Repointing the Bolt URL will NOT make its emitted DDL ArcadeDB-compatible. **This is a fork/port, the dominant cost.** `aura` hard-depends on this sidecar healthy at boot. |
| **G2** | **Schema DDL is a subsystem** | Go `migrations/000{1,2}.cypher` + `schema.go`; Python `SchemaManager` (12 constraints + 16 indexes + 6 vector + 1 point) | ArcadeDB is schema-typed (pre-define vertex types) and uses `LSM_VECTOR/HNSW METADATA {...}`, not Neo4j `OPTIONS{indexConfig}`. Plus `SHOW VECTOR/INDEXES/CONSTRAINTS` catalog introspection + the dim-validation guard must be reimplemented. |
| **G3** | **Vector queries ≈ 20 sites, not ~3** | Python 14× `db.index.vector.queryNodes` (6 indexes) + Go `chunk_embedding` | All → ArcadeDB SQL `vectorNeighbors()`. Bigger, but mechanical. |
| **G4** | **Fulltext search** | Go `db.index.fulltext.queryNodes('chunk_text')` (search.go) | Separate rewrite class the plan never named. |
| **G5** | **Manual cosine** | Go `vector.similarity.cosine()` (retrieve.go doc-scoped) | Brute-force per-doc cosine ≠ index ANN; no 1:1 `vectorNeighbors()` map. |
| **G6** | **Spatial `point()` / POINT INDEX** | Python `long_term`/`query_builder`/`queries` + `entity_location_idx` + `geocoder` | ArcadeDB geo ≠ Neo4j `point{lat,lon,CRS}`. **Unscoped — the main schedule risk.** |
| **G7** | **Boot version gate** | Go `ping.go` asserts `dbms.components()` == Neo4j 5.26 | **Hard refuse-to-start** against ArcadeDB until rewritten. |
| **G8** | **Backup subsystem** | Go cron `apoc.export.cypher.all`; `/backups` bind | No ArcadeDB APOC export → HTTP `backup database` + runbook rewrite. |
| **G9** | **APOC ≈ 13–15 sites, not ~1** | Python `apoc.map.removeKeys` ×5; Go `apoc.convert.toJson`+`apoc.map.removeKey` across 3 graphview compilers + schema compiler + 2 example stores + `apoc.export` | Mechanical → pure Cypher, but **tests assert APOC is used** (`graphview_test.go:273-289`) → tests change too. |
| **G10** | **MCP serialization contract** | Go `client.go` `decodeRows`; `neostore.AsFloats`; "lists→NULL→toJson", "list-params dropped→UNWIND $rows" | These workarounds are `mcp-neo4j-cypher`-specific. ArcadeDB's built-in MCP changes row/embedding serialization → the injection guards + decoders must be re-probed, not assumed. |

**Architecture correction:** the Go runtime data path is **Cypher-over-MCP-subprocess** (`mcp-neo4j-cypher` JSON-RPC/stdio), not the native driver (native driver only in schema DDL, readiness probe, backup cron). So "adopt ArcadeDB MCP" (action d) is a **client rewrite** — tool names, JSON-RPC shape, injection guards (`graphview_guard.go`), result decoding — not an env swap.

## Corrected effort (per surface)

| Surface | Agent estimate | Dominant cost |
|---|---|---|
| Python agent-memory | 3–5 days *if the engine's Cypher were compatible* — but G1 makes it **~1–2+ wks** | engine is Neo4j-only (fork/port) + schema DDL + spatial |
| Go (`internal/knowledge`, `internal/documents`) | **~1.5–2 wks** | schema DDL, fulltext, cosine, version gate, backup, MCP client rewrite, APOC×10, test changes |
| Infra / config / CI | 2–4 days mechanical | gated by the code items above |
| **Total (working, tested, DoD ≥9.8 E2E)** | **≈4–8 weeks** | G1 (memory engine) + G2 (schema) + G6 (spatial) + G10 (MCP contract) |

## Go / no-go — recommendation

The migration is **feasible but is a multi-week project**, dominated by the Neo4j-only agent-memory engine
(G1), the schema-DDL subsystem (G2), spatial (G6), and the MCP client-contract rewrite (G10). Dropping
GDS (validated safe) removes exactly one blocker; it does **not** turn this into a days-long port.

→ **Hold to ADR 0038.** This is confirmed **contingency**, triggered only by the appliance-ships +
GPLv3-rejected condition — not a now task. If/when triggered, the critical-path spikes to run first are:
1. **G1 — prove the memory engine over ArcadeDB Bolt** (does its runtime-emitted Cypher incl. vector/point
   index creation work, or is a fork required?). This gates everything; run it before committing.
2. **G6 — spatial**: decide rewrite vs drop location entities.
3. **G10 — MCP serialization**: probe ArcadeDB's built-in MCP row/embedding shape vs the baked-in contract.
4. **G2 — schema DDL** translation + catalog introspection.

Everything else (data-plane Cypher, vector→vectorNeighbors, APOC→pure-Cypher, GDS delete, infra swap) is
mechanical once those four are proven.
