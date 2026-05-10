---
focus: arch
generated: 2026-05-10
last_mapped_commit: 6b6fa8245e19b9f49fb48e39a28a424cdfcda03f
---

# Structure — Aura Codebase

## Top-Level Layout

```
.
├── cmd/                  # 22 entry-point packages (1 main + 21 debug/tool CLIs)
├── internal/             # ~41 domain packages — all application logic
├── web/                  # Vite + React 19 dashboard (TypeScript)
├── docker/               # Docker support files (pyodide image)
├── data/                 # Runtime data — secrets, caches
├── .github/workflows/    # CI/CD: Docker image build on tag, GoReleaser
├── Dockerfile            # Multi-stage Go + Node build
├── Dockerfile.test       # Test-only Docker image
├── compose.yaml          # Production compose (11 services)
├── compose.image.yaml    # Pre-built image compose variant
├── Makefile              # Build/test targets
├── go.mod / go.sum       # Go 1.26.2 module
└── .goreleaser.yml       # Release automation
```

## Entry Points — `cmd/`

| Directory | Purpose |
|---|---|
| `cmd/aura/` | **Main application** — tray, HTTP server, Telegram bot, scheduler |
| `cmd/seed_e2e_env/` | E2E test data seeding |
| `cmd/build_icon/` | Tray icon generator |
| `cmd/debug_agent_jobs/` | Agent job lifecycle debugging |
| `cmd/debug_db_recover/` | SQLite recovery tool |
| `cmd/debug_qdrant/` | Qdrant vs chromem comparison |
| `cmd/debug_sandbox/` | Sandbox execution smoke tests |
| `cmd/debug_searxng/` | SearXNG integration debugging |
| `cmd/debug_telegram_sandbox/` | Telegram sandbox interaction debugging |
| `cmd/debug_memory_closure/` | Memory hygiene / closure audit |
| `cmd/debug_memory_quality/` | Memory quality checks |
| `cmd/debug_files/` | File extraction testing (docx, pdf, xlsx) |
| `cmd/debug_ingest/` | Ingestion pipeline debugging |
| `cmd/debug_llm/` | LLM client debugging |
| `cmd/debug_settings/` | Settings application debugging |
| `cmd/debug_swarm/` | Swarm orchestration debugging |
| `cmd/debug_tools/` | Tool execution debugging |
| `cmd/debug_backup/` | Backup/export debugging |
| `cmd/debug_docx/`, `cmd/debug_pdf/`, `cmd/debug_xlsx/` | Per-format file extraction debugging |

Every `cmd/*/` entry has a corresponding `main_test.go` verifying CLI behaviour and invariants.

## Application Core — `internal/`

### Request & Routing Layer
| Package | Responsibility |
|---|---|
| `internal/api/` | HTTP API router, handlers, middleware, static assets, MCP endpoints, writes |
| `internal/api/dist/` | Embedded React dashboard build output |
| `internal/auth/` | Bearer token middleware + store |
| `internal/mcp/` | MCP client — stdio/SSE transport |
| `internal/mcppolicy/` | MCP server access policy enforcement |

### Agent System
| Package | Responsibility |
|---|---|
| `internal/agent/` | Core agent runner — tool invocation loop |
| `internal/agentloop/` | Deduplication, guardrails, loop management |
| `internal/agentruntime/` | Session management, terminal interaction, runner orchestration |
| `internal/swarm/` | Multi-agent swarm orchestration (manager, plan, store) |
| `internal/swarmtools/` | Delegation policy tools for swarm agents |
| `internal/budget/` | Token/step budget tracking |

### Tools Framework
| Package | Responsibility |
|---|---|
| `internal/tools/` | 30+ tool implementations: exec, search, web, files, ingest, memory, MCP, scheduler, auth |
| `internal/toolsets/` | Tool grouping/selection |
| `internal/skill/` | Per-skill sandbox management (platform-specific) |

### Data & Storage
| Package | Responsibility |
|---|---|
| `internal/db/` | SQLite database handle + connection management |
| `internal/db/migrations/` | Schema migrations (with testdata snapshots) |
| `internal/dbrecovery/` | Corrupted database recovery tooling |
| `internal/settings/` | User settings CRUD + defaults + application |
| `internal/conversation/` | Conversation storage, context assembly, archival, summarization |
| `internal/conversation/summarizer/` | LLM-driven conversation summarization |
| `internal/wiki/` | Wiki page CRUD, memory hygiene, graph, schema, parser |
| `internal/scheduler/` | Scheduled agent jobs — CRUD, wake logic, maintenance |
| `internal/source/` | Source ingestion — Go, PDF, pyodide extraction |

### Search & Memory
| Package | Responsibility |
|---|---|
| `internal/search/` | Multi-backend search: SQLite FTS, Qdrant vector, chromem-go embedding, graph doc search |
| `internal/memoryindex/` | Vector index rebuild + health |
| `internal/memoryquality/` | Memory quality auditing |
| `internal/ingest/` | Document ingestion pipeline |

### External Integration
| Package | Responsibility |
|---|---|
| `internal/llm/` | LLM client — OpenAI-compatible, Ollama, with retry logic |
| `internal/telegram/` | Telegram bot — handlers, adapters, streaming, markdown, conversation, setup |
| `internal/ocr/` | Mistral OCR client + image rendering |
| `internal/files/` | Office document extraction (docx, pdf, xlsx) |
| `internal/backup/` | Database export + S3 upload |

### Infrastructure
| Package | Responsibility |
|---|---|
| `internal/config/` | Configuration loading from env + files |
| `internal/logging/` | Daily log rotation, zap→slog bridge |
| `internal/setup/` | First-run setup: dotenv, locale, presets, server probe |
| `internal/health/` | Health dashboard — sanitize + expose metrics |
| `internal/sandbox/` | Sandbox execution — process runner, pyodide, pyodide container, smoke tests |
| `internal/tray/` | System tray icon — Windows native + cross-platform fallback |
| `internal/runtimebootstrap/` | Runtime layout bootstrap + default skills |
| `internal/workspace/` | Workspace root path resolution |
| `internal/release/` | Release metadata |
| `internal/tracing/` | Tracing instrumentation |
| `internal/debugguard/` | Live DB guard for debugging |
| `internal/orchestration/` | Orchestration utilities |

## Web Dashboard — `web/`

```
web/
├── src/
│   ├── App.tsx                       # Root — routing + auth guard
│   ├── main.tsx                      # Entry point
│   ├── api.ts                        # API client
│   ├── components/
│   │   ├── Shell.tsx                 # App shell (sidebar + content)
│   │   ├── Sidebar.tsx               # Navigation sidebar
│   │   ├── Login.tsx                 # Bearer token login
│   │   ├── HealthDashboard.tsx       # System health dashboard
│   │   ├── WikiPanel.tsx             # Wiki list
│   │   ├── WikiPageView.tsx          # Wiki page detail
│   │   ├── WikiEditor.tsx            # Tiptap wiki editor
│   │   ├── WikiGraphView.tsx         # Force-graph visualization
│   │   ├── SourceInbox.tsx           # Source ingestion inbox
│   │   ├── TasksPanel.tsx            # Scheduled tasks management
│   │   ├── SettingsPanel.tsx          # Settings editor
│   │   ├── SwarmPanel.tsx            # Swarm management
│   │   ├── MCPPanel.tsx              # MCP connector configuration
│   │   ├── SkillsPanel.tsx           # Skills management
│   │   ├── BackupsPanel.tsx          # Backup management
│   │   ├── MaintenancePanel.tsx      # Maintenance operations
│   │   ├── SummariesPanel.tsx        # Conversation summaries
│   │   ├── ConversationsPanel.tsx    # Conversation list
│   │   ├── PendingUsersPanel.tsx     # User access management
│   │   ├── common/                   # Shared: AppSkeletons, ConfirmModal, ErrorCard, MarkdownReader, ThemeToggle
│   │   └── ui/                       # shadcn/ui primitives (18 components)
│   ├── hooks/                        # useApi, useAppTheme, useAuraTimeZone, useLocale
│   ├── lib/                          # auth, confirmModal, timezone, utils
│   ├── i18n/                         # i18next setup + types
│   └── types/api.ts                  # API type definitions
├── e2e/                              # 10 Playwright specs + fixtures
│   ├── fixtures.ts                   # Auth fixture (bearer token from env)
│   ├── all-pages.spec.ts             # Smoke: every route loads
│   ├── dashboard.spec.ts             # Health dashboard
│   ├── settings.spec.ts              # Settings CRUD
│   ├── mcp-connectors.spec.ts        # MCP connectors
│   ├── source-universal-upload.spec.ts
│   ├── summaries-evidence.spec.ts
│   ├── tasks-and-cleanup.spec.ts
│   ├── confirm-modal.spec.ts
│   ├── form-labels.spec.ts
│   └── other-pages.spec.ts
├── vite.config.ts
├── playwright.config.ts
└── package.json
```

## Docker Services — `compose.yaml`

11 services: `aura`, `aura-secrets`, `nginx`, `searxng`, `redis`, `qdrant`, `garage`, `ollama`, `ollama-webui`, `pyodide`, `watchtower`

Key volumes: `./data/` (SQLite, logs, caches), `/workspace` (wiki, skills, MCP config, prompt overlays)

## Naming Conventions

### Go
- **Packages:** lowercase, single word preferred (`agent`, `tools`, `search`). Some compound (`agentloop`, `memoryindex`, `swarmtools`)
- **Files:** lowercase, underscore-separated (`conversations_write.go`, `mcp_setup.go`)
- **Interfaces:** single-method named after the method (`Runner`, `Store`, `Parser`)
- **Structs:** PascalCase exportable (`AgentJob`, `ConversationManager`)
- **Tests:** co-located `*_test.go`, functions named `Test<Subject><Scenario>` (`TestMainRunsMigrationsBeforeStoreConstruction`)

### TypeScript / React
- **Components:** PascalCase files (`WikiEditor.tsx`, `SettingsPanel.tsx`)
- **Hooks:** `use*` prefix (`useApi.ts`, `useAppTheme.ts`)
- **UI primitives:** lowercase in `ui/` (`button.tsx`, `dialog.tsx`) — shadcn convention
- **Types:** PascalCase interfaces in `types/api.ts` or co-located

## Where to Add Code

| What | Where |
|---|---|
| New LLM tool | `internal/tools/` — add definition + implementation |
| New API endpoint | `internal/api/` — add handler + register in `router.go` |
| New wiki feature | `internal/wiki/` + `web/src/components/Wiki*.tsx` |
| New dashboard panel | `web/src/components/<Name>Panel.tsx` + route in `App.tsx` |
| New Docker service | `compose.yaml` + secrets in `docker/secrets/` |
| New DB migration | `internal/db/migrations/` + `migrations.go` |
| New config option | `internal/config/config.go` + `internal/settings/` |

## Generated vs Committed

- **Committed:** All `.go`, `.tsx`, `.ts`, config files, migrations
- **Generated at build:** `internal/api/dist/` (web build output embedded via `embed.go`)
- **Generated at runtime:** `data/` directory contents (SQLite DB, logs, caches)
- **Not committed:** `data/` (except seed/secret templates), `web/node_modules/`
