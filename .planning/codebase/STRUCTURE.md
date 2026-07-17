# Codebase Structure

**Analysis Date:** 2026-07-17

> **Honesty statement.** Every count below came from a command run on 2026-07-17 against
> `master`. The tree is **abridged**: it shows the 68 `internal/` packages by name but does
> not enumerate every file. **Absence from this doc is not evidence of absence in the repo.**
> The authoritative inventory is `go list ./internal/...`.

## Measured facts (2026-07-17)

| Metric | Value | How measured |
|---|---|---|
| Packages under `internal/` | **68** | `go list ./internal/... \| wc -l` |
| Packages under `cmd/` | **2** | `go list ./cmd/... \| wc -l` |
| Non-test Go LOC (`internal`+`cmd`) | **98,150** | `find … ! -name '*_test.go' \| xargs wc -l` summed across xargs batches |
| Test Go LOC (`internal`+`cmd`) | **143,580** | same, `-name '*_test.go'` |
| Non-test Go files | 602 | `find … \| wc -l` |
| Test Go files | 777 | `find … \| wc -l` |
| sqlc-generated LOC | **7,046** | `find internal/db/sqlc -name '*.go' \| xargs wc -l` |
| Postgres migrations | **40** (latest `0040_shared_links`) | `ls internal/db/migrations/*.up.sql \| wc -l` |
| Neo4j Cypher migrations | **2** (`0001_init`, `0002_documents`) | `ls internal/knowledge/migrations/` |
| HNSW vector dimensions | **768** | `internal/knowledge/migrations/0001_init.cypher:12` |

> ⚠️ **`wc -l` + `xargs` footgun.** `find … | xargs wc -l | tail -1` silently reports only
> the **last batch's** total when the file list exceeds one `xargs` invocation. That path
> under-reports test LOC as ~38.9k instead of 143,580 — a 3.7× error. Always sum:
> `| grep -w total | awk '{s+=$1} END {print s}'`.

## Directory Layout

```text
Aura/
├── cmd/
│   ├── aura/                    # main binary, 13,745 LOC; switch-based subcommand dispatch
│   └── compaction-test-worker/  # 2nd binary: multi-process compaction-claim tests
├── internal/                    # 68 packages — abridged below, grouped by role
│   ├── agent/                   # LLM loop (5,276 LOC) + subpkgs:
│   │                            #   tools/ (7,295) mcptools/ prompt/ workflow/
│   │                            #   display/ panicobs/ agenttest/ (test support)
│   ├── agui/                    # cockpit HTTP API + SSE — 55 files, 10,111 LOC
│   ├── webui/                   # stdlib-only leaf; //go:embed all:dist
│   ├── runner/  swarm/  cron/   # the 3 composition roots (cron/handlers/)
│   ├── gateway/                 # Phase-35 PEP (1,174 LOC)
│   ├── toolinvocations/         # append-only ledger; owns exported Reserve
│   ├── config/                  # incl. RuntimeProfile axis
│   ├── db/                      # pgx + migrations/ + queries/ + sqlc/ (generated)
│   ├── knowledge/ neostore/     # Neo4j: cypher migrations, graph store
│   ├── conversations/           # threads + the L1→L2→L2.4→L2.5 compaction ladder
│   ├── identity/ identityctx/   # Phase-36 identity isolation
│   ├── webauth/ breakglass/     # Authula sessions; break-glass escape hatch
│   ├── objectstore/             # per-identity buckets (+ garageadmin/)
│   ├── sandbox/usersandbox/     # per-user Docker sandbox (1,545 LOC) — NO CI job
│   ├── share/                   # Phase 37F — ON DISK, IMPORTED BY NOTHING (in flight)
│   ├── channels/telegram/       # Telegram channel (5,795 LOC)
│   ├── documents/ assets/       # ingest + asset pipeline
│   ├── llm/ multimodal/ rerank/ # providers (+ llm/openai_compat/)
│   ├── mcp/                     # MCP client (+ mcp/manager/)
│   ├── skills/ skilladapters/   # skills subsystem
│   ├── semindex/ activelearn/   # embedding index substrate
│   ├── reasoning{fifo,learn,store,trace}/   toolselect{learn,store}/
│   ├── memory/ profile/ onboarding/ settings/ setup/ scoring/ eval/
│   └── obs/ secret/ envutil/ pgnumeric/ canonicaljson/ boundedbuffer/
│       askuser/ agentrender/ cachemetrics/ web/ …   ← leaf utilities, not exhaustive
├── web/                         # Vite + React cockpit; builds to ../internal/webui/dist
├── docker/                      # 299 files — sidecar images (aura, sandbox, egress, agent-memory)
├── docs/                        # 328 files — incl. docs/audit/, quality snapshot
├── scripts/                     # 79 files — CI gates (agui_boundary_check.sh, coverage_gate.sh)
├── deploy/                      # systemd units (aura.service, aura-scheduler.service)
├── caddy/  searxng/             # reverse proxy + search sidecar config
├── finetune/                    # finetune/exporter Go pkg + python tooling
├── testdata/                    # shared fixtures (compaction/)
├── output/  graphify-out/       # tracked generated artifacts
└── dist/  runtime-workspace/    # git-ignored build/runtime output
```

**Note:** `tests/` exists at top level but is **empty** (0 files). Go tests are co-located
in `internal/`; browser E2E lives in `web/e2e/`.

## Directory Purposes

**`cmd/aura`:**
- Purpose: the single shipped binary and its composition wiring
- Key files: `main.go` (entry + dispatch switch), `chat*.go`, `serve*.go`, `db.go`, `doctor.go`
- Note: subcommands are dispatched by a hand-rolled `switch os.Args[1]`, **not** Cobra

**`internal/agui`:**
- Purpose: the cockpit's entire HTTP API — auth, onboarding, governance, documents,
  assets, approvals, graph, connect, voice, settings, audit, storage-orphans
- Key files: `server.go`, `translator.go`, `fanout.go`, `server_sse.go`, `*_api.go` (~20 route files)
- **Not** a one-way SSE bridge

**`internal/webui`:**
- Purpose: static embed host for the built SPA
- Key files: `embed.go`, `doc.go`, `dist/` (committed build output)
- **Invariant:** imports **only** stdlib — CI-enforced by `scripts/agui_boundary_check.sh`

**`internal/db`:**
- Purpose: Postgres access
- Key files: `tx.go` (`WithTx:22`, `WithIdentityTx:55`), `migrations/` (40 up + 40 down),
  `queries/` (sqlc input), `sqlc/` (**generated — never hand-edit**)

**`internal/sandbox/usersandbox`:**
- Key files: `router.go`, `docker_backend.go`, `docker_backend_exec.go`,
  `docker_backend_lifecycle.go`, `egress.go`, `materialize.go`, `reap.go`, `spec.go`
- ⚠️ Docker runtime is `docker_integration`-tagged and **has no CI job** → zero CI coverage

**`internal/share` (Phase 37F — IN FLIGHT, NOT SHIPPED):**
- Files present: `token.go`, `snapshot.go`, `redact.go`, `expiry.go`, `markdown.go`, `jsonfmt.go`
- Migration `0040_shared_links.up.sql` is on disk
- **Verified today:** `grep -rln '"github.com/chetto1983/aura/internal/share"' internal/ cmd/`
  returns **nothing** — no production code imports it yet
- `.planning/STATE.md`: `status: executing`, `stopped_at: Completed 37F-06-PLAN.md`,
  of **19** plans in the phase directory
- **Do not present sharing as a shipped capability.**

## Key File Locations

**Entry points:**
- `cmd/aura/main.go` — CLI dispatch
- `internal/agui/server.go` — HTTP server
- `cmd/compaction-test-worker/` — test-only second binary

**Composition roots (the only `NewLlmAgent` sites):**
- `internal/runner/runner.go:559` (interactive)
- `internal/swarm/swarm.go:172` (fan-out workers)
- `internal/cron/handlers/handler.go:124` (scheduled)

**Policy:**
- `internal/gateway/decide.go:30` — the PEP
- `internal/gateway/classify.go` → `decide.go` → `approve.go` → `reserve.go`
- `internal/toolinvocations/store_reserve.go:28` — exported `Reserve`
- `internal/config/config_runtimeprofile.go:20` — `RuntimeProfile`

**Identity isolation:**
- `internal/db/tx.go:55` — `WithIdentityTx`
- `internal/db/migrations/0032_owner_rls.up.sql` — RLS policies
- `internal/identityctx/identityctx.go` (single file), `internal/webauth/authula.go`

**CI gates:**
- `scripts/agui_boundary_check.sh`, `scripts/coverage_gate.sh`,
  `scripts/coverage_docker.sh`, `scripts/quality_snapshot_gate.sh`

## Naming Conventions

**Go files — `<base>_<concern>.go` splitting (the NO GOD CLASS rule in practice):**
- `internal/agent/llm_agent.go` + `llm_agent_dispatch.go`, `llm_agent_retry.go`,
  `llm_agent_pause.go`, `llm_agent_finalize.go`, … (16 concern files off one base)
- `internal/config/config_runtimeprofile.go`, `internal/db/sqlc/store_reserve.go`
- **Verified:** the only files >600 LOC are sqlc-generated
  (`document_control_plane.sql.go` 1,037; `models.go` 744; `assets.sql.go` 722).
  Every hand-written file is under the cap.

**Tests:** co-located `<file>_test.go`. Variants observed:
`*_integration_test.go`, `*_property_test.go`, `*_unit_test.go`, `main_test.go`
(package-level `TestMain`), `bench_soak_test.go`.

**Migrations:** `NNNN_snake_name.up.sql` + matching `.down.sql`, zero-padded to 4
(`0032_owner_rls`, `0040_shared_links`). Cypher: `NNNN_name.cypher`.

**Packages:** single lowercase word, no underscores (`toolinvocations`, `identityctx`,
`usersandbox`, `breakglass`).

**Env vars:** `AURA_<DOMAIN>_<UNIT>`; third-party keeps upstream naming
(`TELEGRAM_BOT_TOKEN`, `OPENROUTER_API_KEY`).

**Frontend:** `web/src/<domain>/` (`chat`, `documents`, `governance`, `graph`, `auth`,
`onboarding`, `settings`, `approvals`, `audit`, `admin`, `conversations`, `health`),
plus `api/`, `lib/`, `i18n/`, `theme/`, `routes/`, `components/`. Tests are co-located
`.test.tsx`; E2E in `web/e2e/`.

## Where to Add New Code

**New tool:**
- Implementation: `internal/agent/tools/<name>.go`, spec constant in the same file
- Big tools set `Deferred: true` (kept out of the LLM-visible manifest; fetched via `tool_search`)
- Small tools (`text_response`, `ask_user`) stay `Deferred: false`

**New HTTP route (cockpit):**
- `internal/agui/<domain>_api.go`, registered in `internal/agui/server.go`
- Owner-scoped reads/mutates must route through `db.WithIdentityTx`

**New Postgres table/column:**
- `internal/db/migrations/00NN_<name>.up.sql` **+** `.down.sql`
- Query in `internal/db/queries/<domain>.sql`, then regenerate: `sqlc generate` (WSL, v1.31.1)
- **Never hand-edit `internal/db/sqlc/`**
- Owner-scoped tables need an RLS policy in the style of `0032_owner_rls.up.sql`

**New cross-cutting agent dependency:**
- Add a nil-tolerant field to `runner.Deps` (`internal/runner/runner.go:66`) and inject at
  all **three** composition roots — otherwise headless (swarm/cron) paths silently diverge

**New frontend feature:**
- `web/src/<domain>/`; i18n keys in **both** `en` + `it` (`web/src/i18n/`)
- Rebuild dist (`outDir: '../internal/webui/dist'`, `web/vite.config.ts:177`) so the
  binary embeds it

**New sandbox capability:**
- `internal/sandbox/usersandbox/`; because there is no `docker_integration` CI job, also
  add **daemon-free unit tests** for pure logic (spec/tar builders, path-traversal and
  symlink guards, nil/disabled early returns) or the coverage floor silently drops

## Special Directories

**`internal/db/sqlc/`:** generated by sqlc; committed; excluded from the coverage
owned-surface floor. Never hand-edit.

**`internal/webui/dist/`:** Vite build output; **committed** (the binary embeds it).

**`dist/`, `runtime-workspace/`:** git-ignored (verified via `git check-ignore`).

**`output/`, `graphify-out/`:** generated but **tracked** (`graphify-out/` is 4,606 files).

**`docker/`:** 299 files — sidecar images: `aura/`, `aura-sandbox/`, `aura-egress/`,
`agent-memory/`. Cold rebuild is expensive; preserve build cache.

**`tests/`:** exists, **empty**.

---

*Structure analysis: 2026-07-17*
