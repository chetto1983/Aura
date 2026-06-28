---
id: 044
title: Memory Ingest Provenance Dedup
verdict: VALIDATED
date: 2026-06-12
related:
  - 033-agent-memory-write-read-ground-truth
  - 034-agent-memory-dedup-chaos
  - 043b-privategpt-async-ingest-reference
---

# 044 - Memory Ingest Provenance Dedup

## Question

Given converted markdown from a large document, what chunk metadata and idempotency contract keeps Aura from cross-document semantic over-merge while still avoiding duplicate re-ingest?

## Harness

Run:

```powershell
go run ./.planning/spikes/044-memory-ingest-provenance-dedup
```

The harness builds a deterministic markdown fixture larger than 5 MiB, chunks it, writes it to an in-memory store using the proposed Phase 15 metadata contract, and then repeats ingestion under same and different provenance keys.

## Contract

Every chunk write should carry:

- `source_id`
- `document_id`
- `content_hash`
- `chunk_hash`
- `chunk_index`
- `chunk_count`
- `byte_start`
- `byte_end`

The idempotency key is `source_id + document_id + content_hash`. Chunk identity additionally includes `chunk_hash`. Entity text such as a person name must never be the dedup scope for document chunks.

## Findings

- Same source, same document, same content becomes a no-op on re-ingest.
- Same content under a different document is preserved as a distinct artifact.
- Semantically similar documents mentioning the same entity remain isolated because the provenance key, not the entity name, controls chunk identity.
- Chunk offsets and counts are sufficient for later citation rendering.

## Recommendation

Map this contract onto the agent-memory sidecar through either the existing memory tool metadata fields or a Phase 15 document-ingest adapter that writes chunk nodes through the graph surface. Keep the provenance-safe dedup behavior proven by spikes 033 and 034: dedup is welcome inside one source/document, but cross-source semantic collapse is not.

## Verdict

VALIDATED as a contract spike. It proves the metadata and idempotency model Aura should use before a live document-ingest adapter is built on top of the memory MCP.
