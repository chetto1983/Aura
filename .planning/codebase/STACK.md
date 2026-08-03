# Technology Stack

**Analysis Date:** 2026-08-03

## Languages

**Primary:**
- Go 1.26.5 - Primary implementation language for the `aura` CLI/daemon, the AG-UI gateway, agent runtime, persistence adapters, and the ArcadeDB MCP server; the module version is authoritative in `go.mod`, with entry points in `cmd/aura/main.go` and `cmd/arcadedb-mcp/main.go`.
- TypeScript 6.0.3 with TSX - Operator cockpit SPA and browser-side API/AG-UI clients under `web/src/`; compiler settings are strict and bundler-oriented in `web/tsconfig.json`, and the version is declared in `web/package.json`.

**Secondary:**
- SQL (PostgreSQL dialect) - Schema evolution and handwritten queries live in `internal/db/migrations/` and `internal/db/queries/`; `sqlc.yaml` generates the typed pgx client into `internal/db/sqlc/`.
- Python 3.12 - Runs the MarkItDown/FastAPI document sidecar in `docker/markitdown/app.py`, release/evidence tooling in `scripts/*.py`, and the optional fine-tuning spike in `finetune/aura_finetune/`.
- POSIX shell and PowerShell - Quality gates, smoke tests, deployment checks, and operator scripts live in `scripts/*.sh` and `scripts/*.ps1`; the primary command surface is coordinated by `Makefile`.
- JavaScript (ES modules), CSS, and HTML - Vite/theme generation and browser tooling use `web/tokens/*.mjs`, `web/scripts/*.mjs`, `web/src/styles/`, and `web/index.html`; the integration console is embedded from `cmd/aura/integrations_console.html`.

## Runtime

**Environment:**
- Go 1.26.5 - Native backend/runtime toolchain from `go.mod`; the production builder uses `golang:1.26.5-alpine` in `docker/aura/Dockerfile`.
- Node.js 24.16.0 - Cockpit build/runtime-tool baseline from `.nvmrc` and `.node-version`; `docker/aura/Dockerfile` uses Node 24 both for the discardable SPA build stage and for runtime skill/snippet tooling.
- Python 3.12 - Sidecar base runtime in `docker/markitdown/Dockerfile`; the main appliance also carries Python 3 tooling for agent-owned document/data work in `docker/aura/Dockerfile`.
- Debian Bookworm slim - Final `aura` appliance base in `docker/aura/Dockerfile`; purpose-built sandbox and egress images also use pinned Debian Bookworm bases in `docker/aura-sandbox/Dockerfile` and `docker/aura-egress/Dockerfile`.

**Package Manager:**
- Go modules - Backend dependency graph is declared in `go.mod` and locked by `go.sum`; no `go.work` workspace is present at the repository root.
- npm 12.0.0 - Frontend package manager is declared in `web/package.json`; `web/package-lock.json` is present with lockfile version 3.
- pip - Python sidecar dependencies are image-pinned in `docker/markitdown/Dockerfile`; fine-tuning ranges are declared without a lockfile in `finetune/requirements.txt`.
- uv/uvx 0.11.32 - Preinstalled in the appliance for agent-driven Python work by `docker/aura/Dockerfile`; it is not the Go or frontend dependency manager.

## Frameworks

**Core:**
- Go standard-library `net/http` - The backend deliberately uses standard HTTP servers, clients, and Go 1.22+ method-pattern routing instead of a web framework; representative composition lives in `cmd/aura/serve.go`, `cmd/aura/serve_webui.go`, and `internal/agui/server.go`.
- React 19.2.7 + React DOM 19.2.7 - Cockpit UI runtime declared in `web/package.json`, bootstrapped from `web/src/main.tsx`.
- Vite 8.0.16 + `@vitejs/plugin-react` 6.0.2 - Frontend development/build pipeline in `web/package.json` and `web/vite.config.ts`; the build emits directly to the Go-embedded directory `internal/webui/dist/`.
- Tailwind CSS 4.3.1 + shadcn/ui/Radix - Styling and source-owned UI primitives are configured by `web/components.json`, `web/src/styles/index.css`, and `web/package.json`; the project uses the New York shadcn style and Lucide icons.
- assistant-ui 0.14.22 + assistant-stream 0.3.23 - Chat interaction/runtime primitives declared in `web/package.json`; the wire-side event contract uses the AG-UI Go SDK from `go.mod` and `internal/agui/`.
- React Router 8.3.0 + TanStack Query 5.101.0 - Client routing and server-state access declared in `web/package.json` and consumed under `web/src/`.
- Authula 1.34.0 - Embedded email/password, TOTP, session, CSRF, and rate-limit authentication framework wired in `internal/webauth/authula.go` and `cmd/aura/serve_auth.go`.
- Model Context Protocol Go SDK 1.7.0 - Streamable-HTTP MCP server support in `cmd/arcadedb-mcp/main.go` and client/transport support under `internal/mcp/`; the dependency is pinned in `go.mod`.
- FastAPI 0.140.6 + Uvicorn 0.51.0 - Document conversion HTTP sidecar defined and pinned in `docker/markitdown/Dockerfile`, with the application in `docker/markitdown/app.py`.

**Testing:**
- Go `testing` + race detector - Backend unit/integration tests are run by `Makefile` and `.github/workflows/ci.yml`; leak and property support come from `go.uber.org/goleak` 1.3.0 and `pgregory.net/rapid` 1.3.0 in `go.mod`.
- Vitest 4.1.9 + Testing Library 16.3.2 + jsdom 29.1.1 - Frontend unit/component suite configured in `web/vitest.config.ts` and declared in `web/package.json`, with an enforced 85% coverage floor.
- Playwright 1.61.0 - Browser E2E suite under `web/e2e/`, configured by `web/playwright.config.ts` and invoked from `web/package.json` and `.github/workflows/ci.yml`.
- Stryker 9.6.1 - Frontend mutation gate configured by `web/stryker.config.json` and `web/vitest.stryker.config.ts`; the command is declared in `web/package.json`.

**Build/Dev:**
- GNU Make - Primary local quality, generation, integration, and service orchestration targets in `Makefile`.
- Docker BuildKit + Docker Compose - Multi-stage application/sidecar builds under `docker/` and the self-hosted stack in `compose.yaml`; CPU/minipc overrides live in `compose.minipc.yaml`.
- GoReleaser 2.17.1 - Cross-platform archives, checksums, SBOMs, and multi-architecture GHCR image publication configured in `.goreleaser.yaml` and `.github/workflows/release.yml`.
- sqlc 1.31.1 - Typed PostgreSQL code generation is pinned by the install instruction in `Makefile` and configured in `sqlc.yaml`.
- golangci-lint 2.12.2 - Go lint/format gate pinned in `Makefile` and `.github/workflows/ci.yml`, with enabled rules in `.golangci.yml`.
- ESLint 10.5.0 + Prettier 3.8.4 - Typed frontend linting, accessibility rules, import ordering, and formatting configured in `web/eslint.config.js` and declared in `web/package.json`.
- React Compiler 1.0.0 - Babel compiler pass runs before the React Vite plugin in `web/vite.config.ts`; versions are declared in `web/package.json`.

## Key Dependencies

**Critical:**
- `github.com/jackc/pgx/v5` 5.10.0 - PostgreSQL pooling, queries, transactions, and integration boundaries throughout `internal/db/`, `internal/conversations/`, `internal/documents/`, and `cmd/aura/`; pinned in `go.mod`.
- `github.com/golang-migrate/migrate/v4` 4.19.1 - Embedded forward migration runner in `internal/db/migrate.go`, pinned in `go.mod`.
- `github.com/aws/aws-sdk-go-v2/service/s3` 1.106.0 - Garage/S3 object storage and presigned uploads in `internal/objectstore/s3.go`, pinned in `go.mod`.
- `github.com/ag-ui-protocol/ag-ui/sdks/community/go` - AG-UI types and SSE encoding used by `internal/agui/translator.go`, `internal/agui/server_sse.go`, and the cockpit transport; the pseudo-version is pinned in `go.mod`.
- `github.com/modelcontextprotocol/go-sdk` 1.7.0 - MCP streamable-HTTP server implementation in `cmd/arcadedb-mcp/`; Aura's client abstractions live under `internal/mcp/`.
- `gopkg.in/telebot.v4` 4.0.0-beta.10 - Telegram long-polling channel implemented in `internal/channels/telegram/`, pinned in `go.mod`.
- `github.com/Authula/authula` 1.34.0 - Embedded cockpit authentication boundary in `internal/webauth/authula.go`, pinned in `go.mod`.
- `go.opentelemetry.io/otel` 1.44.0 + `github.com/prometheus/client_golang` 1.24.1 - Traces, metrics, and private scrape handler in `internal/obs/`, pinned in `go.mod`.
- `github.com/moby/moby/client` 0.5.0 - Docker-backed per-identity sandbox lifecycle under `internal/sandbox/usersandbox/`, pinned in `go.mod`.
- `codeberg.org/readeck/go-readability/v2` 2.1.2 + `github.com/JohannesKaufmann/html-to-markdown/v2` 2.5.2 - Web fetch extraction/normalization used under `internal/web/`, pinned in `go.mod`.
- `cytoscape` 3.34.0 + `cytoscape-fcose` 2.2.0 - ArcadeDB Studio-compatible cockpit memory graph visualization, declared in `web/package.json` and isolated under `web/src/graph/`.
- TipTap 3.29.2, Shiki 4.2.0, and React Markdown 10.1.0 - Rich document/chat rendering declared in `web/package.json` and consumed under `web/src/chat/` and `web/src/documents/`.

**Infrastructure:**
- PostgreSQL 18.4 Alpine - Primary control-plane/document/auth database service in `compose.yaml`; runtime and migration roles are separated in `internal/db/config.go` and `internal/db/migrate.go`.
- ArcadeDB 26.7.3 - Long-term graph/vector memory service pinned in `compose.yaml`; `internal/arcadedb/client.go` independently refuses versions below 26.4.2.
- Garage 2.3.0 - S3-compatible asset store and admin API image pinned in `compose.yaml` and copied into the appliance by `docker/aura/Dockerfile`.
- SearXNG 2026.7.26 build - Local search aggregator pinned in `compose.yaml`, configured by `searxng/settings.yml`, and called by `internal/web/searxng.go`.
- llama.cpp server images - Local LLM, embedding, and optional OCR/vision OpenAI-compatible endpoints are declared as `aura-llm`, `aura-llama-embed`, and `aura-ocr-vl` services in `compose.yaml`; CPU overrides are in `compose.minipc.yaml`.
- faster-whisper and Kokoro sidecars - Speech-to-text and text-to-speech services are declared as `aura-stt` and `aura-tts` in `compose.yaml`, with clients in `internal/multimodal/stt.go` and `internal/multimodal/tts.go`.
- Caddy 2 - Appliance TLS/reverse-proxy edge in `compose.yaml` and `caddy/Caddyfile`; it uses an internal CA and exposes only HTTPS by default.
- Prometheus 3.13.1, Tempo 2.9.4, Grafana 12.3.9 - Optional observability profile pinned in `compose.yaml`, configured under `observability/`.

## Configuration

**Environment:**
- Root configuration is environment-first with a best-effort `.env` load in `internal/config/config.go` and `cmd/aura/main.go`; `.env` and `.env.example` are present at the repository root, but their contents are intentionally not part of this map.
- LLM configuration uses the explicit precedence `built-ins < .env < ~/.aura/llm.json < environment overrides` in `internal/llm/config.go`; OpenRouter is the built-in provider, while private/local OpenAI-compatible URLs may run without a key through `cmd/aura/llm_client.go`.
- MCP configuration merges `~/.aura/mcp/servers.json`, `AURA_MCP_SERVERS_JSON`, active profiles, and default-on catalog recipes in `internal/mcp/managed_config.go`, `internal/config/config_mcp.go`, and `internal/mcp/manager/catalog.go`.
- Operator/runtime knobs use `AURA_<DOMAIN>_<UNIT>` names, while provider-issued variables retain upstream names; the concrete catalog and defaults are implemented across `internal/config/config.go`, `internal/config/config_knobs.go`, `internal/llm/config.go`, and `internal/channels/telegram/config.go`.
- Secret-bearing MCP registry files are written with user-only permissions by `internal/mcp/managed_config.go`; per-identity object-store secrets are encrypted before storage by `internal/objectstore/identity_store.go`.

**Build:**
- Go module and generation inputs: `go.mod`, `go.sum`, `sqlc.yaml`, `.golangci.yml`, and `Makefile`.
- Frontend build inputs: `web/package.json`, `web/package-lock.json`, `web/tsconfig.json`, `web/vite.config.ts`, `web/vitest.config.ts`, `web/eslint.config.js`, and `web/components.json`.
- Container/deployment inputs: `docker/aura/Dockerfile`, `docker/arcadedb-mcp/Dockerfile`, `docker/markitdown/Dockerfile`, `compose.yaml`, `compose.minipc.yaml`, and `caddy/Caddyfile`.
- Release/CI inputs: `.goreleaser.yaml`, `.github/workflows/ci.yml`, `.github/workflows/codeql.yml`, `.github/workflows/production-readiness.yml`, and `.github/workflows/release.yml`.

## Platform Requirements

**Development:**
- Use Go 1.26.5, Node 24.16.0, npm 11-12, Docker Engine with Compose, Git, and GNU Make as reflected in `go.mod`, `.nvmrc`, `web/package.json`, `Makefile`, and `compose.yaml`.
- WSL/Linux is the full primary quality environment because native CGO/race, Docker-backed integration tiers, and the Go quality toolchain are required by `CLAUDE.md`, `Makefile`, and `.github/workflows/ci.yml`.
- GPU is optional: the default compose routes local embedding/OCR/LLM services to CUDA llama.cpp images in `compose.yaml`, while `compose.minipc.yaml` provides CPU-oriented overrides and OCR/local-LLM services are profile-gated.
- A running Postgres/ArcadeDB/Garage/sidecar stack is required only for the corresponding tagged integration tiers; service bring-up targets are documented and implemented in `Makefile` and `.github/workflows/ci.yml`.

**Production:**
- Primary deployment target is a self-hosted Linux appliance using Docker Compose and the systemd wrapper in `deploy/aura.service`; `compose.yaml` keeps data/control ports loopback-only and publishes Caddy HTTPS.
- The long-lived scheduler/runtime can also run as the native systemd user service defined in `deploy/aura-scheduler.service`, with runtime environment supplied outside the repository from `~/.aura/env`.
- GoReleaser publishes native `aura` archives for Linux, macOS, and Windows on amd64/arm64 plus a Linux multi-architecture GHCR appliance image, as configured in `.goreleaser.yaml` and `.github/workflows/release.yml`.
- Releases are self-hosted artifacts rather than a managed cloud deployment: GitHub Releases and GHCR are the distribution targets in `.github/workflows/release.yml`, while runtime hosting is owned by `compose.yaml`, `caddy/Caddyfile`, and `deploy/aura.service`.

---

*Stack analysis: 2026-08-03*
