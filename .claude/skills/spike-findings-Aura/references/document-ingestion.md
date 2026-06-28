# document ingestion

Implementation blueprint from session-11/12 spikes (2026-06-12): the Phase-15 large-document
ingestion lane. Telegram markitdown size tiers, PrivateGPT async-ingest as a REFERENCE (not a
dependency), provenance-scoped chunk identity, retrieval/citation signal, the Telegram ingest-job
UX state machine, and the fast-lane sparse-first industrial PDF path proven on the real Siemens
G220 manual.

## Requirements

These are binding (MANIFEST §Session-11, lines 80-90). A future build MUST honor each.

- **Conversion thresholds are exact and tested**: `<=5 MiB` sync, `>5 MiB && <=50 MiB` async,
  `>50 MiB` refused — unless the product contract is deliberately changed. The seam is
  `documentsClient.Convert` in `internal/channels/telegram/documents.go` (constants
  `asyncTierMinBytes = 5 << 20`, `refuseTierMinBytes = 50 << 20`). Spike 043a found the original
  switch used `>=` for both (exactly 5 MiB routed async, exactly 50 MiB refused, contradicting the
  `<=5`/`>50` comments); the codebase now uses `>` for both — keep it that way and add exact-5-MiB
  and exact-50-MiB boundary tests (still missing).
- **The 5-50 MiB path must be memory-bounded.** No "full in-memory multipart body + a sidecar that
  reads the whole upload into memory." Use streaming or temp-file transfer before calling the path
  bounded. (`postConvert` still builds a `bytes.Buffer` multipart body, and `docker/markitdown`'s
  FastAPI wrapper does `await file.read()` — both materialize the payload; this is NOT yet fixed.)
- **Document memory ingest creates a job/status object BEFORE work starts.** Minimum exposed states:
  `accepted`, `running`/progress, `success`, `failure`, `refused`, `canceled`, plus pollable status.
  Oversize refusal stays immediate and allocates NO job. A job ID before work begins is the key UX
  difference from the current goroutine-only path; failure needs a durable status because memory
  ingest can outlive a single handler context.
- **Every chunk write carries 8 provenance fields**: `source_id`, `document_id`, `content_hash`,
  `chunk_hash`, `chunk_index`, `chunk_count`, `byte_start`, `byte_end`. Idempotency key =
  `source_id + document_id + content_hash`; chunk identity additionally includes `chunk_hash`.
  **Entity text (e.g. a person name) is NEVER the dedup scope for document chunks.** This contract
  is already realized in `internal/documents/ids.go` (`BuildExtractedDocument` → `Chunk` with
  `SourceID`/`DocumentID`/`ContentHash`/`ChunkHash`/`ChunkIndex`/`ChunkCount` + a `Locator`).
- **PrivateGPT is reference material ONLY.** Borrow lifecycle semantics (async task ID, status
  endpoint, file-hash no-op for same artifact, different-artifact preservation, bounded batches,
  temp cleanup); do NOT adopt its Celery/S3/Qdrant/Python dependency shape.
- **Industrial PDF ingest exposes `searchable` early** — after page-text extraction + sparse/
  page-aware indexing, BEFORE dense embeddings. Do NOT synchronously embed 1000+ chunks before the
  user can search; dense vectors are a background job. Retrieval starts hybrid (sparse first, dense
  rerank when available). Production retrieval must **down-rank table-of-contents hits** or resolve
  them to their actual section pages before showing the final citation.
- **Retrieval acceptance is eventually a LIVE vector/hybrid smoke**: convert → chunk → embed →
  write → retrieve seeded beginning/middle/end facts with citations and record p95. The local
  keyword harnesses (044/045) are signal only, not the acceptance gate.

## How to Build It

### Size tiering + transport (spike 043a, PARTIAL)

The router already exists at `internal/channels/telegram/documents.go`:

```go
const (
    asyncTierMinBytes  = 5 << 20  // 5 MiB
    refuseTierMinBytes = 50 << 20 // 50 MiB
)
switch {
case len(payload) > refuseTierMinBytes:      // >50 MiB → refuse, NO sidecar call
case len(payload) > asyncTierMinBytes:        // 5-50 MiB → async (today: goroutine + WaitGroup)
default:                                       // <=5 MiB → sync inline
}
```

What still needs building before this is a "production large-file lane":
- Replace the in-memory `multipart.NewWriter(&body)` + `fw.Write(payload)` in `postConvert` with a
  streaming/temp-file POST so 5-50 MiB files don't double-resident (payload + multipart body), and
  fix the markitdown FastAPI side (`await file.read()`) to spool to a temp file.
- The async tier is a tracked goroutine drained by `Stop` (goleak-clean) — promote it to a job with
  durable status (see 046 below), because the callback-only path can't survive a handler context
  ending mid-ingest.

### PrivateGPT lifecycle to copy (spike 043b, VALIDATED — reference checkout `D:/tmp/private-gpt` @ `8ac84e3c35ba48447d7b0eb136f5a1369bab7b2d`)

Pin facts to source, not the published docs (the public site still shows older `/v1/ingest/file`
pages). Source-of-record route family + mechanisms:

- Routes under `prefix="/v1/artifacts"`: `/ingest` (sync), `/ingest/async` (returns task ID),
  `/ingest/async/{task_id}` (queryable status).
- Async enqueues `vector_index_task` via `celery_app.send_task` instead of holding the HTTP request
  open. Non-URI async input is first uploaded to a temporary S3 bucket (`upload_file_to_s3` →
  `UriArtifact`) because Celery args can't safely carry large binaries.
- **Idempotency by file hash inside the same artifact**: `MetadataKeys.FILE_HASH` +
  `retrieve_ingested_nodes` → re-ingesting the same file+artifact returns 0 new documents
  (`test_reingest_same_file_and_same_artifact` asserts `len(data) == 0`); the same file under a
  DIFFERENT artifact still ingests (`test_reingest_same_file_and_different_artifact` asserts `== 1`).
- **Bounded batches**: vector insert `insert_batch_size = 512`; async index path is
  `bounded_concurrent_execute(..., concurrency_limit=1)`.
- **Temp cleanup is explicit + tested**: success and non-autoretry failure remove temp input;
  autoretry failures retain it for retry (`cleanup_temporal_files`, `AUTORETRY_EXCEPTIONS`,
  `ProgressStatus`).

Aura's local desktop lane needs the pattern, NOT the stack: a durable ingest job ID, status
polling/event replay, explicit progress/failure states, idempotency by `source_id`/`document_id`/
content hash, bounded insert/chunk batches, and temp-artifact cleanup.

### Provenance-scoped chunk identity (spike 044, VALIDATED — SHIPPED in `internal/documents/ids.go`)

The contract spike's in-memory store proved: same source+document+content → no-op re-ingest;
same content under a different `document_id` → preserved as a distinct artifact; semantically
similar docs mentioning the same entity ("Mario Rossi") stay isolated because the **provenance
key, not the entity name, controls chunk identity**. The realized derivations:

```go
// internal/documents/ids.go
DocumentID = "doc_" + sha256(contentHash + ":" + sourceID)[:32]
ChunkID    = fmt.Sprintf("chunk_%s_%06d", documentID, index)
ChunkHash  = sha256(NormalizeText(text) + "\n" + json(locator))   // text+locator, not entity text
```

`BuildExtractedDocument` stamps every `Chunk` with `SourceID`/`ContentHash`/`ChunkHash`/
`ChunkIndex`/`ChunkCount` + a `Locator` (page/offset abstraction that subsumes the spike's raw
`byte_start`/`byte_end` and is what carries PDF page citations). Idempotency key in the spike store
is `sourceID + "\x00" + documentID + "\x00" + contentHash`; chunk-level dedup adds the chunk hash.
Map this onto the agent-memory sidecar via memory-tool metadata fields or a Phase-15 document-ingest
adapter that writes chunk nodes through the graph surface — keep the provenance-safe dedup proven by
spikes 033/034 (dedup welcome inside one source/document; cross-source semantic collapse is not).

### Retrieval + citation signal (spike 045, PARTIAL)

Synthetic >5 MiB doc, three facts planted at start/middle/end, overlap chunking (`chunkSize = 48<<10`,
`overlap = 768` in the harness). Proven locally: byte offsets + `source_id` + `document_id` are
enough to retrieve and render a citation for all three positions; local keyword retrieval p95
< 25 ms. This is a SIGNAL harness — it does NOT prove Neo4j vector recall, embedding quality, or
end-to-end agent-memory MCP latency. Promote to a live smoke: convert → write chunks with the 044
metadata → embed through the granite sidecar (`:8081`) → retrieve by vector/hybrid → assert
start/middle/end recall + source citation → record p95. `internal/knowledge/smoke_test.go` already
has a usable direct Neo4j/vector smoke shape to adapt; `internal/documents/{graphrag,retrieve,
live_p95}*` are the live homes.

### Telegram ingest-job state machine (spike 046, VALIDATED)

Create the job BEFORE conversion work. Proven state set + transitions (see
`sources/046-telegram-ingest-job-ux/main.go`):

```
accepted → running(progress%) → success            (job ID issued at accept, pollable)
accepted → running            → failure            (durable status, not just a callback)
                                refused             (oversize: immediate, allocates NO job, no ID)
accepted → running            → canceled           (cancel must exist BEFORE memory writes begin)
```

`Submit` refuses `> refuseTierMinBytes` with no ID; converts `<= asyncTierMinBytes` synchronously;
otherwise issues `ingest-%04d` in `accepted`. Terminal states (`success`/`failure`/`refused`/
`canceled`) are sticky. `Status(id)` returns `(*job, ok)` — unknown IDs return `ok=false`
explicitly. Telegram renders this as a short accepted message, optional progress edits/follow-ups,
and a final success/failure message. Start in-process, but shape the public contract so it can move
to persisted jobs later. Cancel/delete semantics matter if a delete request races an ingest job.

### Fast-lane industrial PDF (spike 047, VALIDATED — the chosen first lane)

Born-digital PDFs (selectable text) go through a page-aware **sparse-first** lane, NOT synchronous
dense embedding. Proven pipeline (`sources/047-fast-lane-industrial-pdf-ingest/main.py`):

```python
import fitz                                   # PyMuPDF — page.get_text("text")
from sklearn.feature_extraction.text import TfidfVectorizer
from sklearn.preprocessing import normalize
# chunk per page: max_chars=1800, overlap=180, cut on \n / ". " / "; " past 55% of window
TfidfVectorizer(lowercase=True, ngram_range=(1,2), strip_accents="unicode",
                sublinear_tf=True, max_df=0.92)
sparse = normalize(vectorizer.fit_transform(texts), axis=1)   # cosine via sparse @ qv.T
```

Order of operations: extract page text → chunk by page (carry the page number for citation) → build
TF-IDF sparse index → mark the job `searchable`. Dense embeddings (granite via `aura-llama-embed`)
run AFTER that, in the background, as enrichment. Retrieval is hybrid: sparse answers immediately;
dense reranks when ready. TOC/parameter-list pages surface before richer explanatory sections — they
need down-ranking or section-target resolution before production.

## What to Avoid

- **Do NOT route exactly-5-MiB to async or refuse exactly-50-MiB** (spike 043a landmine). The `>=`
  switch contradicted the `<=5 sync / >50 refuse` comments. Use `>` and add exact-boundary tests.
- **Do NOT call the 5-50 MiB path "memory-bounded" while it buffers the whole multipart body** and
  the sidecar does `await file.read()` (043a). Both sides materialize the payload; for a 49 MiB file
  the harness measured payload + multipart body both resident (>1.70× amplification) before the
  sidecar reads it a third time.
- **Do NOT ship the async tier as a goroutine-only callback** (046). Memory ingest outlives the
  handler context; a fire-and-forget callback gives the user no durable, pollable status and no
  cancel hook.
- **Do NOT import PrivateGPT's stack** (043b). Celery + S3 temp bucket + Qdrant is the right
  *architecture lesson* and the wrong *dependency* for a single-operator desktop lane. Borrow the
  semantics; build them in Go in-process.
- **Do NOT use entity text as a chunk dedup key** (044). Two unrelated documents both naming the
  same person must stay isolated artifacts; provenance (`source_id`+`document_id`+`content_hash`)
  controls identity, never the entity. Upstream agent-memory long-term semantic dedup is NOT
  provenance-safe (session-8 red finding) — Aura must supply exact identity/source/session keys.
- **Do NOT synchronously dense-embed 1000+ chunks before search** (047). The interrupted G220 dense
  E2E that motivated this spike left the embedding sidecar busy and gave the user nothing for a long
  time. Sparse first, dense in the background.
- **Do NOT treat the local keyword harnesses (044/045) as the retrieval acceptance gate** (045 is
  PARTIAL). They prove the chunking/citation contract, not live vector/GraphRAG recall or MCP p95.
- **Do NOT show raw top-1 hits without TOC handling** (047). The score lost most of its points to
  table-of-contents and parameter-list pages ranking above explanatory sections.

## Constraints

- **Thresholds**: `asyncTierMinBytes = 5 << 20` (5 MiB), `refuseTierMinBytes = 50 << 20` (50 MiB).
  Telegram Bot API upload/download cap is 50 MB, which aligns with the refuse ceiling.
- **PrivateGPT reference**: repo `github.com/zylon-ai/private-gpt`, checkout `D:/tmp/private-gpt`,
  commit `8ac84e3c35ba48447d7b0eb136f5a1369bab7b2d`. Route prefix `/v1/artifacts`. Insert batch
  cap 512; async index `concurrency_limit=1`.
- **Spike 047 numbers** (Siemens `G220_op_instr_0824_en-US.pdf`, run 2026-06-12): file 28.97 MiB,
  830 pages (827/830 with text, ~3 low/empty), 1,171,929 chars, 1035 chunks; fast ingest 1.626 s;
  retrieval avg 0.0011 s, p95 0.0017 s; industrial score 90.4/100 ("excellent"). Chunking
  `max_chars=1800`, `overlap=180`. 8 query probes (safety/mechanical/electrical/commissioning-web/
  commissioning-startdrive/diagnostics/technical-data/options); safety, mechanical, options 5/5.
  Verdict thresholds in the harness: VALIDATED needs score ≥75 AND total fast-ingest ≤5 s AND
  p95 ≤0.2 s.
- **Sparse-lane Python deps**: `pymupdf` (fitz), `scikit-learn`, `numpy`. This is the spike harness
  stack; the production lane folds into the Go `internal/documents` pipeline + the markitdown
  sidecar (`docker/markitdown/app.py`, `/convert`, GLM-OCR for images/scanned PDFs per commit
  `f52ba1a3`).
- **Dense embedding sidecar**: granite (`granite-embedding-97m-r2`, 384d) at `:8081` via
  `internal/documents/embedder.go`'s `EmbeddingClient`. Vector store = Neo4j HNSW (cosine, 384d).
  Agent-memory MCP sidecar at `http://127.0.0.1:8091/mcp/` (streamable HTTP, 16 tools, deferred).
- **Local-harness latency facts** (signal only): 045 local keyword p95 < 25 ms on a >5 MiB synthetic
  doc; 044/045 chunking `chunkSize 48-64 KiB`, `overlap 512-768 B`. These are NOT the live numbers.

## Origin

Synthesized from spikes: 043a, 043b, 044, 045, 046, 047 (sessions 11-12, 2026-06-12).
Source files in: `sources/043a-aura-large-doc-markitdown/`,
`sources/043b-privategpt-async-ingest-reference/`, `sources/044-memory-ingest-provenance-dedup/`,
`sources/045-large-doc-retrieval-signal/`, `sources/046-telegram-ingest-job-ux/`,
`sources/047-fast-lane-industrial-pdf-ingest/`.
Verdicts: 043a PARTIAL (async + visible-failure path exist, but 5-50 MiB transport is not
memory-bounded and had exact-boundary drift), 043b VALIDATED (PrivateGPT = reference architecture,
not a dependency), 044 VALIDATED (provenance/dedup contract — shipped in `internal/documents/ids.go`),
045 PARTIAL (local retrieval/citation contract proven; live vector/GraphRAG recall is future work),
046 VALIDATED (ingest-job state machine), 047 VALIDATED (page-aware sparse-first PDF lane; dense
embeddings are background enrichment).
