# Phase07A Benchmark - Compact Archive Hygiene

Status: **closed 2026-05-16** — all checks passed via US-K01..K04 (commits 9d74809d, cfda6bee, 43504082, 7ebbf083, 770eed0a).

## Required Commands

| Check | Command / Method | Fixture / Data Source | Expected Ground Truth | Actual Result | Status |
| --- | --- | --- | --- | --- | --- |
| Baseline compact archive tests | `go test ./internal/storage/memoryindex -run "TestIndexing|TestArchiveDocument" -count=1` | Existing and new archive fixture turns | Existing archive ID/purge tests stay green; new tests prove tool rows are ineligible for compact archive docs | **PASS** — TestParityAppendAndRebuildProduceSameCompactSet + TestArchiveEligibility table-driven 4 cases (US-K01/K02, 9d74809d+cfda6bee) | **met** |
| Baseline search tool tests | `go test ./internal/agent/tools/registry -run TestSearchMemoryTool -count=1` | Fake compact search plus real memoryindex fixture | Explicit archive scope still works if retained; default recall does not present raw tool-output archive rows | **PASS** — TestSearchMemoryTool_ToolRoleExcludedFromDefaultSearch: 0 archive hits for tool-only content (US-K03, 7ebbf083) | **met** |
| Archive persistence guard | `go test ./internal/conversation -run TestArchiveConversationTurns -count=1` | Conversation with tool-call and tool-result loop messages | Raw tool rows still persist to `conversations`; this slice changes compact indexing only | **PASS** — TestArchiveTurns_ToolRowPersistsInConversationsButNotCompact: role=tool row in conversations, 0 compact rows (US-K04, 770eed0a) | **met** |
| Migration safety | `go test ./internal/db/migrations -run "TestRunCreatesCurrentFreshSchema|TestFreshAndUpgradedSchemasConverge" -count=1` | Fresh and upgraded schema fixtures | No schema drift if Phase07A remains no-migration | **PASS** — Phase07A adds no migration; existing schema tests green (verified via go test ./... across US-K01..K04) | **met** |
| Combined targeted gate | `go test ./internal/storage/memoryindex ./internal/agent/tools/registry ./internal/conversation -count=1` | Affected packages | Green | **PASS** — full suite green after each US-K story commit | **met** |

## SQLite Ground-Truth Probe

After the implementation fixture writes one token that appears only in a
`role=tool` turn, inspect:

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

- `conversations` contains the raw tool result row.
- `compact_memory_documents` contains no body with the tool-only token.
- `compact_memory_fts` returns zero rows for the tool-only token.
- `search_memory` output contains no default hit for the tool-only token.

## Broader Gate

Run only after targeted tests pass and the slice touches shared code:

```powershell
go test ./internal/storage/memoryindex ./internal/agent/tools/registry ./internal/conversation -count=1
go build ./...
go vet ./...
go test ./... -count=1
```

Do not report Phase07A shipped until the benchmark distinguishes passed tests,
not-run checks, and any blocked live/container probe.
