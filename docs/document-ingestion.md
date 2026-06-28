# Aura Document Ingestion

Aura indexes documents in every markitdown-readable format through a two-lane
pipeline. Files can enter through the CLI, Telegram, or the shared asset pipeline
used by the web cockpit:

```text
file -> extractor sidecar /extract -> Postgres job state
     -> Neo4j Document/Chunk fulltext upsert -> searchable
     -> embedding sidecar -> Neo4j vector properties -> complete
```

## Stores

Postgres stores ingestion jobs in `aura.document_ingest_jobs`: status, progress,
errors, source id, document id, and timing.

Neo4j stores document data:

- `(:Document)` metadata and lifecycle.
- `(:Chunk)` text, locator JSON, hashes, optional embedding.
- `(:Document)-[:HAS_CHUNK]->(:Chunk)`.
- `(:Chunk)-[:NEXT_CHUNK]->(:Chunk)` reading-order chain (one document has
  `chunk_count - 1` such edges; the upsert is idempotent on re-ingest).

The agent-memory MCP server is separate. It is for conversational memory,
entities, preferences, and facts. It is not the primary store for document
chunks.

## Status Lifecycle

```text
accepted -> extracting -> searchable -> embedding -> complete
                       \-> failed
```

`searchable` means sparse fulltext retrieval is ready. Dense embeddings are a
background enhancement and must not block user questions.

## Supported Files

A single allowlist (`internal/documents/extensions.go`) gates ingestion. Each
format gets a format-aware locator (page, sheet/row range, slide, or section):

- `.pdf` — page locators
- `.docx` — section/heading locators
- `.pptx` — one chunk per slide, slide + title locators
- `.xlsx`, `.xlsm` — sheet + row-range locators
- `.csv` — row-range locators
- `.html`, `.htm` — section/heading locators
- `.md`, `.markdown`, `.txt`, `.json`, `.xml`, `.epub` — generic markdown
- `.png`, `.jpg`, `.jpeg`, `.gif`, `.webp` — markitdown image text extraction

Any other markitdown-readable format still ingests through the generic-markdown
fallback (`_extract_markdown`) to at least one chunk with an empty-but-valid
locator. The size ceiling (`DefaultMaxIngestBytes`, 50 MiB) is enforced for all.

Default size policy:

- `<= 5 MiB`: synchronous Telegram path.
- `> 5 MiB` and `<= 50 MiB`: accepted large-document path.
- `> 50 MiB`: refused by default.

## CLI

Start dependencies:

```powershell
docker compose up -d neo4j aura-llama-embed markitdown
# Optional GPU reranker (RET-01); retrieval falls back to RRF when it is absent.
docker compose up -d aura-rerank
```

Apply Neo4j schema:

```powershell
go run ./cmd/aura neo4j migrate
```

Ingest:

```powershell
go run ./cmd/aura docs ingest "C:\Users\Davide\OneDrive - Sonepar\Documenti\G220_op_instr_0824_en-US.pdf"
```

Search:

```powershell
go run ./cmd/aura docs search "safety reset" --limit 5
```

Benchmark:

```powershell
go run ./cmd/aura docs bench "C:\Users\Davide\Desktop\Gestito Linea Automazione 2025.xlsx" --query "automazione linea"
```

Live E2E:

```powershell
$env:AURA_DOC_TEST_PDF='C:\Users\Davide\OneDrive - Sonepar\Documenti\G220_op_instr_0824_en-US.pdf'
$env:AURA_DOC_TEST_XLSX='C:\Users\Davide\Desktop\Gestito Linea Automazione 2025.xlsx'
$env:AURA_DOC_TEST_DOCX='C:\Users\Davide\OneDrive - Sonepar\Documenti\Corso Robot\Corso Base Robot.docx'
$env:AURA_DOC_TEST_PPTX='C:\Users\Davide\OneDrive - Sonepar\Documenti\deck.pptx'  # exercises a non-PDF format
$env:AURA_DOC_TEST_HTML='C:\Users\Davide\OneDrive - Sonepar\Documenti\page.html'  # optional, section locators
$env:AURA_DOC_TEST_RESET='1'
go test -tags document_ingest_live ./internal/documents -run TestLiveDocumentIngestE2E -count=1 -v
```

The live tier asserts each file reaches `searchable` with `>= 1` chunk and that
the `:NEXT_CHUNK` edge count equals `chunk_count - 1`. It enforces
no-skip-as-green: with none of the `AURA_DOC_TEST_*` vars set it skips locally
but `t.Fatal`s under `$CI`. New `.pptx`/`.html` handlers require a markitdown
sidecar rebuilt from this revision (`docker compose up -d --build markitdown`).

## Telegram

When `DocumentIngest` is wired by `aura serve`, Telegram document uploads use the
shared asset pipeline and reply when the file is indexed. The asset service stores
the original object, records lifecycle state in Postgres, and hands supported
documents to this ingestion pipeline.

## Web And Asset Uploads

The web cockpit uploads documents through `/api/assets`. A document asset becomes
usable for questions when its asset status reaches `searchable` or `complete`.
`searchable` has the same meaning as in the direct ingestion path: fulltext
retrieval is ready, while embeddings may still be catching up.

See [Aura Asset Pipeline](asset-pipeline.md) for object-store setup, upload
smoke testing, and asset troubleshooting.

## Agent Tool

The deferred `document_search` tool searches indexed chunks and returns cited
results with document id, chunk id, file name, locator, score, and text.

### Two-stage retrieval (RET-02)

`document_search` routes through `documents.Service.Retrieve`, which runs the fast
order proven by spike 070 Q4:

1. **Seed** — embed the query and take the top `<= 15` candidates from the dense
   `chunk_embedding` HNSW index. If the query cannot be embedded (embed sidecar
   down), the seed falls back to the sparse `chunk_text` fulltext index (RRF order).
2. **Rerank the seeds** — the ~15 seed chunks (not the expanded pool) are reordered
   by the `aura-rerank` sidecar with short truncation (~480 chars). A non-monotonic
   guard (`RerankThreshold`) keeps the seed order when the top rerank score is weak.
3. **Expand the winners** — only the reranked top-K winners are 1-hop expanded over
   `(:Chunk)-[:NEXT_CHUNK]-(:Chunk)` to attach reading-order context; winners stay
   first, unique neighbours follow.

The whole path is **fail-soft and regression-free**: with no reranker configured it
is exactly the current fulltext `Search`; when the reranker is absent or returns its
identity order, the result is the pre-rerank RRF/vector seed order. Retrieval is
message-prefix-safe — it operates only on chunk rows and never touches the cached
`messages[0]` system prefix.

### GraphRAG connected-nodes retrieval (RET-04)

`documents.Service.GraphRAG` exposes the same spike-070 Q4 order as a *connected-nodes*
result with **per-stage timing**, so the retrieval budget is observable (and provable
by the perf gate). It returns a `GraphRAGResult`:

- `Hits` — the reranked winner chunks (the answer), in reranked order.
- `Context` — their unique 1-hop connected neighbours over the connected-document graph
  (`(:Chunk)-[:NEXT_CHUNK|HAS_CHUNK]-(:Chunk)`, bounded by a per-winner neighbour cap;
  `:NEXT_CHUNK` is the reading-order edge that supplies context today, `:HAS_CHUNK`
  being the `Document->Chunk` membership edge).
- `Stages` — `VectorMS`, `ExpandMS`, `RerankMS`, each timed on a monotonic clock.

The order is fixed **seed → rerank → expand** and never re-seeds from the expanded pool;
only the reranked winners are expanded. Every stage is fail-soft, exactly as RET-02, and
rerank failure never blocks GraphRAG (it degrades to the seed order with context still
attached).

**Per-stage p95 budget:**

| Stage | p95 (spike, direct Bolt) | p95 (Aura, via MCP) | Notes |
|-------|--------------------------|----------------------|-------|
| vector seed (`db.index.vector.queryNodes`) | ~10–12 ms | ~50–65 ms | trivially cheap |
| graph expand (1-hop `:NEXT_CHUNK`) | ~15 ms | ~50–65 ms | trivially cheap |
| rerank (Qwen3-Reranker-0.6B Q4_K_M GPU) | ~333 ms (267 ms fast-path) | same (GPU) | the dominant bounded cost |
| end-to-end (seed-rerank path) | — | ~130 ms (no rerank) | < 500 ms with the GPU reranker |

Spike 070 Q2 measured the cheap stages at ~10–15 ms over a **direct Bolt driver**; Aura
reads the graph through the **`mcp-neo4j-cypher` MCP seam** (CLAUDE.md bans a native Go
driver), which adds ~40–50 ms of subprocess/JSON-RPC round-trip per read. The live tier
therefore holds the cheap stages to a **150 ms** per-stage ceiling — still a small
fraction of the 333 ms GPU rerank and the 500 ms end-to-end budget, so the spike thesis
holds: vector seed and graph expand are cheap, **rerank dominates** and scales with
`pool_size × doc_length`, which is why only the ~15 seeds (not the expanded pool) are
reranked.

Run the live tier (ingests + embeds the fixture, then asserts the per-stage budget). It
is **no-skip-as-green**: with `AURA_DOC_TEST_PDF` unset it skips locally but `t.Fatal`s
under `$CI`. The reranker is optional — without `AURA_RERANK_BASE_URL` (GPU-less host)
the rerank-dominant comparison is skipped but the vector + expand + end-to-end budget
still asserts:

```powershell
$env:AURA_DOC_TEST_PDF='C:\Users\Davide\OneDrive - Sonepar\Documenti\G220_op_instr_0824_en-US.pdf'
$env:AURA_RERANK_BASE_URL='http://127.0.0.1:8085'  # optional; omit on a GPU-less host
go test -tags graphrag_live -run TestGraphRAGLive ./internal/documents -count=1 -v
```

The tier logs a per-stage p50/p95 table for vector / expand / rerank / end-to-end.

## Reranker (optional, GPU)

A cross-encoder reranker is available as an optional GPU sidecar, `aura-rerank`
(Apache-2.0 Qwen3-Reranker-0.6B Q4_K_M on llama.cpp). It exposes `POST /v1/rerank`
and is wired through `AURA_RERANK_BASE_URL` (default `http://127.0.0.1:8085`).

The reranker is **fail-soft**: when the sidecar or GPU is absent, retrieval
degrades to the existing RRF/vector order and never hard-fails. CPU reranking is
unusably slow (~23s), so the sidecar is GPU-only and intentionally not a boot
dependency.

```powershell
docker compose up -d aura-rerank
curl http://127.0.0.1:8085/health
```

### Memory-recall reranking

The agent-memory MCP sidecar applies the same reranker to conversational recall.
`neo4j_agent_memory.rerank.rerank()` (stdlib only, mirroring the Go client) post-
processes the embedding-ranked `SEARCH_*_BY_EMBEDDING` results in
`ShortTermMemory.search_messages` via the fail-soft `BaseMemory.rerank_results`
hook. It reads `AURA_RERANK_BASE_URL` and, on any failure (no URL, transport,
non-2xx, decode, length/index mismatch), leaves the original embedding order
unchanged. It only **reorders** already-scoped results, so it can never widen memory
scope.

## Troubleshooting

Neo4j unreachable:

```powershell
go run ./cmd/aura neo4j ping
```

Missing MCP binary:

```powershell
pip install mcp-neo4j-cypher==0.6.0
```

Sidecar down:

```powershell
curl http://127.0.0.1:8083/health
```

Embedding dimension mismatch:

- Check `AURA_EMBED_DIMENSIONS`.
- The default Neo4j vector index expects 384 dimensions.

Fulltext index missing:

```powershell
go run ./cmd/aura neo4j migrate
go run ./cmd/aura neo4j status
```
