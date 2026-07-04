---
spike: 085
name: document-ingest-tenancy
type: standard
validates: "Given A ingests a document under its identity, when B runs document_search, then B misses A's chunks — chaining 075/077 through the 083 tenancy planes"
verdict: VALIDATED
related: [083, 075, 077, 044, 032]
tags: [documents, retrieval, multi-user, per-identity, neo4j, tenancy, phase-36, v2.0.0]
---

# Spike 085: 2-identity document-ingest tenancy (leak → fix)

## What This Validates

083 closed per-identity isolation for box + Garage + memory but flagged one follow-up: the **document-ingest** path (upload → chunks → `document_search`, spikes 075/077) was never checked for cross-identity leakage. This spike closes it — and connects directly to **yesterday's MCP change** (commit `9a4ca594` *fix(memory): scope MCP graph tools by user*).

## The finding: the documents pipeline is identity-blind (unlike memory, now)

Yesterday's `9a4ca594` gave the **agent-memory** MCP per-user graph scoping via a `(:User {identifier})-[:HAS_FACT|HAS_ENTITY|HAS_CONVERSATION]->(…)` ownership pattern + `_SCOPED` query variants (this is what 083's memory plane exercised: A's fact `fact_count 1`, B's `0`). **The documents pipeline did NOT get that treatment** and is a *separate* graph path:

- `internal/documents` writes `:Document`/`:Chunk` through `internal/knowledge` (mcp-neo4j-cypher raw Cypher), **not** the agent-memory MCP's scoped tools.
- `:Chunk`/`:Document` carry **no** owner/identity/tenant property ([indexer.go](../../../internal/documents/indexer.go) `chunkUpsertQuery`): only `document_id`, `source_id`, `content_hash`, `text`, …
- `SearchRequest` ([types.go](../../../internal/documents/types.go)) is `{Query, DocumentID, Limit}` — **no identity field**. `IngestRequest` likewise.
- `Searcher.Search` with an empty `DocumentID` runs `db.index.fulltext.queryNodes('chunk_text', …)` over **all** chunks with no identity filter ([search.go](../../../internal/documents/search.go) `sparseSearchQuery`).

So an **unscoped `document_search` by identity B returns identity A's chunks.** Identity scoping today exists only in the *separate* Postgres `CatalogService` (identity→document_id metadata); it does not reach the graph retrieval path.

## How to Run

```bash
export AURA_NEO4J_BOLT_URL=bolt://127.0.0.1:7687 NEO4J_USER=neo4j NEO4J_PASSWORD=<pw>
go run ./.planning/spikes/085-document-ingest-tenancy/
```

The harness uses the **real** `documents.Indexer` + `documents.Searcher` over a thin neo4j-Go-driver `KnowledgeClient` adapter, against the live `chunk_text` fulltext index. It self-cleans (`spike085-*` nodes) before and after.

## What to Expect

The leak is reproduced through the real `Searcher`; the proposed fix isolates both ways.

## Investigation Trail

1. Read the pipeline: chunk write, `SearchRequest`, and both search queries → confirmed zero identity threading graph-side.
2. Traced the operator's steer ("look MCP we modify yesterday") to `9a4ca594` — the memory MCP's new `:User`-ownership scoping — and saw the documents path is the un-scoped twin.
3. Built the leak-then-fix harness on the real Indexer/Searcher. First run also surfaced two gotchas: (a) Cypher requires `WITH` between `MERGE` and `MATCH` in the ownership-edge write; (b) `os.Exit` skips `defer`, so cleanup must be called explicitly (fixed — the graph is left clean).

### Live-run evidence (2026-07-04, live neo4j 5.26)

```
INGEST  A ingest (alice doc, term "quetzalcoatl")  -> searchable    B ingest (bob doc, "borborygmus") -> searchable
LEAK    unscoped Searcher.Search("quetzalcoatl") -> hits=1, document_id=spike085-doc-a
        => B's agent running an unscoped document_search gets A's CONFIDENTIAL chunk. SearchRequest has no identity field.
FIX (yesterday's memory pattern applied to docs): add (:User{identifier})-[:HAS_DOCUMENT]->(:Document)
        B scoped search for "quetzalcoatl" -> 0 hits          <- B misses A's doc
        A scoped search for "quetzalcoatl" -> 1 hit (doc-a)   <- A finds its own
        symmetry: B finds own (1) + A misses B's (0)
SUMMARY VALIDATED — leak reproduced with real Searcher; :User HAS_DOCUMENT scope isolates docs (exit 0)
```

## What to Avoid

- **Do not assume the catalog's identity scoping protects retrieval.** The Postgres `CatalogService` scopes *metadata* (which `document_id`s an identity owns) but the graph `document_search` does not consult it; an unscoped call bypasses it entirely.
- **Do not rely on 077's catalog-injection alone for isolation.** 077 makes the agent *usually* pass a `document_id`; but any unscoped/generic `document_search` (or a prompt-injected one) leaks. Isolation must be enforced graph-side, not by hoping the agent always scopes.
- **Do not invent a new scoping mechanism.** Mirror the memory MCP's shipped `:User`-ownership pattern (`9a4ca594`) so documents and memory isolate identically.

## Constraints

- The fulltext index (`chunk_text`) cannot be restricted to a node set, so the scoped query filters hits with `EXISTS { (:User {identifier})-[:HAS_DOCUMENT]->(:Document {id: node.document_id}) }` after the fulltext yield — the same "seed then ownership-filter" shape as the memory `_SCOPED` queries. The dense vector seed (`docScopedVectorSeedQuery`, 075) needs the identical ownership predicate added.
- Ingest must create the ownership edge atomically with the document (like memory's `MERGE (:User)…MERGE (:User)-[:HAS_*]->`). Requires threading an `IdentityID` into `IngestRequest` → `ExtractedDocument` → the indexer write.

## Results

**VALIDATED ✓** — the document-ingest tenancy leak is real and reproduced through Aura's production `Searcher`, and the fix is the **same `:User`-ownership pattern shipped for memory yesterday** (`9a4ca594`), now proven to isolate documents both ways. This closes 083's flagged follow-up: with the ownership edge, A ingests → B's `document_search` misses it.

**Signal for the build (Phase 36 — this is the concrete plumbing that was "missing"):**
1. Add `IdentityID` to `documents.IngestRequest` and thread it to `ExtractedDocument`; on ingest, `MERGE (:User {identifier})` + `MERGE (:User)-[:HAS_DOCUMENT]->(:Document)` (atomic with the document upsert), mirroring the memory MCP.
2. Add `IdentityID` to `documents.SearchRequest`; make **every** retrieval query (`sparseSearchQuery`, `docScopedVectorSeedQuery`, the two-stage `Retrieve` seeds, the graphrag expand) require `EXISTS { (:User {identifier: $identity_id})-[:HAS_DOCUMENT]->(:Document {id: node.document_id}) }`. An empty identity must fail closed (return nothing), not fall back to the global index.
3. Thread the identity principal from `identityctx` into the `document_search` tool call and the ingest path — the same principal 083 fans out to box/bucket/memory. This unifies all four planes on one `:User {identifier}` = `identityctx` key.
4. Backfill: existing chunks with no `:User` edge become invisible under fail-closed scoping — the migration must attach ownership edges (from the Postgres catalog's identity→document_id map) before flipping retrieval to scoped.

**Open:** none for the isolation mechanism — leak + fix both proven live. The production change is a bounded, low-risk mirror of an already-shipped pattern (memory `9a4ca594`); it is Phase-36 build work, not further spiking.
