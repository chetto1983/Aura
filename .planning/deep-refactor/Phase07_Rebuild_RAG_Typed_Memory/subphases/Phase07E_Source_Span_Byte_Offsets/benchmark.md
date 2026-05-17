# Phase07E Benchmark - Source Span And Byte Offsets

Status: closed with local and live verification on 2026-05-17. No independent
verifier/subagent has reviewed this folder in this turn.

| Check | Command / Method | Fixture / Data Source | Expected Ground Truth | Actual Result | Status |
| --- | --- | --- | --- | --- | --- |
| Compact schema carries source spans | `go test ./internal/db/migrations -run "TestRunCreatesCurrentSchema|TestRunUpgradesV302SchemaPreservesRowsAndIsIdempotent" -count=1` | Fresh DB and upgraded legacy DB fixtures | `compact_memory_documents` has `chunk_index`, `byte_start`, and `byte_end` with safe defaults; existing rows survive migration | passed 2026-05-17 | passed |
| Compact document span roundtrip | `go test ./internal/storage/memoryindex -run "Test(DocumentRoundTrip|StoreSourceIDFilter|SourcePageDocumentsPreserveByteSpans)" -count=1` | Source `Document` with page, chunk index, and byte range | Upsert/search returns the same `SourceID`, `Page`, `ChunkIndex`, `ByteStart`, and `ByteEnd`; `Filter.SourceID` still returns only sibling source rows | passed 2026-05-17 | passed |
| Rebuild stamps page byte spans | Same memoryindex command | OCR markdown with two `## Page N` sections, leading/trailing whitespace, and unique tokens | Each compact source row carries the expected page number and byte span; slicing the source markdown by the stored range equals the trimmed page body | passed 2026-05-17 | passed |
| Search output cites page/span | `go test ./internal/agent/tools/registry -run "Test(SearchMemoryTool.*Source|ReadSourceTool.*Byte|ReadSourceTool_Modes)" -count=1` plus full registry package gate | Fake compact search result with source page and byte span | `search_memory(scope=sources)` output includes `page=N`, `span=bytes=start-end`, stable `handle=source:<id>#page=N`, and a `source(action=read,...)` follow-up with the same byte range | passed 2026-05-17 | passed |
| Backward-compatible old source hits | Full registry package gate | Source row with `ByteStart=0` and `ByteEnd=0` | Output omits bogus `span=bytes=0-0` and falls back to full-source `source(action=read,source_id=<id>,mode=ocr)` | passed 2026-05-17 | passed |
| Precise source read returns exact bytes | `go test ./internal/agent/tools/registry -run "Test(SearchMemoryTool.*Source|ReadSourceTool.*Byte|ReadSourceTool_Modes)" -count=1` | Temp source store containing `ocr.md` and readable text fallback source | `byte_start`/`byte_end` returns exactly the selected bytes from the same artifact current source reads serve | passed 2026-05-17 | passed |
| Invalid span errors are explicit | Same source tool test command | Negative, reversed, empty, metadata-mode, and out-of-range byte spans | Tool returns explicit validation errors and does not read broad source content as fallback | passed 2026-05-17 | passed |
| Phase07E targeted gate | `go test ./internal/db/migrations ./internal/storage/memoryindex ./internal/agent/tools/registry ./cmd/probe_chat -count=1` | All local Phase07E package fixtures | Local schema, indexing, retrieval, source-read, and probe helper tests pass together | passed 2026-05-17 | passed |
| Full repo gate | `go vet ./...`; `go build ./...`; `go test ./... -count=1` | Whole repository after implementation | Green without weakening tests or modifying tests to hide production defects | passed 2026-05-17 | passed |
| Live source-span chat golden | `docker compose run --rm -v ${PWD}\data:/data test go run ./cmd/probe_chat -case phase07e-source-span-read -url http://aura:8080/api/chat -api http://aura:8080/api -db /data/aura.db -token <seeded-token> -timeout 240 -json` | Disposable source fixture with unique token and known byte span | Probe verifies the assistant/tool path uses source retrieval plus precise `source(action=read,...)`; cited bytes match authoritative source markdown/raw bytes; temporary source rows are cleaned when possible | passed 2026-05-17: `pass=true`, `tool_calls=2`, `llm_calls=3`, reply exactly `SPAN=<target>` | passed |

## Pass/Fail Threshold

Phase07E is closed: every local row above passed and the live golden passed
against source bytes. A passing tool-call count alone was not used as evidence.

## PRD Gate Proven

- Source hits cite source/page/span or stable artifact handle.
- Federated recall keeps layer label and citation handle.
- Chunk-to-parent source expansion is preserved by resolvable follow-up.
- Golden source-answer eval uses authoritative source bytes, not assistant
  prose.
