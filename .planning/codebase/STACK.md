---
last_mapped_commit: 26745a062dd1017c8e9de39a39089bc63559b553
---

# Technology Stack

**Analysis Date:** 2026-08-13

## Languages

**Primary:**
- Go 1.26.6 (`go.mod:3`) — the daemon (`cmd/aura`), the memory MCP server (`cmd/arcadedb-mcp`), the filecard CLI (`cmd/aura-filecard`), and all `internal/*` packages (~68 packages under `internal/`)
- TypeScript (React 19.2.7) — the web cockpit under `web/src/`, built by Vite and embedded into the Go binary via `internal/webui/dist` (`//go:embed`, see `docker/aura/Dockerfile:6-9`)

**Secondary:**
- Python 3.12 — the document-ingestion sidecar `services/ingest/` (`app.py`, `arcade.py`, `chunk.py`, `extract.py`, `identity.py`, `source.py`), built by `docker/aura-ingest/Dockerfile` on `python:3.12-slim-bookworm`
- Shell (POSIX `sh`/bash) — `scripts/*.sh` quality gates, compose entrypoints, `docker/aura/aura-garage-bootstrap.sh`

## Runtime

**Environment:**
- Go 1.26.6 toolchain (`go.mod:3`); the runtime container builds with `golang:1.26.6-alpine` (`docker/aura/Dockerfile:24`) and the ingest sidecar builds Go 1.26 code (`golang:1.26-bookworm`, `docker/aura-ingest/Dockerfile:16`) purely to compile `aura-filecard` as a vendored binary
- Runtime image: `debian:bookworm-slim` (`docker/aura/Dockerfile:38`), with Node 24 LTS installed via NodeSource for skill-snippet execution (JS skills, `npx skills` self-extension) — orthogonal to the SPA, which is pre-built and embedded (`docker/aura/Dockerfile:53-63`)
- Node.js `>=24.16.0 <25` for the web cockpit build (`web/package.json:6-9`), built in a discardable `node:24-bookworm-slim` stage (`docker/aura/Dockerfile:9`)
- Python 3.12 in the ingest sidecar container only; the main `aura` container also carries `python3`/`python3-pip`/`python3-venv` for agent-driven skill execution (`docker/aura/Dockerfile:48-50`)

**Package Manager:**
- Go modules (`go.mod` / `go.sum`); `go mod download` in the Dockerfile build stage
- npm `>=11 <13` for `web/` (`web/package.json:7`), pinned via `packageManager: npm@12.0.0`; lockfile `web/package-lock.json` present (`npm ci` used in CI/Docker build, `docker/aura/Dockerfile:12`)
- pip for the ingest sidecar, installing pinned versions from `docker/aura-ingest/requirements.txt`

## Frameworks

**Core (Go backend):**
- Standard-library `net/http` for every HTTP surface — no third-party web framework; the AG-UI server (`internal/agui/server.go`), the setup server, and the MCP HTTP transport are all built directly on `net/http`
- `github.com/ag-ui-protocol/ag-ui/sdks/community/go` — AG-UI protocol event types + SSE encoding (`go.mod:11`), consumed by `internal/agui`
- `github.com/modelcontextprotocol/go-sdk` v1.7.0 (`go.mod:27`) — MCP protocol types/schema (`github.com/google/jsonschema-go` is its indirect dependency)
- `github.com/jackc/pgx/v5` v5.10.0 — Postgres driver + connection pooling (`internal/db`)
- `sqlc` (installed via `go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1`, `Makefile:6-8`) — generates `internal/db/sqlc/` (31 files) from `internal/db/queries/`; NOT in `go.mod` (build-time codegen tool, not a runtime dependency)
- `github.com/golang-migrate/migrate/v4` v4.19.1 — Postgres schema migrations, driven by `aura db migrate` (`compose.yaml:341-364`), migration files in `internal/db/migrations/` (94 numbered migrations as of `0094_verification_evidence`, 138 files total incl. down-migrations)
- `github.com/Authula/authula` v1.40.0 — embedded auth provider (`internal/webauth/authula.go`), pulls a large transitive tree (Kafka/NATS/RabbitMQ/Redis/Watermill message-bus adapters, SQLite/MySQL/Bun ORM support) that Aura does not use directly — Aura's own auth path is Postgres-only
- `gopkg.in/telebot.v4` v4.0.0-beta.10 — Telegram Bot API client (`internal/channels/telegram`)
- `github.com/aws/aws-sdk-go-v2` (+ `config`, `credentials`, `service/s3`, `smithy-go`) — S3-compatible client used against Garage (`internal/objectstore/s3.go`)
- `go.opentelemetry.io/otel/*` v1.45.0 family (sdk, trace, metric, otlptracegrpc, stdouttrace) + `go.opentelemetry.io/otel/exporters/prometheus` v0.67.0 — tracing/metrics (`internal/obs`, `internal/tracesink`)
- `github.com/prometheus/client_golang` v1.24.1 — `/metrics` exposition
- `github.com/moby/moby/client` v0.5.1 + `github.com/moby/moby/api` v1.55.0 — Docker Engine API client used by the sandbox router (`internal/sandbox`) to spawn per-identity boxes
- `github.com/pkoukk/tiktoken-go` v0.1.8 — token counting for context budgeting
- `github.com/adhocore/gronx` v1.20.1 — cron expression parsing (`internal/cron`)
- `github.com/goccy/go-yaml` v1.19.2, `github.com/google/shlex`, `codeberg.org/readeck/go-readability/v2`, `github.com/JohannesKaufmann/html-to-markdown/v2`, `github.com/PaulSonOfLars/gotg_md2html` — parsing/rendering helpers (readability extraction, HTML→Markdown, Telegram MarkdownV2)
- `github.com/mdp/qrterminal/v3` + `rsc.io/qr` — terminal QR rendering for the Telegram onboarding/link flow
- `github.com/sergi/go-diff` — diff rendering

**Core (Web cockpit):**
- React 19.2.7 + `react-dom` 19.2.7, `react-router` v8, `@tanstack/react-query` v5 (`web/package.json:33-49`)
- `@assistant-ui/react` 0.14.22 + `@assistant-ui/react-markdown` + `assistant-stream` 0.3.23 — the chat/streaming UI runtime consuming the AG-UI/SSE wire protocol
- `radix-ui` v1.6 + individual `@radix-ui/react-*` primitives (dialog, hover-card, label, slot, tabs, tooltip) — headless UI primitives
- `@svar-ui/react-filemanager` + `@svar-ui/react-core` + `@svar-ui/core-locales` — file-manager UI component
- `cytoscape` 3.34.0 + `cytoscape-fcose` — the memory/knowledge graph explorer view
- `@tiptap/react` + `@tiptap/starter-kit` + `tiptap-markdown` — rich-text composer
- `docx-preview` + `xlsx` (SheetJS CDN build, pinned via URL not npm, `web/package.json:52`) — client-side Office document preview (chosen over server-proxied viewers to keep documents in-origin, see project memory `docx-preview-inorigin-xss-scrub`)
- `shiki` + `@shikijs/langs` + `@shikijs/themes` — code syntax highlighting
- `i18next` + `react-i18next` — internationalization
- Tailwind CSS v4 (`@tailwindcss/vite`) — styling, with `tw-animate-css` and `class-variance-authority`/`clsx`/`tailwind-merge` for variant composition
- No global state library (no Redux/Zustand/Jotai in `web/package.json`) — state is React Query cache + component/context state

**Testing:**
- Go: standard `testing` package + table-driven tests throughout; `go.uber.org/goleak` v1.3.0 (goroutine-leak detection); `pgregory.net/rapid` v1.3.0 (property-based testing); `github.com/testcontainers/testcontainers-go` (+ `modules/postgres`, `modules/mysql`, indirect via Authula) for integration fixtures; build-tag-gated tiers (`db_integration`, `arcadedb_integration`, `docker_integration`, `agent_eval`) compiled/run via `scripts/tagged_tier_compile.sh` and `scripts/coverage_gate.sh`
- Mutation testing: `go-mutesting` (`github.com/avito-tech/go-mutesting`, installed in WSL only — the only fork supporting Go 1.26, per `Makefile:16` and CLAUDE.md)
- Web: Vitest v4 + `@vitest/coverage-v8`, `@testing-library/react`, `jsdom`; `@playwright/test` v1.61 for E2E (`web/package.json:15,66-70`); `@stryker-mutator/core` + `@stryker-mutator/vitest-runner` for mutation testing (`npm run mutation`)
- Python (ingest sidecar): `services/ingest/tests/` (test runner not independently verified in this pass — no `pyproject.toml`/`requirements-dev.txt` found alongside `services/ingest/`)

**Build/Dev:**
- `Makefile` — the canonical local/CI task runner: `make quality` (deadcode, vet, file-size, lint, race tests, vuln, build — no containers), `make quality-full` (+ coverage gate against the live container stack), `make coverage`/`make coverage-docker` (`scripts/coverage_gate.sh` / `scripts/coverage_docker.sh`), `make web-lint`/`make web-test`/`make web-mutation`
- `golangci-lint` v2.12.2 (pinned, `.golangci.yml`, `Makefile:44`) — linters enabled: errcheck, govet, ineffassign, staticcheck, unused, misspell, gosec, revive, dupl (at threshold 100, tests excluded), modernize (Go 1.21+ simplifications)
- `staticcheck`, `govulncheck`, `dupl`, `gotestsum`, `deadcode`, `goimports` — installed via `make tools` into `$GOPATH/bin`
- Vite 8 (`web/vite.config.ts`) — web bundler; `tsc -b` for typechecking; ESLint 10 + Prettier 3.8 for the web lint/format gate
- Docker Compose (`compose.yaml`, top-level `name: aura`) — orchestrates the full stack; `docker/*/Dockerfile` per service (`arcadedb`, `arcadedb-mcp`, `aura`, `aura-egress`, `aura-ingest`, `aura-sandbox`, `garage`)
- lefthook — git hooks manager (installed via `make tools`; lint runs at pre-commit, file-size check + tests at pre-push per project memory)

## Key Dependencies

**Critical:**
- `github.com/jackc/pgx/v5` v5.10.0 — sole Postgres driver; `sqlc`-generated typed queries in `internal/db/sqlc/` sit on top of it
- `github.com/Authula/authula` v1.40.0 — the embedded web-auth/session provider backing `internal/webauth`; a large dependency surface (message brokers, multiple SQL dialects) most of which Aura never exercises
- `github.com/ag-ui-protocol/ag-ui/sdks/community/go` — defines the wire contract (`events.Event`, SSE encoding) the cockpit and any AG-UI-speaking client rely on; `internal/agui` re-exports `agui.Event` so call sites never import the SDK directly
- `github.com/modelcontextprotocol/go-sdk` v1.7.0 — MCP tool/schema types shared by both `internal/mcp` (client, consumed servers) and `cmd/arcadedb-mcp` (hosted server)
- `github.com/moby/moby/client` — Docker Engine API access for `internal/sandbox`'s per-identity box lifecycle (gated to strict deployment profiles only)
- `github.com/aws/aws-sdk-go-v2/service/s3` — S3-protocol client against Garage object storage (`internal/objectstore`)

**Infrastructure:**
- `go.opentelemetry.io/otel/*` + `go.opentelemetry.io/otel/exporters/prometheus` + `github.com/prometheus/client_golang` — metrics/tracing surface consumed by the optional `observability` compose profile (Prometheus, Tempo, Grafana)
- `github.com/pkoukk/tiktoken-go` — token accounting for the context/history budget system
- `codeberg.org/readeck/go-readability/v2` + `github.com/JohannesKaufmann/html-to-markdown/v2` — web-fetch content extraction for the agent's web tools

## Configuration

**Environment:**
- All Aura-native knobs follow `AURA_<DOMAIN>_<UNIT>` (e.g. `AURA_SWARM_MAX_DEPTH`); third-party/upstream credentials keep their canonical name (`TELEGRAM_BOT_TOKEN`, `OPENROUTER_API_KEY`, `MULTIMODAL_*`, `LLAMA_*`, `POSTGRES_*`, `GARAGE_RPC_SECRET`)
- Load order for `llm.Config` (representative pattern used across subsystems, `internal/llm/config.go:199-272`): built-in default < `.env` (`OPENROUTER_API_KEY`) < `~/.aura/llm.json` < `AURA_LLM_*` env — malformed numeric overrides fail fast, malformed booleans silently fall back to default
- `internal/settings` — a Postgres-backed (`aura.settings`, migration 0024) cockpit-editable overlay: `OverlayEnv` writes an allowlisted set of model-backend keys onto the process environment **before** `config.Load` runs at boot, so DB values win over pre-set env for local↔cloud backend swaps (embed/STT/TTS/vision) and the OpenRouter key — connection/security env (`POSTGRES_*`, `ARCADEDB_PASSWORD`, `AURA_WEB_AUTH_SECRET`) is excluded from the overlay
- `.env` file exists at repo root (contents not read — forbidden per this analysis's scope) and feeds `compose.yaml` interpolation plus `godotenv.Load()` calls inside Go config loaders; `.env.example` exists as the template
- `~/.aura/llm.json` — optional operator override file for LLM config (JSON, pointer-typed fields so absent keys don't clobber lower tiers)

**Build:**
- `web/vite.config.ts` — sets `build.outDir = ../internal/webui/dist` so the Go `//go:embed` picks up the SPA build (`docker/aura/Dockerfile:6-8`)
- `.golangci.yml` — lint rule set, `version: "2"`, linter set includes core staple (errcheck, govet, ineffassign, staticcheck, unused) plus misspell (typo detection), gosec (security issues), revive (style), modernize (Go 1.21+ idioms), and dupl (code duplication at threshold 100 for production code, excluded in test files)
- `Makefile` — single source of truth for local/CI parity; `golangci-lint` version there must match `.github/workflows/ci.yml`
- `scripts/go_packages.sh` — resolves the canonical Go package set used consistently across `make vet`/`lint`/`deadcode`/`test`/`test-race`/CI, to avoid drift between local and CI package lists

## Platform Requirements

**Development:**
- WSL2 is the documented primary dev environment for the full quality toolchain (`gcc`/GNU `make`, `CGO_ENABLED=1` native `-race`, `~/go/bin` toolchain) — reaches the Windows Docker Desktop stack via `127.0.0.1`
- Windows-native `.sh` fork/exec is explicitly unsupported for gating (per CLAUDE.md and project memory); Windows is used for editing/IDE only
- Docker Desktop (Windows host) or native Docker (Linux/WSL) required to run `compose.yaml`

**Production:**
- Target platform: Linux containers only (Ubuntu/DGX Spark per project memory `aura-runs-container-ubuntu-only`); `docker/aura/Dockerfile` produces a `linux/amd64`-class `debian:bookworm-slim` runtime image built with `CGO_ENABLED=0 GOOS=linux`
- Optional GPU (NVIDIA, via Compose `deploy.resources.reservations.devices` with `driver: nvidia`) for the embedding sidecar (`aura-llama-embed`), optional local chat LLM (`aura-llm`, `profiles: [localllm]`), and OCR/vision sidecar (`aura-ocr-vl`, `profiles: [ocr]`); a `compose.minipc.yaml` overlay removes GPU reservations for GPU-less hosts (`compose.yaml:691-696` comment)
- Optional `runsc` (gVisor) runtime tier via `AURA_RUNTIME=runsc` (`compose.yaml:20-26`) — native-Linux-only, requires host-level `runsc` registration in `/etc/docker/daemon.json`, not available on Docker Desktop
- Reverse proxy/TLS: Caddy 2 (`caddy` service, `compose.yaml:437-456`), fronting the AG-UI/cockpit port with `AURA_ACCESS_TOKEN`

---

*Stack analysis: 2026-08-13*
