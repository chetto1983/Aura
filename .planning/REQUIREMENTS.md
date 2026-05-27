# Requirements: Aura v4.1 Codebase Cleanup

**Defined:** 2026-05-27
**Milestone:** v4.1 Codebase Cleanup (Phase-CLEAN)
**Core Value:** The wiki is the graph — write once, retrievable forever, owned by the user. This milestone protects that core by mechanically enforcing the no-debt rules so the substrate doesn't rot.
**Source plan:** `docs/phase-clean-plan-2026-05-27.md` (1031 LOC, locked 2026-05-27)

## v4.1 Requirements

Requirements are 1:1 with the atomic Codex stories in the source plan. REQ-IDs follow the plan's `US-CLEAN-XX` numbering for direct traceability. Each story = one atomic commit per CLAUDE.md §C1 (Wave 5 batched-sweep exception explicitly carved out).

### Wave 0 — Baselines & Guardrails

- [ ] **CLEAN-00**: Capture pre-sweep state in `docs/cleanup-baseline-2026-05-27.json` and wire `golangci-lint` + `dupl` into CI as **warnings** (continue-on-error) so regressions during the sweep are visible without breaking the build.

### Wave 1 — Errcheck Production (50 findings → 0)

- [ ] **CLEAN-01**: `internal/install/download.go:43,192,268` close leaks — wrap deferred `Close()` for the bootstrap downloader (`embeddinggemma`, `whisper.cpp`); silent failures mask corrupted partial downloads.
- [ ] **CLEAN-02**: `internal/dbrecovery/recovery.go:228,235` — Rollback + stmt close; silent rollback failures hide WAL/index corruption incidents.
- [ ] **CLEAN-03**: Query-loop close across 4 files (`internal/conversation/summarizer/proposals.go:209`, `internal/secrets/store.go:100`, `internal/storage/freshness/store.go:201`, `internal/agentnote/store.go:63`).
- [ ] **CLEAN-04**: HTTP/fsnotify body close in 3 files (`internal/llm/openai_stream.go:24`, `internal/storage/sources/ocr/client.go:135`, `internal/mcp/watcher.go:91`).
- [ ] **CLEAN-05**: Logging lifecycle (4 leaks) — `internal/logging/daily_writer.go:58,97`, `internal/logging/zap_slog.go:38,57`. Silent failures here = silent log drops.
- [ ] **CLEAN-06**: File/dir handle leaks (3 files) — `internal/files/xlsx.go:189`, `internal/probe/docinspect/docinspect.go:41`, `internal/workspace/root.go:412`.
- [ ] **CLEAN-07**: Sandbox temp-dir cleanup — `internal/sandbox/process_runner.go:159,165` — silent failure = disk-fill creep.
- [ ] **CLEAN-08**: Skills atomic-write tail — `internal/skills/admin.go:257` best-effort tmp removal after atomic rename.
- [ ] **CLEAN-09**: CLI/probe close leaks bundled — `cmd/build_icon/main.go:118,135`, `cmd/debug_searxng/main.go:100`, `cmd/probe_reasoning/main.go:126`, `cmd/aura/web_chat_helpers.go`.

### Wave 4 — Staticcheck (2 findings → 0)

- [ ] **CLEAN-29**: De Morgan rewrites for QF1001 — `internal/agent/promptplan_test.go:14`, `internal/storage/memoryindex/priority_section_test.go:62`. Trivial mechanical fix.

### Wave 6 — CI Hard-Gate

- [ ] **CLEAN-50**: Promote CI lint + dupl steps from warning → fail-on-delta. New `docs/dupl-baseline.txt` (near-empty for production); `golangci-lint run --new-from-rev=HEAD~1` matches existing depguard pattern.
- [ ] **CLEAN-51**: Make `.golangci.yml` explicit — enable `errcheck`, `staticcheck`, `unused`, `ineffassign`, `govet` alongside the existing `depguard`; remove stale `_archive_phaseG_dead_dispatch` reference.

### Wave 2 — Cross-File Dupl Production (41 clusters → 0)

- [ ] **CLEAN-10**: Wiki 6-way cluster → graph helper. Files: `internal/wiki/{diff,graph,questions,repairs,surprise}.go` + new `internal/wiki/graph_helpers.go`. **Highest single-cluster ROI**; Phase-WIKI-B gate lifted 2026-05-27.
- [ ] **CLEAN-11**: `cmd/debug_docx/main.go` ↔ `cmd/debug_pdf/main.go` writer helpers — 2 clusters folded into new `cmd/internal/debugio/writer.go`.
- [ ] **CLEAN-12**: Install model-fetch helper — `internal/install/embedding.go:45-61` ↔ `internal/install/whisper.go:35-51` → new `internal/install/fetch.go` (`FetchModelArchive(ctx, spec)`).
- [ ] **CLEAN-13**: MCP setup builder helper — `internal/api/mcp_database_setup.go:31-62` ↔ `internal/api/mcp_setup.go:32-63` → shared builder in `internal/api/mcp_setup_common.go`.
- [ ] **CLEAN-14**: Runs store row-builder — `internal/storage/runs/store_event.go:127-137` ↔ `internal/storage/runs/store_run.go:48-59` → `scanRunRow` / `scanEventRow` helpers.
- [ ] **CLEAN-15**: Generic `GetByID` helper (summarizer ↔ cron) — `internal/conversation/summarizer/proposals.go:247-259` ↔ `internal/cron/issues.go:119-131` → new substrate package `internal/storage/sqlitex/getbyid.go` using generics.
- [ ] **CLEAN-16**: memoryindex/search row-builder — `internal/storage/memoryindex/store_helpers.go:215-227` ↔ `internal/storage/search/graph_documents.go:270-282`. Depguard-aware placement.
- [ ] **CLEAN-17**: Search-fusion helper (qdrant ↔ sqlite within `internal/storage/search/`) — easiest extract of Wave 2; new `fusion_helpers.go`.
- [ ] **CLEAN-18**: Generic `UniqueNonEmpty[T]` helper → new substrate package `internal/sliceutil/unique.go`. Folds `direct_fetch.go:uniqueStrings` ↔ `identity/store_helpers.go:uniqueNonEmptyCapabilities`.
- [ ] **CLEAN-19**: Ask-user lifecycle helper — `internal/channels/telegram/ask_user_resume.go:110-124` ↔ `internal/chat/hub_lifecycle.go:213-227`. Helper in `internal/chat/`.
- [ ] **CLEAN-20**: `internal/telegram/documents.go` intra-file 215-245/251-281 dedup + optional split if file exceeds 600 LOC after extraction.
- [ ] **CLEAN-21**: Upload pipeline helper (API ↔ Telegram) — `internal/api/upload.go:292-307` ↔ `internal/telegram/documents.go:457-473`. Both channels feed source ingestion.

### Wave 3 — Intra-File Dupl Production (13 clusters → 0)

- [ ] **CLEAN-22**: `internal/agent/tools/registry/workspace_files.go` — 2 intra-file clusters at `:230-243 ↔ :245-258` and `:260-278 ↔ :280-298`.
- [ ] **CLEAN-23**: `internal/agent/tools/registry/tool_definitions.go:46-84 ↔ 66-104` — overlapping duplicate; schema-builder table folded to a loop. LLM-visible manifest — byte-identical JSON Schema required (verify via probe).
- [ ] **CLEAN-24**: `internal/storage/sources/ingest/extractor.go:434-448 ↔ 449-463` — adjacent 14-LOC duplicate.
- [ ] **CLEAN-25**: `internal/api/setup_locale.go:23-57 ↔ 59-93` — 34-LOC halves folded into a slice + loop.
- [ ] **CLEAN-26**: `internal/agent/tools/attempts/sqlite.go:127-140 ↔ 195-208` — query dedup.
- [ ] **CLEAN-27**: Single-file helpers batch — 5 sub-stories (one commit each): **27a** `internal/wiki/graph_index.go`, **27b** `internal/telegram/setup.go`, **27c** `internal/agent/tools/registry/exec.go`, **27d** `internal/config/runtime_settings.go`, **27e** `internal/api/tool_compaction.go`.
- [ ] **CLEAN-28**: `cmd/debug_telegram/main.go:37-49 ↔ 60-72` — 12-LOC duplicate in a CLI.

### Wave 5 — Test Cleanup (Mainline, post-Wave-6)

- [ ] **CLEAN-30**: 17 × test errcheck batched in one mechanical sweep — covers `_test.go` files across `conversation/summarizer`, `db`, `dbrecovery`, `files`, `llm`, `mcp`, `probe/docinspect`, `skills`, `storage/qdrant`, `testutil`. Batching allowed per rule C1 exception.
- [ ] **CLEAN-31**: `internal/llm/openai_test.go` 3-way cluster → `openaifixture_test.go` helpers.
- [ ] **CLEAN-32**: `internal/telegram/documents_test.go` 5-way + 6-way clusters → `documentsfixture_test.go`.
- [ ] **CLEAN-33**: store_test 4-way (`auth/store_test.go ↔ config/store_test.go ↔ cron/scheduler_test.go ↔ swarm/store_test.go`) → `internal/testutil/storetest/`.
- [ ] **CLEAN-34**: qdrant_test 3-way + compact_qdrant_test 3-way → `qdrantfixture_test.go`.
- [ ] **CLEAN-35**: `files_test.go` 3-way (registry) → fixture helper.
- [ ] **CLEAN-36**: `mcp/client_test.go` 4-way → fixture helper.
- [ ] **CLEAN-37**: `voice_handler_test.go` 4-way → fixture helper.
- [ ] **CLEAN-38**: Remaining ~12 × 2-way test clusters batched per package (allowed exception).
- [ ] **CLEAN-39**: Reserved — fallout / overflow from CLEAN-31..38.
- [ ] **CLEAN-40**: Reserved — fallout / overflow.
- [ ] **CLEAN-41**: Reserved — fallout / overflow.

## Out of Scope

Explicitly excluded from this milestone. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| God-class split for `cmd/probe_chat/cases.go` (1511 LOC) | Grandfathered in `.file-size-baseline.txt:11`; split is a separate refactor, not lint cleanup. CONCERNS C-1 — future milestone. |
| Phase-CONS consolidation | Active in `scripts/ralph/prd.json`, separate phase, no overlap with Phase-CLEAN per source plan §0 #5. |
| SQLite WAL Windows-bind-mount code guard | CONCERNS C-2 (Critical); needs a boot-time journal-mode assertion. Defer to a follow-on hotfix milestone — not a cleanup story. |
| `internal/workspace/root.go` 940 LOC split | Memory note: file is exempt or near-baseline-limit; do not modify `.file-size-baseline.txt` in this milestone. |
| Phase-WIKI-B re-open | Wave A + B + FIX all shipped 2026-05-21/22; gate is lifted. Wiki touches in CLEAN-10 / CLEAN-27a are dupl folds, not feature work. |
| New features (ONB, RAG, MCP-UI, ROUNDUP, DGX bundle) | This is a cleanup milestone; feature wave is the next milestone after v4.1 completes. |

## Traceability

Phase → Wave mapping. Phase numbering follows the locked execution sequence from the source plan (W0 → W1 → W4 → W6 → W2 → W3 → W5).

| Requirement | Phase | Wave | Status |
|-------------|-------|------|--------|
| CLEAN-00 | Phase 1 | W0 | Pending |
| CLEAN-01..09 | Phase 2 | W1 | Pending |
| CLEAN-29 | Phase 3 | W4 | Pending |
| CLEAN-50 | Phase 4 | W6 | Pending |
| CLEAN-51 | Phase 4 | W6 | Pending |
| CLEAN-10..21 | Phase 5 | W2 | Pending |
| CLEAN-22..28 | Phase 6 | W3 | Pending |
| CLEAN-30..41 | Phase 7 | W5 | Pending |

**Coverage:**
- v4.1 requirements: 44 total (33 core W0-W3 + 12 W5 test cleanup, with 3 reserved overflow slots)
- Mapped to phases: 44
- Unmapped: 0 ✓

**Story sequence locked by source plan:** W0 → W1 → W4 → W6 → W2 → W3 → W5. **Do not reorder** — Wave 6 (CI hard-gate) intentionally runs after W4 so dupl/staticcheck cleanup commits are CI-protected; Wave 5 (test cleanup) runs last so fixture extractions are gate-protected from regression.

---
*Requirements defined: 2026-05-27*
*Last updated: 2026-05-27 after Phase-CLEAN milestone bootstrap (rescan-grounded).*
