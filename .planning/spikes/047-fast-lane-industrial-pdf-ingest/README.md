---
spike: 047
name: fast-lane-industrial-pdf-ingest
type: standard
validates: "Given a real 830-page industrial PDF manual, when Aura uses a page-aware sparse ingest lane before dense embeddings, then the document becomes searchable in seconds with page citations and an industrial retrieval score."
verdict: VALIDATED
related:
  - 043a-aura-large-doc-markitdown
  - 044-memory-ingest-provenance-dedup
  - 045-large-doc-retrieval-signal
  - 046-telegram-ingest-job-ux
tags: [large-doc, pdf, ingestion, retrieval, industrial-manual, phase-15]
---

# Spike 047: Fast-Lane Industrial PDF Ingest

## What This Validates

Given the real Siemens `G220_op_instr_0824_en-US.pdf` manual, when Aura extracts page text, chunks by page, and builds a sparse retrieval index before dense embedding, then the manual becomes searchable quickly enough for an accepted/searchable ingest job state.

## Research

Prior spikes 043a-046 established the hard constraints:

- 043a: the current markitdown upload path is not the right synchronous lane for 5-50 MiB files.
- 044: document chunks need provenance metadata and page/offset fields.
- 045: local retrieval with citation metadata is useful but live vector retrieval remains future work.
- 046: long-running ingest must be a job with visible status.

PDF extraction guidance from the `document-pdf` skill says to first distinguish born-digital from scanned PDFs and prefer direct text extraction when selectable text exists. The G220 manual is born-digital: PyMuPDF extracts text from 827/830 pages with about 1.17M characters.

| Approach | Tool/Library | Pros | Cons | Status |
|---|---|---|---|---|
| Direct page text + sparse index | PyMuPDF + scikit-learn TF-IDF | Very fast, page citations, strong for exact industrial terms | Less semantic than dense vectors; needs TOC down-ranking | Chosen |
| Markitdown full conversion first | aura-markitdown | Existing sidecar path | Full multipart/read memory cost; slower and less page-aware | Not first lane |
| Dense embedding all chunks synchronously | aura-llama-embed | Semantic recall later | Too slow for immediate user response on 1000+ chunks | Background only |

Chosen approach: use a fast sparse first lane, then treat dense embeddings as background enrichment.

## How to Run

Default PDF path:

```powershell
go run ./.planning/spikes/047-fast-lane-industrial-pdf-ingest
```

Any PDF:

```powershell
go run ./.planning/spikes/047-fast-lane-industrial-pdf-ingest --pdf "C:\path\manual.pdf"
```

JSON only:

```powershell
go run ./.planning/spikes/047-fast-lane-industrial-pdf-ingest --json
```

## What to Expect

The harness prints:

- file size/pages/chars/chunks,
- extraction/chunk/index timings,
- retrieval average and p95,
- an industrial score out of 100,
- eight query probes across safety, mechanical, electrical, commissioning, diagnostics, technical data, and options.

## Observability

The harness emits category-tagged stdout lines:

- `[FILE]`
- `[INGEST]`
- `[RETRIEVAL]`
- `[SCORE]`
- `[RESULTS]`
- `[SUMMARY]`

`--json` emits the full machine-readable result.

## Investigation Trail

1. A full dense embedding E2E over the 830-page manual was interrupted because it took too long and left the embedding sidecar busy.
2. The embedding sidecar was restarted to clear the abandoned request.
3. A fast lane was tested manually: PyMuPDF extracted 1.17M chars in about 1 second, chunking produced 1035 chunks, and sparse retrieval answered eight industrial queries with p95 near 1 ms.
4. This spike turns that manual result into a reusable GSD harness with the normal `go run` entry point.

## Results

Run on 2026-06-12 against:

`C:\Users\Davide\OneDrive - Sonepar\Documenti\G220_op_instr_0824_en-US.pdf`

Observed:

- File size: 28.97 MiB
- Pages: 830
- Extracted text: 1,171,929 chars
- Chunks: 1035
- Fast ingest: 1.626s
- Retrieval average: 0.0011s
- Retrieval p95: 0.0017s
- Industrial score: 90.4/100, excellent

Eight query probes covered safety, mechanical, electrical, commissioning, diagnostics, technical data, and options. Safety, mechanical, and options scored 5/5; lower scores were mostly caused by table-of-contents or parameter-list pages surfacing before richer explanatory sections.

## Verdict

VALIDATED. The real 830-page industrial PDF is ingestible in seconds if Aura uses a page-aware sparse first lane. The dense vector path should be background enrichment, not the blocking ingest step.

Build signal:

- `searchable` must be an early ingest state after sparse indexing, before dense embeddings finish.
- Dense vectors must be a background job, not a blocking step.
- Retrieval should start hybrid: sparse first, dense rerank when available.
- TOC-heavy pages need down-ranking or section-target resolution before production.
