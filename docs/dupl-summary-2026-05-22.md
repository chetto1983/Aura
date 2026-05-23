# Dupl Report Summary — 2026-05-22 (post-Wave A)

Generated from `.planning/dupl-report-2026-05-22.txt` after all Wave A file-splits landed.
Command: `~/go/bin/dupl -t 60 internal/ cmd/`

**Total clone groups**: 114 (41 production, 73 test) — **pre-Wave-C baseline**

**Post-Wave-C (DEDUP-00..04)**: 34 production clusters — net reduction **7** vs baseline.

Severity score = `clone_count × line_span`.

---

## Top 20 Production Clusters (non-test files)

| # | Score | Clones | Span | Package(s) | File:lines |
|---|-------|--------|------|------------|-----------|
| 1 | 240 | 2 | 120 | `internal/agent/tools/registry` | files_docx.go:1,120 · files_pdf.go:1,122 |
| 2 | 171 | 3 | 57 | `internal/agent/tools/registry` | files_docx.go:32,88 · files_pdf.go:33,89 · files_xlsx.go:35,95 |
| 3 | 88 | 2 | 44 | `internal/agent/tools/registry` | recall_operational.go:180,223 · recall_user_memory.go:160,203 |
| 4 | 70 | 2 | 35 | `internal/api` | setup_locale.go:23,57 · setup_locale.go:59,93 |
| 5 | 64 | 2 | 32 | `internal/api` | mcp_database_setup.go:31,62 · mcp_setup.go:32,63 |
| 6 | 62 | 2 | 31 | `internal/telegram` | documents.go:215,245 · documents.go:251,281 |
| 7 | 60 | 2 | 30 | `cmd/aura`, `internal/channels/telegram` | web_chat.go:215,244 · invocation_builder.go:548,577 |
| 8 | 58 | 2 | 29 | `cmd/debug_ingest`, `cmd/debug_tools` | main.go:448,476 · main.go:196,225 |
| 9 | 50 | 2 | 25 | `internal/files` | docx.go:87,111 · pdf.go:57,81 |
| 10 | 46 | 2 | 23 | `internal/api` | pending.go:59,81 · pending.go:83,105 |
| 11 | 40 | 2 | 20 | `cmd/probe_chat` | qa_phase_helpers.go:288,307 · qa_phase_helpers.go:326,345 |
| 12 | 38 | 2 | 19 | `internal/agent/tools/registry` | tool_definitions.go:44,62 · tool_definitions.go:64,82 |
| 13 | 36 | 2 | 18 | `internal/wiki` | graph_index.go:79,96 · graph_index.go:355,374 |
| 14 | 36 | 2 | 18 | `internal/storage/search` | qdrant_hybrid.go:104,121 · sqlite.go:196,213 |
| 15 | 36 | 2 | 18 | `internal/api` | conversations.go:87,104 · conversations.go:106,123 |
| 16 | 34 | 2 | 17 | `internal/install` | embedding.go:45,61 · whisper.go:35,51 |
| 17 | 34 | 2 | 17 | `cmd/debug_docx`, `cmd/debug_pdf` | main.go:52,68 · main.go:49,65 |
| 18 | 34 | 2 | 17 | `cmd/aura`, `internal/channels/telegram` | web_chat.go:538,554 · invocation_builder.go:579,595 |
| 19 | 32 | 2 | 16 | `internal/api`, `internal/telegram` | upload.go:285,300 · documents.go:457,473 |
| 20 | 32 | 2 | 16 | `cmd/debug_docx`, `cmd/debug_pdf` | main.go:202,217 · main.go:153,168 |

---

## Clusters by Package

### `internal/agent/tools/registry` (4 clusters, total score 537)

Highest-priority target. Three distinct patterns:

1. **files_docx/pdf whole-file parity** (score 240): `files_docx.go:1,120` vs `files_pdf.go:1,122` — the entire file bodies are structural twins. Targeted by US-MOD-DEDUP-01.
2. **files_docx/pdf/xlsx tool-setup block** (score 171): `files_docx.go:32,88` / `files_pdf.go:33,89` / `files_xlsx.go:35,95` — same 3-file tool-setup registration boilerplate. Targeted by US-MOD-DEDUP-01.
3. **recall_operational vs recall_user_memory** (score 88): `recall_operational.go:180,223` / `recall_user_memory.go:160,203` — search+format loop duplicated across the two recall tools.
4. **tool_definitions.go** (score 38): `tool_definitions.go:44,62` / `tool_definitions.go:64,82` — two adjacent tool-definition blocks with identical structure.

### `internal/api` (4 clusters, total score 216)

1. **setup_locale.go** (score 70): `setup_locale.go:23,57` / `setup_locale.go:59,93` — two locale-setup blocks; extractable to a shared helper.
2. **mcp_database_setup / mcp_setup** (score 64): `mcp_database_setup.go:31,62` / `mcp_setup.go:32,63` — MCP registration boilerplate.
3. **pending.go** (score 46): `pending.go:59,81` / `pending.go:83,105` — pending-user list handler duplicated internally.
4. **conversations.go** (score 36): `conversations.go:87,104` / `conversations.go:106,123` — conversation read pattern.

### `cmd/aura` + `internal/channels/telegram` (2 clusters, total score 94)

Cross-package parity between `web_chat.go` and `invocation_builder.go` — the web-chat bridge and the Telegram invocation builder share ~30-line and ~17-line blocks.

### `internal/telegram` (1 cluster, score 62)

`documents.go:215,245` / `documents.go:251,281` — 31-line document-download handler duplicated internally.

### `internal/files` (1 cluster, score 50)

`docx.go:87,111` / `pdf.go:57,81` — shared 25-line template-init block.

### `internal/storage/search` (1 cluster, score 36)

`qdrant_hybrid.go:104,121` / `sqlite.go:196,213` — 18-line search normalization block.

### `internal/wiki` (1 cluster, score 36)

`graph_index.go:79,96` / `graph_index.go:355,374` — 18-line graph traversal loop.

### `internal/install` (1 cluster, score 34)

`embedding.go:45,61` / `whisper.go:35,51` — 17-line model-download progress block.

### `cmd/debug_*` (3 clusters, total score 124)

Debug harness duplication — lower priority (not production serving path), included for completeness.

### `cmd/probe_chat` (1 cluster, score 40)

`qa_phase_helpers.go:288,307` / `qa_phase_helpers.go:326,345` — test helper duplication. Lower priority.

---

## DEDUP-01..04 Target Mapping

| Story | Target clusters | Expected score reduction |
|-------|----------------|--------------------------|
| DEDUP-01 | registry#1 + registry#2 (files_docx/pdf/xlsx) | ~411 |
| DEDUP-02 | config/store.go GetInt/GetFloat/GetBool (score ~22, not in top 20) | ~22 |
| DEDUP-03 | 3-way agent-job spec (confirmed dissolved by Wave A splits — no jobspec.go needed) | 0 |
| DEDUP-04 | registry#3 + files#1 + api#3 + api#4 + wiki#1 (recall / sanitize-filename / pending / summaries / graph-traversal) | −7 clusters |

**Note (DEDUP-03 — verified 2026-05-23)**: The original 3-way cluster (`swarm/tools.go:516-528`, `cron/agent_job.go:165-177`, `swarm/store.go:436-448`) is **confirmed dissolved** by Wave A splits. Evidence:
- `internal/swarm/store.go` ends at line 435 — the original cluster range 436-448 no longer exists.
- `internal/cron/agent_job.go:165-177` now contains `fallbackAgentJobTools` / `containsString` helpers (different code).
- `internal/agent/tools/swarm/tools.go:516-528` now contains `workerRolesForPolicy` / `defaultDelegationRoles` (different code).
- `dupl -t 60 internal/ cmd/` reports 0 matches involving any of these three files.
- `go test ./internal/swarm/... ./internal/cron/... ./internal/agent/tools/swarm/...` all green.
No `internal/swarm/jobspec.go` is needed — the dedup goal is already achieved.

---

## DEDUP-04 Extractions (2026-05-23)

Five extractions applied; 7 production clusters eliminated (from 41 → 34).

| Extraction | Helper added | Files cleaned | Clusters killed |
|-----------|--------------|---------------|-----------------|
| `persistAndDeliverFile` | `internal/agent/tools/registry/files.go` | files_docx.go, files_pdf.go, files_xlsx.go | 2 (whole-file + 3-way Execute) |
| `formatIndexedDocumentResults` | `internal/agent/tools/registry/memory_search_format.go` | recall_operational.go, recall_user_memory.go | 1 (44-line format loop) |
| `sanitizeFilenameExt` | `internal/files/docx.go` | pdf.go | 1 (25-line sanitize block) |
| `handlePendingAction` | `internal/api/pending.go` | — (same file) | 1 (46-score internal dup) |
| `handleSummaryAction` | `internal/api/summaries.go` | — (same file) | 1 (approve/reject handler dup) |
| `expandSubnodeLinks` | `internal/wiki/graph_index.go` | — (same file) | 1 (18-line BFS subnode expansion) |

**Total clusters eliminated**: 7 (net: 41 → 34)

## DEDUP-04 Skipped Clusters

| Cluster | Reason skipped |
|---------|---------------|
| `setup_locale.go` (score 70) | Two locale translation map literals — cannot extract, data not code |
| `mcp_database_setup.go` / `mcp_setup.go` (score 64) | Different generic type parameters per MCP setup type; no obvious helper signature |
| `exec.go` isNilExecutor / isNilCommandExecutor | Typed nil interface check — cannot safely box into `any` helper without breaking nil equality semantics |
| `tool_definitions.go:44,62` / `tool_definitions.go:64,82` (score 38) | Registering distinct tool structs — structural similarity is unavoidable boilerplate, not extractable logic |
