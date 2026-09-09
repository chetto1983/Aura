# Long document ingestion: ArcadeDB manual

## Scope and baseline

Measured 2026-09-09 against the running Garage, CocoIndex 1.0.20,
iscc-tika 0.6.0, EmbeddingGemma and ArcadeDB stack. Source:
`D:/tmp/arcadedb-docs/ArcadeDB-Manual.pdf`, 42,790,866 bytes, 1,133 pages,
SHA-256 `42d8390351fbeeee7fde53490ae06eef87a127706e17d561ea079e93a0f2bbd7`.
The benchmark uses a disposable identity, bucket and database and the production app.
Local raw evidence lives in `artifacts/long-document-benchmark/`.

The original extraction returns exactly 500,000 characters and Tika metadata
`X-TIKA:EXCEPTION:write_limit_reached=true`. Keeping the object returned by
`Extractor().set_extract_string_max_length(50_000_000)` yields 1,944,898 characters,
processes all 1,133 pages, and removes that truncation flag (6.07 s extraction).
The original code discarded the returned configured extractor; the Python API uses
a builder, as shown in the [upstream example](https://github.com/iscc/iscc-tika#python).

The original production app takes 49.585 s excluding a 1.649 s S3 upload, creates
141 passages, and covers 366,671 / 1,580,286 non-whitespace reference characters
(23.203%). The unchanged reconciliation takes 0.063 s with no extraction or embedding.
Stored offsets, hashes and embedding dimensions are valid for that incomplete prefix.

On the full extracted text the original splitter produces 443 chunks in 27.652 s;
20.137 s is inside the native fixed-width regex splitter. A prototype that first
reuses `RecursiveSplitter` with a smaller measured byte budget produces 445 chunks
in 3.410 s, without invoking the regex fallback. The native public API supports
`chunk_size`, `min_chunk_size`, `chunk_overlap` and exact `TextPosition` offsets.

## Decision before implementation

- Retain the configured Tika instance and reject a reported write-limit truncation.
- Retry oversized pieces through the existing recursive splitter with a measured
  smaller byte budget, preserving overlap and absolute locators. Keep the existing
  window fallback for content without usable boundaries.
- Compare full-document timings after fixing extraction alone and after optimizing
  splitting. Never present the faster partial ingestion as a complete baseline.
- Assert text coverage, vector bounds, source hashes, unchanged reconciliation and
  answer-bearing retrieval evidence before claiming improvement.

These observations do not establish scanned-PDF/OCR quality, mixed-layout fidelity,
universal retrieval accuracy, production concurrency or independent answer quality.
Timing samples are local observations rather than a latency distribution.

## Implemented result

| Variant | Ingest (s) | Extraction (s) | Splitting (s) | Embedding (s) | Passages | Text coverage |
|---|---:|---:|---:|---:|---:|---:|
| Original, truncated | 49.585 | 1.976 | 11.493 | 32.273 | 141 | 23.203% |
| Extraction fixed, original splitter | 68.297 | 5.335 | 22.781 | 34.110 | 443 | 100% |
| Extraction fixed, adaptive splitter | 48.363 | 5.437 | 3.853 | 33.484 | 445 | 100% |
| Experimental 1,024-token target | 50.218 | 5.236 | 4.331 | 31.947 | 891 | 100% |

Equal-coverage ingestion is 29.19% faster (1.41x throughput); the splitting stage is
83.09% faster. Total time includes bootstrap, S3 discovery/read and reconciliation;
upload is measured separately. The difference between total and named stages is
not an isolated database-write measurement. The unchanged optimized pass is 0.043 s
and performs no extraction or embedding.

All 445 stored passages reconstruct their source spans exactly and retain the correct
source hash, passage hash and 768-dimensional vector. Every prefixed embedding input
was independently checked with the running model tokenizer: maximum 2,044 tokens,
zero overflows. Coverage is non-whitespace characters in the complete **Tika text**,
not visual-layout, image, OCR or table-structure fidelity.

Ten source-grounded questions sample sections from supply-chain examples through the
late graph algorithm reference. All ten answer excerpts exist in the new passages;
only two existed in the truncated baseline. Using the production `vector.fuse` RRF
query with groupSize=1 retrieves the exact excerpt at rank one in 6/10 cases. One
additional result explains the same Raft failure behavior in different words, so
literal excerpt matching understates that case. This is not a 10/10 answer-quality
claim: the remaining first-result misses concern ILIKE, MCP graph depth, and the
default vector beam width. The 1,024-token trial still scores 6/10 literally, changes
which cases miss, and costs more records and total time; it was not adopted.

A small 24-passage embedding batch trial also did not establish a substantial win:
single requests took 2.067 s initially / 1.685 s warm, batches of four 1.607 s, and
batches of sixteen 1.633 s. The existing embedding transport was retained.

## Verification and reproduction

- All 130 ingestion tests pass inside the ingestion image, including live ArcadeDB
  integration and the 12-format extraction fixtures.
- Changed production modules cover 131/135 statements (97.04%): chunking 100/103,
  extraction 31/32. Five critical mutation checks were killed by behavioral assertions.
- Regression tests reproduce the 500K truncation before the fix, reject reported
  truncation, and check dense multilingual text, natural boundaries and absolute
  byte/character/line/column locators. Overlap has its own dense-prose case because
  native overlap does not promise to repeat a whole line larger than its budget.
- The benchmark is `scripts/long_document_benchmark.py`, run inside the image with
  a dedicated identity, S3 bucket and ArcadeDB credentials (provisioning pattern in
  `scripts/ingest_reconcile_e2e.sh`). Arguments: PDF path, output directory,
  `--reference` complete extracted text. Default minimum coverage is 100%; use
  `--minimum-coverage 0` only when measuring a knowingly incomplete baseline.
- Use a fresh CocoIndex state and fresh disposable database per initial-run variant;
  the script also measures a second unchanged pass. Keep source bytes and the embedding
  model fixed, and do not run competing benchmark loads during timing comparisons.

Machine-readable measurements and the exact questions are in
[the accompanying JSON](2026-09-09-long-document-ingestion.json).

## Reuse audit: memory and native ArcadeDB

Read the local copies requested by the operator:

- `D:/tmp/arcadedb-src`, revision `ebdf527` (2026-09-03), including
  `SQLFunctionVectorFuse`, `SQLFunctionVectorRerank`, and `SQLFunctionVectorMmr`.
- `D:/tmp/arcadedb-docs`, revision `d75e523` (2026-09-03), especially
  `concepts/vector-search.adoc`, `reference/extended-functions/vector.adoc`, and
  `how-to/data-modeling/full-text-index.adoc` under `src/main/asciidoc`.
- `D:/tmp/aura-memory-fix-verify` and the current `internal/arcadedb/memory_vector.go`,
  plus the memory retrieval and graph validation ledgers.

Memory already uses native fusion followed by cosine reranking, a measured relevance
floor, exact technical-identifier admission and support-aware graph expansion.
Those memory thresholds are not calibrated for document passages. Graph expansion
requires actual supported relationships; indexed passages currently have source IDs
and ordinals, not a document knowledge graph.

The installed `arcadedb-engine-26.9.1-SNAPSHOT.jar` contains the native rerank, MMR,
fusion, dense/sparse retrieval, score-transform and BM25 classes. Read-only SQL probes
confirmed rerank/MMR/fusion execute on the existing 445-passage manual index.

| Native query | Exact excerpt at rank 1 | Within returned top 3 |
|---|---:|---:|
| Current RRF, groupSize=1 | 6/10 | 6/10 |
| RRF, groupSize=3 | 6/10 | 8/10 |
| DBSF, groupSize=3 | 6/10 | 8/10 |
| LINEAR, groupSize=3 | 7/10 | 8/10 |
| Memory-style cosine rerank | 4/10 | 5/10 |
| Cosine rerank + native MMR, lambda=0.8 | 4/10 | 5/10 |

All arms use the same source-grounded ten questions and stored embeddings. Top-three
queries have a larger result/context budget than groupSize=1. The native grouped RRF
probe averaged 18.7 ms once warm; the earlier multi-stage probes ran alongside broader
verification and are not a controlled latency comparison. Literal excerpt matching
still understates equivalent explanations, including the Raft example above.

The direct measured opportunity is retrieving more than one passage inside a known
long document. Blindly applying the memory reranker loses useful exact-term evidence
on this sample. No document ranking policy, graph schema, quantization, relevance
threshold or tuned fusion weights were changed by this ingestion fix. The wider
multi-document retrieval oracle is required before promoting a ranking policy.

## Deployed verification and open answer-quality limit

The rebuilt ingestion image is
`sha256:41e04a4b340574dcfc032e717596ff44b0e92deaeeaa1d12d1968a569c79566c`.
The running sidecar was recreated and is healthy. Its image contract passes; all
130 tests pass again on the rebuilt image (225.67 s under concurrent verification).
Reusing the original truncated run's CocoIndex state with the new image re-extracts
the unchanged manual and completes with zero missing sources. A fresh final pass
also retains all 445 passages and 100% text coverage (51.71 s under concurrent build
and coverage load, excluded from the controlled speed comparison).

The required disposable Go coverage gate passes: 34,870/40,292 owned statements
(86.54%), with all package-local policies passing. No Go production code was changed.

Three guided cases were exercised through the real Runner and `document_search`,
using a temporary overlay of the existing document-agent oracle and a disposable
identity. The configured Ollama route was loaded through `settings.OverlayEnv`.
The WSL attempt could not reach the Windows-only Ollama endpoint; the executed
Windows run maps that endpoint to localhost without changing production settings.

| Agent case | Result |
|---|---|
| PostgreSQL JDBC auto-commit | PASS: correct `conn.setAutoCommit(true)` |
| Graph Analytical View lifecycle | FAIL: answer omits `NOT_BUILT` after five searches |
| Redis default port | PASS: correct `6379` |

This is **2/3, not a passing answer-quality gate**. The omitted state is in the
complete source and indexed passages. The ingestion correction is verified, while
end-to-end answer quality on long documents remains an explicit open limitation.
Raw agent receipts remain in `artifacts/long-document-benchmark/agent-live-windows.log`.
