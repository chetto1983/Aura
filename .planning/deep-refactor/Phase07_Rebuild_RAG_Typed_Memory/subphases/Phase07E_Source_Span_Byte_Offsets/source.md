# Phase07E Source Audit - Source Span And Byte Offsets

Status: closed with local and live verification on 2026-05-17. No independent
verifier/subagent has reviewed this folder in this turn.

## Objective

Phase07E closes the Phase 7 source-citation gap: source retrieval hits carry
`source_id`, page when available, and a stable span/byte reference that the
production `source(action=read,...)` tool can resolve back to the source
artifact Aura actually serves to the agent.

## Source Evidence

| Source | Decision Supported | Adopt | Reject / Avoid | Status |
| --- | --- | --- | --- | --- |
| `D:/Aura/prd.md` memory taxonomy lines 668-720 | `source_corpus` is raw files, OCR/extracts, page spans, and immutable provenance | Treat source bytes/extracted markdown as evidence, not curated wiki truth | Wiki-only citations for raw source claims | read |
| `D:/Aura/prd.md` RAG contract lines 806-890 | Retrieval owns citations and follow-up handles | Source hits need handles the agent can expand | Unbounded source dumps into context | read |
| `D:/Aura/prd.md` Phase 7 lines 1509-1545 | Phase gate requires source/page/span or stable artifact handle | Add executable benchmark around source/page/span resolution | Calling page-only source hits complete | read |
| `D:/Aura/docs/phase07b-current-types-audit-2026-05-16.md` B.2, E.2, G.2.3 | Current source rows have `SourceID` and `Page` but no chunk index or byte offset | Implement the deferred source span/chunk offset slice now | Re-open Phase07B typed registry as the owner | read |
| `D:/Aura/.planning/deep-refactor/Phase07_Rebuild_RAG_Typed_Memory/plan.md` | Parent Phase07 sub-phase map | Phase07E owns source span offsets after Phase07D closure | Editing Phase07F wiki frontmatter in this slice | read |
| `D:/Aura/internal/db/migrations/migrations.go` lines 160-178 | `compact_memory_documents` has `source_id` and `page` but no span columns | Add nullable/defaulted compact source span columns through a new migration and current schema | Separate sidecar span table for this first slice | read |
| `D:/Aura/internal/storage/memoryindex/store.go` lines 38-67 | `Document` carries source/page/freshness fields and `Filter.SourceID` already exists | Extend the existing compact document row with span fields | New retrieval store or new source truth store | read |
| `D:/Aura/internal/storage/memoryindex/rebuild.go` lines 154-185 | `sourcePageDocuments` creates one compact row per page and truncates body to `sourceSnippetLimit` | Stamp page rows with chunk index and source-artifact byte range | Treat compacted/truncated body as the byte source of truth | read |
| `D:/Aura/internal/storage/memoryindex/rebuild.go` lines 195-214 | `splitSourcePages` uses byte indices from the page-heading regex but currently discards them | Preserve trimmed page byte start/end from the source markdown | Character offsets; regex-only page number without span | read |
| `D:/Aura/internal/agent/tools/registry/memory_search.go` lines 461-468 and 693-710 | Search output shows page and handle, but follow-up omits page/span | Surface `span=bytes=start-end` and precise `source(action=read,...)` follow-up | Invented tool names or prose-only offset hints | read |
| `D:/Aura/internal/agent/tools/registry/source_read.go` lines 27-75 | `ReadSourceTool` only accepts `source_id` and `mode` | Add optional byte range arguments with strict validation | Letting the model re-read an arbitrary full source to find a snippet | read |
| `D:/Aura/internal/agent/tools/registry/source.go` lines 64-90 | `readSourceMarkdown` resolves `ocr.md` or original text/url/artifact fallback | Apply byte ranges to the same artifact path source reads already serve | Binary PDF coordinate extraction in this slice | read |
| `D:/Aura/cmd/probe_chat` source helpers | Existing probes can upload/fetch/delete sources and verify raw bytes | Add a Phase07E live probe after local tests pass | Using tool-call count as completion evidence | read |

## Adopted Decisions

- The canonical Phase07E offset is a UTF-8 byte range into the text artifact
  Aura serves through `source(action=read,...)`: `ocr.md` for OCR sources,
  otherwise the readable extracted/original text fallback.
- Page-level source documents remain one compact row per page for this slice.
  `chunk_index` starts at the page row ordinal and keeps room for future
  sub-page chunking without changing the citation contract again.
- Existing `source:<id>#page=N` handles remain valid. Phase07E adds explicit
  span fields and a precise
  `source(action=read,...,byte_start=...,byte_end=...)` follow-up rather than
  breaking old handles.
- Byte offsets are persisted in `compact_memory_documents`, because compact
  source rows already own the searchable source hit identity and existing
  `Filter.SourceID` can retrieve sibling pages for the same source.
- Existing source files, OCR artifacts, wiki pages, Qdrant data, and user data
  are not rewritten by this slice. A rebuild can restamp compact source rows
  from existing source artifacts.

## Rejected Or Deferred Decisions

- PDF coordinate spans and OCR JSON bounding boxes are deferred. Phase07E proves
  stable text-artifact byte spans first.
- Sub-page semantic chunking is deferred unless a page-sized fixture proves the
  page row cannot satisfy the PRD gate. The schema leaves `chunk_index` ready.
- Wiki frontmatter schema, prompt versions, graph edges, community reports, and
  source-to-wiki promotion stay in Phase07F or later Phase 7 graph slices.
- No new vector database, graph database, cache backend, or source sidecar store
  is introduced.

## Closed Gap

Before this slice, source retrieval was joinable but not precisely resolvable.
A hit could say `source_id=src_x` and `page=2`, and it could provide a page
handle, but it could not say which byte range of `ocr.md` or a readable original
produced the hit. Phase07E now persists compact source span fields, renders
span-aware source follow-ups, and validates exact byte-range reads.

## Implementation Evidence

Affected files are bounded:

- `D:/Aura/internal/db/migrations/migrations.go`
- `D:/Aura/internal/db/migrations/migrations_test.go`
- `D:/Aura/internal/storage/memoryindex/store.go`
- `D:/Aura/internal/storage/memoryindex/store_test.go`
- `D:/Aura/internal/storage/memoryindex/rebuild.go`
- `D:/Aura/internal/storage/memoryindex/rebuild_test.go`
- `D:/Aura/internal/agent/tools/registry/memory_search.go`
- `D:/Aura/internal/agent/tools/registry/memory_search_test.go`
- `D:/Aura/internal/agent/tools/registry/source.go`
- `D:/Aura/internal/agent/tools/registry/source_read.go`
- `D:/Aura/internal/agent/tools/registry/source_test.go`
- `D:/Aura/cmd/probe_chat`

Verification is recorded in `benchmark.md`. Local gates and the live
`phase07e-source-span-read` chat probe passed on 2026-05-17.
