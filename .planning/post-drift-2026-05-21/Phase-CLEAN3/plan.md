# Phase-CLEAN3 — Codebase Audit Follow-Through

**Status:** ⚪ absorbed into Phase-MODERNIZE and cleanup-on-touch; do not execute as-is
**Provenance:** codebase-cleanup-audit scout #6 (full deliverable `docs/research-2026-05-21/codebase-cleanup-audit.md`)
**Estimated effort:** ~2 sessions
**LOC delta:** -700

---

## Why this phase

Cleanup audit identified 101 dupl clusters, 12 god-files >600 LOC, 50 errcheck items (most noise but 2 real bugs land in Phase-BUG), 1 unused symbol (lands in Phase-BUG), 1 broken boundary (`logging→api`, lands in Phase-BUG).

The REMAINING cleanup (~30 picks): production-code dupl folds, god-file splits, stale-comment fictions, dupl-cluster folds. Cumulative: -700 LOC + 0 god-files + 0 production dupl clusters.

Lands LAST in the post-DRIFT sequence per ANALYSIS-DEEP.md §7 — cleanup is most valuable AFTER the substrate refactors (Phase-OUT, Phase-CONS, Phase-TOOL, Phase-CTX, Phase-STREAM) settle. Doing it earlier would mean re-cleaning the same files when those phases touch them.

---

## Stories

### US-CLEAN3-01 — Generic `mcpSetupHandler[T]` fold

- **Scope:** Extract `mcp_database_setup.go` + `mcp_setup.go` (mail) into generic `mcpSetupHandler[T]`. 8 endpoints × 2 templates, 100% structural clone. Blocks Phase-MCP-UI roundup.
- **Files:** NEW or refactor [internal/api/mcp_setup_handler.go](internal/api/mcp_setup_handler.go); MODIFY [internal/api/mcp_database_setup.go](internal/api/mcp_database_setup.go), [internal/api/mcp_setup.go](internal/api/mcp_setup.go) (collapse onto generic).
- **LOC delta:** -180 to -250.
- **Acceptance:** `go test ./internal/api/...` green. dupl -t 60 reports 0 clusters in mcp_*_setup.

### US-CLEAN3-02 — `files_docx + files_pdf + files_xlsx` parameterised registrar

- **Scope:** Collapse three 90%-identical files (~190 LOC each) into ONE parameterised `fileTool[Spec]` with `Builder` interface. Largest file-level clone in repo.
- **Files:** NEW [internal/agent/tools/registry/files_generic.go](internal/agent/tools/registry/files_generic.go); MODIFY/DELETE [internal/agent/tools/registry/files_docx.go](internal/agent/tools/registry/files_docx.go), [files_pdf.go](internal/agent/tools/registry/files_pdf.go), [files_xlsx.go](internal/agent/tools/registry/files_xlsx.go).
- **LOC delta:** -300 to -380.
- **Acceptance:** `go test ./internal/agent/tools/registry/...` green. dupl -t 60 reports 0 file-level clones for files_*. All 3 tools produce identical output to pre-refactor.

### US-CLEAN3-03 — Split `migrations.go` (1431 LOC → 24 per-migration files)

- **Scope:** Move each `addXxx` migration func into its own file `internal/db/migrations/m20XX_*.go`. Runner + registry stay in `migrations.go` (~250 LOC).
- **Files:** SPLIT [internal/db/migrations/migrations.go](internal/db/migrations/migrations.go) into 24 files + slim runner.
- **LOC delta:** 0 net, 1 god-file → 24 files ≤80 LOC each.
- **Acceptance:**
  - `go test ./internal/db/...` green. Migration order preserved.
  - Each migration file ≤80 LOC.
  - Runner file ≤300 LOC.

### US-CLEAN3-04 — Split `cmd/probe_chat/cases.go` (1587 LOC → 4-5 files)

- **Scope:** Mechanical move of flat slice literal into `cases_static.go`, `cases_swarm_web.go`, `cases_voice.go`, `cases_helpers.go`. No behavior change.
- **Files:** SPLIT [cmd/probe_chat/cases.go](cmd/probe_chat/cases.go).
- **LOC delta:** 0 net, 4-5 files ≤500 LOC.
- **Acceptance:** `go test ./cmd/probe_chat/...` green.

### US-CLEAN3-05 — Split `internal/storage/memoryindex/store.go` (1143 LOC → 4 files)

- **Scope:** Split along existing seams: `store_core.go` (CRUD), `store_search.go` (ftsSearch + exactSearch + RRF merge), `store_archive.go` (purge + decay + pin), `store_helpers.go` (utility funcs lines 1050+).
- **Files:** SPLIT [internal/storage/memoryindex/store.go](internal/storage/memoryindex/store.go).
- **LOC delta:** 0 net, 4 files ≤350 LOC.
- **Acceptance:** `go test ./internal/storage/memoryindex/...` green. All package-private bindings preserved.

### US-CLEAN3-06 — Split `internal/api/types.go` (771 LOC → 6 domain files)

- **Scope:** Pure type-decl rebalancing into `types_health.go`, `types_wiki.go`, `types_sources.go`, `types_mcp.go`, `types_mail.go`, `types_skills.go`. Zero behavior risk.
- **Files:** SPLIT [internal/api/types.go](internal/api/types.go).
- **LOC delta:** 0 net, 6 files ≤180 LOC.
- **Acceptance:** `go build ./...` clean.

### US-CLEAN3-07 — Promote `IsNilOrZero` + `safeName` + dup helpers

- **Scope:** Promote 4 repeated helpers to shared packages:
  - `IsNilOrZero(any) bool` → new `internal/reflectutil` (replaces `isNilExecutor` + `isNilCommandExecutor` + `identity/store_helpers.go:270`).
  - `safeName` filename sanitisation → `internal/storage/sources/name.go` (replaces `api/upload.go:285` + `telegram/documents.go:457`).
  - Setup locale it/en pair → table-driven matcher.
  - `applySettingTransform(key, value, normalizer)` for `config/store.go` + `config/runtime_settings.go` 2× pairs.
- **Files:** NEW [internal/reflectutil/](internal/reflectutil/); NEW [internal/storage/sources/name.go](internal/storage/sources/name.go); MODIFY all consumers.
- **LOC delta:** -130.
- **Acceptance:** `go test ./...` green. dupl -t 60 reports 0 clusters for the folded helpers.

### US-CLEAN3-08 — Fold remaining production-code dupl clusters

- **Scope:** Cluster fold the ~10 production-code patterns from cleanup audit §3.1:
  - `wiki/graph_index.go` adjacency-set helpers → promote to method-on-graph.
  - `storage/memoryindex/store.go` 4-way "fetch operational rows then map → Document" → `scanOperationalRows(rows)`.
  - `storage/runs/store.go` `GetRun`/`GetEvent` boilerplate → generic `getOne[T]`.
  - `storage/search/search.go` 4-way scan-rows-into-Result → `scanResults(rows, mapper)`.
  - `agent/tools/registry/tool_definitions.go` 4-way definition skeletons → table-driven init slice.
  - `telegram/documents.go` PDF-vs-other delivery flow → `deliverDocument(ctx, kind)`.
  - `telegram/documents.go` + `voice_handler.go` post-delivery cleanup → shared helper.
  - `telegram/setup.go` command-menu registration → loop over `[]commandSpec`.
  - `attempts/sqlite.go` 2× scan helpers → `scanAttemptRow`.
  - `chat/hub_lifecycle.go` + `channels/telegram/ask_user_resume.go` resume-from-pending guard → helper in `chat/`.
- **Files:** MODIFY ~12 files; NEW small helpers in each owner package.
- **LOC delta:** -350.
- **Acceptance:**
  - `go test ./...` green.
  - `dupl -t 60 internal/` reports zero production-code clusters in touched areas.

### US-CLEAN3-09 — Strip stale comment fictions

- **Scope:**
  - `internal/agent/pool.go:14-26` + `internal/agent/promptplan.go:32` — tell the reader `tool_search` is the only always-on tool; in current build it's one of many. Delete or rewrite to reflect always-on tool set.
  - Delete two stale `release_config_test.go` skips for goreleaser/desktop workflow files.
  - Audit `internal/agent/README.md` + `internal/agent/tools/registry/README.md` for fiction.
- **Files:** MODIFY [internal/agent/pool.go](internal/agent/pool.go), [internal/agent/promptplan.go](internal/agent/promptplan.go), [internal/agent/release_config_test.go](internal/agent/release_config_test.go), agent READMEs.
- **LOC delta:** -40.
- **Acceptance:** Comments accurate to current behavior. Tests green.

### US-CLEAN3-10 — Sleep-based test cleanup

- **Scope:** Replace flake-candidates with channel-based completion signals:
  - `internal/chat/hub_swarm_test.go:108,236,250` — 100ms+5ms+300ms sleeps.
  - `internal/agent/tools/index/reconciler_test.go:442` — 200ms fsnotify wait.
- **Files:** MODIFY 2 test files.
- **LOC delta:** +20.
- **Acceptance:** Same tests green under `-race` on slow CI.

---

## Sequencing

US-CLEAN3-01 → 02 → 03 → 04 → 05 → 06 (mostly independent, sequence by file-count impact) → 07 → 08 → 09 → 10.

**One story = one commit** (US-CLEAN3-03 with 24 new migration files is ONE commit — it's mechanical move, not behavior change).

---

## Risks

- **R1 (US-CLEAN3-03)**: migration file split must preserve order. Mitigation: registry slice in `migrations.go` keeps the order; individual files are by-name.
- **R2 (US-CLEAN3-02)**: `files_docx/pdf/xlsx` collapse may break golden-test fixtures. Mitigation: artifact-level tests (per `feedback_inspect_artifact_visually_not_just_pass_status`) — open the produced file, assert structure.
- **R3 (US-CLEAN3-05)**: package-private bindings between memoryindex helpers — moving may surface hidden coupling. Mitigation: `golangci-lint run ./internal/storage/memoryindex/...` clean.
- **R4 (US-CLEAN3-09)**: README rot — comments may reflect older architecture by a phase or two. Mitigation: read CLAUDE.md + prd.md before rewriting.

---

## Verification

- `go test ./...` green.
- `~/go/bin/golangci-lint run ./...` clean.
- `~/go/bin/dupl -t 60 ./internal/...` 0 production-code file-level clones in touched files.
- Every touched file ≤600 LOC.
- Total LOC delta: -700 verified by `wc -l` before/after.

---

*Updated 2026-05-21. This is the FINAL phase in the post-DRIFT sequence — closes the cleanup the substrate refactors couldn't address inline.*
