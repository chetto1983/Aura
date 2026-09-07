# Technology Stack

**Analysis Date:** 2026-09-07

## Languages

**Primary:**
- Go 1.26.6 — the whole backend: `cmd/aura` (daemon + CLI), `cmd/arcadedb-mcp`, `cmd/aura-filecard`, `cmd/aura-media-index`, `cmd/aura-ingest-supervisor`, and ~68 packages under `internal/`. Module path `github.com/chetto1983/aura` (`go.mod`). No `toolchain` directive — the `go` line is the floor and CI resolves via `go-version-file: go.mod`.
- TypeScript ~6.0 / TSX — the cockpit SPA in `web/src`, built by Vite and embedded into the Go binary (`internal/webui/embed.go`, `//go:embed all:dist`).

**Secondary:**
- SQL — 188 files (94 up/down pairs, latest `0119_drop_orphan_content_parts`) in `internal/db/migrations/`, plus hand-written queries in `internal/db/queries/` consumed by sqlc.
- Python 3 — only inside the ingestion sidecar image (`docker/aura-ingest/Dockerfile`, `docker/aura-ingest/requirements.txt`). No Python in the repo's own source tree.
- Shell — the gate/verification layer under `scripts/` (`coverage_gate.sh`, `coverage_docker.sh`, `go_packages.sh`, `check-file-size.sh`, `deadcode_gate.sh`, `install.sh`).
- Node (ESM `.mjs`) — installer veneer `scripts/create-aura.mjs` (root `package.json` `bin: create-aura-appliance`) and `web/tokens/generate-theme.mjs`.

## Runtime

**Environment:**
- Go 1.26 (`go.mod`). Release binaries are `CGO_ENABLED=0` static builds for linux/darwin/windows × amd64/arm64 (`.goreleaser.yaml`). CGO is only needed for the `-race` test tier.
- Node.js 24.16.0 pinned by `.nvmrc` and `.node-version`; `web/package.json` `engines` requires `>=24.16.0 <25` with npm `>=11 <13`.
- Docker / Docker Compose — the appliance runtime (`compose.yaml`, `name: aura`); also a *runtime dependency* of the product itself (per-identity sandboxes are containers).
- Optional gVisor isolation tier: `runtime: ${AURA_RUNTIME:-runc}` on the `aura` service.

**Package Manager:**
- Go modules; `go.sum` present; `modules-download-mode: readonly` enforced in `.golangci.yml`.
- npm 12.0.0 declared via `packageManager` in `web/package.json`; `web/package-lock.json` committed.

## Frameworks

**Core (Go):**
- `net/http` stdlib with Go 1.22+ pattern routing (`mux.HandleFunc("GET /api/connect/pim/accounts", …)` in `internal/agui/connect_pim_api.go`) — no web framework.
- `github.com/jackc/pgx/v5` v5.10.0 — Postgres driver/pool.
- sqlc (CLI v1.31.1, config `sqlc.yaml`) — generates `internal/db/sqlc/` from `internal/db/migrations` + `internal/db/queries`, `sql_package: pgx/v5`, `emit_interface: true`.
- `github.com/golang-migrate/migrate/v4` v4.19.1 — migration runner behind `aura db migrate` (`cmd/aura/db.go`).
- `github.com/modelcontextprotocol/go-sdk` v1.7.0 — MCP client and server (`internal/mcp/`, `cmd/arcadedb-mcp/`).
- `github.com/ag-ui-protocol/ag-ui/sdks/community/go` — AG-UI event protocol for the cockpit gateway (`internal/agui/`).
- `github.com/openai/openai-go/v3` v3.54.0 — OpenAI-compatible wire client (`internal/llm/openai_compat/client.go`).
- `gopkg.in/telebot.v4` v4.0.0-beta.10 — Telegram bot (`internal/channels/telegram/bot.go`).
- `github.com/Authula/authula` v1.43.0 — embedded auth provider (`internal/webauth/authula.go`).
- `github.com/moby/moby/client` v0.5.1 + `github.com/moby/moby/api` v1.55.0 — Docker Engine API for the sandbox backend (`internal/sandbox/usersandbox/docker_backend.go`).
- `github.com/adhocore/gronx` v1.20.3 — cron expression evaluation (`internal/cron/`).
- `github.com/lestrrat-go/jwx/v3` v3.2.0 — JWT/JWKS for MCP OAuth (`internal/mcpoauth/`, `internal/webauth/mcp_oauth_server.go`).
- `github.com/aws/aws-sdk-go-v2/service/s3` v1.109.1 — S3 client against Garage (`internal/objectstore/s3.go`).

**Core (frontend):**
- React 19.2 + React DOM, `react-router` v8, `@tanstack/react-query` v5.
- `@assistant-ui/react` v0.15 + `assistant-stream` — the chat surface.
- Radix UI primitives + Tailwind CSS v4 (`@tailwindcss/vite`), `class-variance-authority`, `tailwind-merge`, `tw-animate-css`.
- `shiki`/`@shikijs/*` syntax highlighting, `react-markdown` + `remark-gfm` + `rehype-sanitize`, `@tiptap/react` editor, `cytoscape` + `cytoscape-fcose` for the memory graph view, `@svar-ui/react-filemanager` for the file browser, `i18next`/`react-i18next`.

**Testing:**
- Go stdlib `testing` with build-tag tiers (`db_integration`, `docker_integration`, `arcadedb_integration`, `agent_eval`, live/e2e tiers). `make tagged-tier-compile` enumerates them.
- `go.uber.org/goleak` v1.3.0 — goroutine-leak assertions in `main_test.go` files.
- `pgregory.net/rapid` v1.3.0 — property-based tests (e.g. `internal/gateway/classify_property_test.go`).
- `github.com/testcontainers/testcontainers-go` v0.42.0 (indirect, via Authula) — container fixtures.
- `go-mutesting` (avito-tech fork) — mutation spot-checks, ≥70% killed per critical boundary (`make critical-mutation`).
- Frontend: Vitest 4 + `@vitest/coverage-v8` (≥85% thresholds in `web/vitest.config.ts`), Playwright 1.62 (`web/playwright.config.ts`, `web/e2e/`), Stryker 10 mutation (`web/stryker.config.json`, break=70), `axe-core` for a11y.

**Build/Dev:**
- Vite 8 + `@vitejs/plugin-react` + `babel-plugin-react-compiler` (`web/vite.config.ts`); `npm run build` = `node tokens/generate-theme.mjs && tsc -b && vite build`, output committed to `internal/webui/dist`.
- GoReleaser v2 (`.goreleaser.yaml`) for tagged releases.
- `Makefile` is the gate index (`make help` lists every target).
- `lefthook.yml` — pre-commit (gofmt/vet/golangci-lint on staged packages) and pre-push hooks.
- `golangci-lint` v2.12.2 pinned in `Makefile:tools` and CI.

## Key Dependencies

**Critical:**
- `github.com/jackc/pgx/v5` — every control-plane read/write.
- `github.com/modelcontextprotocol/go-sdk` — both the tool surface Aura consumes and the memory server she publishes.
- `github.com/openai/openai-go/v3` — the single wire client for OpenRouter, llama.cpp, and Ollama alike.
- `github.com/moby/moby/client` — no sandbox without it; `internal/sandbox/usersandbox` is the only backend.
- `github.com/Authula/authula` — cockpit sessions, TOTP, operator identity.

**Infrastructure:**
- OpenTelemetry v1.46.0 (`otel`, `sdk`, `sdk/metric`, OTLP gRPC trace exporter, stdout exporter, Prometheus exporter) — `internal/obs/init.go`, `tracer.go`, `meter.go`.
- `github.com/prometheus/client_golang` v1.24.1 — `/metrics` on `AURA_METRICS_BIND`.
- `github.com/pkoukk/tiktoken-go` v0.1.8 — token accounting for context budgets.
- `github.com/goccy/go-yaml` v1.19.2 — skill frontmatter and config parsing.
- `github.com/google/jsonschema-go` v0.4.3 — tool schemas.
- `codeberg.org/readeck/go-readability/v2` + `github.com/JohannesKaufmann/html-to-markdown/v2` — web fetch text extraction (`internal/web/fetcher_text.go`).
- `github.com/mdp/qrterminal/v3` + `rsc.io/qr` — WhatsApp pairing QR in the terminal.
- `github.com/mattn/go-sqlite3` — local sidecar state.
- `github.com/PaulSonOfLars/gotg_md2html` — Telegram MarkdownV2 rendering (`internal/channels/telegram/mdv2.go`).

**Sidecar (Python, `docker/aura-ingest/requirements.txt`):**
- `cocoindex[amazon_s3,postgres]==1.0.20`, `iscc-tika==0.6.0`, `neo4j==5.28.1`. Both extras are load-bearing (the file documents the exact import failures without them).

## Configuration

**Environment:**
- Convention `AURA_<DOMAIN>_<UNIT>`; **338 distinct `AURA_*` keys** are read from Go source across `internal/` and `cmd/`. Third-party names keep upstream spelling: `POSTGRES_*`, `TELEGRAM_BOT_TOKEN`, `TELEGRAM_API_BASE_URL`, `TELEGRAM_FILE_BASE_URL`, `OPENROUTER_API_KEY`, `OPENAI_API_KEY`, `MULTIMODAL_*`, `STT_*`, `TTS_*`, `SEARXNG_URL`, `ARCADEDB_*`, `LLAMA_ARG_*`, `ASPNETCORE_URLS`, `CALENDAR_MCP_*`, `DESEC_TOKEN`.
- Loader: `internal/config/config.go` plus the split knob files (`config_embed.go`, `config_sandbox.go`, `config_retention.go`, `config_web.go`, `config_agui_run.go`, `config_routes.go`, …); primitives are read via `internal/envutil`. `github.com/joho/godotenv` loads `.env`.
- `.env.example` (635 lines) is the committed catalog; `.env` exists locally and is never read by tooling other than Compose interpolation and the daemon.
- Composed DSNs matter: `AURA_DB_URL` / `AURA_DB_MIGRATE_URL` / `AURA_DB_BOOTSTRAP_URL` are what tests read, while `config.Load` composes them from the `POSTGRES_*` primitives for the CLI.

**Build:**
- `go.mod` / `go.sum`, `sqlc.yaml`, `.golangci.yml`, `Makefile`, `lefthook.yml`, `.goreleaser.yaml`.
- Frontend: `web/vite.config.ts`, `web/tsconfig.json`, `web/tsconfig.node.json`, `web/eslint.config.js`, `web/vitest.config.ts`, `web/playwright.config.ts`, `web/stryker.config.json`, `web/knip.json`, `web/components.json`.
- Images: `docker/aura/`, `docker/arcadedb/`, `docker/arcadedb-mcp/`, `docker/aura-ingest/`, `docker/aura-sandbox/`, `docker/aura-egress/`, `docker/caddy/`, `docker/garage/`.
- Host units: `deploy/aura.service`, `deploy/aura-scheduler.service`, `deploy/aura-image-update.{service,timer,sh}`.

## Platform Requirements

**Development:**
- WSL is the primary environment (CLAUDE.md): `gcc` + GNU make, `CGO_ENABLED=1` for native `go test -race`, Go quality toolchain in `~/go/bin` (`make tools` installs golangci-lint v2.12.2, staticcheck, govulncheck, dupl, gotestsum, deadcode, goimports, go-mutesting, lefthook). `~/.local/bin:~/go/bin` must be prepended to PATH.
- Docker daemon reachable (WSL dials the Windows stack via `127.0.0.1`).
- NVIDIA GPU for the llama.cpp sidecars (`ghcr.io/ggml-org/llama.cpp:server-cuda`); the `localllm` and `ocr` Compose profiles are opt-in.
- Gates: `make quality` (no containers) → deadcode, vet, file-size (600 LOC cap), embedding/LLM model contracts, lint, test-race, vuln, then `go build`. `make quality-full` adds `scripts/coverage_gate.sh` (owned-surface floor 85% plus the per-package policy in `scripts/coverage_package_policy.json`).

**Production:**
- Single-host Docker Compose appliance (`compose.yaml`, project `aura`): 21 services — `aura`, `aura-migrate`, `garage-bootstrap`, `docker-socket-proxy`, `caddy`, `postgres`, `garage`, `arcadedb`, `arcadedb-mcp`, `aura-llama-embed`, `aura-ingest`, `whatsapp`, `searxng`, `aura-stt`, `aura-tts`, `aura-ocr-vl` (profile `ocr`), `aura-pim-mcp`, `aura-llm` (profile `localllm`), `prometheus`/`tempo`/`grafana` (profile `observability`).
- Pinned images: `postgres:18.4-alpine3.24`, `arcadedata/arcadedb:26.9.1-SNAPSHOT` (digest-pinned; ≥26.4.2 enforced by `VerifySecureVersion` for CVE-2026-44221), `dxflrs/garage:v2.3.0`, `tecnativa/docker-socket-proxy:v0.5.0`, `searxng/searxng:2026.7.26`, `prom/prometheus:v3.13.1`, `grafana/tempo:2.9.4`, `grafana/grafana:12.3.9`.
- Published images: `ghcr.io/chetto1983/aura:edge`, `ghcr.io/chetto1983/whatsapp-mcp:latest`, `ghcr.io/chetto1983/aura-pim-mcp:latest`.
- CI: GitHub Actions — `ci.yml` (build-and-lint, unit-test, capability-eval, cache-invariant, vulncheck, observability-contract, integration-test, sqlc-golden, web-integration-test, knowledge-integration-test, arcadedb-integration-test, ingest-sidecar-test, reasoning-tier-test, multimodal-integration-test, telegram-integration-test, whatsapp-integration-test, calendar-integration-test, web-lint, web-test, web-mutation, race-db-integration-gates, sandbox-docker-integration), plus `codeql.yml`, `production-readiness.yml`, `skills.yml`, `release.yml`, `publish-aura-edge.yml`, `publish-arcadedb-mcp.yml`, `retire-aura-images.yml`, `create-aura-appliance.yml`, `claude.yml`.

---

*Stack analysis: 2026-09-07*
