# Technology Stack

**Analysis Date:** 2026-08-25

## Languages

**Primary:**
- Go 1.26.6 - Application server, CLI, orchestration, MCP clients/servers, storage adapters, authentication, observability, and integration logic under `cmd/`, `internal/`, and `services/arcadedb-mcp/`; the module declaration and toolchain contract live in `go.mod`.
- TypeScript 6.0.3 with React TSX - Embedded cockpit UI under `web/src/`; compiler settings are defined in `web/tsconfig.json` and the exact toolchain is locked in `web/package-lock.json`.

**Secondary:**
- Python 3.12 - CocoIndex ingestion sidecar under `services/ingest/`, built from `docker/aura-ingest/Dockerfile`; model fine-tuning utilities under `finetune/` have a separate, optional dependency set in `finetune/requirements.txt`.
- SQL - PostgreSQL migrations under `internal/db/migrations/` and sqlc queries under `internal/db/queries/`; ArcadeDB queries are issued from Go in files such as `internal/arcadedb/client.go`.
- Shell, PowerShell, YAML, and Dockerfile syntax - Build, release, deployment, and operational automation in `scripts/`, `deploy/`, `.github/workflows/`, `docker-compose.yml`, and `docker/`.

## Runtime

**Environment:**
- Go 1.26.6 - Required by `go.mod`; production binaries are compiled with `CGO_ENABLED=0` in `docker/aura/Dockerfile` and `.goreleaser.yaml`.
- Node.js 24.16.0 - Cockpit build runtime, pinned by `.nvmrc`, `.node-version`, and the `engines.node` field in `web/package.json`.
- Python 3.12 - Deployed ingestion runtime from `python:3.12-slim-bookworm` in `docker/aura-ingest/Dockerfile`.
- Debian Bookworm - Main production runtime base in `docker/aura/Dockerfile`; the image also supplies document conversion, OCR, PostgreSQL client, Node.js, Python, `uv`, and Git tooling.
- Alpine Linux - Build/runtime base for the ArcadeDB MCP service in `docker/arcadedb-mcp/Dockerfile` and the Go build stage in `docker/aura/Dockerfile`.

**Package Manager:**
- Go modules - Dependencies are declared in `go.mod` and resolved by `go.sum`; both files are present.
- npm 11-12 - `web/package.json` accepts npm `>=11 <13` and declares `npm@12.0.0` as the package manager; `web/package-lock.json` is a lockfile-v3 lockfile and must remain authoritative for exact frontend versions.
- pip - Python dependencies are pinned for the ingestion image in `docker/aura-ingest/requirements.txt`; experimental fine-tuning constraints live separately in `finetune/requirements.txt`.
- `uv` 0.11.32 - Installed in the main application and sandbox images by `docker/aura/Dockerfile` and `docker/aura-sandbox/Dockerfile` for Python tool execution.

## Frameworks

**Core:**
- Go standard-library HTTP stack - The application server and REST/SSE routes are assembled under `cmd/aura/` and `internal/agui/`; use the existing `net/http` handler and middleware patterns when adding endpoints.
- React 19.2.7 and React DOM 19.2.7 - Cockpit component runtime in `web/src/`, with exact versions in `web/package-lock.json`.
- Vite 8.0.16 - Cockpit build and development server configured in `web/vite.config.ts`; production output is written to `internal/webui/dist/` for embedding in the Go binary.
- React Router 8.3.0 - Client-side routing used under `web/src/`; use route definitions and navigation patterns already present there.
- TanStack Query 5.101.0 - Server-state fetching and mutation layer in `web/src/`; keep API state in query hooks rather than duplicating it in component-local caches.
- Tailwind CSS 4.3.1 - Utility CSS pipeline wired through `web/vite.config.ts` and frontend styles under `web/src/`.
- Assistant UI 0.15.14 - Chat and assistant surface in `web/src/`, backed by `@assistant-ui/core` 0.3.13 and `assistant-stream` 0.3.23 as locked in `web/package-lock.json`.
- TipTap 3.29.2 - Rich-text editing used by cockpit components under `web/src/`.
- Cytoscape 3.34.0 - Graph visualization dependency used by cockpit graph views under `web/src/`.
- Svar UI 2.6.0 - Calendar/scheduling UI dependency used under `web/src/`.
- CocoIndex 1.0.20 - Identity-scoped document ingestion and reconciliation in `services/ingest/app.py`, with S3 and PostgreSQL extras pinned in `docker/aura-ingest/requirements.txt`.

**Testing:**
- Go `testing` - Unit and integration tests live beside packages throughout `internal/`, `cmd/`, and `services/arcadedb-mcp/`; run them through targets in `Makefile`.
- `go.uber.org/goleak` 1.3.0 - Goroutine leak assertions in Go tests, declared in `go.mod`.
- `pgregory.net/rapid` 1.3.0 - Property-based Go tests, declared in `go.mod`.
- Vitest 4.1.9 - Frontend unit/component runner configured by `web/vitest.config.ts` and scripts in `web/package.json`.
- Testing Library 16.3.2 - React component testing utilities used by tests under `web/src/`.
- Playwright 1.61.0 - Browser end-to-end tests under `web/e2e/`, configured by `web/playwright.config.ts`.
- Stryker 9.6.1 - Frontend mutation testing configured in `web/stryker.config.json`, with the dedicated Vitest configuration in `web/vitest.stryker.config.ts`.

**Build/Dev:**
- React Compiler - Babel-based React compilation is configured in `web/vite.config.ts`; keep the compiler plugin before the React plugin when changing Vite configuration.
- TypeScript strict mode - `web/tsconfig.json` enables strict checking, `noUncheckedIndexedAccess`, and `exactOptionalPropertyTypes`; new frontend code must satisfy these contracts.
- GoReleaser v2 - Cross-platform packaging, archives, checksums, SBOMs, GitHub Releases, and GHCR images are configured in `.goreleaser.yaml`.
- sqlc v2 - Generates pgx/v5 database code from `internal/db/queries/` and `internal/db/migrations/` into `internal/db/sqlc/`, configured by `sqlc.yaml`.
- golang-migrate 4.19.1 - Applies the migration chain under `internal/db/migrations/`; schema changes must use new paired up/down migrations.
- golangci-lint 2.12.2, Staticcheck, govulncheck, deadcode, goimports, and dupl - Quality tools and versions are coordinated by `Makefile` and hook commands in `lefthook.yml`.
- Docker Compose - Defines the appliance topology, profiles, health checks, networks, volumes, and service dependencies in `docker-compose.yml`.

## Key Dependencies

**Critical:**
- `github.com/Authula/authula` 1.40.0 - Embedded identity provider and authentication plugin system used from `internal/webauth/authula.go`.
- `github.com/jackc/pgx/v5` 5.10.0 - PostgreSQL driver and pool used by the control plane, Authula, generated sqlc code, and persistence packages under `internal/`.
- `github.com/golang-migrate/migrate/v4` 4.19.1 - PostgreSQL schema lifecycle used by startup and migration code under `internal/db/`.
- `github.com/modelcontextprotocol/go-sdk` 1.7.0 - Generic MCP client/server transport, protocol, and OAuth support under `internal/mcp/` and `cmd/arcadedb-mcp/`.
- `github.com/ag-ui-protocol/ag-ui/sdks/community/go` pseudo-version `e9e910b230b9` - AG-UI event and run protocol used by the assistant ingress under `internal/agui/`.
- AWS SDK for Go v2 (`aws` 1.43.7, `config` 1.32.38, `credentials` 1.19.37, `service/s3` 1.107.3) - S3-compatible Garage storage and presigning in `internal/objectstore/s3.go`.
- `gopkg.in/telebot.v4` 4.0.0-beta.10 - Telegram long-polling channel implementation under `internal/channels/telegram/`.
- `codeberg.org/readeck/go-readability/v2` 2.1.2 and `github.com/JohannesKaufmann/html-to-markdown/v2` 2.5.2 - Web-page extraction and Markdown conversion in `internal/web/`.
- `github.com/goccy/go-yaml` 1.19.2 - YAML parsing for configuration and tooling paths declared in `go.mod`.

**Infrastructure:**
- PostgreSQL 18.4 (`postgres:18.4-alpine3.24`) - Durable control-plane and Authula relational store declared in `docker-compose.yml`.
- Garage 2.3.0 (`dxflrs/garage:v2.3.0`) - S3-compatible object store declared in `docker-compose.yml`; the same server binary is copied into the main image by `docker/aura/Dockerfile`.
- ArcadeDB 26.7.3 (`arcadedata/arcadedb:26.7.3`) - Identity-scoped long-term graph/document memory declared in `docker-compose.yml` and accessed by `internal/arcadedb/`.
- llama.cpp server (`ghcr.io/ggml-org/llama.cpp:server-cuda`) - Embedding runtime and optional local chat/OCR runtimes declared as separate services in `docker-compose.yml`.
- faster-whisper server (`hwdsl2/whisper-server:latest`) - Local speech-to-text OpenAI-compatible endpoint declared in `docker-compose.yml`.
- Kokoro FastAPI (`ghcr.io/remsky/kokoro-fastapi-cpu:latest`) - Local text-to-speech OpenAI-compatible endpoint declared in `docker-compose.yml`.
- SearXNG (`searxng/searxng:2026.7.26-b060c780d`) - Local metasearch service declared in `docker-compose.yml` and called by `internal/web/searxng.go`.
- Caddy 2 with the deSEC DNS module - HTTPS ingress built by `docker/caddy/Dockerfile` and configured by `Caddyfile`.
- `ghcr.io/chetto1983/aura-pim-mcp:c497224cf8a0c8eeaea02210d5101b1e032661fb` - Mail, calendar, and contacts MCP sidecar pinned in `docker-compose.yml`.
- `ghcr.io/chetto1983/whatsapp-mcp:sha-9911eb8` - WhatsApp MCP and management sidecar pinned in `docker-compose.yml`.
- Prometheus 3.13.1, Tempo 2.9.4, and Grafana 12.3.9 - Optional observability profile declared with digest-pinned images in `docker-compose.yml` and configured under `observability/`.

## Configuration

**Environment:**
- Application configuration is loaded from environment variables in `internal/config/config.go`; `godotenv.Load()` permits a repository-local `.env` file during development. A `.env` file is present and contains environment configuration; do not read or commit its values.
- LLM configuration precedence is `built-in defaults < .env OPENROUTER_API_KEY < ~/.aura/llm.json < AURA_LLM_*`, implemented in `internal/llm/config.go`.
- Compose deployment variables and required-value guards are declared in `docker-compose.yml`; keep secrets out of source-controlled configuration and inject them at runtime.
- Embedding dimension, revision, and fingerprint form a compatibility contract in `internal/config/config_embed.go`; update storage/index compatibility deliberately when changing the embedding model.
- Managed MCP server definitions and encrypted environment/header material are stored in PostgreSQL by `internal/mcpregistry/store.go`, using the schema introduced by `internal/db/migrations/0101_mcp_server_registry.up.sql`.

**Build:**
- Go module and dependency configuration: `go.mod`, `go.sum`.
- Frontend dependency and compiler configuration: `web/package.json`, `web/package-lock.json`, `web/tsconfig.json`, `web/vite.config.ts`, `web/vitest.config.ts`, and `web/playwright.config.ts`.
- Database generation and migration configuration: `sqlc.yaml`, `internal/db/queries/`, and `internal/db/migrations/`.
- Container and topology configuration: `docker-compose.yml`, `docker/aura/Dockerfile`, `docker/aura-ingest/Dockerfile`, `docker/arcadedb-mcp/Dockerfile`, `docker/aura-sandbox/Dockerfile`, `docker/aura-egress/Dockerfile`, and `docker/caddy/Dockerfile`.
- Release and CI configuration: `.goreleaser.yaml`, `.github/workflows/`, `Makefile`, and `lefthook.yml`.

## Platform Requirements

**Development:**
- Use WSL as the primary full-development environment according to `CLAUDE.md`; Go 1.26.6, Node 24.16.0, npm 11-12, Docker with Compose, and PostgreSQL tooling cover the standard build and verification paths.
- Run Go and web gates through `Makefile` and `web/package.json`; generated cockpit assets under `internal/webui/dist/` must match the `web/` source before packaging.
- Use Docker Compose profiles from `docker-compose.yml` only when their facilities are needed: `ingest`, `localllm`, `ocr`, `sandbox`, and `observability`.
- Use the document/OCR tooling provisioned by `docker/aura/Dockerfile` or `docker/aura-sandbox/Dockerfile` when reproducing production document-processing behavior.

**Production:**
- Deploy as a self-hosted Docker Compose appliance defined by `docker-compose.yml`, with systemd lifecycle support in `deploy/aura.service` and optional native scheduler support in `deploy/aura-scheduler.service`.
- Publish HTTPS through the custom Caddy image from `docker/caddy/Dockerfile`; public wildcard certificates use the deSEC DNS-01 module configured in `Caddyfile`.
- Release binaries target Linux, macOS, and Windows on amd64/arm64, while the appliance image is published to GHCR through `.goreleaser.yaml` and `.github/workflows/release.yml`.
- Persist application state in the named PostgreSQL, Garage, ArcadeDB, and Caddy volumes declared in `docker-compose.yml`; treat the application container filesystem as replaceable.

---

*Stack analysis: 2026-08-25*
