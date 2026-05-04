# Codebase Structure

**Analysis Date:** 2026-05-04

## Directory Layout

```
D:\Aura/
├── .agents/                  # Claude Code agent skill definitions
├── .claude/                  # Claude Code project-specific skills + settings
├── .codex/                   # Codex configuration cache
├── .github/                  # GitHub templates (issues, PRs)
├── .planning/                # GSD planning artifacts
├── cmd/                      # Go entry points (binaries + debug harnesses)
│   ├── aura/                 #   Main binary (Telegram bot + dashboard)
│   ├── build_icon/           #   Tray icon generator
│   ├── debug_agent_jobs/     #   Background agent job smoke test
│   ├── debug_docx/           #   DOCX generation smoke test
│   ├── debug_files/          #   Natural-prompt file creation tests
│   ├── debug_ingest/         #   Ingestion pipeline smoke test
│   ├── debug_llm/            #   LLM connectivity smoke test
│   ├── debug_memory_quality/ #   Memory quality benchmark
│   ├── debug_pdf/            #   PDF generation smoke test
│   ├── debug_settings/       #   Settings system smoke test
│   ├── debug_summarizer/     #   Summarizer runner smoke test
│   ├── debug_swarm/          #   Swarm manager smoke test
│   ├── debug_tools/          #   Tool registry smoke test
│   ├── debug_xlsx/           #   XLSX generation smoke test
│   └── seed_e2e_env/         #   E2E test environment seeder
├── docs/                     # Project documentation
│   ├── plans/                #   Historical GSD plans
│   ├── implementation-tracker.md  # Phase/slice completion tracker
│   ├── llm-wiki.md           #   LLM Wiki pattern documentation
│   ├── picobot-tools-audit.md     # Picobot tool parity audit
│   └── REVIEW.md             #   Cross-phase review issues
├── internal/                 # Go internal packages (30 packages)
│   ├── agent/                #   Reusable LLM/tool loop runner
│   ├── api/                  #   HTTP JSON API + embedded SPA serving
│   ├── auth/                 #   Bearer token + allowlist + pending store
│   ├── budget/               #   Token cost tracking
│   ├── config/               #   Environment configuration loading
│   ├── conversation/         #   Conversation state + archive + summarizer
│   │   └── summarizer/       #     Post-turn knowledge extraction pipeline
│   ├── files/                #   File generation (XLSX/DOCX/PDF)
│   ├── health/               #   Health server + component health providers
│   ├── ingest/               #   Source→Wiki ingestion pipeline
│   ├── llm/                  #   LLM client abstraction (OpenAI + Ollama)
│   ├── logging/              #   Structured logging (zap → slog adapter)
│   ├── mcp/                  #   MCP client (stdio + Streamable HTTP)
│   ├── ocr/                  #   Mistral Document AI OCR pipeline
│   ├── orchestrator/         #   (empty — absorbed into agent runner)
│   ├── sandbox/              #   Isolated Python code execution (Pyodide)
│   ├── scheduler/            #   SQLite-backed job scheduler + maintenance
│   ├── search/               #   Vector search (chromem-go) + FTS fallback
│   ├── settings/             #   Runtime-configurable SQLite settings
│   ├── setup/                #   First-run setup wizard
│   ├── skill/                #   In-process skill execution with sandbox
│   ├── skills/               #   Anthropic skill format loader + catalog
│   ├── source/               #   Immutable source file store
│   ├── swarm/                #   AuraBot swarm manager
│   ├── swarmtools/           #   LLM-facing swarm management tools
│   ├── telegram/             #   Telegram bot integration (telebot.v4)
│   ├── tools/                #   Tool registry + all LLM-callable tools
│   ├── toolsets/             #   Tool grouping abstraction
│   ├── tracing/              #   OpenTelemetry integration
│   ├── tray/                 #   Windows system tray icon
│   └── wiki/                 #   Wiki page store + markdown parser
├── Logo/                     # Project logo assets
├── logs/                     # Daily-rotating log files (runtime)
├── loops/                    # (reserved for future use)
├── reports/                  # Generated report files (runtime)
├── runtime/                  # Sandbox runtimes (Pyodide bundle)
│   └── pyodide/              #   Bundled Pyodide distribution
├── skills/                   # Installed Anthropic-format skills
├── tasks/                    # Task state directory (runtime)
├── test-results/             # Test output artifacts
├── web/                      # React 19 dashboard SPA
│   ├── e2e/                  #   Playwright end-to-end tests
│   ├── public/               #   Static assets (favicon, etc.)
│   └── src/                  #   React source
│       ├── assets/           #     Images, icons
│       ├── components/       #     React components
│       │   ├── common/       #       Shared utilities (skeletons, confirm modals, theme toggle)
│       │   └── ui/           #       shadcn/ui primitives (button, card, dialog, etc.)
│       ├── hooks/            #     Custom React hooks (useApi, useAppTheme, useLocale)
│       ├── i18n/             #     Internationalization (English/Italian)
│       ├── lib/              #     Utility libraries (auth token, utils)
│       └── types/            #     TypeScript API types
├── wiki/                     # Git-tracked wiki pages
│   └── raw/                  #   Source files (PDFs + OCR + metadata)
├── .env.example              #   Safe configuration template
├── .gitignore                #   Git exclusion rules
├── .goreleaser.yml           #   GoReleaser multi-platform build config
├── AGENTS.md                 #   Agent collaboration instructions
├── CONTRIBUTING.md           #   Contribution guidelines
├── go.mod                    #   Go module definition (Go 1.25.5)
├── go.sum                    #   Go dependency checksums
├── INSTALL.md                #   Installation guide
├── License.md                #   Project license
├── Makefile                  #   Build automation (all, build, test, run, web, web-build)
├── mcp.example.json          #   MCP server configuration template
├── pdr.md                    #   Product Design Requirements (detailed)
├── prd.md                    #   Product Requirements Document v4.1
├── prd.json                  #   Machine-readable PRD
├── progress.txt              #   Development progress notes
├── README.md                 #   Project README
├── skills-lock.json          #   Skills installation lock file
├── USER.md                   #   Durable user facts (prompt overlay)
├── SOUL.md                   #   Personality prompt overlay
├── TOOLS.md                  #   Tool guidance prompt overlay
└── VISION.md                 #   Product vision document
```

## Directory Purposes

### `cmd/` — Entry Points
- Purpose: All executable binaries — the main application and 14 debug/smoke-test harnesses
- Contains: Go `main` packages, each a standalone program
- Key files: `cmd/aura/main.go` (242 lines — full startup sequence), `cmd/aura/versioninfo.json` (Windows build metadata), `cmd/aura/resource.syso` (generated — Windows resource embedding)
- Build: Each command builds to a separate binary. `go build ./cmd/aura` produces `aura.exe`

### `internal/` — Core Application Packages
- Purpose: All application logic, organized by subsystem. Enforced by Go — no external import.
- Contains: 30 packages, each self-contained with its own tests
- Pattern: Package imports are directed — leaf packages (e.g., `internal/llm`, `internal/wiki`) have fewer dependencies; orchestrator packages (`internal/telegram`, `internal/api`) wire everything together.

### `web/` — React Dashboard
- Purpose: Single Page Application with React 19, TypeScript, Vite, and shadcn/ui
- Contains:
  - `web/src/App.tsx` — Root component with React Router v7, code-split lazy-imported panels, `RequireAuth` gate
  - `web/src/components/` — Page-level panels (one per dashboard tab: `HealthDashboard`, `WikiPanel`, `SourceInbox`, `TasksPanel`, `SkillsPanel`, `MCPPanel`, `PendingUsersPanel`, `ConversationsPanel`, `SummariesPanel`, `MaintenancePanel`, `SettingsPanel`, `SwarmPanel`, `WikiGraphView`, `WikiPageView`, `WikiEditor`, `Login`) and shared components (`Sidebar`, `Shell`, `ErrorBoundary`, `EventStrip`, `ConversationDrawer`, `StderrLogSheet`, `Markdown`)
  - `web/src/components/common/` — Reusable UI: `AppSkeletons`, `ConfirmModal`, `ErrorCard`, `MarkdownReader`, `ThemeToggle`
  - `web/src/components/ui/` — shadcn/ui primitives: badge, button, card, checkbox, dialog, drawer, input, label, progress, scroll-area, separator, sheet, skeleton, tabs, tooltip
  - `web/src/hooks/` — `useApi.ts` (authenticated fetch wrapper with 401 redirect), `useAppTheme.ts`, `useLocale.ts`
  - `web/src/lib/` — `auth.ts` (token get/set/clear in localStorage), `confirmModal.ts` (custom prompt/confirm dialog), `utils.ts`
  - `web/src/types/api.ts` — TypeScript interfaces for all API response types
  - `web/src/i18n/` — English/Italian locale strings
- Build output: `web/dist/` → embedded into Go binary via `//go:embed all:dist` in `internal/api/static.go`
- Build command: `make web-build` (runs `cd web && npm install && npm run build`)

### `docs/` — Documentation
- Purpose: Project documentation, plans, audits, and review logs
- Contains: `implementation-tracker.md` (slice-level completion tracking), `llm-wiki.md` (LLM Wiki pattern reference), `picobot-tools-audit.md` (Picobot tool parity), `REVIEW.md` (cross-phase review issues), various audit docs, and `docs/plans/` (historical GSD plans)

### `wiki/` — Knowledge Base
- Purpose: Markdown wiki pages maintained by the LLM, versioned in Git
- Structure:
  - Root: `*.md` files (wiki pages with YAML frontmatter)
  - `wiki/raw/` — Immutable source files: `src_<sha256-16hex>/` directories containing `original.pdf`, `source.json`, `ocr.json`, `ocr.md`
  - `wiki/tools/` — Persistent LLM-written Python tools (`.py` files with header comments)
  - `wiki/index.md` — Auto-generated category index
  - `wiki/log.md` — Append-only audit trail
  - `wiki/SCHEMA.md` — Wiki page format documentation
  - `wiki/.git/` — Git repository tracking all wiki changes

### `skills/` — Installed Skills
- Purpose: Anthropic-format AI skills installed from skills.sh catalog
- Structure: `skills/<name>/SKILL.md` with YAML frontmatter (`name`, `description`) and markdown body
- Also loaded from `.claude/skills/` as a secondary root (catalog-installed path)

### `logs/` — Runtime Logs
- Purpose: Daily-rotating structured log files (JSON format, one file per day)
- Generated: Yes
- Committed: No (in `.gitignore`)

### `runtime/` — Sandbox Runtimes
- Purpose: Bundled Pyodide runtime for Python code execution in sandbox
- Contains: `runtime/pyodide/` — Pyodide distribution files
- Generated/Installed: Yes (must be installed before sandbox features work)
- Committed: No (in `.gitignore`)

### `reports/`, `tasks/`, `test-results/`, `loops/`
- Purpose: Runtime artifacts directory
- `reports/` — Generated report outputs
- `tasks/` — Task state persistence
- `test-results/` — Test run output
- `loops/` — Reserved for future agent loop state
- Generated: Yes
- Committed: No

## Key File Locations

**Entry Points:**
- `cmd/aura/main.go`: Main binary — full startup sequence (242 lines), config, logging, tracing, health server, bot launch, tray icon
- `cmd/debug_llm/`: LLM connectivity smoke test
- `cmd/debug_tools/`: Tool registry smoke test
- `cmd/debug_ingest/`: Ingestion pipeline smoke test
- `cmd/debug_xlsx/`: Spreadsheet generation test harness
- `cmd/debug_files/`: Natural-prompt file creation tests
- `cmd/seed_e2e_env/`: E2E environment seeder

**Configuration:**
- `.env.example`: Template with all supported env vars and documentation (80 lines)
- `.env`: Runtime env vars (gitignored, never committed)
- `internal/config/config.go`: Config struct definition + `Load()` function (205 lines)
- `internal/settings/store.go`: SQLite-based runtime settings store
- `mcp.example.json`: MCP server configuration template
- `.goreleaser.yml`: Cross-platform build configuration
- `Makefile`: Build/test/run automation (35 lines)

**Core Logic:**
- `internal/telegram/bot.go`: Central `Bot` struct — holds all subsystem references (159 lines)
- `internal/telegram/conversation.go`: Per-turn LLM conversation loop with speculative search, skill injection, streaming, archival (383 lines)
- `internal/agent/runner.go`: Reusable agent runner for background tasks (371 lines)
- `internal/tools/registry.go`: Thread-safe tool registration and dispatch (107 lines)
- `internal/llm/client.go`: LLM Client interface + request/response types (83 lines)

**API:**
- `internal/api/router.go`: HTTP API route definitions with Go 1.22 method+path patterns (286 lines)
- `internal/api/static.go`: Embedded SPA serving (`//go:embed all:dist`) with deep-link fallback (87 lines)
- `internal/auth/middleware.go`: Bearer token validation middleware
- `web/src/api.ts`: Client-side API fetch wrapper with 401 handling

**Dashboard Pages (web/src/components/):**
- `HealthDashboard.tsx`: Home page — health cards, process info, compounding rate
- `WikiPanel.tsx`: Wiki page listing with search
- `WikiPageView.tsx`: Single wiki page viewer with markdown rendering
- `WikiGraphView.tsx`: Force-directed wiki link graph visualization
- `WikiEditor.tsx`: Wiki page editor
- `SourceInbox.tsx`: Source file management (upload, OCR, ingest)
- `TasksPanel.tsx`: Scheduled task management
- `SkillsPanel.tsx`: Installed skills + catalog browser
- `MCPPanel.tsx`: MCP server tool invocation
- `PendingUsersPanel.tsx`: User approval queue
- `ConversationsPanel.tsx`: Archived conversation browser
- `SummariesPanel.tsx`: LLM-generated summary review queue
- `MaintenancePanel.tsx`: Wiki maintenance issue queue
- `SettingsPanel.tsx`: Runtime settings editor
- `SwarmPanel.tsx`: AuraBot swarm run observability
- `Login.tsx`: Bearer token login form
- `Shell.tsx`: Main layout (sidebar + content area)
- `Sidebar.tsx`: Navigation sidebar with keyboard chord shortcuts
- `EventStrip.tsx`: Real-time event notification strip
- `Markdown.tsx`: Markdown rendering component
- `StderrLogSheet.tsx`: Stderr log viewer

**Testing:**
- Tests are co-located with source files (Go convention: `*_test.go`)
- `web/e2e/`: Playwright end-to-end tests for the dashboard
- Key test files per package: essentially every `.go` file has a companion `_test.go`

## Naming Conventions

**Files:**
- Go: `snake_case.go` (e.g., `source.go`, `store_test.go`, `embed_cache.go`)
- TypeScript/React: `PascalCase.tsx` for components, `camelCase.ts` for utilities and hooks
- Entry points: `main.go` or descriptive names (`debug_llm`, `debug_tools`)

**Directories:**
- Go packages: lowercase, no underscores unless multi-word (`internal/`, `internal/telegram/`)
- Web: flat at `components/` level, `common/` and `ui/` subdirectories for shared/generated code
- Cmd: descriptive lowercase names matching the binary purpose (`cmd/aura`, `cmd/debug_llm`)

**Go package names:**
- Match the directory name (e.g., `package telegram` in `internal/telegram/`)
- Aliased on import when collision is possible: `auraskills "github.com/aura/aura/internal/skills"` when the `skills` variable name conflicts

**Frontend:**
- Components: `PascalCase.tsx` — one file per component
- Hooks: `usePascalCase.ts` — `useApi`, `useAppTheme`, `useLocale`
- Library files: `camelCase.ts` — `auth.ts`, `utils.ts`

## Where to Add New Code

**New Feature (backend):**
- Primary code: New package in `internal/<featurename>/` or new file(s) in an existing package
- Tests: Co-located `*_test.go` files in the same package

**New LLM Tool:**
- Implementation: New file in `internal/tools/` implementing the `Tool` interface
- Registration: Add to tool registration in `internal/telegram/bot.go` setup or `internal/tools/` init

**New API Endpoint:**
- Handler: New file in `internal/api/` (e.g., `internal/api/<resource>.go`)
- Route: Add `mux.HandleFunc()` call in `internal/api/router.go` `NewRouter()`
- Deps: Add any new store/interface fields to the `Deps` struct in `router.go`
- Frontend: Add TypeScript types in `web/src/types/api.ts`, add fetch call in `web/src/api.ts`

**New Dashboard Panel:**
- Implementation: New `PascalCase.tsx` file in `web/src/components/`
- Route: Add `<Route>` entry in `web/src/App.tsx` and sidebar navigation item in `web/src/components/Sidebar.tsx`
- Build: Run `make web-build` to rebuild the embedded SPA

**New Command/Utility:**
- Implementation: New directory in `cmd/<name>/` with a `main.go`

**Utilities:**
- Shared helpers: Add to relevant `internal/` package; avoid a catch-all "utils" package
- Frontend shared helpers: `web/src/lib/utils.ts`

**Configuration:**
- New env var: Add field to `internal/config/config.go` `Config` struct, add `Load()` parsing, add to `.env.example`
- New runtime setting: Add to `internal/settings/`

## Special Directories

**`internal/api/dist/`:**
- Purpose: Embedded SPA build output
- Generated: Yes (by `npm run build` from `web/`)
- Committed: No — built as part of pre-compile step

**`internal/orchestrator/`:**
- Purpose: Originally intended as the orchestrator package — now absorbed into `internal/agent/runner.go` and `internal/telegram/conversation.go`
- Generated: N/A
- Committed: Empty directory (kept for future use)

**`wiki/raw/`:**
- Purpose: Immutable source storage (PDFs, OCR output, metadata)
- Generated: Yes (runtime)
- Committed: Contents gitignored (too large), but directory structure tracked via `.gitignore` exceptions

**`runtime/pyodide/`:**
- Purpose: Bundled Pyodide runtime for sandbox code execution
- Generated: Installed separately
- Committed: No (large binary assets)

**`logs/`, `reports/`, `test-results/`, `tasks/`:**
- Purpose: Runtime output artifacts
- Generated: Yes
- Committed: No

**`.codex/`, `.agents/`, `.claude/`:**
- Purpose: AI assistant configuration (Claude Code, Codex)
- Contains: Installed skills, project-specific settings
- Committed: Partially — `.claude/skills/` may contain catalog-installed skills

---

*Structure analysis: 2026-05-04*
