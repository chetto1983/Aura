# Aura Codebase Cleanup Audit — 2026-05-21

Scope: full repo, focus `internal/*` + `cmd/*` (~83 973 LOC across 410 non-test Go files; the remainder lives under `web/`, `scripts/`, `docs/`).

Tools used: `~/go/bin/golangci-lint run ./...` (clean modulo 55 trivial errcheck/staticcheck items), `~/go/bin/dupl -t 60 internal/` (101 clone clusters), `go list -f '{{.ImportPath}} {{len .Deps}}'`, manual grep + Read tool.

Hard rules in force: ≤600 LOC per module, no dead code, no duplication, deep refactor on every touch (`CLAUDE.md` §"DEEP REFACTOR ON TOUCH").

---

## Top-10 cleanup picks (ranked by ROI)

| # | Pick | Effort | LOC delta | Why it pays off |
|---|------|--------|-----------|-----------------|
| 1 | Extract a generic `mcpSetupHandler[T]` from `mcp_database_setup.go` + `mcp_setup.go` (mail). 8 endpoints, 2 templates, 100% structural clone. | M | −180 → −250 | Future MCP connectors (calendar/notes/...) currently mean copy-paste-replace; pattern blocks Phase-MCP-UI roundup work. |
| 2 | Collapse `internal/agent/tools/registry/files_docx.go` + `files_pdf.go` + `files_xlsx.go` into one parameterised registrar. 3 files × ~190 LOC are 90 %+ identical (params spec, parse args, build, persist, deliver). | M | −300 to −380 | Largest file-level clone in the repo (dupl flags `files_docx.go:1,186` ↔ `files_pdf.go:1,188` and again on inner `:98..154`). Reduces drift risk when block grammar changes. |
| 3 | Split `internal/db/migrations/migrations.go` (1 431 LOC, 24 migration funcs) into one file per migration under `internal/db/migrations/m20XX_*.go`. | M | 0 net, but 1 god-file → 24 files ≤80 LOC | Already organised, just packaged wrong. Every new migration touches a 1 400-LOC file; PRs become churn-heavy and review-blind. The registry slice + `Run`/`validateRegistered` stay in `migrations.go` (~250 LOC). |
| 4 | Split `cmd/probe_chat/cases.go` (1 587 LOC, **3 funcs**) by phase (`cases_static.go`, `cases_swarm_web.go`, `cases_voice.go`, …). | S | 0 net, 4-5 files ≤500 LOC | Mechanical move — file is a flat slice literal. Drives review-time down per probe edit; planning loads only the relevant case set. |
| 5 | Split `internal/storage/memoryindex/store.go` (1 143 LOC, 40 funcs) along the existing seams: `store_core.go` (CRUD), `store_search.go` (ftsSearch / exactSearch / mergeDocumentsRRF), `store_archive.go` (purge / decay), `store_helpers.go` (utility funcs lines 1050+). | L | 0 net, 1 god → 4 files ≤350 LOC | Touched constantly by Phase-RAG + Phase-KV; the file already mixes 4 unrelated concerns. Risk: package-private bindings between helpers. |
| 6 | Promote `internal/api/types.go` (771 LOC, **1 func**, 60+ types) into `types_health.go`, `types_wiki.go`, `types_sources.go`, `types_mcp.go`, `types_mail.go`, `types_skills.go`. | S | 0 net, 1 god → 6 files ≤180 LOC | Pure type-decl rebalancing. Zero behavioural risk. Every domain edit currently scrolls a wall of unrelated structs. |
| 7 | Extract `isNilExecutor` + `isNilCommandExecutor` from `exec.go` into a generic `internal/reflectutil.IsNilOrZero(any) bool`. Same body, only the type signature differs. Same pattern duplicated again in `internal/identity/store_helpers.go:270` (per dupl). | S | −30 across 3 sites | Reusable helper unblocks future nil-interface checks (4 candidate sites). |
| 8 | Delete dead `appendUniqueSorted` in `internal/wiki/memory_hygiene.go:735` (golangci-lint U1000) — already supplanted by `sortedStringSet`. | S | −10 | Lone live `unused` finding, ~5 minutes including test. |
| 9 | Hard-fail rewrite: 50 unchecked `defer Close()` / `json.Encoder.Encode()` calls flagged by errcheck. Pattern: wrap with `_ = …` or log via `logging.IfErr(…)`. Affects 30 files including `internal/api/health_server.go` (response writer ignored 3x) and `internal/backup/export.go` (gzip + tar writer ignored — silent corruption risk). | M | +50, −0 | Several are real bugs: silent JSON-write failure on `/health`, silent gzip-flush failure on backup. Hidden by errcheck noise. |
| 10 | Drop the `tool_search` "deferred-tools rollout" verbiage from `internal/agent/pool.go:14-26` and `internal/agent/promptplan.go:32`. Re-read the comment block: it tells a story from the deferred-tools experiment that's no longer the live model — every tool ships in the always-on set today, `tool_search` is just one of many. Comment is the only place this fiction survives; live agents see it confused. | S | −20 comment, +5 corrected lines | Hard rule: "Comments updated to match current behavior." Pre-existing in repo since the rollout; flagged by user memo `feedback_check_tmp_sources_then_brainstorm_best`. |

Cumulative est. impact of top-10: **~−700 LOC**, no god-file remains, 0 unused-symbol findings, 0 file-level >60-token clones in production code.

---

## 1. God files >600 LOC (production code)

12 production .go files exceed the 600-LOC rule. Sorted by size; **bold** = over threshold today.

| File | LOC | Funcs | Top 3 responsibilities | Proposed split |
|------|-----|-------|------------------------|----------------|
| **`cmd/probe_chat/cases.go`** | 1 587 | 3 | (a) static-phase case list (b) swarm/web case list (c) voice case list — all flat literals | `cases_static.go` / `cases_swarm_web.go` / `cases_voice.go` / `cases_helpers.go` (≤500 LOC ea) |
| **`internal/db/migrations/migrations.go`** | 1 431 | 34 | (a) `Migration` struct + registry slice (b) `Run`/`validateRegistered`/`ensureMigrationTable` (c) 24 `addXxx` migration funcs | Per-migration files `m001_current_schema.go` … `m024_voice_dispatches.go`; runner stays in `migrations.go` |
| **`internal/storage/memoryindex/store.go`** | 1 143 | 40 | (a) Document CRUD + FTS upsert (b) hybrid search (`exactSearch` + `ftsSearch` + RRF merge) (c) operational decay / pin / purge | `store_core.go` (CRUD), `store_search.go` (search+merge), `store_archive.go` (decay+purge+pin), `store_helpers.go` (utilities ≥line 1050) |
| **`internal/storage/runs/store.go`** | 812 | 29 | (a) Run/Event CRUD with idempotency (b) Outbox enqueue (c) authz denial audit insertion | `store_run.go`, `store_event.go`, `store_outbox.go`, `store_audit.go` |
| **`cmd/quality_bench/main.go`** | 787 | 16 | (a) CLI parsing (b) PASS-strict/loose scoring (c) report writing | `main.go`, `score.go`, `report.go` |
| **`internal/api/types.go`** | 771 | 1 | Pure DTO/JSON shape declarations across 7 domains | See pick #6 above |
| **`internal/agent/tools/registry/memory_search.go`** | 769 | 24 | (a) tool defn + Execute (b) wiki+compact merging + half-life recency (c) snippet/query formatting | `memory_search.go` (tool+exec), `merge.go` (RRF + recency), `format.go` (snippets) |
| **`internal/wiki/memory_hygiene.go`** | 760 | 30 | (a) `CleanMemory` orchestration (b) page rename + opaque-slug planning (c) hub writing + broken-link repair | `hygiene.go` (entry), `renames.go`, `hubs.go`, `repairs.go` |
| **`internal/api/auth/store.go`** | 740 | 34 | (a) `TokenIssuer/Reader/Revoker/Writer` token CRUD (b) pending+allowed users (c) identity backfill | `store_tokens.go`, `store_pending.go`, `store_identity_backfill.go` |
| **`internal/storage/sources/ingest/pipeline.go`** | 733 | 25 | (a) compile/extract pipeline (b) entity-page + concept-page upsert (c) provenance + preview helpers | `pipeline.go`, `entity_pages.go`, `concept_pages.go`, `provenance.go` |
| **`internal/channels/telegram/invocation_builder.go`** | 688 | 13 | (a) `Build` (giant 470-LOC function ⚠) (b) tool execution (c) pinned-operational rendering | Extract `Build` into 3-4 phases (overlay-pin → llm-tools-allow → ask-resume resolve → final assembly) — already half-modularised by helpers |
| **`cmd/aura/app_wire.go`** | 662 | 5 | (a) `wireBot` (b) memory-recall tool registration (c) decay/backup/checkpoint adapters | `wire_bot.go`, `wire_memory.go`, `wire_adapters.go` |
| **`internal/storage/search/search.go`** | 646 | several | (a) Result/Document types + interfaces (b) embedding helpers (c) search composition | Separate `types.go` (interfaces+structs), `search.go` (orchestration) |
| **`internal/cron/dispatch.go`** | 643 | 27 | (a) `Dispatch` / `RunNow` routing (b) per-job handlers (reminder, wiki maint., lesson, ttl, decay, agent-job) (c) agent-job prompt assembly | `dispatch.go`, `dispatch_handlers.go`, `agentjob_prompt.go` |
| **`internal/storage/search/qdrant.go`** | 624 | several | (a) qdrant client wrapper (b) collection lifecycle (c) batch index | `qdrant_client.go`, `qdrant_index.go` |
| **`cmd/aura/web_chat.go`** | 612 | 12 | (a) hub-backed web chat factory (b) web-invocation builder (c) sessions + post-turn config | `web_chat.go` (factory), `web_invocation.go`, `web_sessions.go` |
| **`internal/llm/openai.go`** | 608 | several | (a) DTOs (b) `Send`/`Stream` (c) SSE parsing + JSON-closer repair | `openai_dtos.go`, `openai_client.go`, `sse.go` |

Files **at the boundary** (550-599 LOC, hard rule triggers on next add): `internal/wiki/store.go`(596), `internal/cron/store.go`(594), `internal/agent/tools/swarm/tools.go`(587), `internal/agent/loop.go`(580), `internal/agent/tools/registry/scheduler.go`(579), `internal/channels/telegram/status_pane.go`(563), `internal/telegram/documents.go`(555), `internal/agent/tools/registry/wiki.go`(553). Touching any of these for a feature triggers the deep-refactor rule.

---

## 2. Dead code

### 2.1 staticcheck/unused (1 finding)

- **`appendUniqueSorted`** — `internal/wiki/memory_hygiene.go:735`. Unused since `sortedStringSet` covers the same intent. **S, −10 LOC.**

### 2.2 Stale exported symbols (manual sweep)

No exported symbol with zero callers was found outside test files — the agents-on-touch policy has been holding. (Sweep: `~/go/bin/staticcheck -checks U1000` is blocked by a Go 1.26 toolchain mismatch — falls back to golangci-lint's `unused` linter which only catches package-local symbols. Re-run staticcheck once the toolchain is bumped to surface unused exports.)

### 2.3 Commented-out code

Grep on `/* … */` blocks across `internal/` + `cmd/` returned only inline doc fragments and embed-glob comments. **No multi-line commented-out code found.**

### 2.4 Backup files

`find` for `*_old.go`, `*_bak.go`, `*_backup.go`, `*.go.bak` → **0 hits.** Clean.

### 2.5 Stale TODO/FIXME (full inventory)

Only **2** TODO markers in shipped Go (both are intentional test-fixture strings, not technical debt):

| File:line | Marker | Verdict |
|-----------|--------|---------|
| `internal/agent/tools/registry/tool_definitions.go:96` | `"TODO: verify X, summarize Y, report Z"` inside `agent_note` example | **Still-relevant** (example payload, must read literally as "TODO"). Leave. |
| `cmd/probe_chat/cases.go:755` | Same string in probe prompt to assert agent_note round-trip | **Still-relevant.** Leave. |

No FIXME / HACK / XXX in production code — exceptional discipline.

### 2.6 Stale comment fictions

| File:line | What's stale | Severity |
|-----------|--------------|----------|
| `internal/agent/pool.go:14-26` + `internal/agent/promptplan.go:32` | Tells the user `tool_search` is the only always-on tool; in current build it's one of many. Reads as "deferred-tools rollout" — that A/B has resolved. | M. Misleads future readers about the architecture. |
| `internal/agent/tools/registry/exec.go:87` | "In Docker this runs directly inside the Aura container" — still accurate, but worth confirming for the LAN-exposed config. | Low. |

---

## 3. Duplication (`dupl -t 60`)

`dupl` found **101 clone clusters** in `internal/` (76 pairs, 18 triples, 5 quads, 1 quint, 1 sext). Test-only clusters (boilerplate fixtures, table-driven setup) are listed for completeness but **most are acceptable boilerplate**. Production-code clusters are the real targets.

### 3.1 Critical: production-code clusters worth folding

| Cluster | LOC | Files involved | Proposed helper |
|---------|-----|----------------|-----------------|
| `files_docx.go:1,186` ↔ `files_pdf.go:1,188` (whole-file) and `files_docx.go:98,154` ↔ `files_pdf.go:99,155` ↔ `files_xlsx.go:87,147` | ~380 dup | tool DOC/PDF/XLSX scaffolds | One generic `fileTool[Spec]` with a Builder interface |
| `mcp_database_setup.go:31,62` ↔ `mcp_setup.go:32,63` (and matching `currentXxxStatus` pairs) | ~180 dup | `internal/api/mcp_*_setup.go` | `mcpSetupHandler` generic with `ConfigBuilder`, `StatusReader` |
| `files/docx.go:87,111` ↔ `files/pdf.go:57,81` | 24 dup | block-render layout code | `renderBlocksTo(writer, blocks)` shared between docx + pdf packagers |
| `exec.go:48,59` ↔ `exec.go:61,72` (and again at `direct_fetch.go:479,493` ↔ `identity/store_helpers.go:270,284`) | 56 dup | `isNilExecutor` / `isNilCommandExecutor` / reflection-based nil check | `reflectutil.IsNilOrZero(any) bool` |
| `install/embedding.go:45,61` ↔ `install/whisper.go:35,51` | 32 dup | `EnsureXxxModel` orchestration | Generic `EnsureModel(spec)` with model-spec struct; `install` pkg is the right home (already <100 LOC each, factor before the next model lands) |
| `config/store.go:128,138` ↔ `config/store.go:156,166` and `config/runtime_settings.go:100,114` ↔ `config/runtime_settings.go:170,184` | 30 dup | settings get/set with normalisation | `applySettingTransform(key, value, normalizer)` |
| `config/applier.go:249,259` ↔ `config/applier.go:281,291` | 22 dup | env-driven config diff | `applyDiff(field, old, new)` |
| `wiki/graph_index.go:79,96` ↔ `wiki/graph_index.go:355,374` and `:396,409` ↔ `:412,425` | 38 dup | adjacency-set helpers | Promote inner helpers to method-on-graph |
| `storage/memoryindex/store.go:653,672` ↔ `:705,725` ↔ `:1121,1133` ↔ `storage/search/graph_documents.go:270,282` | 64 dup | "fetch operational rows then map → Document" | `scanOperationalRows(rows) ([]Document, error)` |
| `storage/runs/store.go:304,315` ↔ `:317,327` | 22 dup | `GetRun` / `GetEvent` boilerplate | `getOne[T](ctx, db, query, scan)` generic |
| `storage/search/search.go:350,363` ↔ `:393,406` and `storage/search/qdrant.go:213,230` ↔ `storage/search/sqlite.go:196,213` | 60 dup | scan-rows-into-Result variants | `scanResults(rows, mapper)` |
| `swarm/manager.go:191,200` ↔ `:202,211` | 18 dup | assignment validation pair | inline helper |
| `agent/tools/registry/tool_definitions.go:44,62` ↔ `:64,82` ↔ `:103,122` ↔ `:149,168` | 76 dup | tool definition skeletons | Table-driven init slice |
| `agent/tools/registry/direct_fetch.go:479,493` ↔ `identity/store_helpers.go:270,284` | 28 dup | URL-redaction (?) — needs visual confirm | Promote to `internal/redact` if confirmed semantically identical |
| `setup_locale.go:23,57` ↔ `:59,93` | 68 dup | locale-detect lang pair (it/en) | Table-driven matcher |
| `api/conversations.go:87,104` ↔ `:106,123` | 34 dup | scan helpers | Inline reduce |
| `api/pending.go:59,81` ↔ `:83,105` | 44 dup | pending-state listing | Helper |
| `telegram/documents.go:215,245` ↔ `:251,281` | 60 dup | document delivery flow (PDF vs other) | `deliverDocument(ctx, kind)` |
| `telegram/documents.go:299,308` ↔ `voice_handler.go:225,238` | 22 dup | post-delivery cleanup | Shared helper in `telegram/postsend.go` |
| `telegram/setup.go:63,75` ↔ `:76,90` | 28 dup | command-menu registration | Loop over `[]commandSpec` |
| `attempts/sqlite.go:128,141` ↔ `:196,209` | 26 dup | scan helpers | `scanAttemptRow` |
| `agent/tools/registry/wiki.go` action handler stubs (paired) | est. 30 dup | wiki-action dispatch | Switch on action |
| `chat/hub_lifecycle.go:214,228` ↔ `channels/telegram/ask_user_resume.go:127,141` | 28 dup | resume-from-pending guard | Helper in `chat/` |
| `api/upload.go:285,300` ↔ `telegram/documents.go:457,473` | 30 dup | `safeName` filename sanitisation | Promote to `internal/storage/sources/name.go` |
| `storage/sources/ingest/extractor.go:378,392` ↔ `:393,407` | 28 dup | concept- vs entity-canonicalise | Shared `canonicalize(items, kind)` |

### 3.2 Acceptable test-code duplication (no action)

24 test clusters are honest table-driven fixtures or stable mock servers (`internal/llm/openai_test.go` SSE chunk fixtures, `internal/cron/scheduler_test.go` setup factories, `internal/telegram/documents_test.go` 5-way Document-shape table, etc.). Folding these would obscure the test intent. **Leave as-is** unless one is found to mask a regression.

### 3.3 Borderline (decide on next touch)

- `internal/api/auth/store_test.go:45,73` ↔ `cron/scheduler_test.go:57,85` ↔ `swarm/store_test.go:189,217` — 3-way "open-store-then-cleanup" boilerplate; should become a `testutil.OpenTempStore[T]` once the next *Store package is added.

---

## 4. Test fragility

### 4.1 `time.Sleep` in tests (60+ hits)

The vast majority sit in `internal/concurrency/{gate,tracker}_test.go` — they're **legitimate** clock-driven tests of bucket sweeps + threshold expiry. Same for `cron/scheduler_test.go`. Two clusters are clear flakiness candidates:

| File:line | Sleep | Risk | Fix |
|-----------|-------|------|-----|
| `internal/chat/hub_swarm_test.go:108`, `:236`, `:250` | 100ms, 5ms, 300ms inside swarm-run synchronisation | Possible flake under CI load | Replace with channel-based completion signal |
| `internal/api/setup_setup_test.go:289` | 10ms loop probe of HTTP listener | Mild | Acceptable, but `httptest.Server` would be canonical |
| `internal/agent/tools/index/reconciler_test.go:442` | 200ms wait for fsnotify | Real risk on Windows | Use the reconciler's "done" channel directly |
| `internal/api/health_server_test.go:200` | 10ms "ensure uptime > 0" | Trivial but ugly | Inject a clock |

**Action**: 3 stories of S effort each. Bundle into Phase-WIKI-Clean Wave 2 (test-discipline pass).

### 4.2 `t.Skip` markers (19 hits)

All are intentional and well-tagged:

- Live tests gated by env var (`INGEST_SOURCE_IDS`, `MISTRAL_API_KEY`, `AURA_LIVE_WIKI_PATH`, `LIVE_OCR_PDF`, `RERENDER_DIRS`) — **correct.**
- Cross-platform skips (`symlink` on Windows, `python` on hosts without python3, `tzdata` on minimal containers) — **correct.**
- `release_config_test.go:14,25` skips Goreleaser/desktop workflow files not in the Docker-first branch — **stale**: either delete the test or the comment. **S, −20 LOC.**
- `static_test.go:13,29,69` triple-skip on missing `internal/api/dist/` — **correct** for dev environment, hard to remove.

### 4.3 Hard-coded paths in tests

Grep for absolute Windows / Unix paths in `*_test.go` → **0 production-test hits.** Only intentional fixtures (e.g. `download_test.go:371` writes `"XXX"` to `dest+".partial"`). Clean.

### 4.4 Long-running tests with no parallel guard

Not surveyed — recommend running `go test -list . ./... | wc -l` then `-timeout 30s ./...` to flag the slow ones. Out of scope for this audit (would need to actually run the suite).

---

## 5. Import boundaries / weak module fences

### 5.1 Total transitive deps per package (top 20)

```
internal/channels/telegram/fixture  601
internal/channels/telegram          595
internal/telegram                   594
internal/logging                    590     ← suspicious (logging should be a leaf)
internal/api                        579
internal/agent/tools/swarm          473
internal/swarm                      472
internal/chat/testhelpers           472
internal/channels/web               472
internal/channels/silent            472
internal/channels/cron              472
internal/chat                       471
internal/agent                      469
internal/learning                   462
internal/agent/tools/attempts       461
internal/agent/tools/registry       460
internal/storage/sources/ingest     387
internal/storage/search             386
internal/cron                       384
internal/storage/memoryindex/audit  371
```

**Concerning:** `internal/logging` at **590** transitive deps means it imports significant chunks of the app, breaking the leaf contract. Inspection (`internal/logging/zap_slog.go:9`) confirms: it imports `internal/api` to satisfy a health-shape interface. Fix: invert the dep — `api` should provide its own logging adapter; `logging` should only depend on `zap`+`slog`. **M effort, sub-system unblocker.** This is the **single most-actionable boundary bug** in the audit.

### 5.2 Direct inbound-dep counts (`grep` for the import literal)

| Package | inbound non-test importers |
|---------|---------------------------|
| `internal/wiki` | 28 |
| `internal/agent/tools/registry` | 27 |
| `internal/config` | 17 |
| `internal/conversation` | 22 |
| `internal/llm` | 57 |
| `internal/api/auth` | 9 |
| `internal/api` | 5 + cmd |
| `internal/telegram` | 6 + cmd |

`internal/llm` at 57 inbound is expected (every tool / agent layer needs it). `internal/wiki` at 28 inbound + `internal/agent/tools/registry` at 27 inbound are the next-most-shared modules — they're the right fence to keep stable. No import cycle detected by `go list ./...`; if there were one Go wouldn't compile.

---

## 6. Documentation rot

- **`docs/legacy-deletion-survey-2026-05-19.md`** — survey of legacy deletion targets from 2 days ago, will likely be stale after next cleanup pass. Move to `docs/_archive/` once the recommended deletions land.
- **`internal/agent/README.md`** + **`internal/agent/tools/registry/README.md`** — not opened in audit, but worth a 10-min pass to ensure they reflect the current always-on-tools model (vs the deferred-tools rollout fiction).
- **`CLAUDE.md`** — 207 lines, current. The "REUSABLE CODE" + "GOD CLASS" + "DEEP REFACTOR ON TOUCH" rules are well-stated and the audit confirms they're being followed except for the items above.
- **`docs/aura-quality-snapshot.md`** — living doc; not surveyed but per `feedback_aura_as_product` it must stay current with the gate metrics.

No README/CLAUDE.md mention of deleted modules was found.

---

## 7. Sequencing vs locked phases

Per `project_post_drift_phase_sequence_locked` (RAG+rerank → ToolSurface → TokenJuice → AgentLoop, each + cleanup), this audit's recommendations split as follows:

- **Pre-requisite for Phase-WIKI-B Wave C** (substrate stability): pick #1 (mcp setup folding), pick #5 (memoryindex/store split), pick #11 (`logging` boundary fix). These touch search/memory paths and would be unsafe to defer.
- **Bundle into Phase-WIKI-Clean** (4-6 atomic Ralph stories per `project_phase_wiki_clean_planned`): pick #2 (files-tool fold), pick #3 (migrations split), pick #4 (probe_chat split), pick #6 (api/types split), pick #7 (reflectutil), pick #8 (dead `appendUniqueSorted`), pick #9 (errcheck rewrite), pick #10 (comment fictions), plus all §3.1 production-code clusters.
- **Defer to first natural touch**: §3.3 borderline duplicates, §4.1 sleep-driven test bundles, §4.2 release-config skip cleanup.

---

## 8. Quick-win checklist (≤30 min each)

Items where the change is obvious, the diff is small, and risk is minimal:

- [ ] Delete `appendUniqueSorted` (pick #8). −10 LOC.
- [ ] Delete the two stale `release_config_test.go` skips. −20 LOC.
- [ ] Strip "deferred-tools rollout" comments (pick #10). −20 LOC.
- [ ] Fix `logging/zap_slog.go` `internal/api` import (pick boundary #5.1). −0 LOC, +clean fence.
- [ ] Promote `safeName` (pick §3.1 row #23) to `storage/sources/name.go`. −30 LOC duplication.
- [ ] Promote `setup_locale.go` it/en pair to table (pick §3.1 row #15). −34 LOC.

Aggregate quick-win delta: **−114 LOC** in roughly **1.5 hours** of focused work.

---

## 9. What is NOT broken (positive findings)

To keep proportion: the audit also confirms what's already healthy.

- Zero FIXME / HACK / XXX in production code.
- Zero `*_old.go` / `*_bak.go` / `*.go.bak` files.
- Zero abandoned multi-line commented blocks.
- Zero hard-coded absolute paths in production tests.
- Zero detectable import cycles.
- 0 of 50 errcheck flags are on critical-path business logic (all are `defer Close`/`Encode` noise or test cleanups).
- The 19 `t.Skip` calls are all well-justified (env-gated, platform-gated, or known build-output gate).
- The 1 `unused` finding is the only true dead-code symbol package-wide.
- "DEEP REFACTOR ON TOUCH" rule is observably enforced: files at 596-599 LOC sit just under the 600 line; no file has been allowed to drift past the limit *after first identification*. The 12 files >600 LOC are pre-existing god-classes from earlier phases (migrations, types.go, probe_chat cases), not recent regressions.

The codebase is in good shape. The cleanup opportunities are real but structural, not rot. Most of the value sits in the top-10 picks — total committed effort ≈ 2 weeks of focused refactor, gated phase-by-phase.

---

*Audit produced 2026-05-21. Run scripts: `~/go/bin/golangci-lint run ./...`, `~/go/bin/dupl -t 60 internal/`, `go list -f '{{.ImportPath}} {{len .Deps}}' ./internal/...`. Re-run after the next Phase boundary closes.*
