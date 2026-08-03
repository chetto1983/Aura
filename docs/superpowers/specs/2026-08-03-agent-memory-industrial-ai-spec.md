# Agent Memory Industrial AI Specification

**Status:** decided  
**Date:** 2026-08-03  
**Production framework:** custom Go on the official MCP Go SDK  
**Storage:** ArcadeDB 26.7.3, one database and credential per Aura identity  
**Embedding:** EmbeddingGemma-300M Q8_0, 768 dimensions

## Objective

Make Aura's existing long-term memory service measurable, bounded and
provenance-safe without creating a second memory system. The production path is
`Aura agent -> Streamable HTTP MCP -> arcadedb-mcp -> tenant ArcadeDB`, with the
llama.cpp EmbeddingGemma sidecar as an optional dense signal. Lexical and graph
retrieval remain available when that signal is unavailable.

## Framework Quick Reference

| Candidate | Fit | Decision |
|---|---:|---|
| Custom Go + `modelcontextprotocol/go-sdk` | 4.90/5 | Production choice |
| LlamaIndex | 2.45/5 | Offline experiments only |
| LangGraph | 2.10/5 | Rejected: orchestration duplication |
| OpenAI Agents SDK | 1.70/5 | Rejected: second agent runtime |
| CrewAI | 1.60/5 | Rejected: second runtime and sidecar |

ArcadeDB owns traversal, full-text, HNSW and RRF. EmbeddingGemma owns only
text-to-vector. The Go MCP owns tenancy, validation, timeouts, fallbacks,
observability and maintenance. No framework may duplicate one of those owners.

## 2026 Research Baseline

The source snapshots are stored outside Aura under `D:\tmp`. Whole-system code
is not copied; licenses and architecture remain isolated.

| Donor | Pattern adopted | Pattern rejected |
|---|---|---|
| Hindsight | bounded query input; semantic, keyword, graph and temporal signals as peers | Python service, cross-encoder hot path |
| Graphiti | exact-fact reuse, bitemporal invalidation, episode provenance | Neo4j storage adapter |
| Cognee | multi-source ownership and reproducible namespaced eval artifacts | multi-database Python pipeline |
| Mem0 | source-bound extraction and batch-oriented work | hosted-only score claims and hand-tuned fusion |
| MemMachine | ground-truth episodes and explicit retrieval strategies | second memory router |
| MemOS / Letta / LangMem | lifecycle and background-maintenance contracts | additional agent/runtime layers |

Current upstream snapshots include `hindsight`, `MemMachine`, `MemOS`, `letta`,
`memory-benchmarks` and `langmem`; existing snapshots include `agent-memory`,
`mem0`, `graphiti`, `cognee` and Supermemory.

## Data Contract

An `Entity` has a stable canonical name and optional kind. A `FACT` joins two
entities and carries:

- natural-language statement and predicate;
- valid-time window (`valid_from`, `valid_to`);
- knowledge-time window (`created_at`, `expired_at`);
- one or more source-run references and source-memory identifiers;
- an optional EmbeddingGemma vector produced from the document task prefix.

An exact replay does not create a second edge. Another source supporting the
same fact is attached to it. Forget-by-source removes only that support; the edge
is deleted only after its last source is detached. A contradiction closes the
old valid-time window and creates the new fact. Manual entity merge remains the
only mutating entity-resolution authority.

## Retrieval Contract

1. Validate query length and result limit before I/O.
2. Compute one EmbeddingGemma query vector with the query task prefix.
3. Run bounded dense and Lucene rankings inside ArcadeDB, applying the valid-time
   window before each ranking is truncated.
4. Admit only dense rows at cosine distance `<= 0.55`. For lexical queries with
   two or more whitespace-separated terms, require Lucene `$score >= 2`; for an
   explicit single-term lookup, admit every positive `SEARCH_INDEX` match. The
   split keeps `Caraglio`/`Torino` and historical `as_of` lookups reachable while
   preventing one entity-only match from satisfying a longer unsupported claim.
   These EmbeddingGemma/Lucene defaults are recalibrated when the embedding model
   changes.
5. Fuse the admitted rankings with ArcadeDB RRF, hydrate by RID and restore
   fused order. If both legs are empty, return no facts and mark the response as
   an abstention instead of returning the least-distant unrelated rows.
6. Fall back to escaped, bounded and relevance-gated Lucene when the embedder or
   vector query fails.
7. Use `memory_facts_about` for exact bidirectional entity traversal.

Every response is bounded and identifies the effective retrieval path. Empty
memory is a valid empty result; malformed structured content, unbounded output
or silent dense failure is not.

The default service envelope is 2,048 runes per query, 512 per entity name,
4,096 per fact statement, 100 per predicate/source identifier, 64 source-memory
identifiers, 100 returned rows or maintenance rows, 20 digest facts per entity,
2,000 scanned digest facts, 400 candidates per hybrid leg and a 1 MiB MCP HTTP
body. Values above 100 are env-overrideable and every override remains a
positive hard bound.

## Evaluation Strategy

The canonical artifact schema is `aura.agent-memory-eval/v1`. It binds the Aura
commit, dirty-file hashes, ArcadeDB version, MCP server version, embedding model,
dimension, dataset hashes, seed and timestamps.

The Memory Reliability Score is a 100-point conformance score:

| Dimension | Weight | Required evidence |
|---|---:|---|
| Isolation and security | 25 | invalid identity refusal, two-tenant adversarial reads/writes, credential containment |
| Fact and provenance correctness | 25 | exact replay, multi-source attach/detach, supersession, as-of/known-at, erasure |
| Retrieval quality | 20 | lexical, cross-lingual dense, RRF order, temporal candidate filtering, Recall@K/MRR/nDCG |
| MCP and runtime resilience | 15 | initialize/list/call, structured errors, fallback, cancellation, restart/readiness |
| Operability and quality | 15 | p50/p95, output caps, re-embed/backfill, race, package coverage, no stale model contract |

The release threshold is `MRS > 96.5`. The following hard gates cannot be
compensated by points: zero cross-tenant leakage; zero skipped or missing cases;
successful live MCP initialize/list/call; EmbeddingGemma output exactly 768;
owned-package coverage at least 85%; and end-to-end local-appliance
`memory_search` p95 at or below 1,000 ms over at least 25 sequential samples.
The latency interval includes operator identity resolution, MCP initialize and
the tool call, with cold samples retained.

LoCoMo is reported separately. Evidence Recall@1/5/10, MRR and nDCG are retrieval
metrics; answer accuracy and faithfulness require a separately pinned answerer
and judge. Adversarial questions are included for abstention evaluation. No
metric is compared with a leaderboard using a different protocol.

## Reference Dataset

- deterministic synthetic tenant set for isolation, replay, supersession,
  multilingual retrieval and failure injection;
- frozen LoCoMo source with recorded hash and every category;
- production-shaped anonymized facts only when an operator explicitly exports
  them to an eval fixture;
- no evaluation writes to a person's tenant database.

## Guardrails

- One database and one derived credential per identity; UUID validation precedes
  provisioning.
- No arbitrary ArcadeDB query tool and no database name on the wire.
- Query, statement, entity, list and batch caps are enforced in the service, not
  trusted to callers.
- Dense failures preserve lexical availability and become observable.
- Destructive tools keep explicit annotations and dry-run where applicable.
- Evaluation fixtures use disposable identities/databases and clean them up.
- No secrets, raw personal memory or embeddings enter reports.

## Production Monitoring

Track requests and latency by tool and outcome, dense success/fallback reason,
candidate and returned counts, backfill backlog, re-embed failures, tenant
provisioning failures and MCP readiness. Alert on sustained dense fallback,
non-zero malformed results, p95 breach, backfill growth, repeated tenant
provisioning or any isolation-test failure. Logs carry tenant-safe hashes, never
credentials, statements or embedding payloads.

## Implementation Sequence

1. Land this specification and PRD amendment.
2. Remove Qwen/1024 and retired Neo4j contracts from tests, fixtures and prose.
3. Add input/output caps, entity kinds, escaped fallback and pre-rank temporal
   filtering with fail-first tests.
4. Add exact replay and multi-source provenance semantics with source-scoped
   forget tests.
5. Add the versioned evaluator and run it through the mounted live MCP on WSL.
6. Run package coverage, race, vet/build, security scan and remote CI before
   merge/push closure.
