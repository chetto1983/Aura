# Phase07E Progress - Source Span And Byte Offsets

Status: closed with local and live verification on 2026-05-17.

| Date | Actor | Change | Verification | Blockers | Deviations From Plan |
| --- | --- | --- | --- | --- | --- |
| 2026-05-17 | Codex | Created Phase07E planning folder, mapped the current source/page-only gap, selected compact source row byte spans as the first vertical implementation slice, and defined benchmark rows for schema, rebuild, retrieval output, precise `read_source`, full repo gates, and a live `cmd/probe_chat` golden. | Read-only source inspection of PRD, Phase07 parent files, Phase07B audit, `memoryindex`, migrations, `search_memory`, `read_source`, source helpers, and probe helpers. No production code changed yet. | No independent verifier/subagent review in this turn. Benchmarks not run because implementation has not started. | Page-level rows remain the first slice; sub-page semantic chunking and PDF OCR coordinate spans are deferred. |
| 2026-05-17 | Codex | **Phase07E CLOSED** - added compact source span columns, stamped source page rows with UTF-8 byte offsets, rendered span-aware `search_memory` source output, exposed byte-range reads through the production `source(action=read,...)` tool, and added a live `phase07e-source-span-read` probe. | Targeted gates passed; `go test ./internal/db/migrations ./internal/storage/memoryindex ./internal/agent/tools/registry ./cmd/probe_chat -count=1`; `go vet ./...`; `go build ./...`; `go test ./... -count=1`; live Compose probe `phase07e-source-span-read` passed with `pass=true`, `tool_calls=2`, `llm_calls=3`, exact `SPAN=<target>` reply. | No independent verifier/subagent review in this turn. | Production follow-up uses the registered `source(action=read,...)` tool instead of the historical `read_source` name. PDF OCR coordinate spans and sub-page semantic chunking remain deferred. |

## Next Action

Move to Phase07F Wiki Frontmatter Schema And Prompt-Version Promotion after
promoting its source-backed sub-phase folder.
