# Phase07E Plan - Source Span And Byte Offsets

Status: implemented and closed with local/live verification on 2026-05-17.
Self-audited; not independently verified in this turn.

## Goal

Source retrieval hits preserve enough provenance for the agent to re-read the
exact source text artifact span behind a hit. A source answer should cite
`source_id`, page, and byte span or an equivalent stable artifact handle, and
the follow-up tool must resolve that span against durable source bytes.

## Boundary

Phase07E owns source hit span metadata, compact source row persistence,
`search_memory` source output, and `source(action=read,...)` byte-range
expansion.

Phase07E does not own wiki frontmatter promotion, GraphRAG edge weighting,
community reports, OCR bounding boxes, source-to-wiki write policy, or source
deletion/rename invalidation beyond preserving existing behavior.

## Work Slices

### 1. Compact Source Span Schema

Files:

- `internal/db/migrations/migrations.go`
- `internal/db/migrations/migrations_test.go`
- `internal/storage/memoryindex/store.go`
- `internal/storage/memoryindex/store_test.go`

Changes:

- Add compact document fields `chunk_index`, `byte_start`, and `byte_end` with
  `INTEGER NOT NULL DEFAULT 0`.
- Add these fields to the current schema, the old compact-memory migration
  schema, and a new additive migration for existing databases.
- Roundtrip the fields through `Document`, `Upsert`, `ReplaceKind`, searches,
  and scans.

Verification:

- `go test ./internal/db/migrations -run "TestRunCreatesCurrentSchema|TestRunUpgradesV302SchemaPreservesRowsAndIsIdempotent" -count=1`
- `go test ./internal/storage/memoryindex -run "Test(DocumentRoundTrip|StoreSourceIDFilter)" -count=1`

### 2. Source Page Span Extraction

Files:

- `internal/storage/memoryindex/rebuild.go`
- `internal/storage/memoryindex/rebuild_test.go`

Changes:

- Preserve byte offsets from `splitSourcePages` using the regex byte indices
  already returned by Go.
- Trim leading/trailing whitespace while keeping exact UTF-8 byte boundaries.
- Stamp each source page row with `chunk_index`, `byte_start`, and `byte_end`.
- Keep existing page handles and source IDs stable.

Verification:

- A fixture with multi-page OCR markdown proves each page row has the expected
  page number and byte span, and slicing the original markdown at that range
  returns the expected page body.
- `go test ./internal/storage/memoryindex -run "TestSource.*Page.*Span|Test.*Rebuild.*Source" -count=1`

### 3. Retrieval Output And Follow-Up Handle

Files:

- `internal/agent/tools/registry/memory_search.go`
- `internal/agent/tools/registry/memory_search_test.go`

Changes:

- Carry source span fields from `memoryindex.Document` into `memoryResult`.
- Render source hits with `page=N`, `span=bytes=start-end`, and the existing
  stable `handle=source:<id>#page=N` when available.
- Change source follow-up output to
  `source(action=read,source_id=<id>,mode=ocr,byte_start=<start>,byte_end=<end>)`
  when a span exists, falling back to the existing full-source source read call
  for old rows.

Verification:

- `search_memory` source tests prove source output includes page, span, handle,
  score components/freshness if present, and a resolvable follow-up.
- Old rows with zero spans remain readable and do not emit bogus spans.
- `go test ./internal/agent/tools/registry -run "TestSearchMemoryTool.*Source|Test.*FollowUp" -count=1`

### 4. Precise Source Reads

Files:

- `internal/agent/tools/registry/source_read.go`
- `internal/agent/tools/registry/source.go`
- `internal/agent/tools/registry/source_test.go`

Changes:

- Add optional `byte_start` and `byte_end` parameters to `ReadSourceTool` and
  expose them through the production `source(action=read,...)` tool schema.
- Validate `0 <= byte_start < byte_end <= len(artifact_bytes)` when a span is
  requested.
- Apply the byte range to the same text artifact selected by current
  `read_source` behavior, then apply the existing output cap.
- Keep `metadata`, full `ocr`, and default `excerpt` modes backward compatible.

Verification:

- Tests prove source reads return exactly the requested bytes for `ocr.md` and
  readable text fallback sources.
- Tests reject negative, reversed, and out-of-range byte spans with explicit
  errors.
- `go test ./internal/agent/tools/registry -run "TestReadSourceTool" -count=1`

### 5. Ground-Truth Probe And Closure

Files:

- `cmd/probe_chat/cases.go`
- `cmd/probe_chat/phase07e.go` or equivalent small probe file
- `cmd/probe_chat` tests if helper behavior changes
- Parent Phase07 progress/benchmark files

Changes:

- Add a live Phase07E probe that creates or uploads a unique source fixture,
  forces/observes compact source indexing, asks the agent to retrieve the source
  hit and use the precise `source(action=read,...)` follow-up, and verifies the
  cited bytes against the authoritative source artifact.
- Cleanup temporary fixture rows/sources after the probe when possible.

Verification:

- Local targeted package tests.
- `go vet ./...`
- `go build ./...`
- `go test ./... -count=1`
- Live Compose probe:
  `docker compose run --rm -v ${PWD}\data:/data test go run ./cmd/probe_chat -case phase07e-source-span-read -url http://aura:8080/api/chat -api http://aura:8080/api -db /data/aura.db -token <seeded-token> -timeout 240 -json`

## Expected Final State

- `compact_memory_documents` stores span fields for source rows.
- Rebuilt source compact rows can be sliced back to the exact source artifact
  range that produced the hit.
- `search_memory(scope=sources)` exposes stable page/span provenance.
- `source(action=read,...)` can resolve byte spans without making the model
  manually scan a full source body.
- The closure report distinguishes local tests, full repo gates, live probe
  status, and any blocked live dependency.

## Risks

- Existing databases need additive migration only; no source data is rewritten.
- UTF-8 byte spans must not split runes when Aura creates them. User-provided
  byte ranges may still be invalid and must fail clearly.
- `ocr.md` headings are stable enough for page spans, but future extractor
  variants may need a second span parser for `extract.md`.
