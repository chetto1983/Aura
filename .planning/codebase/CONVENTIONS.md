# Coding Conventions

**Analysis Date:** 2026-05-04

## Go Coding Standards

### Formatting and Linting

The project uses Go's built-in toolchain:

- **Format:** `go fmt ./...` — enforced in `Makefile` (target: `fmt`)
- **Vet:** `go vet ./...` — available in `Makefile` (target: `vet`) but not always enforced in CI
- **Build:** `go build ./...` — `Makefile` target `build` produces `aura.exe` with embedded version info

No additional linters (golangci-lint, staticcheck) are configured. The `CONTRIBUTING.md` states: "Follow existing Go formatting and style (`gofmt`, idiomatic Go)."

### Package Organization

Packages follow Go convention: one package per directory, package name matches directory name.

```
internal/
  config/       — env-var loading, Config struct, defaults
  auth/         — SQLite-backed bearer tokens, approval queue
  api/          — HTTP handlers, DTOs, router
  wiki/         — Markdown page store
  source/       — immutable source (PDF/DOCX/XLSX/Text) store
  ocr/          — Mistral Document AI OCR client
  ingest/       — source → wiki ingestion pipeline
  scheduler/    — SQLite-backed reminders, agent jobs, nightly maintenance
  llm/          — OpenAI-compatible LLM client interface
  conversation/ — conversation context, summaries, system prompt
  health/       — health server, secret sanitization handler
  logging/      — zap+slog integration, daily log rotation
  mcp/          — MCP protocol client (stdio + Streamable-HTTP)
  skills/       — Anthropic skill manifest loading
  search/       — semantic search with embeddings
  telegram/     — bot wiring, markdown→HTML renderer, handlers
  sandbox/      — Pyodide sandbox code execution
  settings/     — dynamic runtime settings
  swarm/        — AuraBot multi-agent orchestration
  toolsets/     — named tool profiles for agent jobs
  tray/         — Windows system tray
  budget/       — LLM cost tracking
  tools/        — LLM-executable tool implementations
cmd/
  aura/         — main binary
  debug_llm/    — LLM smoke-test utility
  debug_tools/  — tool smoke-test utility
  debug_ingest/ — ingest smoke-test utility
  debug_agent_jobs/ — agent job e2e harness (with _test.go)
  build_icon/   — tray icon generator
  debug_files/   debug_summarizer/  debug_memory_quality/
  debug_xlsx/   debug_pdf/  debug_docx/  debug_swarm/
  debug_settings/  seed_e2e_env/
```

### Naming Conventions

- **Exported types and functions:** `PascalCase` — `NewStore`, `IsAllowlisted`, `OpenStore`
- **Unexported helpers:** `camelCase` — `hashToken`, `zapLevel`, `isStaticAssetPath`
- **Constants:** `PascalCase` or `UPPER_SNAKE_CASE` for env-var-like defaults — `DefaultOllamaWebBaseURL`, `StatusStored`, `idPrefix`
- **Struct fields:** `PascalCase` with `json:"field_name"` struct tags — `type Source struct { ID string \`json:"id"\` ... }`
- **Interfaces:** Named for the role they abstract — `WikiStore`, `SourceStore`, `SchedulerStore`, `Client` (LLM)
- **Variables:** Short, idiomatic — `s` for store, `ctx` for context, `rr` for ResponseRecorder
- **Test helpers:** `newTestEnv(t)`, `newAuthedTestEnv(t)`, `newTestStore(t)` — always call `t.Helper()`

### Error Handling Patterns

- **Sentinel errors:** Exported for comparison with `errors.Is()` — `var ErrInvalid = errors.New("auth: invalid token")`, `ErrNoStaticAssets`
- **Error wrapping:** Always `%w` for internal propagation — `return nil, fmt.Errorf("open auth db: %w", err)`
- **Error checks:** Use `errors.Is(err, target)` and `errors.As(err, &target)` consistently
- **HTTP handler errors:** Use helpers `writeError(w, logger, status, message)` for client-facing errors; logged separately to slog
- **Defensive patterns:** Check for nil dependencies before use — `if deps.Auth == nil { writeError(...); return }`
- **Best-effort writes:** Non-critical operations tolerate failure — `_, _ = s.db.ExecContext(ctx, `UPDATE api_tokens SET last_used = ? ...`, now, hash)` in `Lookup`

### Database Access (SQLite)

All SQLite access uses `modernc.org/sqlite` (no CGo). Patterns:

- **Schema as string constants** (not migration files): `const schemaSQL = \`CREATE TABLE IF NOT EXISTS ...\``
- **Idempotent migrations:** All `CREATE TABLE IF NOT EXISTS` / `CREATE INDEX IF NOT EXISTS`
- **Context propagation:** Always `db.ExecContext(ctx, ...)`, `db.QueryRowContext(ctx, ...)`, `db.QueryContext(ctx, ...)`
- **Transaction lifecycle:** Explicit `tx.BeginTx(ctx, nil)` → `defer tx.Rollback()` → operations → `tx.Commit()`
- **Null handling:** `sql.NullString` for optional columns — `var revokedAt sql.NullString`
- **Rows affected checking:** `res.RowsAffected()` then `if n == 0 { return ErrInvalid }`
- **Connection ownership:** `owned bool` field tracks who is responsible for `Close()`
- **Shared connections:** `NewStoreWithDB(db)` pattern allows co-location (e.g., auth + scheduler)

### Configuration / Environment Variables

Configuration in `internal/config/config.go` uses struct tags (`envconfig:"KEY"`) for documentation, but loading is manual (not using the `envconfig` library; `env.go` provides `getEnv`, `getEnvInt`, `getEnvFloat`, `getEnvBool` helpers).

Key conventions:
- **Secrets in `.env`:** API keys, bot tokens — never committed
- **Defaults in config.go:** Every field has a `default:"value"` in the struct tag comment
- **First-run bootstrap:** Missing token signals setup wizard state; `IsBootstrapped()` gate
- **Mistral separation:** `MISTRAL_API_KEY` (OCR) is separate from `EMBEDDING_API_KEY` and `LLM_API_KEY`
- **Template file:** `.env.example` is tracked as the safe configuration reference

### Logging Patterns

- **Structured logging:** Uses `log/slog` from the standard library, backed by `go.uber.org/zap` for production JSON output
- **Levels:** `debug`, `info`, `warn`, `error` configured via `LOG_LEVEL` env var (default: `info`)
- **Key-value pairs:** `deps.Logger.Warn("api: list wiki pages", "error", err)` — message first, then alternating key-value pairs
- **Prefix convention:** Messages use `"subsystem: action description"` — e.g., `"api: health wiki list"`, `"api: read wiki page"`, `"auth: user id required"`
- **Dual output:** stdout (JSON) + daily-rotating file under `LOG_DIR` (default: `./logs`)
- **Secret sanitization:** `internal/health/sanitize.go` — `api_key` fields replaced with `[REDACTED]` via middleware wrapping `slog.Handler`
- **NopLogger:** `NewNopLogger()` returns a no-op logger for tests and headless contexts

### HTTP Handler Patterns

- **HandlerFunc wrappers:** `func handleWikiPage(deps Deps) http.HandlerFunc` — closures over dependencies
- **Response helpers:** `writeJSON(w, logger, status, payload)` and `writeError(w, logger, status, message)` standardize the boundary
- **DTO separation:** API types in `types.go` are deliberately decoupled from internal models ("DTOS in this file are deliberately separate from the internal models so that internal field renames don't break the frontend")
- **Input validation:** `isValidSlug()`, URL path regex, `strings.TrimSpace()`, `requireLoopback` check (127.0.0.1 only for write endpoints)
- **Router:** Uses `http.ServeMux` with explicit middleware chaining (`RequireBearer`, `requireLoopback`)
- **Status codes:** `400 BadRequest` for bad input, `404 NotFound` for missing resources, `405 MethodNotAllowed`, `503 ServiceUnavailable` when optional deps are nil

### Comment / Documentation Patterns

- **Package-level doc comment:** `// Package <name> <description>.` — used at the top of most files (e.g., `// Package auth manages bearer tokens for the dashboard HTTP API.`)
- **Threat-model annotations:** Security-relevant code includes sections like `// Threat model (PDR phase-10-ui §10d):`
- **Inline justifications:** Comments explain _why_, not just what — `// Belt-and-suspenders: SQLite already keyed on the hash, but the constant-time compare keeps a future code path from regressing`
- **PDR references:** Implementation tracker references — `// Slice 14b: blank TELEGRAM_TOKEN is no longer an error`
- **Struct field comments:** Every exported struct field has an inline comment — `// HTTPClient lets tests inject a fake server. Defaults to an http.Client...`
- **Go doc format:** Sentences end with periods, complete sentences preferred

### Import Organization

Standard Go convention: standard library first, then third-party, then internal — each group separated by a blank line:

```go
import (
	"context"
	"crypto/rand"
	...

	"go.uber.org/zap"
	"modernc.org/sqlite"

	"github.com/aura/aura/internal/health"
)
```

The `_` import pattern is used for SQLite drivers: `_ "modernc.org/sqlite"`.

## Commit Message Style

From `git log` analysis, three conventions coexist:

1. **Phase/slice references:** `slice 19g: add agent job e2e harness`, `slice 18d: add batch proposal review`
2. **Prefixed conventional commits:** `@ feat: add sandbox manager Go package`, `@ test: add sandbox integration test skeletons`, `docs: save Aura handoff`
3. **Plain imperative:** `feat: add execute_code tool for sandboxed Python execution`, `feat: migrate common components to react-i18next`

The `@` prefix appears to distinguish commits merged from another branch/tool. `CONTRIBUTING.md` recommends: "Use clear, descriptive commit messages (e.g., `feat: add new health check endpoint`)."

## React / TypeScript Conventions (`web/`)

### Technology Stack

- **React 19** with TypeScript ~6.0
- **Vite 8** as build tool, `@vitejs/plugin-react`
- **Tailwind CSS v4** via `@tailwindcss/vite`
- **shadcn/ui** component library (Radix UI primitives)
- **react-router-dom v7** for client-side routing
- **i18next** + `react-i18next` for internationalization (en, it)
- **Tiptap** editor for wiki content
- **Zod v4** for schema validation

### ESLint Configuration

`web/eslint.config.js` uses the flat ESLint config format:
- `@eslint/js` recommended rules
- `typescript-eslint` recommended rules
- `react-hooks` full rules
- `react-refresh` rules for Vite HMR
- `dist/` ignored

Lint is run via: `cd web && npm run lint`

### TypeScript Configuration

- **Path alias:** `@/*` → `./src/*` (configured in `tsconfig.json` and `vite.config.ts`)
- **Strict mode:** Enabled via `tsconfig.app.json` references
- **TypeScript ~6.0:** Includes `ignoreDeprecations: "6.0"` for compatibility

### Component Patterns

- **Functional components with TypeScript:** All components are exported functions with typed props
- **shadcn/ui conventions:** UI primitives in `src/components/ui/` (button, card, dialog, etc.)
- **Path alias imports:** `import { Shell } from '@/components/Shell'` consistently
- **Hooks:** Custom hooks in `src/hooks/` — `useLocale`, `useApi`, `useAppTheme`
- **Library code:** `src/lib/` — `auth.ts` (token management), `utils.ts` (cn helper), `confirmModal.ts`
- **API layer:** `src/api.ts` — typed `ApiError` class, `authHeaders()` helper, bearer token from localStorage

### Internationalization

- `src/i18n/index.ts` — i18next initialization
- `src/i18n/types.ts` — type-safe translation keys
- `src/i18n/locales/en.json`, `it.json` — namespace-organized translation files
- Components use `useLocale()` hook → `t('key.subkey')` pattern

### Build Output

- Vite builds to `../internal/api/dist/`
- Go embeds via `//go:embed all:dist` in `internal/api/static.go`
- Single `make web-build` target does `npm install && npm run build`

## Frontend Code Style

- **No Prettier config** present — ESLint handles formatting
- **Tailwind utility classes:** Inline in JSX, `cn()` utility from `@/lib/utils` for merging variants
- **Component structure:** Imports → Hook calls → JSX return → (if complex) helper function at bottom of file
- **Accessibility:** `aria-label` on icon buttons, `sr-only` for hidden headings, semantic `<header>`/`<main>`/`<nav>`
- **Responsive design:** Mobile-first with `md:` breakpoints; sidebar collapses to Sheet on mobile
- **Keyboard shortcuts:** Global `keydown` listener with chord support (`g v`, `g u`, `g x`), `?` help dialog

## Local Files Policy

From `.env.example` and `AGENTS.md`:
- Never commit `.env`, `aura.db`, binaries, or generated wiki raw data
- `.env.example` is the safe configuration template
- Generated wiki pages use `schema_version` frontmatter field for compatibility
