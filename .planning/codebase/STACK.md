# Technology Stack

**Analysis Date:** 2026-07-17

> **Provenance rule.** Every number below was measured by a command run on 2026-07-17 at the
> then-current `master` HEAD. Counts that drift are marked with the command that regenerates
> them — prefer re-running the command over trusting the frozen number. Where a fact could not
> be verified in-session it is labelled **Not verified**. (This document replaces a predecessor
> that carried month-stale counts propagated between docs; do not reintroduce unsourced numbers.)

## Languages

**Primary:**
- **Go 1.26.5** — the entire backend. Pinned in `go.mod:3` (`go 1.26.5`); CI resolves the
  toolchain with `go-version-file: go.mod` (`.github/workflows/ci.yml:60`), so `go.mod` is the
  single source of truth for the Go version.
  - Module path: `github.com/chetto1983/aura` (`go.mod:1`)
  - **98,150 LOC** non-test across `cmd/` + `internal/`, of which **7,046 LOC** are
    sqlc-generated (`internal/db/sqlc/`) — i.e. ~91k hand-written.
    Regenerate: `find cmd internal -name '*.go' ! -name '*_test.go' | xargs wc -l | grep -v ' total$' | awk '{s+=$1} END {print s}'`
  - **143,580 LOC** of tests (`*_test.go`) — tests outweigh non-test code ~1.46:1.
  - **68 packages** under `internal/`, **72** total including `cmd/`.
    Regenerate: `go list ./internal/... | wc -l`

**Secondary:**
- **TypeScript ~6.0** (`web/package.json` devDependency `typescript: ^6.0.3`) — the operator SPA
  under `web/src/`. **32,601 LOC** non-test `.ts`/`.tsx`.
- **SQL** — 40 golang-migrate migration pairs in `internal/db/migrations/` and hand-written
  queries in `internal/db/queries/` (sqlc input).
- **Cypher** — 2 Neo4j migrations in `internal/knowledge/migrations/`.

## Runtime

**Environment:**
- **Go 1.26.5**, built `CGO_ENABLED=0 GOOS=linux` in `docker/aura/Dockerfile`; runtime image is
  `debian:bookworm-slim`, build image `golang:1.26.5-alpine`.
- **Node 24** — build-time only. `docker/aura/Dockerfile` builds the SPA in a discardable
  `node:24-bookworm-slim` stage; **no Node exists in the runtime image**. `web/package.json`
  `engines` requires `node >=24.16.0 <25`, `npm >=11 <13`; CI pins `AURA_CI_NODE_VERSION: "24.x"`
  (`.github/workflows/ci.yml:29`).

**Package Manager:**
- Go modules — `go.mod` + `go.sum` present. 43 direct requires, ~150 indirect.
- npm 12 (`web/package.json` → `packageManager: npm@12.0.0`); `web/package-lock.json` present
  (`npm ci` in the Docker webbuild stage).

## Frameworks

**Core (Go):**
- `github.com/ag-ui-protocol/ag-ui/sdks/community/go` — AG-UI protocol SDK; the SSE event
  contract between the agent loop and the SPA (`internal/agui/`).
- `github.com/Authula/authula v1.15.0` — embedded auth provider; mounts its own `/auth/*` handler
  and runs its own schema migrations (`internal/webauth/authula.go`).
- `github.com/jackc/pgx/v5 v5.10.0` — Postgres driver (sqlc's `sql_package: pgx/v5`).
- `github.com/golang-migrate/migrate/v4 v4.19.1` — schema migrations (`internal/db/`).
- `github.com/neo4j/neo4j-go-driver/v5 v5.28.4` — **narrowly scoped**: only 3 non-test files
  import it (`internal/knowledge/probe.go`, `internal/knowledge/schema.go`,
  `internal/cron/handlers/backup.go`). Agent-facing Cypher goes through the `mcp-neo4j-cypher`
  MCP server instead (`internal/knowledge/client.go:2` documents the native-driver ban).
- `gopkg.in/telebot.v4` — Telegram bot channel (`internal/channels/telegram/`); long-polling,
  not webhooks (`bot.go:279` — default `LongPoller`).
- `github.com/aws/aws-sdk-go-v2/service/s3 v1.105.0` — S3-compatible object store client, pointed
  at Garage (`internal/objectstore/s3.go`).
- `github.com/prometheus/client_golang v1.23.2` — `/metrics` endpoint (`internal/agui/server.go:246`).
- `go.opentelemetry.io/otel v1.44.0` (+ `sdk`, `trace`, OTLP gRPC and stdout exporters) —
  tracing, wired in `internal/obs/init.go`.
- No vendor LLM SDK. The LLM client is hand-rolled OpenAI-compatible HTTP
  (`internal/llm/`, `internal/llm/openai_compat/`) — provider-neutral by design.

**Frontend:**
- **React 19.2** + **react-dom 19.2**, with the **React Compiler** (`babel-plugin-react-compiler`
  via `@rolldown/plugin-babel`; plugin order is load-bearing — see the comment in
  `web/vite.config.ts`).
- **Vite 8** (`web/vite.config.ts`) with **Tailwind CSS 4** (`@tailwindcss/vite`).
- `@assistant-ui/react 0.14.22` + `assistant-stream` — chat surface.
- `radix-ui` / `@radix-ui/*` primitives; `lucide-react` icons.
- `@tanstack/react-query`, `react-router-dom 7`, `react-i18next 17` + `i18next 26`.
- `sigma 3` + `graphology` + `@react-sigma/core` — graph visualization.
- `shiki 4` — syntax highlighting, aggressively code-split (see `codeSplitting.groups` in
  `web/vite.config.ts`).

**Testing:**
- Go: stdlib `testing`, plus `go.uber.org/goleak v1.3.0` (goroutine-leak assertions),
  `pgregory.net/rapid v1.3.0` (property-based), `github.com/stretchr/testify v1.11.1` and
  `github.com/testcontainers/testcontainers-go v0.42.0` (both **indirect** — pulled in by Authula,
  not direct Aura test deps).
- Web: **Vitest 4** + `@vitest/coverage-v8`, `@testing-library/react`, `jsdom`;
  **Playwright 1.61** for E2E; **Stryker 9** for mutation; `axe-core` for a11y.

**Build/Dev:**
- `Makefile` — the gate entry points: `quality` (= `vet file-size lint deadcode test-race vuln`),
  `quality-full` (= `quality` + `coverage`), `coverage`, `coverage-docker`, `sqlc`, `db-migrate`,
  `neo4j-migrate`, `smoke`, `web-quality`, `restore-drill`.
- **sqlc v2** (`sqlc.yaml`) — generates `internal/db/sqlc/` from `internal/db/migrations` (schema)
  + `internal/db/queries` (queries). No separate `schema.sql`. `emit_interface: true`, so the
  generated `Querier` is the seam tests fake against.
- **golangci-lint v2.12.2** — pinned in `.github/workflows/ci.yml:85`; config `.golangci.yml`
  (`version: "2"`). `dupl` enabled at threshold 100, `_test.go` excluded.
- `govulncheck` (`make vuln`, CI `vulncheck` job).

## Key Dependencies

**Critical:**
- `github.com/Authula/authula v1.15.0` — owns identity/session/2FA. `authula.New` **panics** on
  init error (`internal/webauth/authula.go:91` comment), so misconfiguration is fail-fast.
  It drags a large indirect tree (watermill + sarama + testcontainers + bun + resend + jwx),
  which is why `go.mod`'s indirect block dwarfs the direct one.
- `github.com/jackc/pgx/v5` — every Postgres path.
- `github.com/ag-ui-protocol/ag-ui/sdks/community/go` — the SPA↔backend streaming contract.
- `github.com/pkoukk/tiktoken-go v0.1.8` — token accounting for the context budget.
- `github.com/moby/moby/client v0.5.0` + `github.com/moby/moby/api v1.55.0` — Docker API for the
  per-user sandbox (`internal/sandbox/usersandbox`).

**Content / ingest:**
- `codeberg.org/readeck/go-readability/v2` — article extraction (`web_fetch`).
- `github.com/JohannesKaufmann/html-to-markdown/v2` — HTML→Markdown.
- `github.com/adhocore/gronx` — cron expression parsing (`internal/cron/`).
- `github.com/goccy/go-yaml` — skill frontmatter / config parsing.
- `github.com/mdp/qrterminal/v3` + `rsc.io/qr` — WhatsApp pairing QR in the terminal.
- `github.com/PaulSonOfLars/gotg_md2html` — Telegram MarkdownV2 rendering.

**Infrastructure:**
- `github.com/joho/godotenv v1.5.1` — `.env` loading (note: the compiled binary does **not**
  auto-load `.env` in every path; `internal/llm/config.go` loads it for the LLM tier).
- `github.com/google/uuid`, `golang.org/x/sync`, `golang.org/x/crypto`, `golang.org/x/net`,
  `golang.org/x/text`, `golang.org/x/image`, `golang.org/x/term`, `golang.org/x/tools`.

## Configuration

**Environment:**
- Convention `AURA_<DOMAIN>_<UNIT>`. Third-party names keep upstream canon
  (`OPENROUTER_API_KEY`, `TELEGRAM_BOT_TOKEN`, `POSTGRES_*`, `NEO4J_*`, `MULTIMODAL_*`,
  `SEARXNG_URL`, `GARAGE_RPC_SECRET`).
- **255 distinct env var name literals** appear in Go source (includes ~20 test-only fixtures such
  as `AURA_TEST_*`).
  Regenerate: `grep -rhoE '"(AURA|OPENROUTER|TELEGRAM|POSTGRES|NEO4J|MULTIMODAL|LLAMA|LMCACHE|SEARXNG|AWS|GARAGE|RESEND)_[A-Z0-9_]+"' internal cmd --include='*.go' | sort -u | wc -l`
- Central loader: `internal/config/config.go` (+ `config_mcp.go` for MCP servers,
  `internal/envutil/` for typed parsing helpers). LLM config has its own tiered loader
  (`internal/llm/config.go`): built-in defaults → `.env` → `~/.aura/llm.json` → `AURA_LLM_*`.
- `.env` and `.env.example` exist at the repo root. `.env` holds live secrets — never read or quote.
- `compose.yaml` interpolates the **whole file** on any `compose up`, so several vars are
  `:?`-required even for jobs that never start the service they belong to (see the CI `env:` block
  comment, `.github/workflows/ci.yml:12-27`).

**Build:**
- `go.mod` / `go.sum`, `sqlc.yaml`, `.golangci.yml`, `Makefile`
- `web/vite.config.ts`, `web/tsconfig*.json`, `web/package.json`, `web/tokens/generate-theme.mjs`
  (`npm run build` = generate theme → `tsc -b` → `vite build`)
- `docker/aura/Dockerfile` (+ `docker/{agent-memory,aura-egress,aura-sandbox,garage,markitdown,mcp-neo4j-cypher}/`)
- `compose.yaml` — 18 services (see INTEGRATIONS.md)

**SPA embedding:**
- Vite writes to `../internal/webui/dist` (`web/vite.config.ts` → `build.outDir`) because Go's
  `//go:embed` is package-relative. `internal/webui/embed.go:15` uses `//go:embed all:dist` — the
  `all:` prefix is load-bearing (a bare `//go:embed dist` silently drops files). The committed
  bytes are pinned equal to a fresh build by the `web-dist-freshness` CI job.

## Platform Requirements

**Development:**
- **WSL is the primary dev environment** (gcc + GNU make + `CGO_ENABLED=1` for native
  `go test -race`; `mcp-neo4j-cypher` via pipx in `~/.local/bin`; Go tooling in `~/go/bin` — the
  login shell puts neither on PATH).
- Node/web gates (vitest, tsc, prettier, playwright) run on Windows — WSL has no Node.
- Docker + the compose stack for every integration tier.
- `mcp-neo4j-cypher` on PATH for `neo4j_integration` tests (`AURA_MCP_NEO4J_CYPHER_BIN` overrides).

**Production:**
- Docker Compose appliance (`compose.yaml`, `name: aura`), Caddy HTTPS ingress.
- **GPU is the product default, not an optional extra.** Three llama.cpp sidecars ship
  `ghcr.io/ggml-org/llama.cpp:server-cuda` with `-ngl 99` (offload all layers):
  `aura-llama-embed` (`AURA_EMBED_NGL:-99`), `aura-ocr-vl` (`AURA_OCR_VL_NGL:-99`), `aura-rerank`
  (`AURA_RERANK_NGL:-99` — the comment records CPU rerank at ~23s, i.e. "GPU mandatory"). CPU-only
  is the CI/installer fallback.
- Resource caps on the `aura` service: `mem_limit ${AURA_MEM_LIMIT:-768m}`,
  `cpus ${AURA_CPUS:-1.0}`, `pids_limit ${AURA_PIDS_LIMIT:-512}`.

## Test & Tag Matrix

Build tags found in `*_test.go` (regenerate:
`grep -rhoE '//go:build [a-z_ &|!()]+' internal cmd --include='*_test.go' | sort | uniq -c | sort -rn`):

| Tag | Files | Runs in CI? |
|-----|-------|-------------|
| `db_integration` | 83 | Yes — `integration-test`, `knowledge-integration-test`, `compaction-distributed-gates` |
| `neo4j_integration` | (paired) | Yes — `knowledge-integration-test` |
| `web_integration` | 2 (+12 `!web_integration`) | Yes — `web-integration-test` |
| `docker_integration` | 9 | **No — zero CI jobs** |
| `cot_eval` | 10 | No (paid/live, opt-in) |
| `neo` | 8 (+3 `db_integration && neo`) | Partially |
| `reasoning_live` | 5 | No (live) |
| `live_e` / `live_finalize` | 5 / 1 | No (live) |
| `memory_integration` | 3 | Yes — `memory-integration-test` |
| `telegram_integration`, `calendar_integration`, `multimodal_integration`, `whatsapp_integration`, `webauth_integration`, `rerank_integration`, `integrations_integration` | 1 each | Mostly yes (dedicated jobs) |
| `smoke`, `serve_smoke`, `retrieval_eval` | 1 each | Partially |

**Coverage gate:** `scripts/coverage_gate.sh` — floor `AURA_COVERAGE_MIN:-85` over `internal/*`,
tags `AURA_COVERAGE_TAGS:-"db_integration neo4j_integration"`, `-p 1` mandatory (the integration
tiers share one Postgres; parallel packages race `CREATE ROLE` to `XX000` and deadlock
golang-migrate's advisory lock). Excludes generated `internal/db/sqlc`, the pre-rewrite
`llm/client.go` skeleton, `internal/agent/agenttest` (test support), and `cmd/aura` (CLI glue,
covered behaviourally).

> **`docker_integration` contributes ZERO coverage.** No workflow in `.github/workflows/`
> references the tag (verified: `grep -rn 'docker_integration' .github/workflows/` → no matches).
> The `internal/sandbox/usersandbox` Docker backend therefore counts as **uncovered** against the
> 85% owned-surface floor. Daemon-gated runtime code needs daemon-free unit tests for its pure
> logic (spec/tar builders, path-traversal + symlink guards, nil/disabled early returns,
> "not supported" structural errors) or the aggregate silently drops below the floor.

**Local safety rail:** the gate refuses `db_integration` against a DB named `aura` when
`GITHUB_ACTIONS` is unset (`scripts/coverage_gate.sh:33-43`) — the tier TRUNCATEs shared auth
tables. Use `make coverage-docker` (disposable `aura_cov` DB, dropped on exit).

## CI Workflows

`.github/workflows/`: `ci.yml`, `codeql.yml`, `release.yml`, `skills.yml`.

`ci.yml` jobs (regenerate: `grep -nE '^  [a-z0-9_-]+:$' .github/workflows/ci.yml`):
`build-and-lint`, `unit-test`, `windows-unit`, `cache-invariant`, `vulncheck`, `integration-test`,
`musr-e2e`, `sqlc-golden`, `web-integration-test`, `knowledge-integration-test`,
`multimodal-integration-test`, `telegram-integration-test`, `memory-integration-test`,
`calendar-integration-test`, `integrations-proxy-test`, `web-lint`, `web-test`, `web-mutation`,
`web-dist-freshness`, `web-e2e`, `compaction-evaluator`, `compaction-distributed-gates`,
`compaction-mutation`, `compaction-e2e-acceptance` — 24 jobs.

## Milestone Position

- v0.0.0 (Phases 0-21) shipped 2026-06-15; v1.0.0 web cockpit (Phases 22-30) shipped 2026-06-29;
  latest tag `v1.0.1`. **v2.0.0 (Phases 31-42) in progress.**
- **Phase 37F (conversation/artifact sharing + export) is IN FLIGHT, not shipped.** Migration
  `0040_shared_links` is on disk and `internal/share/` exists (`token.go`, `expiry.go`,
  `snapshot.go`, `redact.go`, `markdown.go`, `jsonfmt.go`), but `.planning/STATE.md:28` reads
  `Phase: 37F … — EXECUTING`. Do not treat public sharing as a shipped capability.

---

*Stack analysis: 2026-07-17*
