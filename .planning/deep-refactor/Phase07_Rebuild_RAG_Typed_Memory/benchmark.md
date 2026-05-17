# Phase07 Benchmark

Status: parent benchmark contract. Phase07A through Phase07F are closed with
executable benchmarks.

| Check | Command / Method | Fixture / Data Source | Expected Ground Truth | Actual Result | Status |
| --- | --- | --- | --- | --- | --- |
| Phase07A compact archive hygiene | `go test ./internal/storage/memoryindex -run "TestArchiveDocument|TestIndexing" -count=1` | Archive turns with user, final assistant, intermediate assistant, and tool-result rows | `conversations` can retain raw tool rows, but compact archive docs exclude raw tool output and loop scaffolding | met via Phase07A benchmark |
| Phase07A recall default hygiene | `go test ./internal/agent/tools/registry -run TestSearchMemoryTool -count=1` | Fake compact search with source, proposal, archive, and tool-output-only archive rows | Default recall does not surface tool-output archive rows; explicit archive scope remains possible and labelled | met via Phase07A benchmark |
| Phase07A SQLite ground-truth probe | SQL-backed fixture after archive append/rebuild | `conversations`, `compact_memory_documents`, `compact_memory_fts` | Unique token present only in a tool result exists in `conversations` and returns zero rows from compact docs/FTS | met via Phase07A benchmark |
| Typed layer registry | Phase07B targeted package tests | Registry fixture for wiki/source/user/archive/operational layers | Collection enum/descriptor registry, back-compat constants, score components, follow-up handles, and SourceID filter are present | met via Phase07B benchmark |
| Hybrid retrieval and RRF | future golden retrieval eval | Golden wiki/source/user-memory fixture | Hybrid result order beats vector-only and keyword-only on expected hits, with score components returned | score components met in Phase07B; broader golden eval planned |
| Phase07C projection state | `go test ./internal/storage/freshness ./internal/db/migrations ./cmd/aura -count=1` | Fresh SQLite schema plus app boot seeding fixture | `projection_state` exists, freshness store roundtrips rows, optimistic concurrency rejects stale writes, and boot seeding creates exactly five canonical projection rows | passed 2026-05-16 by Codex |
| Phase07C compact document freshness | `go test ./internal/storage/memoryindex -count=1` | Compact memory docs, archive append, rebuild, and hash-drift fixtures | Compact docs roundtrip `content_hash`, `embedding_model_id`, `index_build_id`; append/rebuild bump/reset pending counters without deleting archive rows | passed 2026-05-16 by Codex |
| Phase07C retrieval freshness annotation | `go test ./internal/agent/tools/registry -count=1` | `search_memory` fixture with fresh, degraded, and empty-default compact docs | Fresh hits emit `freshness=` without `degraded_read`; degraded projection emits `degraded_read=true`; old empty rows omit freshness tokens | passed 2026-05-16 by Codex |
| Phase07D user/operational typed tiers | Phase07D baseline plus `go test ./cmd/aura -run TestRegisterMemoryRecallToolsWiresTypedTiersAndFreshness -count=1`, registry broad-scope tests, and `cmd/probe_chat -case phase07d-mixed-tier-recall` in Compose | Registry, writer, recall, tool-definition, broad-scope, runtime wiring, disposable SQLite freshness, mixed-tier trap fixtures, and live `/api/chat` | First-class collection descriptors, typed active writers, active-only task-level recall, stable handles, filters, pending exclusion, schema-valid examples, broad `search_memory` exclusion, runtime registration, freshness/degraded annotations, no leakage between typed recall and source/archive/proposal search, and live chat tool use through typed recall | closed 2026-05-17 by Codex; live probe `pass=true`, `tool_calls=2` |
| Phase07E source span byte offsets | `go test ./internal/db/migrations ./internal/storage/memoryindex ./internal/agent/tools/registry ./cmd/probe_chat -count=1`; `cmd/probe_chat -case phase07e-source-span-read` in Compose before closure | Compact source docs, OCR/text source artifact fixtures, `search_memory(scope=sources)`, precise `source(action=read,...)`, and live `/api/chat` source-span fixture | Compact rows store `chunk_index`, `byte_start`, and `byte_end`; rebuilt source pages slice back to exact source artifact bytes; search output cites page/span/stable handle; `source(action=read,...)` resolves the span; live probe verifies assistant/tool path against authoritative source bytes | closed 2026-05-17 by Codex; live probe `pass=true`, `tool_calls=2`, `llm_calls=3`; full repo gates passed | passed |
| Phase07F wiki frontmatter trust metadata | `go test ./internal/wiki ./internal/storage/search ./internal/agent/tools/registry ./cmd/probe_chat -count=1`; `go vet ./...`; `go build ./...`; `go test ./... -count=1`; `cmd/probe_chat -case phase07f-wiki-frontmatter-metadata` after Docker rebuild/restart | Temp wiki, SQLite, Qdrant fake, `search_memory` fake, single-page warm-cache reindex fixture, and disposable live wiki page fixture | Wiki page and graph docs carry `schema_version`, `prompt_version`, `created_at`, `unversioned`, and sources; SQLite/Qdrant parse them into `search.Result`; `search_memory(scope=wiki)` exposes compact trust tokens; live wiki writes upsert changed Qdrant page docs despite warm-cache reuse; live `/api/chat` cites an unprompted marker/source plus metadata | closed 2026-05-17 by Codex; live probe `pass=true`, `tool_calls=2`, `llm_calls=3`; full repo gates passed | passed |
| Freshness visibility beyond compact memory | future projection-state tests | Stale Qdrant/FTS/graph projection fixture | Retrieval returns exact/FTS/graph evidence plus stale/degraded warning; stale vector emptiness cannot suppress evidence | not run; deferred beyond Phase07C | planned |
| Delete/rename projection handling | future wiki/source op-aware tests | Page/source rename and delete fixture | Old projection records removed or marked invalid; new citation handles resolve | not run | planned |
| Wiki GraphRAG local entity queries | future graph tests | Wiki pages with known `[[links]]`, `related`, `sources`, typed pages | Bounded neighborhood/path query returns expected typed edges, weights, degree, source overlap, and citations | not run | planned |
| Wiki GraphRAG global sensemaking | future eval/probe | Community fixture with expected bridge/sparse/isolated pages | Community report includes evidence handles and freshness; report is not written to wiki without proposal/write gate | not run | planned |
| Full compile/vet/test | `go build ./...`; `go vet ./...`; `go test ./...` | Whole repo after implementation | Green without weakening tests | passed 2026-05-17 by Codex after Phase07F closure |

## Phase07A SQL Probe Shape

Use this shape in the sub-phase after the fixture writes one unique token that
appears only inside a `role=tool` conversation row:

```sql
SELECT role, content
FROM conversations
WHERE chat_id = ?
ORDER BY turn_index;

SELECT kind, tags_json, body
FROM compact_memory_documents
WHERE kind = 'archive';

SELECT id
FROM compact_memory_fts
WHERE compact_memory_fts MATCH 'unique_tool_output_token';
```

Pass threshold:

- `conversations` still contains the raw tool result row.
- `compact_memory_documents` does not contain the tool-result body.
- `compact_memory_fts` returns zero rows for the tool-only token.
- `search_memory` does not present the tool-only token as default memory.
