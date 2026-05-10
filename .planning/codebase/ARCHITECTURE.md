<!-- refreshed: 2026-05-10 -->
# Architecture

**Analysis Date:** 2026-05-10

## System Overview

```text
+-------------------------------------------------------------+
|                     User Interfaces                          |
+-------------------+------------------+-----------------------+
|  Telegram Bot     |  Web Dashboard   |  Windows Tray Icon   |
| (internal/telegram)| (web/ + internal/api)| (internal/tray)  |
+-------------------+------------------+-----------------------+
         |                    |                    |
         v                    v                    v
+-------------------------------------------------------------+
|                   Conversation Layer                         |
|  conversation.Context + conversation.Archive + PromptOverlay |
|         `internal/conversation`                               |
+-------------------------------------------------------------+
         |                    ^
         v                    |
+-------------------------------------------------------------+
|      Agent Loop (agentruntime + agentloop)                   |
|  `internal/agentruntime/runner.go`                            |
|  `internal/agentloop/loop.go`                                 |
+-------------------------------------------------------------+
    |                |                      |
    v                v                      v
+------------------+-------------------+--------------------+
| LLM Client       | Tool Registry     | Agent Runner       |
| (failover:       | 50+ tools:        | (AuraBot swarm     |
|  OpenAI+Ollama)  |  wiki, source,    |  worker)           |
| `internal/llm`   |  web, MCP, skills | `internal/agent`   |
|                  |  scheduler,       |                    |
|                  |  sandbox, swarm)  |                    |
|                  | `internal/tools`  |                    |
+------------------+-------------------+--------------------+
         |                                 |
         v                                 v
+-------------------------------------------------------------+
|              Domain Stores & Services                        |
+------------------+-----------------+------------------------+
| Wiki Store       | Source Store     | Search Engine         |
| (MD + frontmatter| (PDF + OCR +     | (chromem + Qdrant +   |
|  + Git tracked)  |  dedup)          |  embed cache)         |
| `internal/wiki`  | `internal/source`| `internal/search`     |
+------------------+-----------------+------------------------+
| OCR Client       | Ingest Pipeline  | Scheduler             |
| (Mistral Doc AI) | (LLM compilation)| (SQLite-backed tasks) |
| `internal/ocr`   | `internal/ingest`| `internal/scheduler`  |
+------------------+-----------------+------------------------+
| Skills Loader    | MCP Clients      | Sandbox (Python)      |
| (progressive      | (stdio + HTTP)   | (process/container/   |
|  disclosure)      | `internal/mcp`   |  pyodide)             |
| `internal/skills` |                  | `internal/sandbox`    |
+------------------+-----------------+------------------------+
         |
         v
+-------------------------------------------------------------+
|                       Persistence                            |
+------------------+-----------------+------------------------+
| SQLite           | File System      | Qdrant (optional)     |
| `aura.db`         | `wiki/`,         | Vector search mirror  |
| (migrations via  |  `skills/`,      | for compact memory    |
|  db/migrations)  |  runtime-workspace|                       |
| `internal/db`    | /                |                       |
+------------------+-----------------+------------------------+
| Garage S3 (optional backup/artifact vault)                   |
| `internal/backup`                                             |
+-------------------------------------------------------------+
```

## Component Responsibilities

| Component | Responsibility | File |
|-----------|----------------|------|
| `cmd/aura/main.go` | Entry point: loads config, bootstraps runtime, wires bot + health server | `cmd/aura/main.go` |
| `telegram.Bot` | Owns all domain services (wiki, search, tools, LLM, scheduler, MCP, sandbox). Wires the full dependency graph during construction. | `internal/telegram/setup.go` |
| `conversation.Context` | Sliding-window message list with Picobot-style cap (50 messages), rolling summarization, speculative wiki search injection | `internal/conversation/context.go` |
| `agentruntime.Run` | Thin orchestration wrapper: emits lifecycle events, delegates to `agentloop.Run` | `internal/agentruntime/runner.go` |
| `agentloop.Run` | Core tool-calling loop: LLM call -> tool dedup -> parallel execute -> result injection. Guardrails: max iterations, duplicate detection, spiral breaker, tiered budgets, retry nudges. | `internal/agentloop/loop.go` |
| `tools.Registry` | Thread-safe tool registry. Registers, dispatches, vector-indexes tools. | `internal/tools/registry.go` |
| `llm.Client` | Interface: `Send()` and `Stream()`. Implementations: OpenAI (primary with retry), Ollama (fallback), combined via `FailoverClient`. | `internal/llm/client.go` |
| `wiki.Store` | File-system wiki in Markdown + YAML frontmatter. Git-tracked, atomically written, file-level mutex. | `internal/wiki/store.go` |
| `source.Store` | Raw source storage with SHA-256 dedup. PDFs + OCR artifacts under `wiki/raw/`. | `internal/source/store.go` |
| `search.Engine` | Chromem-based vector search + SQLite FTS fallback. SHA-keyed embedding cache. | `internal/search/search.go` |
| `api.NewRouter` | Dashboard JSON API with bearer auth. Serves wiki, sources, tasks, skills, MCP, conversations, maintenance, backups, settings, swarm endpoints. | `internal/api/router.go` |
| `agent.Runner` | Self-contained AuraBot worker for swarm delegation. Same LLM+tool loop, no Telegram coupling. | `internal/agent/runner.go` |
| `scheduler.Scheduler` | SQLite-backed task scheduler. Kinds: reminder, wiki_maintenance. Nightly bootstrap at 03:00. | `internal/scheduler/scheduler.go` |
| `sandbox.Manager` | Python code execution runtime. Modes: process (docker), container (pyodide sidecar), pyodide (local). | `internal/sandbox/sandbox.go` |
| `skills.Loader` | Multi-root skill loader with 1s memoization. Progressive disclosure: manifest in prompt, body on `read_skill`. | `internal/skills/loader.go` |
| `mcp.Client` | MCP server integration. Transports: stdio + Streamable-HTTP. Tools registered as `mcp_<server>_<tool>`. | `internal/mcp/client.go` |
| `health.Server` | HTTP server with component health providers. Serves dashboard API + embedded SPA. | `internal/health/server.go` |
| `tray.Tray` | Windows system tray icon. Menu: Open Dashboard, Quit. | `internal/tray/tray.go` |

## Pattern Overview

**Overall:** Monolith with dependency injection via constructor (`telegram.New`). No framework -- single `main.go` wires everything, then starts bot + health server.

**Key Characteristics:**
- Single Go binary (monolith) with embedded React SPA (`//go:embed all:dist`)
- Dependency injection at construction time (`telegram.New` receives all dependencies)
- 1 goroutine = 1 conversation (Telegram chat)
- Tool-calling agent loop with guardrails (max steps, duplicate detection, spiral breaker, tiered budgets)
- Deterministic mode for wiki writes: temperature=0, versioned prompts/schemas
- Multi-provider LLM with failover: OpenAI (primary) + Ollama (fallback)
- Progressive disclosure for skills and MCP tools (only load when model requests)
- SQLite as primary database (single file: `aura.db`), file system for wiki/sources/skills

## Layers

**UI Layer:**
- Purpose: User interaction surfaces
- Location: `internal/telegram/`, `web/src/`, `internal/tray/`
- Contains: Telegram bot handlers, React dashboard components, Windows tray icon
- Depends on: Conversation layer, Agent loop layer
- Used by: End users

**Conversation Layer:**
- Purpose: Context management (sliding window, summarization, prompt overlay, archive)
- Location: `internal/conversation/`
- Contains: `context.go` (message list), `archive.go` (SQLite persistence), `overlay.go` (SOUL.md/USER.md/TOOLS.md injection), `system_prompt.go`
- Depends on: LLM client (for summarization)
- Used by: Telegram handler (`handleConversation`)

**Agent Loop Layer:**
- Purpose: Core tool-calling loop with guardrails
- Location: `internal/agentloop/`, `internal/agentruntime/`
- Contains: `loop.go` (Run loop with dedupe, terminal tools, spiral breaker), `dedupe.go`, `runner.go` (agentruntime event wrapper)
- Depends on: LLM client, Tool executor, State (conversation context)
- Used by: Telegram handler, Agent runner

**Domain Services Layer:**
- Purpose: Core capabilities (tools, search, wiki, sources, scheduler)
- Location: `internal/tools/`, `internal/search/`, `internal/wiki/`, `internal/source/`, `internal/scheduler/`, `internal/ingest/`, `internal/ocr/`, `internal/llm/`
- Contains: Tool registry + 50+ tools, search engine with embedding cache, wiki store, source store with OCR
- Depends on: Persistence layer, External APIs (Mistral, SearXNG, Qdrant)
- Used by: Agent loop, API layer

**Extension Layer:**
- Purpose: Pluggable capabilities (skills, MCP, sandbox, swarm)
- Location: `internal/skills/`, `internal/mcp/`, `internal/sandbox/`, `internal/swarm/`, `internal/swarmtools/`
- Contains: Skills loader (Anthropic format), MCP clients (stdio+HTTP), Python sandbox, AuraBot swarm manager
- Depends on: Domain services, External APIs
- Used by: Tool registry (tools are registered into the shared registry)

**Infrastructure Layer:**
- Purpose: Persistence, configuration, logging, health
- Location: `internal/db/`, `internal/config/`, `internal/logging/`, `internal/health/`, `internal/auth/`, `internal/backup/`, `internal/settings/`, `internal/setup/`, `internal/workspace/`
- Contains: SQLite open/migrate, zap-backed structured logging, bearer auth, Garage S3 backup
- Depends on: External services (optional)
- Used by: All layers

## Data Flow

### Primary Request Path (Telegram message -> response)

1. `internal/telegram/conversation.go:24` -- `handleConversation` receives Telegram message
2. `internal/agentruntime/session.go` -- Session store: checks if existing session exists (by user ID); creates new `conversation.Context` if not
3. `internal/conversation/overlay.go` -- Loads prompt overlay files (SOUL.md, AGENTS.md, USER.md, TOOLS.md) from `PROMPT_OVERLAY_PATH`
4. `internal/telegram/conversation.go:59` -- Composes system prompt: base + runtime info + overlay + skills block
5. `internal/conversation/context.go:57` -- Adds user message, applies speculative search context
6. `internal/agentruntime/runner.go:60` -- `agentruntime.Run` delegates to `agentloop.Run` with full invocation config
7. `internal/agentloop/loop.go:119` -- Agent loop: LLM call via `client.Chat` (streaming), receives response with optional tool calls
8. `internal/agentloop/loop.go:224` -- Deduplicates tool calls, applies duplicate/max-calls policy
9. `internal/telegram/conversation_tool_exec.go` -- `executeToolCalls`: dispatches to `tools.Registry.Execute` for each tool in parallel
10. `internal/agentloop/loop.go:306` -- Terminal tool handler: formats/sanitizes sandbox output, delivers via Telegram
11. `internal/telegram/conversation.go:167` -- Post-turn: archive turn to SQLite, compact tool results, enforce context limits

### Dashboard Request Path

1. `internal/health/server.go` -- Health server handles incoming HTTP requests
2. `internal/api/router.go:147` -- `NewRouter` dispatches to handler functions by method+path
3. `internal/auth/middleware.go` -- `RequireBearer` validates token (SHA-256 hashed in SQLite)
4. Handler reads from domain stores (wiki, sources, scheduler, archive, etc.)
5. Response serialized as JSON

### Sandbox Execution Path

1. Agent requests `execute_code` or `execute_shell`
2. `internal/tools/exec.go` -- `ExecuteCodeTool.Execute` validates code and dispatches to sandbox
3. `internal/sandbox/sandbox.go` -- `Manager.RunCode` delegates to configured runtime
4. Runtime modes: Process runner (`process_runner.go`), Container runner (`pyodide_container_runner.go`), Pyodide runner (`pyodide_runner.go`)
5. Output captured, size-capped, returned as tool result

**State Management:**
- Per-user conversation state: in-memory `conversation.Context` per chat, stored in `agentruntime.SessionStore` (in-memory map keyed by user ID)
- Durable state: SQLite (`aura.db`) for auth tokens, allowlist, pending users, scheduled tasks, conversation archive, proposals, wiki issues, settings, embedding cache
- Wiki state: file system + Git commits
- Source artifacts: file system under `wiki/raw/`
- Compact memory: SQLite FTS + optional Qdrant vector mirror

## Key Abstractions

**`llm.Client`:**
- Purpose: Abstract LLM provider interface with `Send()` and `Stream()` methods
- Examples: `internal/llm/openai.go` (OpenAI-compatible), `internal/llm/ollama.go` (Ollama), `internal/llm/failover_test.go` (failover chain), `internal/llm/retry.go` (retry wrapper)
- Pattern: Interface-based polymorphism with decorator pattern (failover wraps retry wraps provider)

**`tools.Tool`:**
- Purpose: Unit of agent capability. Every tool implements `Name()/Description()/Parameters()/Execute()`
- Examples: `internal/tools/websearch.go`, `internal/tools/mcp.go`, `internal/tools/exec.go`, `internal/tools/scheduler.go`, `internal/tools/files.go`
- Pattern: Interface + shared registry. MCP tools are auto-wrapped via `tools.NewMCPTool`, skills via progressive disclosure

**`agentloop.State`:**
- Purpose: Abstract conversation state that the agent loop reads/writes during tool iterations
- Examples: `internal/conversation/context.go` implements State: `Messages()`, `TrackTokens()`, `AddAssistantMessage()`, `AddToolResultMessage()`
- Pattern: Interface implemented by the conversation layer, consumed by the agent loop

**`agentloop.ChatClient` / `agentloop.ToolExecutor`:**
- Purpose: Pluggable LLM caller and tool execution strategy for the agent loop
- Examples: `internal/telegram/conversation.go:442` implements `ChatClient` with streaming + progressive edit; `internal/telegram/conversation.go:322` wraps tool execution with dynamic tool discovery
- Pattern: Interface-based dependency injection into the loop

## Entry Points

**Primary Entry Point (`cmd/aura/main.go`):**
- Location: `cmd/aura/main.go:50`
- Triggers: Process start (binary execution or `go run ./cmd/aura`)
- Responsibilities: CLI argument parsing, environment loading, config initialization, log setup, database bootstrap + migration, settings reconciliation, first-run wizard (if needed), bot construction, health server start, tray (desktop) or signal wait (headless)

**Command-line commands:**
- `--help` / `--version`: Print info and exit (`cmd/aura/main.go:88`)
- All debug commands under `cmd/debug_*` for smoke testing specific subsystems

**Web Dashboard (`internal/api/`):**
- Location: `internal/api/router.go:147`
- Triggers: HTTP requests to health server (port from `HTTP_PORT`, default `127.0.0.1:8080`)
- Responsibilities: Bearer-authenticated JSON API + served embedded SPA

## Architectural Constraints

- **Threading:** Single-process Go binary. Goroutines: 1 scheduler tick loop, 1 Telegram polling loop, N conversation goroutines (1 per active chat), tool execution goroutines (pool per turn). Tool calls within a single turn are parallelized via goroutines + WaitGroup.
- **Global state:** `telegram.Bot` is the single shared state holder. All domain services live as fields on Bot. `tools.Registry` is the single source of truth for tool availability. `agentruntime.SessionStore` holds per-user conversation contexts. `scheduler.Task` queue lives in SQLite.
- **Circular imports:** Not detected. Packages depend downwards: telegram imports all service packages; service packages are self-contained; infrastructure packages (config, db, logging) have zero internal dependencies.
- **Compile-time code generation:** Tray icon `.ico` embedded via `//go:embed icon_app.ico` (`internal/tray/tray_windows.go:13`). Windows version resource compiled via `goversioninfo` (`Makefile:6`). React SPA embedded via `//go:embed all:dist` (`internal/api/static.go:12`).

## Anti-Patterns

### Constructor-as-Monolith

**What happens:** `telegram.New` (`internal/telegram/setup.go:60`) is a ~630-line function that constructs every service, registers every tool, initializes every connection.
**Why it's wrong:** Adding a new service requires editing a 600+ line function. Testing individual subsystems is hard because they are wired together in one place.
**Do this instead:** Extract domain-level constructors into separate `New*` functions that return their own dependency interfaces, then compose them in a shorter `telegram.New`. Example: `search.NewService(cfg, pool, logger) (*SearchService, error)` returning a facade object.

### Embedded SPA as Build Dependency

**What happens:** The React dashboard is built into `internal/api/dist/` and embedded via `//go:embed` into the Go binary. If the build step hasn't run, the binary starts but the SPA is unavailable.
**Why it's wrong:** Creates a non-hermetic build: `go build ./cmd/aura` produces different results depending on whether `npm run build` was previously run. The fallback (`ErrNoStaticAssets`) is safe but means two build steps are needed.
**Do this instead:** Consider a Go `generate` directive or Makefile target that always runs the web build before `go build`. Currently `make build` does not include `web-build` -- add it as a dependency.

### All-in-One Binary

**What happens:** The single `aura` binary includes the Telegram bot, the dashboard API, the embedded SPA, the scheduler, the MCP clients, the sandbox manager, and the tray icon. Everything runs in one process.
**Why it's wrong:** A crash in any subsystem (e.g., MCP client connection failure, panic in tool execution) can take down the entire application. Resource contention between dashboard traffic and LLM streaming.
**Do this instead:** Consider extracting the dashboard into a separate process (it already has its own HTTP server). Docker Compose already uses sidecars for searxng, garage, qdrant -- the binary remains monolithic inside the aura container. This is acceptable given the local-first design intent.

## Error Handling

**Strategy:** Return errors from functions; log and degrade (never crash) for non-fatal failures.

**Patterns:**
- Tool execution errors are formatted as `tools.FormatToolError(err)` and returned as successful tool results (never crash the loop)
- MCP server connection failures during startup are `logger.Warn` only -- bot continues without that server
- Search engine initialization failures: `logger.Warn`, search disabled
- LLM calls within the agent loop: on failure, return a user-facing apology string rather than aborting
- Scheduler task dispatch errors are logged, never fatal
- `recover()` panics in conversation handlers, not detected globally

## Cross-Cutting Concerns

**Logging:** Structured logging via `zap` (`go.uber.org/zap`). Secret sanitization in log output. Per-turn structured telemetry with 20+ fields (`internal/telegram/conversation.go:177`). Daily rotating log files (`internal/logging/daily_writer.go`).

**Validation:** Tool arguments validated by each tool's `Execute` method. Wiki schema version validated at read time (`internal/wiki/schema.go`). Source ID format validated via regex `^src_[a-f0-9]{16}$` (`internal/api/router.go:260`). Environment variable normalization functions in `internal/config/config.go` (e.g., `NormalizeWorkspaceTools`, `NormalizeTerminalToolPolicy`).

**Authentication:** Telegram: ID-based allowlist (`TELEGRAM_ALLOWLIST` env var + `allowed_users` SQLite table) with pending-user approval flow. Dashboard: bearer tokens hashed (SHA-256) in `api_tokens` SQLite table. Tokens minted via LLM tool `request_dashboard_token` and delivered out-of-band through Telegram.

---

*Architecture analysis: 2026-05-10*
