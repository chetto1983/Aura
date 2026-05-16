# Phase07 Benchmark

Status: parent benchmark contract. Phase07A and Phase07B are closed with their
own executable benchmarks. Phase07C-F remain planned.

| Check | Command / Method | Fixture / Data Source | Expected Ground Truth | Actual Result | Status |
| --- | --- | --- | --- | --- | --- |
| Phase07A compact archive hygiene | `go test ./internal/storage/memoryindex -run "TestArchiveDocument|TestIndexing" -count=1` | Archive turns with user, final assistant, intermediate assistant, and tool-result rows | `conversations` can retain raw tool rows, but compact archive docs exclude raw tool output and loop scaffolding | met via Phase07A benchmark |
| Phase07A recall default hygiene | `go test ./internal/agent/tools/registry -run TestSearchMemoryTool -count=1` | Fake compact search with source, proposal, archive, and tool-output-only archive rows | Default recall does not surface tool-output archive rows; explicit archive scope remains possible and labelled | met via Phase07A benchmark |
| Phase07A SQLite ground-truth probe | SQL-backed fixture after archive append/rebuild | `conversations`, `compact_memory_documents`, `compact_memory_fts` | Unique token present only in a tool result exists in `conversations` and returns zero rows from compact docs/FTS | met via Phase07A benchmark |
| Typed layer registry | Phase07B targeted package tests | Registry fixture for wiki/source/user/archive/operational layers | Collection enum/descriptor registry, back-compat constants, score components, follow-up handles, and SourceID filter are present | met via Phase07B benchmark |
| Hybrid retrieval and RRF | future golden retrieval eval | Golden wiki/source/user-memory fixture | Hybrid result order beats vector-only and keyword-only on expected hits, with score components returned | score components met in Phase07B; broader golden eval planned |
| Freshness visibility | future projection-state tests | Stale Qdrant/FTS/graph projection fixture | Retrieval returns exact/FTS/graph evidence plus stale/degraded warning; stale vector emptiness cannot suppress evidence | not run | planned |
| Delete/rename projection handling | future wiki/source op-aware tests | Page/source rename and delete fixture | Old projection records removed or marked invalid; new citation handles resolve | not run | planned |
| Wiki GraphRAG local entity queries | future graph tests | Wiki pages with known `[[links]]`, `related`, `sources`, typed pages | Bounded neighborhood/path query returns expected typed edges, weights, degree, source overlap, and citations | not run | planned |
| Wiki GraphRAG global sensemaking | future eval/probe | Community fixture with expected bridge/sparse/isolated pages | Community report includes evidence handles and freshness; report is not written to wiki without proposal/write gate | not run | planned |
| Full compile/vet/test | `go build ./...`; `go vet ./...`; `go test ./...` | Whole repo after implementation | Green without weakening tests | met for Phase07A/B story chains; rerun for future sub-phases |

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
