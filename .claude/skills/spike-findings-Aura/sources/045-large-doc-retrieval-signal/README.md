---
id: 045
title: Large-Document Retrieval Signal
verdict: PARTIAL
date: 2026-06-12
related:
  - 044-memory-ingest-provenance-dedup
  - internal/knowledge/smoke_test.go
---

# 045 - Large-Document Retrieval Signal

## Question

Given a large ingested document, can Aura recover facts from the beginning, middle, and end with source/citation metadata?

## Harness

Run:

```powershell
go run ./.planning/spikes/045-large-doc-retrieval-signal
```

The harness creates a synthetic markdown document larger than 5 MiB, plants three unique facts at the beginning, middle, and end, chunks with overlap, and runs a simple local retrieval scorer. It records top chunk, byte range, and p95 latency.

## Findings

- The chunking contract from spike 044 is sufficient to retrieve and cite start, middle, and end facts in a large document.
- Byte offsets plus `source_id` and `document_id` are enough to render a citation after retrieval.
- Local keyword retrieval is fast, but it is only a signal harness. It does not prove Neo4j vector recall, embedding quality, or end-to-end agent memory MCP latency.

## Recommendation

Promote this into a live smoke after the document-ingest adapter exists:

- convert document to markdown,
- write chunks with spike 044 metadata,
- embed through the configured embedding sidecar,
- retrieve by vector or hybrid query,
- assert start/middle/end recall and source citation,
- record p95.

`internal/knowledge/smoke_test.go` already has a useful direct Neo4j/vector smoke shape that can be adapted.

## Verdict

PARTIAL. The retrieval and citation contract is validated locally, but live vector/GraphRAG recall remains future work.
