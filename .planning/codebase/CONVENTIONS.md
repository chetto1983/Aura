# Coding Conventions

**Analysis Date:** 2026-05-10

## Languages

**Go (backend):**
- Go 1.26.2
- Module: `github.com/aura/aura`
- All backend code lives under `cmd/` (entry points) and `internal/` (shared packages).

**TypeScript (frontend):**
- TypeScript ~6.0.2
- React 19.2.5 with Vite 8
- All frontend code under `web/src/` with E2E tests under `web/e2e/`.

## Naming Patterns

### Go: Files

- Files use `snake_case.go` consistently: `daily_writer.go`, `system_prompt.go`, `process_runner.go`, `tool_compaction.go`.
- Test files follow `*_test.go` alongside the source: `db_test.go` is co-located with `db.go`.
- Files with platform-specific builds use Go build constraints: `sandbox_linux.go` and `sandbox_other.go`.

### Go: Packages

- Single-word, lowercase, noun-focused: `config`, `db`, `logging`, `auth`, `budget`, `skill`, `tools`, `scheduler`.
- Compound names use concatenation: `agentloop`, `agentruntime`, `runtimebootstrap`, `memoryindex`, `memoryquality`, `debugguard`, `dbrecovery`, `swarmtools`, `mcppolicy`.
- Package doc comments appear on the first line after `package` in some files (e.g., `internal/db/db.go` line 1: `// Package db provides Aura's shared production SQLite open path.`).

### Go: Types

- Exported types use **PascalCase**: `Config`, `Runner`, `Task`, `Result`, `Turn`, `ArchiveStore`, `Store`, `Loader`, `RetryClient`.
- Unexported types use **camelCase**: `dailyWriter`, `zapHandler`, `discardHandler`, `slowMockStore`.
- Struct field tags use `envconfig:"KEY"` for configuration binding (e.g., `TelegramToken string \`envconfig:"TELEGRAM_TOKEN" required:"true"\`` in `internal/config/config.go`).

### Go: Constants

- Exported constants use **PascalCase** with a `Default` prefix pattern for configuration defaults:

```go
const DefaultOllamaWebBaseURL = "https://ollama.com/api"
const DefaultQdrantCollection = "aura_memory_v1"
const DefaultSpeculativeSearchTimeoutMS = 1500
```

- Unexported constants use **camelCase**:

```go
const defaultMaxIterations = 5
const defaultTimeout       = 60 * time.Second
const cacheTTL = 1 * time.Second
```

### Go: Functions

- Exported functions use **PascalCase**: `Load`, `Open`, `NewRunner`, `NewLoader`, `NormalizeSkillRoutingMode`, `NormalizeTerminalToolPolicy`.
- Unexported functions use **camelCase**: `parseAllowlist`, `normalizeIntRange`, `hashToken`, `sanitizeAttr`, `getEnv`, `getEnvInt`.
- Constructor functions follow the `NewXxx` pattern: `NewRunner`, `NewLoader`, `NewRetryClient`, `NewSanitizeHandler`, `NewTestDB`, `NewNopLogger`, `NewStoreWithDB`.
- Factory-like functions that open resources use `Open` prefix: `Open(path)`, `OpenReadOnly(path)`, `OpenStore(path)`.

### Go: Variables

- Sentinel errors use the `ErrXxx` pattern:

```go
var ErrDuplicateTurn = errors.New("conversation: duplicate (chat_id, turn_index)")
var ErrInvalid = errors.New("auth: invalid token")
var ErrExpired = errors.New("auth: token expired")
```

### TypeScript: Files

- Source files use **PascalCase** for components and **camelCase** for utilities: `SettingsPanel.tsx`, `HealthDashboard.tsx`, `api.ts`, `App.tsx`.
- UI primitives use **kebab-case** directory + **PascalCase** file: `components/ui/button.tsx`, `components/ui/dialog.tsx`.
- E2E tests use **kebab-case**: `dashboard.spec.ts`, `settings.spec.ts`, `confirm-modal.spec.ts`.
- Barrel re-export patterns: `@/types/api` imports via type-only imports in `web/src/api.ts`.

## Code Style

### Go: Formatting

- **`go fmt`** is the only formatting tool in use (invoked via `Makefile` `fmt` target: `go fmt ./...`).
- **`go vet`** is the only static analysis tool (invoked via `Makefile` `vet` target: `go vet ./...`).
- No `.editorconfig`, `.golangci.yml`, or other Go linter configs detected.
- Struct initialization uses field names (never positional): `&Runner{llm: cfg.LLM, tools: cfg.Tools, ...}`.

### Go: Indentation and Whitespace

- Standard `go fmt` conventions: tabs for indentation, spaces for alignment.
- Multi-line struct literals and switch cases are aligned with `gofmt` defaults.
- Blank lines separate logical blocks within functions.

### TypeScript: Formatting

- **ESLint** with TypeScript-ESLint (`typescript-eslint`), React Hooks plugin, and React Refresh plugin.
- No Prettier config detected -- ESLint handles formatting.
- ESLint extends: `js.configs.recommended`, `tseslint.configs.recommended`, `reactHooks.configs.flat.recommended`, `reactRefresh.configs.vite`.
- Global ignores: `dist`.
- File extension coverage: `**/*.{ts,tsx}`.

## Import Organization

### Go

Imports are organized in three groups separated by blank lines:

1. **Standard library** (alphabetical): `context`, `database/sql`, `errors`, `fmt`, `net/http`, etc.
2. **Third-party packages**: `go.uber.org/zap`, `modernc.org/sqlite`, `gopkg.in/yaml.v3`, etc.
3. **Internal packages** (always `github.com/aura/aura/internal/...`): `github.com/aura/aura/internal/llm`, `github.com/aura/aura/internal/db`, etc.

```go
import (
    "context"
    "fmt"

    "go.uber.org/zap"

    "github.com/aura/aura/internal/llm"
    "github.com/aura/aura/internal/tools"
)
```

- Blank import (`_`) is used for SQLite driver registration: `_ "modernc.org/sqlite"`.

### TypeScript

- Type-only imports are explicit using `import type`: `import type { HealthRollup, WikiPageSummary } from '@/types/api'`.
- Path alias `@/` resolves to `web/src/` (Vite alias).
- Order: type imports first, then value imports, with third-party first and local second.

## Error Handling

### Go

- Every function that can fail returns `(value, error)` -- no exceptions or panics.
- Errors are wrapped with `fmt.Errorf` for context using `%w`:

```go
return nil, fmt.Errorf("open sqlite database: %w", err)
return fmt.Errorf("auth approve: commit: %w", err)
```

- Sentinel errors are used for specific conditions callers can check with `errors.Is`:

```go
var ErrInvalid = errors.New("auth: invalid token")
var ErrDuplicateTurn = errors.New("conversation: duplicate (chat_id, turn_index)")
```

- Custom error types implement `Unwrap()` for error chain inspection:

```go
type AuditUpdateError struct {
    UserID string
    Err    error
}
func (e *AuditUpdateError) Unwrap() error { return e.Err }
```

- Null/zero-value checks before operations: `if db == nil { return "", errors.New("db is required") }`.
- SQL errors are checked with `errors.Is(err, sql.ErrNoRows)` to distinguish "not found" from other failures.
- Deferred `Rollback()` for transactions: `defer tx.Rollback()` after `BeginTx`, with explicit `Commit()` at the end.
- Close-on-error pattern: on failure after opening, close immediately: `_ = db.Close(); return nil, fmt.Errorf(...)`.

### TypeScript

- Custom `ApiError` class extends `Error` with a `status` code:

```typescript
export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
    this.name = 'ApiError';
  }
}
```

- Response error body is read and parsed for structured error messages via `readError(res)`.
- 401 responses trigger an automatic redirect to `/login` via `handle401()`.
- Request timeout is enforced with `AbortController` and a configurable `TIMEOUT_MS` (8000ms for GET, unbounded for POST).

## Logging

### Go

- **Zap** (`go.uber.org/zap`) as the underlying structured logger, exposed through Go's `log/slog` interface.
- A custom `zapHandler` implements `slog.Handler` by delegating to zap (`internal/logging/zap_slog.go`).
- Logs are written to stdout (JSON format) and to a daily-rotating file under `LOG_DIR` (`internal/logging/daily_writer.go`).
- **Secret sanitization**: `health.SanitizeHandler` wraps the slog handler to redact known secret key patterns (e.g., `token`, `api_key`, `secret`, `password`) from log output (`internal/health/sanitize.go`).
- Log level is set via `cfg.LogLevel` (default `"info"`).
- Test code uses `slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))` or the `NewNopLogger()` discard logger.
- Caller file/line is included: `zapLogger := zap.New(core, zap.AddCaller())`.

## Comments

### Go

- **Doc comments** on exported types and functions follow Go conventions (sentence starting with the name):

```go
// Runner executes a bounded LLM/tool loop without Telegram coupling. It is the
// small reusable core future AuraBot workers can use inside SwarmManager.
type Runner struct { ... }
```

- **Inline comments** explain non-obvious logic, design decisions, and "why":

```go
// Slice 14b: blank TELEGRAM_TOKEN is no longer an error — it signals
// first-run state so cmd/aura can launch the setup wizard.
```

- **Phase/slice cross-references** appear in comments to track feature provenance:

```go
// Slice 10b — frontend dashboard
// Phase 12a/12b
```

- **TODO markers**: None detected in the current codebase via grep search.

## Function Design

### Go

- **Constructor pattern** with validation:

```go
func NewRunner(cfg Config) (*Runner, error) {
    if cfg.LLM == nil {
        return nil, errors.New("agent runner: llm client required")
    }
    // Apply defaults for zero values
    if maxIterations <= 0 { maxIterations = defaultMaxIterations }
    ...
    return &Runner{...}, nil
}
```

- **Context is always the first parameter** for I/O functions: `func (s *Store) Lookup(ctx context.Context, token string) (string, error)`.
- **Receiver naming**: single-letter or short abbreviation of the type name: `(s *Store)`, `(r *RetryClient)`, `(dw *dailyWriter)`, `(tr *Tracker)`.
- Pointer receivers are used for struct types that maintain state or use mutexes.
- **Normalization functions** return cleaned/default values for user input:

```go
func NormalizeSkillRoutingMode(value string) string { ... }
func NormalizeTerminalToolPolicy(value string) string { ... }
```

### TypeScript

- API calls are collected in a single `api` object exported from `web/src/api.ts`.
- Each endpoint is a typed async function returning a Promise of the expected type.
- Helper functions for query string construction (`qs()`) and auth headers (`authHeaders()`) are private to the module.
- Components are functional (React function components) with hooks.

## Module Design

### Go

- **Interface-first design**: Small, focused interfaces defined near their consumers, composed into larger Repository interfaces:

```go
type TokenReader interface { Lookup(ctx context.Context, token string) (string, error) }
type TokenIssuer interface { Issue(ctx context.Context, userID string) (string, error) }
type TokenRepository interface { TokenReader; TokenWriter }
type Repository interface { TokenRepository; AccessReader; AccessWriter; PendingReader }
```

- **Interface compliance checks** at compile time using blank identifier assignments:

```go
var (
    _ Gate          = (*Tracker)(nil)
    _ UsageRecorder = (*Tracker)(nil)
)
```

- **No barrel files** in Go -- each file exports its own types.
- Packages are cohesive: `internal/db` handles SQLite connection management, `internal/auth` handles tokens, `internal/conversation` handles conversation state.
- Configuration is centralized in `internal/config/config.go` as a single `Config` struct loaded from env vars.

### TypeScript

- Type barrel file at `@/types/api` re-exports all API response types used across components.
- Auth utilities isolated in `@/lib/auth` with `getToken()` and `clearToken()`.
- UI components follow shadcn/ui pattern in `components/ui/` (Radix UI primitives + Tailwind styling).
- Feature panels each get their own file in `components/`: `SettingsPanel.tsx`, `SkillsPanel.tsx`, `ConversationsPanel.tsx`, etc.

## Architectural Patterns

- **Dependency injection via struct configs**: Each component accepts a `Config` struct and validates required fields.
- **Lifecycle ownership**: Components that open resources (DB connections, stores) own their `Close()` lifecycle. Shared connections use `NewStoreWithDB` to avoid double-close.
- **Narrow interfaces for testability**: Each subsystem exposes minimal read/write interfaces so tests only need to mock what they use.
- **Startup order validation**: `cmd/aura/main_test.go` performs source-code analysis to verify that `main.go` maintains correct initialization ordering (config.Load -> runtimebootstrap.EnsureLayout -> db.Open -> migrations.Run -> etc.).

---

*Convention analysis: 2026-05-10*
