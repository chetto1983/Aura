# Technology Stack

**Analysis Date:** 2026-05-04

## Languages

**Primary:**
- Go 1.25.5 — All backend logic: Telegram bot, LLM orchestration, wiki engine, search, scheduler, MCP client, API server. Module path: `github.com/aura/aura`.

**Secondary:**
- TypeScript ~6.0.2 — Frontend dashboard in `web/`. Compiled via Vite 8 with `@vitejs/plugin-react`.
- Python (Pyodide) — Sandboxed code execution for `execute_code` tool. Bundled WebAssembly runtime, no host-Python dependency.

## Runtime

**Environment:**
- Go 1.25.5 (native binary, CGO_ENABLED=0 for release builds)

**Package Manager:**
- Go modules (`go.mod`, `go.sum`)
- npm (for frontend in `web/`)
- Lockfile: `go.sum` present; `web/package-lock.json` presumably present for reproducible builds (used via `npm ci` in goreleaser hooks)

## Frameworks

**Core:**
| Framework | Version | Purpose |
|-----------|---------|---------|
| `gopkg.in/telebot.v4` | v4.0.0-beta.7 | Telegram Bot API client — long-polling and webhook transport |
| `github.com/philippgille/chromem-go` | v0.7.0 | In-process vector database for semantic wiki search |
| `modernc.org/sqlite` | v1.50.0 | Pure-Go SQLite driver — database for settings, auth, scheduler, search index, conversations |
| `fyne.io/systray` | v1.12.0 | Windows system tray icon |
| `github.com/go-git/go-git/v5` | v5.18.0 | Git operations (wiki versioning) |
| `github.com/skip2/go-qrcode` | v0.0.0-20200617195104-da1b6568686e | QR code generation for dashboard auth |

**Web Dashboard:**
| Framework | Version | Purpose |
|-----------|---------|---------|
| React | 19.2.5 | UI framework |
| React Router DOM | 7.0.0 | Client-side routing |
| Tailwind CSS | 4.2.4 | Utility-first CSS framework |
| Radix UI | 1.4.3 | Headless accessible UI primitives |
| Shadcn | 4.4.0 | Component system built on Radix/Tailwind |
| Tiptap | 3.22.4 | Rich text editor (wiki pages) |
| Lucide React | 1.11.0 | Icon library |

**Build/Dev:**
| Tool | Version | Purpose |
|------|---------|---------|
| Vite | 8.0.10 | Frontend dev server and production bundler |
| ESLint | 10.2.1 | TypeScript/React linting |
| goreleaser | v2 config | Cross-platform release automation |

## Key Dependencies

**Critical:**
- `gopkg.in/telebot.v4` — All user interaction flows through Telegram. Blocking dependency.
- `modernc.org/sqlite` — Single-file SQLite holds all persistent state (settings, auth tokens, conversations, reminders, wiki FTS5 index, embedding cache). Pure Go, zero CGO.
- `github.com/philippgille/chromem-go` — In-process vector database. No external DB required. Paired with Mistral embeddings API.
- `go.uber.org/zap` v1.28.0 — Structured logging throughout.
- `gopkg.in/yaml.v3` — Wiki page parsing (legacy YAML format and markdown frontmatter).

**Infrastructure:**
- `go.opentelemetry.io/otel` v1.43.0 — Distributed tracing with stdout exporter (enabled via `OTEL_ENABLED=true`).
- `github.com/disintegration/imaging` v1.6.2 — Image processing/resizing.
- `github.com/xuri/excelize/v2` v2.10.1 — Excel spreadsheet reading for document ingestion.
- `github.com/tavily/tavily-go` (referenced but not in go.mod) — Web search fallback.

## Configuration

**Environment:**
- `.env` file for bootstrap secrets (`TELEGRAM_TOKEN`) and runtime paths
- `DB_PATH=./aura.db` — SQLite database location
- `WIKI_PATH=./wiki` — Wiki markdown file store
- `SKILLS_PATH=./skills` — Anthropic-format skill definitions
- `MCP_SERVERS_PATH=./mcp.json` — MCP server configuration
- Most tunables editable at runtime via dashboard Settings page, persisted to SQLite

**Build:**
- `Makefile` — Targets: `all`, `build`, `test`, `run`, `fmt`, `vet`, `clean`, `web`, `web-build`, `ui-dev`
- `.goreleaser.yml` v2 — Cross-platform builds (linux/darwin/windows, amd64/arm64), GitHub release automation
- `web/vite.config.ts` — Frontend build config: Tailwind v4 plugin, `@` path alias, dev proxy to `localhost:8080`, output to `../internal/api/dist/`

## Platform Requirements

**Development:**
- Go 1.25+ toolchain
- Node.js 20+ (for frontend)
- No external database server required (SQLite)

**Production:**
- Single static binary with embedded React dashboard (`//go:embed all:dist`)
- Deployment targets: Windows x86_64, macOS (amd64 + arm64), Linux (amd64 + arm64)
- Optional Pyodide runtime bundle in `runtime/pyodide/` for code execution sandbox

## Additional Features

**Sandbox:**
- Pyodide (WebAssembly Python) for `execute_code` tool
- Configurable via `SANDBOX_ENABLED`, `SANDBOX_RUNTIME_DIR`, `SANDBOX_TIMEOUT_SEC`
- No host-Python fallback

**Search:**
- Primary: chromem-go vector search with Mistral embeddings
- Fallback: SQLite FTS5 full-text search
- Embedding cache in SQLite to avoid redundant API calls on reindex

**Budget Tracking:**
- `internal/budget` — Token cost tracking with soft/hard caps (`SOFT_BUDGET`, `HARD_BUDGET`)

---

*Stack analysis: 2026-05-04*
