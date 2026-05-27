# Codebase Concerns

**Analysis Date:** 2026-05-27

Scope: comprehensive scan of `D:\Aura` (master @ `67b9ec43`). CLAUDE.md sets the no-debt rules; this document records where reality has drifted, what is queued for cleanup, and what is invisibly fragile. Findings are grouped by **Severity** then **Area**.

The repository is unusually clean for its size — there are **zero `// TODO/FIXME/XXX/HACK` markers in production Go code** (verified via grep across `internal/` and `cmd/`). The debt that exists is structural (file size, duplication, deferred Close errors, sidecar fragility) and is largely already inventoried in `docs/phase-clean-plan-2026-05-27.md` (Phase-CLEAN, 29 atomic commits planned but not started).

---

## Critical

### C-1. `cmd/probe_chat/cases.go` is 1511 LOC — 2.5× the 600-LOC cap

- Files: `cmd/probe_chat/cases.go` (1511 LOC, grandfathered in `.file-size-baseline.txt:11`)
- Why critical: CLAUDE.md §GOD CLASS explicitly forbids files >600 LOC. This is the single largest violator. The next-largest (`cmd/quality_bench/main.go`, 790 LOC) is also baselined.
- Impact: every probe edit re-reads the whole file (CLAUDE.md §READ BEFORE EDIT); merges conflict; the file's `allCases()` function (line 23) is now a 50+ case literal that mixes channels-web, channels-telegram, phase07, multiagent, skill, and QA-phase concerns.
- Fix approach: split by category — `cases_web.go`, `cases_phase07.go`, `cases_multiagent.go`, `cases_qa_phase.go`, `cases_skill.go`. `qa_phase_cases.go` and `qa_phase_helpers.go` already exist alongside, so the convention is set. Target: drop the baseline entry entirely.

### C-2. SQLite WAL + Windows bind-mount corruption is documented but unmitigated in code

- Files: `compose.yaml:53` (`AURA_SQLITE_JOURNAL_MODE: "DELETE"` — workaround in effect), `internal/dbrecovery/recovery.go` (post-corruption REINDEX recipe)
- Memory: `feedback_sqlite_wal_windows_corruption` — concurrent host probe + container Aura writes corrupted `run_events` indexes + FTS5 inverted indexes; required manual REINDEX recovery.
- Impact: the production fix is "don't use WAL on Windows bind-mount". Linux production is unaffected, but every Windows developer runs production with degraded durability. No code-side guard prevents a contributor from flipping the journal mode back to WAL.
- Fix approach: assert journal mode at boot (`internal/db/migrations` or `dbrecovery`); refuse to start on Windows host with `journal_mode=WAL`. Add `make sqlite-doctor` script wrapping the documented REINDEX recipe.

### C-3. Five files at the 600-LOC redline — silent baseline growth risk

- Files at the edge (all `internal/`, all under the cap but ≥570 LOC):
  - `internal/api/files.go` — **exactly 600 LOC** (zero headroom)
  - `internal/channels/telegram/invocation_builder.go:1-596`
  - `internal/cron/store.go:1-594`
  - `internal/agent/loop.go:1-594` (baselined at 621 — already shrunk but still ungrandfathered)
  - `internal/config/config.go:1-590`
- Impact: any single defensive commit pushes them past the cap. The CI `check-file-size.sh` fails the build, blocking unrelated work.
- Fix approach: pre-emptively split `internal/api/files.go` into per-root files (`files_wiki.go`, `files_sources.go`, `files_workspace.go`, `files_skills.go`) — the 4 roots are already conceptually separated in the header comment (`files.go:5-11`).

---

## High

### H-1. Phase-CLEAN backlog — 29 commits of known debt, planned but not started

- Files: `docs/phase-clean-plan-2026-05-27.md` (1031 LOC plan)
- Catalogue:
  - **Wave 1 errcheck (9 commits)**: 50 errcheck findings across `internal/install/download.go:43,192,268`, `internal/dbrecovery/recovery.go:228,235`, query-loop closes in 4 files, HTTP body close in 3 files, logging lifecycle 4 leaks, file/dir handle 3 files, sandbox temp cleanup, skills atomic write tail, CLI/probe closes.
  - **Wave 2 dupl cross-file (12 commits)**: includes the 6-way wiki cluster, debug_docx/debug_pdf parity, install model-fetch helper, MCP setup builder, runs store row builder, summarizer↔cron `GetByID` clone, memoryindex/search row builder, qdrant↔sqlite fusion helper, `UniqueNonEmpty` generic, ask-user lifecycle, telegram documents split, upload pipeline helper.
  - **Wave 3 dupl intra-file (7 commits)**.
  - **Wave 4 staticcheck (1 commit)**: De Morgan rewrites.
  - **Wave 5 test cleanup (12 commits)**.
  - **Wave 6 CI hard-gate (2 commits)** — promotes the warnings added in Wave 0 to fail-the-build status.
- Source baseline: `docs/dupl-summary-2026-05-22.md` lists **41 production clusters + 73 test clusters = 114 clone groups** at the start of the sweep. `docs/deadcode-baseline-2026-05-22.json` is empty `[]` — deadcode is clean.
- Impact: the debt is bounded and triaged but blocks the "no new lint findings" CI hard-gate from being enabled.
- Fix approach: run the plan as written; it is Codex-ready and atomic-commit-shaped.

### H-2. Sidecar boot fragility — Aura blocks on 5 sidecars at startup, degrades silently on 2 more

- Files: `compose.yaml:130-142` (aura.depends_on)
- Hard `depends_on` (Aura refuses to start if any of these fails):
  - `aura-secrets` (one-shot)
  - `searxng`
  - `garage-init` (one-shot)
  - `qdrant`
  - `aura-llama-embed`
  - `aura-markitdown`
- Soft (silent degrade): `aura-whisper`, `aura-pocket-tts`
- Impact: on a slow link, `aura-init-models` (`compose.yaml:23-40`) downloads 731 MB of GGUF models (embeddinggemma + whisper-small) before `aura-llama-embed` starts. A SHA mismatch leaves a 0-byte file on disk and silently fails the next boot, since errcheck findings (US-CLEAN-01) hide the close errors. The download retry path is in `internal/install/download.go` but its 3 deferred-close errors are unchecked.
- Fix approach: ship US-CLEAN-01 (deferred Close errors in `download.go:43,192,268`), then add a startup health-check that surfaces partial-download corruption to the dashboard.

### H-3. Cross-platform `npm install` instead of `npm ci` in CI (lock drift workaround)

- Files: `.github/workflows/ci.yml:93-99` (frontend job), comment confirms it is a Windows-dev / Linux-CI lockfile drift workaround (`@emnapi/*` Linux WASM bindings missing from Windows-generated lock)
- Memory: `feedback_npm_lock_cross_platform_drift`
- Impact: every CI run resolves transitive dep versions from scratch; a malicious / typosquatted upstream could land without lockfile verification. Build also slower (~10-30s).
- Fix approach: regenerate `web/package-lock.json` inside `docker run node:22` once, commit, flip CI back to `npm ci`. Comment already prescribes the fix.

### H-4. `go-git` synchronous git commit on every wiki write — agent loop blocks on disk

- Files: `internal/wiki/store.go:332-380` (`gitCommit`), wrapped in `gitMu` mutex
- Comment at `store.go:332-334`: "gitCommit ignores ctx because go-git's Worktree API is synchronous; we keep the parameter so callers don't need a special case in the wiki write path."
- Impact: every `wiki_page` action serializes through one process-wide mutex AND performs `PlainOpen + worktree.Add + worktree.Commit` synchronously on the agent goroutine. For a large wiki this is multi-100ms latency stacked into the agent reply path. With multiple concurrent users this becomes a global serialization point.
- Fix approach: defer git commits to an async writer goroutine fed by a buffered channel; the user-visible wiki write returns as soon as the atomic file rename succeeds. Commit retry on conflict.

### H-5. CONS Phase in-flight on master — partial consolidation

- Files: `scripts/ralph/prd.json` — Phase-CONS active, US-CONS-02..04 shipped 2026-05-24, US-CONS-05..13 still queued.
- The README of the PRD acknowledges: "BACKEND SSE in CONS-07 is upgraded to speak the current Vercel AI SDK UI Message Stream protocol" — the protocol upgrade lands across multiple stories.
- Impact: web↔telegram parity is partial. The `dupl` report (#7, #18) flags `cmd/aura/web_chat.go ↔ internal/channels/telegram/invocation_builder.go` cross-file twins; those are exactly what CONS is meant to fold but the work is not finished.
- Fix approach: finish the phase on master (no feature branch — see memory `feedback_master_direct_workflow`).

---

## Medium

### M-1. `tmp/` directory in repo is untracked but not gitignored

- Files: `tmp/tool-e2e-driver.py`, `tmp/tool-e2e-results.json`, `tmp/tools-list.json`, plus `tmp/cache-test-body.json`, `tmp/gemma-multimodal-test.py`, `tmp/gemma-pdf-rasterize-test.py` (all untracked)
- `.gitignore` ignores `tmp_*` (line 78, prefix match) but not `tmp/` (directory)
- Impact: contributors can accidentally commit large generated artifacts. Three files appear in `git status` on a clean tree.
- Fix approach: add `/tmp/` to `.gitignore` line 78 group, OR move all such artifacts to `D:/tmp/` (see memory `feedback_no_docs_in_tmp` — `D:/tmp` is the canonical scratch location).

### M-2. `context.Background()` used inside transport-aware code paths

- Files (20 production files surveyed):
  - `internal/channels/telegram/invocation_builder.go:347,534,557` — archive lookup, archive write, conversation limit enforcement all bypass the request context. `setMaxTurnIndex` race noted in `invocation_builder.go:534` (`archiveCtx := context.Background()`).
  - `internal/agent/agentdef/loader.go:29` — loader falls back to `context.Background()` when caller passes nil.
  - `internal/conversation/auto_compact.go`, `internal/conversation/compressor.go`, `internal/cron/store.go`, `internal/mcp/client.go`, `internal/storage/search/embed_cache.go`, `internal/sandbox/process_runner.go` — similar background fallbacks.
- Impact: telegram session shutdown / user cancellation cannot propagate into these paths. Worst case is a slow archive write outliving its parent turn; harmless today but a latent bug if any of those calls grow.
- Fix approach: thread `ctx` from the parent invocation; reserve `context.Background()` for top-level `main.go` ownership only.

### M-3. Panics in production paths (3 files)

- `internal/agent/agentdef/builtin.go:41` — `panic(err)` when an embedded built-in agentdef cannot be unmarshalled. The unmarshal target is hard-coded in the binary; a panic here is a build-time bug, not a runtime fault. Defensible but worth a comment marker.
- `internal/identity/store_helpers.go:361` — `panic(fmt.Sprintf("identity: random id: %v", err))` when `crypto/rand` fails. Reading from `/dev/urandom` failing is catastrophic; panic is correct, but document why.
- `cmd/module_health/main.go:51` — `panic(err)` in CLI is fine.
- Impact: minimal — both prod panics are unreachable in steady state. Cosmetic only.
- Fix approach: replace with `slog.Error` + `os.Exit(1)` to align with `internal/logging` discipline.

### M-4. `internal/wiki/graph_index.go` intra-file 18-line duplication

- Files: `internal/wiki/graph_index.go:79-96` and `internal/wiki/graph_index.go:355-374` — duplicated graph traversal loop (`dupl` cluster #13, score 36).
- Impact: invariant drift risk — two copies of a BFS loop with no shared helper. CLAUDE.md §REUSABLE CODE explicitly forbids this.
- Fix approach: queued as US-CLEAN-27a in Phase-CLEAN Wave 3.

### M-5. `internal/agent/tools/registry` files_docx/files_pdf/files_xlsx whole-file structural twins (score 240)

- Files: `files_docx.go:1-120` ↔ `files_pdf.go:1-122` ↔ `files_xlsx.go:35-95` (`dupl` cluster #1+#2, total score 411)
- Impact: highest-priority dedupe target. Three near-identical files differ only in MIME type and converter. Bug fixes have to be applied 3 times; one place will be missed.
- Fix approach: extract shared `documentToolBase` in a sibling file. US-MOD-DEDUP-01 (deferred to Phase-CLEAN US-CLEAN-11 successor).

### M-6. High recent fix density — 78 of 199 commits since 2026-05-22 are fix-prefixed (~39%)

- Inferred from `git log --since="2026-05-22" --grep="fix\|bug" | wc -l`
- Sample recent fix commits hint at fragile zones:
  - `431e0e6c fix(tools): execute_shell heredoc backtick injection + false success detection` (sandbox security)
  - `1a3f88da fix(search): pin literal wiki slug matches` (RAG correctness)
  - `8e8d6d73 fix(prompt): route user profile recall through graph` (memory routing)
  - `0f321b40 fix(prompt): run scheduled routines immediately when requested` (scheduler UX)
  - `c3c368fa fix(qdrant): dedupe wiki search results by slug` (orphan vectors)
  - `bfe14a59 fix(wiki): purge orphan Qdrant vectors + FTS5 rows for deleted wiki pages`
- Impact: prompt/tool routing and search dedup are the live churn area. Probes targeting these surfaces have the highest regression-catch value.
- Fix approach: lock these regressions into `cmd/probe_chat` cases (some already are — e.g. `naturalLongWorkflowScratchpadCase`, `wikiSubgraphDeltaCase`) and gate via `cmd/quality_bench` Wave thresholds.

### M-7. Quality-bench p95 latency target unmet — 43s vs 30s target

- Files: `docs/aura-quality-snapshot.md` Wave A row: pass 15/20, p95 43s, target ≤30s (✗)
- Outlier: `arxiv-pdf q1` took 103s with 9 tool calls (tool-selection failure picking `web_search` over ingested PDF).
- Impact: real user-perceived latency drag on PDF-grounded questions. The agent occasionally fails to recognize that an ingested source is the better evidence than a fresh web search.
- Fix approach: prompt-level — strengthen the `search`/`web` action descriptions so the LLM prefers `search` (local) over `web.search` when the user has uploaded sources. Verify with `quality_bench` re-run.

### M-8. Recall@5 not measured — quality bench has no direct search-index access

- Files: `docs/aura-quality-snapshot.md:18-22` (note "a")
- The bench measures only end-user reply behaviour; Recall@5 (top-5 ground-truth slug hit rate) is documented as a goal but instrumentation isn't wired.
- Impact: Wave B/C thresholds (≥85% / ≥90%) cannot be verified before promotion. Plan-phase work that depends on substrate health (`docs/phase-kv-plan-revised-2026-05-27.md`, `docs/aura-rag-compaction-slice-2026-05-25.md`) is flying blind.
- Fix approach: add an internal `/internal/search/recall` endpoint or extend `cmd/quality_bench` to call `internal/storage/search` directly. Track Recall@5 alongside the existing E2E metrics.

### M-9. `cmd/quality_bench/main.go` is 790 LOC — second-largest baseline violator

- Files: `cmd/quality_bench/main.go` (790 LOC, baseline line 12)
- Same shape as `probe_chat/cases.go` (C-1): probe orchestration + fixture upload + result aggregation + reporting all in one main.
- Impact: bench evolution is friction-heavy; new metrics (e.g. Recall@5, see M-8) discourage splitting.
- Fix approach: extract `upload.go`, `report.go`, `metrics.go` siblings. Same pattern as `probe_chat/` already uses.

---

## Low

### L-1. Probe coverage gaps for new surfaces

- Existing probes (`cmd/probe_*`): `probe_chat`, `probe_doc`, `probe_ingest_e2e`, `probe_reasoning`, `probe_telegram_ui`, `probe_webfetch` (6)
- Missing direct probe coverage:
  - `internal/storage/qdrant` — no `probe_qdrant_e2e` (debug-only `cmd/debug_qdrant` exists)
  - `internal/audio` (Whisper sidecar) — no probe; smoke validated manually in 2026-05-19 audio spike
  - `internal/audio` (Pocket-TTS) — no probe; documented Wave 3 closed but no automated PASS gate
  - `internal/mcp` — no `probe_mcp_e2e`; live MCP servers covered ad-hoc via `probe_chat` cases with `mcp_calculator_*` allowlist
- Impact: regressions in audio sidecars and MCP wiring are caught only by user reports, not CI.
- Fix approach: ship `probe_audio` and `probe_mcp` as standalone bench harnesses calling `internal/audio.WhisperClient` / `internal/audio.PocketTTSClient` and the MCP registry directly.

### L-2. Migration count growing without consolidation

- Files: `internal/db/migrations/` — m01..m29 (29 migrations as of 2026-05-27)
- Notable: m18 migrates `MISTRAL_API_KEY` from settings to secrets, m26 adds `output_dim` to embedding cache key (was an active bug per memory `feedback_secret_settings_routing_pattern`), m29 adds prompt-health views.
- Impact: a fresh install replays 29 migrations. Most are tiny ALTERs, but the boot-time replay cost grows linearly.
- Fix approach: every 50 migrations or major version bump, consolidate via a "current schema" baseline (the m01 pattern is already named "create_current_schema"). Not urgent until ~m50.

### L-3. `tmp/` in repo + `D:/tmp/` host scratch confusion

- Files: `tmp/` in repo (M-1), separate from `D:/tmp/` host scratch
- Memory: `feedback_no_docs_in_tmp` mandates that user-curated reference snapshots live in `D:/tmp/` (picobot, codex, elysia, etc.) while generated docs go to `docs/`.
- Impact: contributors confuse the two locations. Six untracked files in repo `tmp/` is a symptom.
- Fix approach: rename repo `tmp/` to `.scratch/` (or add `/tmp/` to gitignore — M-1 fix) and add a one-line README pointing contributors to `D:/tmp/` for upstream samples.

### L-4. `_archive_phaseG_dead_dispatch` referenced in `.golangci.yml` exclusions but unclear status

- Files: `.golangci.yml:74` excludes path `_archive_phaseG_dead_dispatch`
- Could not locate this directory in current tree.
- Impact: stale exclusion entry — either the archive is gone (drop the entry) or the entry hides real lint findings.
- Fix approach: verify with `find . -path "*_archive_phaseG_dead_dispatch*"`; if empty, prune the entry in the next `.golangci.yml` touch.

### L-5. `Dockerfile.aura` cap_add NET_RAW/NET_ADMIN scope unclear

- Files: `compose.yaml:121-123` (aura container gets NET_RAW + NET_ADMIN), Dockerfile uses `setcap cap_net_raw,cap_net_admin+eip` for `/usr/bin/nmap`.
- Memory: `project_aura_lan_exposure_2026-05-17` documents the rationale (LAN-mode pen-test tooling).
- Impact: in default `0.0.0.0:18080` bind (`compose.yaml:115`), Aura the binary inherits CAP_NET_RAW. A future tool addition could weaponize it (raw socket beyond nmap).
- Fix approach: drop container-wide cap_add; use `setcap` on nmap binary only, and set explicit `securityContext.capabilities.add` only when LAN-mode is requested via env var.

### L-6. `golangci-lint` only enables `depguard` linter in current `.golangci.yml`

- Files: `.golangci.yml:3-5` — only `depguard` enabled by default. `errcheck`, `staticcheck` referenced in Phase-CLEAN but not enabled in the lint config.
- Impact: 50 errcheck + 2 staticcheck findings (per Phase-CLEAN baselines) are invisible to local `golangci-lint run` unless contributors know to pass `--enable=errcheck,staticcheck`.
- Fix approach: Phase-CLEAN US-CLEAN-50/51 explicitly add these as hard-gate; the open question is whether to also flip the config default before Wave 6 completes (currently warnings-only via CI step proposed in US-CLEAN-00).

---

## Test Coverage Gaps

### Untested production paths

- `internal/dbrecovery/recovery.go` has tests (per Phase-CLEAN US-CLEAN-02 note) but the corruption scenario itself is not reproducible in CI on Linux — only Windows bind-mount triggers it. Memory `feedback_sqlite_wal_windows_corruption` describes the manual recovery recipe; no automated repro.
- `internal/audio/*` (Whisper + Pocket-TTS clients) have unit tests but no live sidecar gate. Memory `project_2026-05-19_phase_mm_wave2_closed` records the manual Telegram voice E2E that confirmed shipping; not re-runnable in CI.
- `internal/mcp/client.go` (523 LOC) — MCP server discovery + reconnect logic. Reconnect on stdio close has historically been brittle (memory `project_wave_2_10_b_tool_reconciler_shipped` records the fix).
- Web frontend has only Playwright e2e settings spec (`web/e2e/settings.spec.ts`) per file listing. No component-level tests for `MCPPanel.tsx` (1141 LOC, largest web file) or `SourceInbox.tsx` (791 LOC).

### Probe assertion strength

- Phase-CTX bench (`docs/aura-quality-snapshot.md` Phase-CTX section) shows Gemma model "compressed the long fixture but failed the keyword follow-up" — passes the savings gate but fails substantive content. The harness flags this but does not block selection of Gemma. Memory `feedback_aura_as_product` mandates "quality matrix obbligatorio" — this row is a regression-catch waiting to happen.

---

## Frontend Hotspots

- `web/src/components/MCPPanel.tsx` — **1141 LOC**, by far the largest component. Combines server list + tool list + tool invocation + result rendering + config form. Split candidates: `MCPServerList.tsx`, `MCPToolPanel.tsx`, `MCPInvocationCard.tsx`.
- `web/src/components/SourceInbox.tsx` — **791 LOC**. Upload + ingest queue + status + reprocess action. Split: per-status sub-component.
- `web/src/types/api.ts` — **704 LOC** of TS types. Acceptable for a type-only module; not a god class.
- `web/src/components/TasksPanel.tsx` — **653 LOC**, near the unwritten 600-LOC TS heuristic.

The TS files have **zero TODO/FIXME markers** (grep on `*.ts` + `*.tsx` returned no matches). Same discipline as Go side.

---

## Security Posture

- **Bearer-token auth**: `internal/api/auth/middleware.go` correctly never logs token values; uses constant-time SHA-256 hash comparison via `internal/api/auth/store_tokens.go`; emits tokens out-of-band via Telegram only. Strong.
- **Secrets store**: `internal/secrets/store.go` separates secret keys from settings — `SELECT * FROM settings` cannot leak. Migration m18 backfilled `MISTRAL_API_KEY`. Memory `feedback_secret_settings_routing_pattern` flags that **adding a new `is_secret: true` setting requires updating 3 places** (`secretKeyMappings`, `secrets.Key` constant, settings catalog). No lint enforcement of this 3-way coupling — a future contributor will miss it.
- **Sandbox**: `internal/sandbox/process_runner.go` enforces `SANDBOX_TIMEOUT_SEC` (default 300s, capped 600s). `execute_shell` heredoc backtick injection fixed 2026-05-25 (`431e0e6c`). Spawning isolation via subprocess, no chroot/cgroup. LAN-mode adds raw-socket caps (L-5).
- **MCP allowlist**: `compose.yaml:97` `AURA_TOOL_ALLOWLIST` defaults to a 13-entry list including `mcp_calculator_*`. No allowlist enforcement at the MCP transport layer — an MCP server can register any tool name; protection is at the registry gate only.
- **Web SSE**: streaming endpoint in `internal/channels/web/streaming_outbound.go` (551 LOC) — needs review that bearer middleware applies before the SSE upgrade.

---

## Concurrency Hazards

- **15 production goroutine spawn sites** (excluding tests, including cmd):
  - `cmd/aura/app.go`, `cmd/aura/app_wire.go`, `cmd/aura/main.go`
  - `internal/agent/executor.go`, `internal/agent/stream_dispatch.go`
  - `internal/agent/tools/registry/exec.go`
  - `internal/api/health_server.go`, `internal/api/setup_server.go`
  - `internal/channels/telegram/invocation_builder.go`
  - `internal/concurrency/tracker.go`
  - `internal/mcp/client.go`
  - `internal/storage/memoryindex/store_helpers.go`
  - `internal/storage/search/qdrant_hybrid.go`
  - `internal/telegram/documents.go`, `internal/telegram/handlers.go`
- All run with `go test -race` per `.github/workflows/ci.yml:45`. No flagged races as of master.
- **Notable serialization points**:
  - `internal/wiki/store.go:28-31` — 4 mutexes (per-file, git, index.md, log.md); H-4 above.
  - `internal/agent/session.go:38` — `closeHookMu` for session-close hooks.
  - `internal/agent/executor.go:105` — explicit comment "Clone arguments so parallel goroutines cannot race on a shared map" (F-002 fix).

---

## Scaling Limits

- **Sliding context window cap**: 50 messages per conversation (CLAUDE.md §Agent Loop, `internal/conversation` enforces). Beyond 50, history is summarized via `internal/conversation/auto_compact.go` + `internal/conversation/compressor.go`. p95 compaction latency: 9.3-11s per Phase-CTX bench (`docs/aura-quality-snapshot.md`).
- **Agent loop iteration cap**: `AURA_AGENT_LOOP_MAX_STEPS` default 50 (`compose.yaml:87`). `MaxIterationsCeiling` in `internal/agent/loop.go` clamps runaway models. Capped at 5 by default for chat per CLAUDE.md but the compose env raises this to 50 for delegated/long workflows.
- **Embedding cache (SHA-keyed SQLite)**: bounded by wiki size. No eviction policy — grows monotonically. Memory `feedback_minipc_cpu_budget` mandates ≤4 threads + ≤4 concurrent index ops.
- **Qdrant collection rebuild**: full reindex measured in 10s of seconds on a small wiki; not benchmarked at >1k pages. `internal/storage/reindex/worker.go` runs in background but blocks new writes briefly during the collection rotate. No documented limit.

---

## Dependencies at Risk

- `gopkg.in/telebot.v4 v4.0.0-beta.7` (`go.mod:5`) — **beta dependency** for the Telegram channel. API may break before 4.0.0 GA. No fallback transport.
- `go-git/v5` — vendored only in `internal/wiki`. Synchronous worktree API forces H-4. No CGO needed, but switching to libgit2 would require Cgo + cross-compile pain.
- `embeddinggemma-300m-Q4_0.gguf` — fetched at install time; if HuggingFace removes the artifact, fresh installs hard-fail. URL override exists (`compose.yaml:33`) but a default-install user has no fallback.

---

## Workspace Hygiene

- **Untracked in repo**: 3 files in `tmp/` (M-1, L-3).
- **Worktrees**: `.claude/worktrees/agent-a9e79b9b314b94f9c/` — automated agent worktree, gitignored via `.claude/`. Shows up in `wc -l` totals if not filtered. Not a concern, but doubles file-size counts in naive scans.
- **Archive**: `docs/_archive/` is well-organized (README + 5 group dirs); past phase artifacts are preserved with clear "do not execute" notice.
- **`.planning/` ignored** (`.gitignore:77`) — keeps in-flight phase plans out of git. Already-shipped plans archived under `scripts/ralph/prd-completed-*.json` (20+ files committed).

---

*Concerns audit: 2026-05-27*
