---
id: 071-arcadedb-adopt-strategy
title: ArcadeDB adopt-strategy — de-Neo4j the appliance by adopting existing pieces, not rewriting
date: 2026-07-08
status: PLANNED (contingency — gated by ADR 0038 triggers; no containers run yet)
type: standard
tags: [arcadedb, neo4j, agent-memory, mcp, gds, leiden, rerank, license, migration]
related: docs/adr/0038-graph-store-license-neo4j-gplv3-vs-arcadedb-apache.md, .planning/spikes/068-arcadedb-pipeline, .planning/spikes/069-arcadedb-vs-neo4j-realdata, .planning/spikes/070-rerank-value-or-overengineered
---

# Spike 071 — ArcadeDB adopt-strategy (de-Neo4j the appliance)

## Why / contingency framing

Goal: be able to **ship the appliance copyleft-free** by moving the graph layer from
Neo4j (GPLv3) to ArcadeDB (Apache-2.0). Per **ADR 0038** this is **NOT needed now** — it's
triggered only when (a) the appliance ships AND legal rejects GPLv3, or (b) hard per-tenant
isolation makes Neo4j Enterprise's price fatal. This spike **pre-scopes** the work so it's
ready when a trigger fires. It is deliberately NOT executed against live containers yet
(deferred to a clean session; a Phase-37A agent is concurrently active).

**Reframing (this spike's contribution):** don't fork-and-rewrite — **adopt existing ArcadeDB
pieces**. No 1:1 agent-memory replacement exists, but the pieces that de-risk the port do.

## The adopt landscape (researched 2026-07-08)

| Candidate | What it is | License / maturity | Use for Aura |
|---|---|---|---|
| **ArcadeDB built-in MCP server** | 5 tools: `list_databases`, `get_schema`, `query` (read, Cypher), `execute_command` (write), `server_status` | Apache-2.0, built into the DB (26.3.1+) | **ADOPT** — drop-in for `mcp-neo4j-cypher`; native `get_schema` removes the `apoc.meta.*` schema need |
| **langchain-arcadedb** (PyPI, official) | LangChain graph store, `GraphCypherQAChain`, pure-Cypher schema-introspection + doc-import, standard neo4j driver over Bolt | MIT, **v0.1.0 (2026-03-01), 4★** — early | **REFERENCE** — proves the "pure Cypher + std driver + Bolt" approach; not an APOC map, not memory |
| **llama-index-graph-stores-arcadedb** | LlamaIndex PropertyGraphStore | (LlamaIndex ecosystem) | Only if Aura adopts LlamaIndex (it doesn't today) |
| **langchain4j-community-arcadedb** | Java embedding store + native HNSW | Java | Irrelevant — Aura's memory sidecar is Python, daemon is Go |
| **stevereiner/flexible-graphrag** | Full-stack GraphRAG: 15 graph backends incl. ArcadeDB adapter, doc pipeline, hybrid search, MCP, FastAPI, Docker Compose | Apache-2.0, 163★, active | **ARCHITECTURAL REFERENCE** — see how they abstract the ArcadeDB adapter; NOT a drop-in (it's doc-RAG, no POLE+O per-user episodic memory) |

**Verdict — no magic repo.** `agent-memory` (neo4j-labs) is category-specific: POLE+O schema,
entity extraction, MCP tools, short/long/reasoning memory. The repos above give the graph store
on ArcadeDB, not Neo4j-Labs' memory logic. → **Strada 1: fork `agent-memory`, repoint to
ArcadeDB Bolt, adopt the built-in MCP, keep POLE+O.** Not a rewrite.

## The three Neo4j bindings (full de-Neo4j scope, code-grounded)

| # | Binding | Real surface (verified) | Port action |
|---|---|---|---|
| 1 | **agent-memory** (Python sidecar, `docker/agent-memory`) | ~200 temporal/graph Cypher constructs (datetime/duration/point/MERGE/FOREACH/CALL — spike 068 cleared 7/9 over ArcadeDB Bolt). **1 real APOC** (`apoc.map.removeKeys` ×5, [queries.py:887-934](docker/agent-memory/src/neo4j_agent_memory/graph/queries.py#L887)). **~3 vector-query sites** (`db.index.vector.queryNodes` → [short_term.py:849](docker/agent-memory/src/neo4j_agent_memory/memory/short_term.py#L849), [consolidation.py:75,255](docker/agent-memory/src/neo4j_agent_memory/memory/consolidation.py#L75)). GDS only in optional `integrations/microsoft_agent` | Repoint `NEO4J_URI`/`Neo4jConfig.uri` ([settings.py:79](docker/agent-memory/src/neo4j_agent_memory/config/settings.py#L79)); rewrite 1 APOC; rewrite vector queries → SQL `vectorNeighbors()`; GDS → see §GDS |
| 2 | **graphview** (Go, `internal/knowledge/graphview_intent.go`) | 3 read intents (seed/expand/schema_overview); uses `apoc.convert.toJson(labels())` + `apoc.map.removeKey`; parameterized, no value interpolation | Repoint `AURA_NEO4J_BOLT_URL`; rewrite the 2 APOC helpers to pure Cypher; spike 068 says the reads port ~as-is over Bolt (RID vs string element-ids) |
| 3 | **mcp-neo4j-cypher** (the LLM↔graph MCP) | schema via `apoc.meta.*`; read/write Cypher over Bolt | **Replace with ArcadeDB's built-in MCP** (native `get_schema`) — adopt, don't port |

**The brief's "6 APOC + Levenshtein" was wrong** (corrected in spike 068, 2026-07-08):
`levenshtein` exists **nowhere** in agent-memory; `apoc.meta.*`/`path.expandConfig` are Go-side
or planning-doc only; `apoc.convert.fromJsonMap` is docstring-only. Real APOC surface ≈ **1
procedure** in the sidecar + 2 helpers in Go.

## The one hard part — GDS (and how the reranker deletes it)

`integrations/microsoft_agent/gds.py` calls `session.run("CALL gds.pageRank.stream…/…louvain…")`
over Bolt, gated by `is_gds_available()`. **ArcadeDB's 70+ algos are Java-API/Graph-OLAP-bound —
NOT callable via `CALL gds.*` over Bolt/SQL** (spike 068). So "map to native" is not a `CALL`
swap. Options:
- **(a) Drop GDS/Leiden entirely** — if the **reranker** (spike 070) + vector retrieval give the
  precision, community detection isn't needed for memory quality. PRD already defers 11c as
  lazy/on-demand. → `gds.py` / `microsoft_agent` GDS binding is **deleted, not ported.** ← recommended
- (b) External `leidenalg`/`graspologic` consolidation job over Bolt (store-agnostic, GraphRAG-standard).
- (c) ArcadeDB Graph-OLAP invocation (unproven over the wire — the main open risk).

**If (a): the migration's only hard blocker disappears** → repoint URL + 1 APOC rewrite + vector
rewrite + adopt built-in MCP + close the ~2% Cypher gap = "giorni netti."

## Execution plan (clean session — NOT run yet)

Prereq: bring up a pinned **release** ArcadeDB (not `:latest`/SNAPSHOT — post-CVE-2026-44221;
26.7.1 is the security-hardening release). Reuse harnesses:
- **`scripts/puppygraph_compat_probe.go`** — env-driven Bolt-compat probe (query table incl.
  `db.index.vector.queryNodes`); repoint at ArcadeDB Bolt.
- **spike-069 `g220_spike.py`** — Python neo4j-driver harness (works against both engines).

1. Stand up ArcadeDB (Bolt plugin, pinned release, `aura[dbadmin:dbadmin]` bracket — not `root`).
2. **Cypher-gap probe:** replay the real Cypher from all 3 bindings → log per-statement pass/fail
   → quantify the 2.2%-TCK gap's blast radius on Aura's actual queries.
3. **Vector:** rewrite the ~3 `db.index.vector.queryNodes` sites → SQL `vectorNeighbors()`; verify
   ranking parity (spike 069 proved 4.5/5 overlap@5 on real data).
4. **APOC:** rewrite `apoc.map.removeKeys`/`removeKey` (map comprehension) + `apoc.convert.toJson`.
5. **MCP:** wire Aura's MCP client to ArcadeDB's built-in MCP; confirm `get_schema` + read/write;
   **verify transport** (docs say HTTP JSON-RPC `POST /api/v1/mcp`; a release note claims stdio —
   resolve which, and how Aura's client attaches).
6. **GDS decision:** if reranker suffices → delete GDS binding; else stand up external `leidenalg`.
7. Run agent-memory integration tests + the 4 Aura flows (memory/ingest) against ArcadeDB.
8. **Tenant-isolation gate (load-bearing):** prove Bolt cross-DB attach is denied on the pinned
   release (spike 068 saw a loose attach boundary + a CVSS-9.0 history here).

## Go / no-go gates (unchanged from spike 068, refined)

1. Cypher-gap blast radius on Aura's real queries ≤ tolerable (measured, not assumed).
2. Vector rewrite verified at ranking parity.
3. GDS resolved (dropped-via-rerank ← preferred, or external job).
4. Bolt tenant isolation airtight on a pinned post-CVE release build.
5. MCP round-trip (read+write+schema) green via the built-in server.

## Open risks
- ArcadeDB MCP transport ambiguity (HTTP-only vs stdio) — verify before wiring.
- `langchain-arcadedb` is v0.1.0/4★ — a *reference*, not a dependency to take on.
- GDS-over-OLAP path unproven — avoid by dropping GDS (rerank) rather than proving it.
- SNAPSHOT vs release: all prior spikes used `:latest` SNAPSHOT; the isolation gate MUST use a pinned release.
