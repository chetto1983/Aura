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
