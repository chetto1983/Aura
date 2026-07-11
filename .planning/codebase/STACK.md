# Technology Stack

**Analysis Date:** 2026-07-04

## Languages

**Primary:**
- Go 1.26.4 (`go.mod` — `module github.com/chetto1983/aura`) — entire backend: `cmd/aura/`, `internal/*` (~14 top-level packages, ~10k LOC non-test at last codebase map, growing)
- TypeScript 6.0 (strict, via `tsc -b`) — cockpit web frontend, `web/src/`

**Secondary:**
- Python 3.x — `docker/agent-memory/` sidecar (a vendored/adapted `neo4j_agent_memory` package: extraction, enrichment, embeddings, graph client) built into the `aura-agent-memory-mcp` image
- Cypher (Neo4j query language, Cypher 25 dialect) — `internal/knowledge/migrations/*.cypher`
- SQL (PostgreSQL dialect) — `internal/db/migrations/*.sql` (golang-migrate up/down pairs), `internal/db/queries/*.sql` (sqlc source)
- Shell (bash) — `scripts/*.sh` (quality gates, smoke tests, restore drill), `Makefile`

## Runtime

**Environment:**
- Go 1.26.4 toolchain (`go.mod` pins `go 1.26.4`; confirmed via `go version` → `go1.26.4 windows/amd64` in this dev environment)
- Node.js `24.16.0` for the frontend (`.nvmrc`, `.node-version`, `web/package.json` engines `>=24.16.0 <25`)
- npm `>=11 <13` (npm 11 and 12 both supported), pinned `packageManager: npm@12.0.0`; `web/.npmrc` sets `allow-remote=root` so npm 12 can still fetch the xlsx SheetJS CDN tarball (`web/package.json`)
- Docker / Docker Compose — primary deployment/runtime unit for the whole stack (Postgres, Neo4j, sidecars, reverse proxy)

**Package Manager:**
- Go modules (`go.mod` / `go.sum`) — lockfile present, ~180+ direct/indirect deps
- npm (`web/package-lock.json`) — lockfile present

## Frameworks

**Core:**
- None (no web framework in the Go backend) — `net/http` standard library is the HTTP surface for the AG-UI gateway (`internal/agui/server.go`) and web tooling; the CLI is a hand-rolled subcommand switch in `cmd/aura/agent.go`, not Cobra/urfave-cli
- React 19.2 + React Router 7 (`web/package.json`) — cockpit SPA (`web/src/`)
- Vite 8 + `@vitejs/plugin-react` (Babel + React Compiler plugin `babel-plugin-react-compiler`) — frontend build/dev server
- Tailwind CSS 4 (`@tailwindcss/vite`) — styling, token-driven theme (`web/tokens/generate-theme.mjs`)
- `@assistant-ui/react` + `assistant-stream` — chat UI primitives for the AG-UI protocol stream
- `@tanstack/react-query` — server-state/data-fetching layer
- Radix UI primitives (`radix-ui`, `@radix-ui/react-*`) — unstyled accessible component base
- `@react-sigma/core` + `sigma` + `graphology` (+ `graphology-layout-forceatlas2`) — the knowledge-graph visualization view

**Testing:**
- Go: standard `testing` package + table-driven patterns; `go.uber.org/goleak` (goroutine-leak detection); `pgregory.net/rapid` (property-based testing, per PRD §Test discipline); build-tag-gated integration suites (`db_integration`, `neo4j_integration`)
- Go mutation testing: `go-mutesting` (WSL-only fork supporting go1.26)
- Frontend: Vitest 4 (`vitest.config.ts`, `vitest.stryker.config.ts`) + `@vitest/coverage-v8`, `@testing-library/react`, `jsdom`
- Frontend mutation testing: Stryker (`@stryker-mutator/core` + `@stryker-mutator/vitest-runner`, `stryker.config.json` / `stryker.onb.json`) — break threshold 70% killed
- E2E: Playwright (`@playwright/test`, `playwright.config.ts`, `web/e2e/`)
- Accessibility: `axe-core` + a custom contrast checker (`web/scripts/contrast-check.mjs`)

**Build/Dev:**
- `sqlc` v1.31.1 (pinned — v1.27.0 panics on Windows/wazero) — generates `internal/db/sqlc/` from `internal/db/queries/*.sql` against the schema in `internal/db/migrations/` (`sqlc.yaml`)
- `golang-migrate/migrate/v4` — Postgres schema migrations (up/down `.sql` pairs)
- `golangci-lint` v2.12.2 (CI-pinned) — Go linting (`.golangci.yml`, 2.x config format)
- `staticcheck`, `govulncheck`, `dupl`, `deadcode`, `goimports`, `gotestsum` — Go quality toolchain (`make tools`)
- `goreleaser` v2 (`.goreleaser.yaml`) — cross-platform (linux/darwin/windows, amd64/arm64) static binary builds (`CGO_ENABLED=0`) + multi-arch Docker image publish to `ghcr.io/chetto1983/aura`
- `lefthook` (`lefthook.yml`) — git hooks (pre-commit/pre-push) wiring gofmt + quality gates
- ESLint 10 (flat config, `web/eslint.config.js`) + Prettier 3 + TypeScript compiler (`tsc -b`) — frontend static gates
- `knip` — frontend dead-code/dependency detector (`web/knip.json`)
- `jscpd` — frontend duplicate-code detector

## Key Dependencies

**Critical:**
- `github.com/jackc/pgx/v5` v5.9.2 — PostgreSQL driver, paired with `sqlc` (`sql_package: pgx/v5` in `sqlc.yaml`)
- `github.com/neo4j/neo4j-go-driver/v5` v5.28.4 — Neo4j Bolt driver (native Go client; LLM-facing graph access instead goes through the `mcp-neo4j-cypher` MCP server, not this driver directly, per CLAUDE.md)
- `gopkg.in/telebot.v4` v4.0.0-beta.9 — Telegram Bot API client (`internal/channels/telegram/`)
- `github.com/Authula/authula` v1.11.0 (+ `config`, `models`, `plugins/{csrf,email-password,rate-limit,session,totp}`, `services` subpackages) — self-hosted auth framework embedded as the cockpit web-auth provider (`internal/webauth/authula.go`); owns its own `authula` Postgres schema via its bundled `uptrace/bun` ORM migrator
- `github.com/aws/aws-sdk-go-v2` (+ `config`, `credentials`, `service/s3`) — S3-compatible client used against the self-hosted Garage object store (`internal/objectstore/s3.go`)
- `github.com/pkoukk/tiktoken-go` — token counting for LLM context-budget accounting
- `github.com/adhocore/gronx` — cron expression parsing (`internal/cron/`, scheduler)
- `github.com/ag-ui-protocol/ag-ui/sdks/community/go` — AG-UI protocol SDK (agent-to-UI event streaming contract, `internal/agui/`, `internal/agentrender/`)
- `codeberg.org/readeck/go-readability/v2` + `github.com/JohannesKaufmann/html-to-markdown/v2` — web content extraction/conversion for `web_fetch`/document ingestion
- `github.com/PaulSonOfLars/gotg_md2html` — Telegram MarkdownV2 rendering helper

**Infrastructure:**
- `go.opentelemetry.io/otel` (+ `sdk`, `trace`, `exporters/otlp/otlptrace/otlptracegrpc`, `exporters/stdout/stdouttrace`) v1.44.0 — tracing (`internal/obs/`); exporter selectable via `AURA_OTEL_EXPORTER` (`stdout`/`otlp`/`none`)
- `github.com/prometheus/client_golang` + `client_model` — metrics (`internal/agent/metrics.go`, `internal/agui/metrics.go`)
- `github.com/joho/godotenv` — `.env` loading (best-effort, non-fatal if absent) at every boot path
- `github.com/goccy/go-yaml` — YAML parsing (config/skill front-matter)
- `github.com/google/uuid` — identifiers throughout (conversations, identities, run IDs)
- `github.com/mdp/qrterminal/v3` + `rsc.io/qr` — terminal QR rendering for the onboarding device-link flow
- `golang.org/x/crypto`, `golang.org/x/net`, `golang.org/x/sync`, `golang.org/x/text`, `golang.org/x/tools` — stdlib-adjacent extensions (errgroup, singleflight, text normalization for skill-injection blocklist matching, etc.)
- `pgregory.net/rapid` — property-based test generation

## Configuration

**Environment:**
- Root composite loader: `internal/config/config.go` (`Load`/`LoadServe`/`LoadDB`) — reads `.env` via `godotenv.Load()` (best-effort, missing file not fatal), then env vars, composing Postgres DSNs from `POSTGRES_*` primitives + `AURA_DB_*_ROLE` role names (never a single hardcoded DSN)
- Convention: `AURA_<DOMAIN>_<UNIT>` for Aura-native knobs (e.g. `AURA_SWARM_MAX_GOALS`, `AURA_AGUI_BIND`); third-party sidecars keep upstream canonical names (`OPENROUTER_API_KEY`, `TELEGRAM_BOT_TOKEN`, `MULTIMODAL_*`, `STT_*`, `TTS_*`, `NEO4J_*`, `POSTGRES_*`, `GARAGE_RPC_SECRET`)
- LLM config has its own isolated 4-tier load order (`internal/llm/config.go`): built-in default < `.env` (`OPENROUTER_API_KEY`) < `~/.aura/llm.json` < `AURA_LLM_*` env overrides; fails fast with `ErrMissingAPIKey` if the key is empty after all tiers (except `LoadAllowEmptyKey`, used by `serve`/setup so channels can boot before a key is configured)
- `.env` / `.env.example` present at repo root (contents not read — forbidden per this agent's file-access policy; `.env.example` is git-tracked as the operator-facing catalog of ~60 documented vars, per CLAUDE.md §Env vars)
- `~/.aura/llm.json` — optional per-user LLM override file (tier 3 of the LLM load order)
- `~/.aura/agents/<id>/Agent.md` — per-identity profile facts file (`AURA_PROFILE_DIR`)
- `~/.aura/skills/` — active skill root (`AURA_SKILLS_DIR`), plus `~/.aura/skills/export` (`AURA_SKILL_EXPORT_DIR`) mounted read-only into sandboxed skill execution
- `~/.aura/pyscripts/<id>/` — durable Python snippet storage (Slice 7e)

**Build:**
- `go.mod` / `go.sum` — Go module + dependency lock
- `sqlc.yaml` — sqlc codegen config (Postgres schema → typed Go, `pgx/v5` driver)
- `.golangci.yml` — golangci-lint v2 config (errcheck, govet, ineffassign, staticcheck, unused, misspell, gosec, revive, dupl; `internal/db/sqlc`, `internal/agent/tools`, `internal/llm/client.go` excluded as generated/pre-rewrite)
- `.goreleaser.yaml` — release build + multi-arch Docker image pipeline
- `web/vite.config.ts`, `web/vitest.config.ts`, `web/tsconfig.json`, `web/tsconfig.node.json` — frontend build/test/type-check config
- `web/eslint.config.js`, `web/components.json` (shadcn-style component registry) — frontend lint/component scaffolding config
- `Makefile` — canonical local/CI entry points (`quality`, `quality-full`, `coverage`, `sqlc`, `db-*`, `neo4j-*`, `smoke`, `restore-drill`)
- `compose.yaml` (base) + `compose.cloud.yaml` / `compose.gpu.yaml` / `compose.gvisor.yaml` / `compose.llm.yaml` (layered overrides) — full-stack container topology
- `.dockerignore`, `docker/` (per-sidecar Dockerfiles, e.g. `docker/agent-memory/Dockerfile`), `deploy/aura.service` + `deploy/aura-scheduler.service` (systemd units for bare-metal/VM deployment)

## Platform Requirements

**Development:**
- Go 1.26.4 toolchain; `CGO_ENABLED=1` required for `go test -race` (native on WSL/Linux; on Windows needs w64devkit + `BASH_ENV` binutils-shadow fix per CLAUDE.md)
- Node 24.16.0 + npm 11.x for `web/`
- Docker Desktop or native Docker (WSL2 recommended per project memory — full stack currently blocked on a pending RAM upgrade for the local dev host)
- `mcp-neo4j-cypher` (pipx-installed or containerized) on PATH for the Neo4j MCP server and for the coverage-gate integration tier
- golangci-lint v2.12.2, staticcheck, govulncheck, dupl, deadcode, goimports, go-mutesting, gotestsum, lefthook (`make tools`)

**Production:**
- Docker Compose deployment target (primary): `compose.yaml` + optional overlays (`compose.cloud.yaml` for an 8GB mini-PC appliance, `compose.gpu.yaml` for NVIDIA GPU sidecars, `compose.gvisor.yaml` for `runsc`-isolated native Linux/arm64 appliances)
- Cross-platform static binaries published via goreleaser: linux/darwin/windows × amd64/arm64, `CGO_ENABLED=0`
- Multi-arch container image: `ghcr.io/chetto1983/aura` (linux/amd64, linux/arm64)
- Systemd units for non-container deployment: `deploy/aura.service`, `deploy/aura-scheduler.service`
- Reverse proxy/ingress: Caddy 2 (`caddy/Caddyfile`, `compose.yaml` `caddy` service) for HTTPS termination

---

*Stack analysis: 2026-07-04*
