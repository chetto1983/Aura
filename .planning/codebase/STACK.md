# Technology Stack

**Analysis Date:** 2026-05-10

## Languages

**Primary:**
- Go 1.26.2 - Backend application server, Telegram bot, HTTP dashboard, all business logic. Module: `github.com/aura/aura` at `go.mod`

**Secondary:**
- TypeScript 6.0.2 - Frontend dashboard SPA (React). Config: `web/tsconfig.json`, `web/tsconfig.app.json`
- Python 3 - Sandboxed runtime execution via Pyodide (WASM) or system Python in container mode

## Runtime

**Environment:**
- Go binary `aura` / `aura.exe` — compiled single static binary (CGO_ENABLED=0)
- Node.js 22 - Used in Docker container for MCP Node.js-based server tools and Pyodide bundling
- Debian Bookworm Slim - Base image for production Docker container (`Dockerfile`)

**Package Manager:**
- Go modules (`go.mod`, `go.sum`) — dependency management
- npm — frontend dependencies (`web/package.json`)
- Lockfile: `web/package-lock.json` present; Go uses `go.sum`

## Frameworks

**Core (Go):**
- Standard library `net/http` - HTTP dashboard server (serves `/api/*` endpoints and frontend SPA assets)
- `gopkg.in/telebot.v4 v4.0.0-beta.7` - Telegram Bot API framework, the primary user-facing interaction channel
- `modernc.org/sqlite v1.50.0` - CGO-free pure-Go SQLite driver; the entire application state lives in a single `aura.db` file

**Core (Frontend):**
- React 19.2.5 - UI library, `web/src/main.tsx`
- React Router DOM 7 - Client-side routing for dashboard pages, `web/src/App.tsx`
- Vite 8.0.10 - Build tool and dev server. Config: `web/vite.config.ts`. Builds to `internal/api/dist/`
- Tailwind CSS 4.2.4 - Utility-first CSS framework via `@tailwindcss/vite` plugin
- Radix UI (via shadcn) - Headless component primitives (tabs, dialog, tooltip, popover, etc.)
- shadcn/ui v2.4.0 - Component library built on Radix + Tailwind. Config: `web/components.json`

**Support (Frontend):**
- TipTap 3.22.4 - Rich text editor (based on ProseMirror) for wiki page editing, `web/src/components/WikiEditor.tsx`
- i18next 26.0.8 + react-i18next - Internationalization framework, `web/src/i18n/`
- Sonner 2.0.7 - Toast notification system
- Zod 4.3.6 - Schema validation library
- lucide-react 1.11.0 - Icon library
- react-force-graph-2d 1.29.1 - 2D force-directed graph visualization for wiki graph view
- react-select 5.10.2 - Accessible select/dropdown component
- react-markdown 8.0.7 + remark-gfm 3.0.1 - Markdown rendering with GitHub Flavored Markdown
- clsx + tailwind-merge + class-variance-authority - Class name utility chain for shadcn components

**Testing:**
- Go `testing` package - Standard library test runner. Run: `go test ./...`
- Playwright 1.59.1 - End-to-end browser testing for the dashboard. Config: `web/playwright.config.ts`

**Build/Dev:**
- GoReleaser v2 - Cross-platform release automation. Config: `.goreleaser.yml`. Targets: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64
- ESLint 10.2.1 - JavaScript/TypeScript linting. Config: `web/eslint.config.js`
- TypeScript ESLint 8.58.2 - TypeScript-aware ESLint rules
- esbuild (via Vite) - Transpilation of TypeScript/JSX
- rolldown (via Vite 8) - JavaScript bundling
- Tailwind CSS 4 via Vite plugin - CSS processing at build time
- PostCSS 8.5.10 - CSS post-processing (autoprefixer)

## Key Dependencies

**Critical (Go):**
- `gopkg.in/telebot.v4 v4.0.0-beta.7` - Telegram bot framework (core interaction channel)
- `modernc.org/sqlite v1.50.0` - SQLite database engine (application state)
- `github.com/aws/aws-sdk-go-v2/service/s3 v1.100.1` - S3-compatible API client (Garage backups)
- `github.com/philippgille/chromem-go v0.7.0` - Local embedding + vector search library (memory search fallback)
- `github.com/go-git/go-git/v5 v5.18.0` - Git operations in pure Go (workspace/wiki management)
- `go.uber.org/zap v1.28.0` - Structured logging (via slog adapter in `internal/logging/zap_slog.go`)
- `gopkg.in/yaml.v3 v3.0.1` - YAML parsing (config, wiki frontmatter)
- `golang.org/x/net v0.50.0` - Networking utilities
- `fyne.io/systray v1.12.0` - System tray icon support for desktop Windows builds
- `github.com/skip2/go-qrcode` - QR code generation

**Frontend (Web Dashboard):**
- `react` / `react-dom` v19.2.5 - Core UI framework
- `@radix-ui/*` - Headless UI primitives via `radix-ui` meta-package v1.4.3 (shadcn dependency chain)
- `tailwindcss` v4.2.4 - CSS framework

**Document Processing (Go):**
- `github.com/xuri/excelize/v2 v2.10.1` - Excel file parsing
- `github.com/go-pdf/fpdf v0.9.0` - PDF generation
- `github.com/disintegration/imaging v1.6.2` - Image processing
- `github.com/yuin/goldmark v1.8.2` - Markdown parsing (indirect via chromem-go)

**MCP/External Tools:**
- `@executeautomation/database-server` v1.1.0 - MCP database server (Node.js, included in Dockerfile)
- `mail-mcp` v0.4.5 - MCP email server (Rust binary, included in Dockerfile)

## Configuration

**Environment:**
- `.env` file - Primary configuration source for bootstrap settings (Telegram token, HTTP port, DB path, etc.)
- `.env.example` - Documented template with all available options
- Environment variables override `.env` values
- Docker Compose provides its own overrides in `compose.yaml` (container-safe defaults)
- Dashboard Settings page persists runtime configuration into `aura.db` SQLite database
- First-run setup wizard at `http://127.0.0.1:8080` when `TELEGRAM_TOKEN` is empty

**Key configs required:**
- `TELEGRAM_TOKEN` - Telegram Bot API token (required for bot operation)
- `LLM_API_KEY` / `LLM_BASE_URL` / `LLM_MODEL` - LLM provider credentials (dashboard-configurable)
- `HTTP_PORT` - Dashboard listen address (default `127.0.0.1:8080`)
- `DB_PATH` - SQLite database file location (default `./aura.db`)

**Build:**
- `web/vite.config.ts` - Frontend build configuration; outputs to `internal/api/dist/`
- `web/tsconfig.json` - TypeScript configuration with `@/*` path alias mapping to `./src/*`
- `.goreleaser.yml` - Cross-platform release build pipeline
- `Dockerfile` - Multi-stage container build (Go 1.26.2, Node 22, Debian Bookworm Slim)
- `Dockerfile.test` - Test container (Go 1.26.2 + Node 22)
- `compose.yaml` - Docker Compose orchestration (aura, searxng, garage, qdrant, test services)

## Platform Requirements

**Development:**
- Go 1.26.2
- Node.js 20+ (for frontend build and Pyodide bundling)
- npm
- Docker (optional, for containerized dev with compose)
- Windows/macOS/Linux (cross-compiled binary)

**Production:**
- Docker deployment via `ghcr.io/chetto1983/aura` image (linux/amd64, linux/arm64)
- Desktop deployment via goreleaser binaries (Windows x86_64, macOS x86_64/arm64, Linux x86_64/arm64)
- Optional companion services (searxng, garage, qdrant) for full feature set
- SQLite database file requires persistent volume mount (`/data/aura.db`)

---

*Stack analysis: 2026-05-10*
