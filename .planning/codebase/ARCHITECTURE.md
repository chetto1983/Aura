# Architecture

**Analysis Date:** 2026-05-04

## Pattern Overview

**Overall:** Monolithic Go binary with embedded React SPA

**Key Characteristics:**
- Single binary artifact (`aura.exe`) containing both the Telegram bot and a React 19 dashboard
- Go-native concurrency (1 goroutine = 1 conversation, tool calls parallelized with `sync.WaitGroup`)
- SQLite for all structured persistence (auth tokens, scheduled tasks, embedding cache, conversation archive, settings)
- File system + Git for wiki pages (markdown with YAML frontmatter)
- Interface-based dependency injection (e.g., `llm.Client`, `WikiStore`, `SchedulerStore`)

## Layers

**Entry Point:**
- Purpose: Process bootstrap, configuration loading, subsystem wiring, graceful shutdown
- Location: `cmd/aura/main.go`
- Contains: Config loading from `.env` + SQLite settings overlay, first-run setup wizard, logging + tracing init, health server + API router mount, Telegram bot launch, tray icon lifecycle
- Depends on: `internal/config`, `internal/settings`, `internal/setup`, `internal/logging`, `internal/tracing`, `internal/health`, `internal/api`, `internal/telegram`, `internal/tray`

**Telegram Bot Layer:**
- Purpose: Receives Telegram messages, routes to LLM conversation loop, sends responses (plain text, HTML-rendered markdown, document delivery)
- Location: `internal/telegram/`
- Contains: `bot.go` (struct `Bot` — the central orchestrator holding all subsystem references), `handlers.go` (route registration: `/start`, `/login`, text messages, document uploads), `conversation.go` (per-turn LLM loop with speculative search, prompt overlay, skill manifest injection, streaming, archival), `markdown.go` (LLM Markdown → Telegram HTML subset renderer), `streaming.go` (progressive edit at 800ms intervals), `access.go` (allowlist + approval gating), `adapters.go` (tool-to-bot bridge interfaces), `setup.go` (startup wiring), `status.go` (`/status` handler), `documents.go` (PDF handler with bounded concurrency + progress edit), `runtime_settings.go` (`/settings` command), `scheduler_handlers.go` (reminder delivery)
- Key pattern: `Bot` struct aggregates every subsystem via explicit field references — no global state. `sync.Map` used for active conversation tracking (`active`) and per-user conversation contexts (`ctxMap`).

**LLM Client Layer:**
- Purpose: Abstracts LLM API calls (non-streaming + streaming with tool-call accumulation)
- Location: `internal/llm/`
- Contains: `client.go` (interfaces `Client`, type `Request`/`Response`/`Message`/`Token`/`ToolDefinition`/`ToolCall`/`TokenUsage`), `openai.go` (OpenAI-compatible HTTP client — primary), `ollama.go` (Ollama client — fallback/offline), `retry.go` (exponential backoff with `LLM_MAX_RETRIES`), `failover_test.go`
- Key pattern: `Client` interface with `Send(ctx, Request) (Response, error)` and `Stream(ctx, Request) (<-chan Token, error)` methods. `Stream()` accumulates partial `function.arguments` by index and surfaces fully-formed `ToolCall` objects on the final token.

**Agent Runner:**
- Purpose: Reusable bounded LLM/tool loop without Telegram coupling — used by background AuraBot workers
- Location: `internal/agent/`
- Contains: `runner.go` (`Runner` struct with configurable max iterations, timeout, tool timeout; `Run()` method executes a `Task` with an allowlist of tools, parallel tool execution, and tool budget enforcement)
- Key pattern: Decoupled from Telegram — takes `llm.Client`, `tools.Registry`, and a `Task` spec. Parallel tool calls via `sync.WaitGroup` goroutines per call.

**Tool System:**
- Purpose: Registry of callable tools exposed to the LLM for agentic actions
- Location: `internal/tools/`
- Contains: `registry.go` (`Registry` — thread-safe tool dispatch with argument-key-only logging), `tool_registry.go` (`ToolRegistry` — persistent LLM-written Python tools under `wiki/tools/`), plus individual tool files: `wiki.go`/`wiki_maintenance.go`/`wiki_proposal.go`, `source.go`, `ingest.go`, `websearch.go`/`webfetch.go`/`ollama_web.go`, `scheduler.go`, `skills.go`, `mcp.go`, `auth.go`, `files.go`/`xlsx.go`/`docx.go`/`pdf.go`, `memory_search.go`, `daily_briefing.go`, `skill_proposal.go`, `tool_mgmt.go`, `exec.go`, `context.go`, `error.go`
- Key pattern: `Tool` interface (`Name()/Description()/Parameters()/Execute()`). `Registry.Execute()` dispatches by name with `sync.RWMutex` protection. Argument values are never logged — only keys and duration.

**API / Dashboard Layer:**
- Purpose: HTTP JSON API + embedded React SPA for dashboard observability and control
- Location: `internal/api/` + `web/`
- Contains: `router.go` (`NewRouter` — Go 1.22 `ServeMux` with method+path patterns, wraps in `auth.RequireBearer` middleware), individual handler files for each resource (wiki, sources, tasks, skills, MCP, auth, pending users, conversations, summaries, maintenance, settings, swarm), `static.go` (`//go:embed all:dist` SPA serving with deep-link fallback to `index.html`)
- Key pattern: All API routes behind bearer auth. Tokens are minted out-of-band via Telegram (`request_dashboard_token` LLM tool → `Bot.SendToUser`). No public login endpoint. Deps interface pattern (`WikiStore`, `SourceStore`, etc.) enables test fakes.

**Persistence Layer:**
- Purpose: Structured data storage and wiki file-system operations
- Components:
  - `internal/auth/` — Bearer token store (`api_tokens` table), allowlist (`allowed_users` table), pending approval queue (`pending_users` table). Tokens stored as SHA-256 hashes; lookup uses `crypto/subtle.ConstantTimeCompare`.
  - `internal/wiki/` — Wiki page CRUD (`Store`), markdown parser with YAML frontmatter, atomic writes (temp + rename), file-level mutex, schema validation, `SCHEMA.md` documentation
  - `internal/source/` — Source file store under `wiki/raw/`, sha256-based dedup, per-id mutex, atomic `source.json` writes, status tracking (stored/ocr_complete/ingested/failed)
  - `internal/scheduler/` — SQLite-backed scheduled tasks (`scheduled_tasks` table), recurring job scheduler with time context injection, wiki maintenance job runner, issues queue (`wiki_issues` table)
  - `internal/conversation/` — In-memory conversation context (`Context` — sliding window, history cap at `MAX_HISTORY_MESSAGES`, summarization fallback), conversation archive (`ArchiveStore` — `conversations` table with per-turn telemetry), buffered appender (`BufferedAppender` — channel-based async write), prompt overlay loader (`SOUL.md`/`AGENTS.md`/`USER.md`/`TOOLS.md`), summarizer subsystem (`conversation/summarizer/` — `Runner`/`Scorer`/`Dedup`/`Proposals`)
  - `internal/search/` — Vector search via chromem-go (primary), SQLite FTS5 (fallback), embedding cache (SHA-keyed in SQLite, warm restart reuse), concurrent indexing (4 goroutines)
  - `internal/settings/` — Runtime-configurable settings persisted to SQLite, layered on top of env-loaded config

**Extension Layer:**
- Purpose: Pluggable capabilities via skills and MCP
- Components:
  - `internal/skills/` — Anthropic skill format loader (`Loader`), multi-root (`SKILLS_PATH` + `.claude/skills`), memoized for 1s, progressive disclosure (manifest only in system prompt, body on `read_skill`), catalog client (`skills.sh/` API), admin-gated install/delete
  - `internal/mcp/` — stdio + Streamable-HTTP MCP client, JSON-RPC 2.0 wire protocol, auto-registers tools as `mcp_<server>_<tool>`
  - `internal/skill/` — In-process skill execution with sandbox runtime

**Infrastructure Layer:**
- Purpose: Cross-cutting concerns
- Components:
  - `internal/config/` — Env-based config loading (`envconfig`-style, custom implementation), `IsBootstrapped()` check for first-run wizard
  - `internal/health/` — HTTP health server (`Server`), component health providers, process info (version, uptime), QR code generation for Telegram bot link, embed cache stats, compounding rate
  - `internal/logging/` — Structured logging via `go.uber.org/zap` with `slog.Handler` adapter, daily-rotating file logs, secret sanitization
  - `internal/tracing/` — OpenTelemetry integration (opt-in via `OTEL_ENABLED`)
  - `internal/budget/` — Token budget tracking per conversation + global, soft warning + hard halt
  - `internal/tray/` — Windows system tray icon (`fyne.io/systray`), "Open Dashboard" menu item, no-op on other platforms (`tray_other.go`)
  - `internal/setup/` — First-run wizard (loopback HTTP server with setup form, writes `.env` + SQLite settings)
  - `internal/sandbox/` — Isolated Python execution via bundled Pyodide runtime (`Manager`/`Runtime` interface), code validation

**Background Processing:**
- Purpose: Parallel agentic execution and tool discovery
- Components:
  - `internal/swarm/` — AuraBot swarm manager (`Manager` — creates `Run`/`Task` records, dispatches to `AgentRunner`, enforces max active + max depth)
  - `internal/swarmtools/` — LLM-facing tools for swarm management
  - `internal/toolsets/` — Tool grouping abstraction
  - `internal/files/` — File generation tools (`create_xlsx`, `create_docx`, `create_pdf` via `xuri/excelize`, `go-pdf/fpdf`)

## Data Flow

### Primary Flow: Telegram Message → LLM Response

```
User → Telegram API → telebot.v4 → Bot.onMessage()
    │
    ├── isAllowlisted() check
    │   ├── No  → Drop (logged warning)
    │   └── Yes → go handleConversation()
    │
    ├── ctxMap.LoadOrStore(userID, conversation.NewContext)
    │
    ├── conversation.RenderSystemPrompt(time.Now())  // runtime context
    │   + LoadPromptOverlay()   // SOUL.md + AGENTS.md + USER.md + TOOLS.md
    │   + Skills.PromptBlock()  // progressive-disclosure manifest
    │   + SwarmRoutingPrompt()  // if AuraBot enabled
    │
    ├── search.Search(userText, 5)  // speculative wiki retrieval
    │   └── convCtx.SetSearchContext(results)
    │
    ├── for i := 0; i < MAX_TOOL_ITERATIONS; i++:
    │   ├── llm.Stream(ctx, Request{Messages, Tools}) or llm.Send()
    │   │   └── Streaming: progressive Telegram edit (placeholder @30 chars, edit @800ms)
    │   │
    │   ├── HasToolCalls?
    │   │   ├── No  → Render response via Markdown→HTML, Send to Telegram, break
    │   │   └── Yes → Execute tools in parallel (sync.WaitGroup + goroutines)
    │   │              → Append tool results to messages
    │   │              → Continue loop
    │   │
    │   └── Budget check: soft warning → hard halt
    │
    ├── Archive conversation turn (BufferedAppender → conversations table)
    ├── Run summarizer (if enabled, every N turns)
    └── Structured per-turn telemetry log
```

### Dashboard Token Auth Flow

```
User (in Telegram) → "give me dashboard access"
    │
LLM (in conversation loop) → request_dashboard_token tool call
    │
tools/auth.go → auth.Store.Issue(userID)
    │   ├── Generates 32-byte random token (crypto/rand)
    │   ├── Stores SHA-256 hash in api_tokens table
    │   └── Returns plaintext token
    │
Bot.SendToUser(userID, token)  // out-of-band delivery via Telegram
    │
User opens http://localhost:8080/login
    │   → Pastes token in login form
    │   → Stored in localStorage as "aura_token"
    │
Every API request: Authorization: Bearer <token>
    │
auth.RequireBearer middleware:
    │   ├── Extract token from header
    │   ├── auth.Store.Lookup(plaintext)  // SHA-256 hash + ConstantTimeCompare
    │   ├── Valid?   → Update last_used, pass to handler
    │   └── Invalid? → 401 Unauthorized
```

### File Upload Flow (PDF → OCR → Wiki)

```
User (Telegram Document or Dashboard Drop-zone)
    │
source.Store.Put(PutInput{Bytes, Filename})
    │   ├── SHA-256 content hash → dedup check
    │   ├── Create wiki/raw/src_<sha256-16hex>/
    │   ├── Write original.pdf + source.json
    │   └── Return Source{ID, Status: stored}
    │
ocr.Client.Process(source)
    │   ├── POST Mistral /v1/ocr with base64 PDF
    │   ├── Render to ocr.md (Page N headers)
    │   └── Return OCRResult{JSON, MD}
    │
ingest.Pipeline.Compile(ctx, source)
    │   ├── LLM-driven: read OCR output, extract facts
    │   ├── Generate wiki page with [[wiki-links]]
    │   └── Write to wiki.Store
```

## Component Diagram (Text)

```
┌─────────────────────────────────────────────────────────────────┐
│                        cmd/aura/main.go                          │
│  Config → Setup Wizard → Logger → Tracing → HealthServer        │
│  → telegram.Bot → tray.Run                                      │
└──────────┬──────────────┬──────────────────┬────────────────────┘
           │              │                  │
    ┌──────▼──────┐ ┌─────▼─────┐    ┌──────▼──────────┐
    │  Telegram   │ │  API      │    │  Tray           │
    │  Bot        │ │  Router   │    │  (Windows)      │
    │  (telebot)  │ │  (net/http)│   │  fyne/systray   │
    └──┬──┬──┬───┘ └──┬───┬───┘    └─────────────────┘
       │  │  │        │   │
       │  │  │   ┌────┘   └──────────────┐
       │  │  │   │                      │
    ┌──▼──▼──▼───▼──────┐    ┌──────────▼──────────┐
    │   LLM Client      │    │   Auth Store        │
    │   OpenAI / Ollama │    │   SQLite            │
    └───────┬───────────┘    └─────────────────────┘
            │
    ┌───────▼───────────┐
    │   Tool Registry   │
    └──┬──┬──┬──┬──┬───┘
       │  │  │  │  │
  ┌────┘  │  │  │  └──────┐
  │  ┌────┘  │  └───┐     │
  ▼  ▼       ▼      ▼     ▼
 Wiki Source Search Scheduler MCP/Skills
 (FS) (FS) (chromem) (SQLite) (External)
```

## Key Architectural Patterns

### 1. One Goroutine = One Conversation
Each Telegram chat conversation runs in its own goroutine (`go b.handleConversation(c)` in `internal/telegram/handlers.go:32`). No fan-out — determinism by design. Active conversations tracked via `sync.Map`.

### 2. Tool-Call Loop with Parallel Execution
The main conversation loop (`internal/telegram/conversation.go`) iterates up to `MAX_TOOL_ITERATIONS` (default 10). When the LLM returns multiple tool calls in a single turn, they execute in parallel via goroutines + `sync.WaitGroup` (slice 11l). Tool results are appended to the message history and fed back to the LLM.

### 3. Atomic File Writes
Wiki pages, source metadata, and OCR output all use an atomic write pattern: write to a `.tmp` file, then `os.Rename` to the final path. This prevents partial writes from corrupting data on crash. Per-file `sync.Mutex` (keyed by source ID or wiki slug) serializes concurrent writes to the same resource.

### 4. Interface-Based Dependency Injection
The API router defines local interfaces for each store dependency (`WikiStore`, `SourceStore`, `SchedulerStore`, etc.) rather than depending on concrete types. This lets test code substitute fakes without spinning up real disk or SQLite backends. The `Bot` struct in `internal/telegram/bot.go` similarly holds interface references (`llm.Client`, `tools.Registry`).

### 5. Progressive Disclosure (Skills)
Skills follow the Anthropic progressive-disclosure format: the system prompt includes only a compact manifest (`name` + `description`), and the full skill body loads on-demand via the `read_skill` LLM tool. The loader caches results for 1 second to avoid re-reading files on every turn.

### 6. Buffered Appending (Conversation Archive)
To avoid blocking the conversation loop on SQLite writes, the archive uses a `BufferedAppender` with a channel buffer of 100 and a drain goroutine. On channel full, it drops with a warning — durability is best-effort, not blocking.

### 7. Settings Overlay
Config starts from environment variables (`.env`), then a SQLite-backed settings table overlays tunable fields. Bootstrap values (`TELEGRAM_TOKEN`, `HTTP_PORT`, `DB_PATH`, path roots) remain env-only; everything else can be modified at runtime from the dashboard Settings page.

### 8. Graceful Shutdown
`cmd/aura/main.go` bridges `SIGINT`/`SIGTERM` to `tray.Stop()`. The tray icon's Quit menu also calls `tray.Stop()`. On shutdown: archiver drains and closes, scheduler stops, auth DB closes, MCP clients close, telebot stops, health server gracefully shuts down.

## Module Communication

Modules communicate through explicit constructor injection — there are no global singletons or service locators. The `Bot` struct (`internal/telegram/bot.go:34-63`) holds references to every subsystem and wires them together at startup. The API router receives a `Deps` struct (`internal/api/router.go:69-149`) with all store interfaces. Cross-cutting concerns like logging and tracing are passed as parameters or injected via `context.Context`.

Embedding is strictly separated from chat: `EMBEDDING_API_KEY` and `EMBEDDING_BASE_URL` are dedicated Mistral settings. The wiki search never falls back to `LLM_API_KEY`. Similarly, OCR uses dedicated `MISTRAL_API_KEY` — no credential sharing across capabilities.
