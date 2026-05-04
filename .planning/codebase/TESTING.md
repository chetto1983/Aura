# Testing Patterns

**Analysis Date:** 2026-05-04

## Test Framework

### Go Unit Tests

**Runner:** Go's built-in `testing` package — no third-party test framework is used.

**Run Commands:**
```bash
go test ./...              # Run all tests (from AGENTS.md and Makefile)
make test                  # Same as go test ./...
make all                   # Runs tests, then builds the project
go test -v ./...           # Verbose output
```

**Coverage:**
```bash
go test -coverprofile=cover.out ./...
go tool cover -html=cover.out  # Opens HTML coverage report
```

A `cover.out` file (99KB) and `cover.html` are present in the project root from a recent coverage run.

**Overall Coverage:** **70.2%** (statement coverage across the entire codebase)

### Frontend E2E Tests

**Runner:** Playwright 1.59 via `@playwright/test`

**Run Commands:**
```bash
cd web
npx playwright test                    # Run E2E tests (serialized)
npm run e2e                            # Same via package.json script
AURA_E2E_TOKEN=<token> npx playwright test  # With auth token
npm run e2e:headed                     # Headed browser
npm run e2e:debug                      # Debug mode
npm run e2e:report                     # Show HTML report
```

**Required environment variables:**
- `AURA_DASHBOARD_URL` — default `http://localhost:8081`
- `AURA_E2E_TOKEN` — bearer token minted via Telegram's `request_dashboard_token` tool

**Config file:** `web/playwright.config.ts`

## Test File Organization

### Unit Tests (Go)

**Location:** Co-located with source files — `foo.go` and `foo_test.go` in the same directory.

**Count:** 104 `*_test.go` files across `internal/` and `cmd/`, containing approximately 785 `Test*` functions.

**Pattern:**
```
internal/
  auth/
    store.go
    store_test.go          # Tests for Store methods
  api/
    router.go
    router_test.go         # Tests for read-side HTTP handlers
    auth_test.go           # Tests for auth middleware + logout/whoami
    writes_test.go         # Tests for write-side HTTP handlers
    health_test.go         # (health_compounding_test.go)
    conversations_test.go  # Tests for conversation list endpoints
    static_test.go         # Tests for static asset serving
  config/
    config.go
    config_test.go         # Tests for Load(), IsAllowlisted(), IsBootstrapped()
  conversation/
    context.go
    context_test.go        # Tests for conversation context management
    archive_test.go        # Tests for conversation archiving
```

### E2E Tests (TypeScript/Playwright)

**Location:** `web/e2e/` directory, separate from source.

**Count:** 7 spec files + 1 fixture file.

```
web/e2e/
  fixtures.ts                       # Shared auth fixture (authedPage)
  dashboard.spec.ts                 # Sidebar nav, chord shortcuts, ? dialog
  confirm-modal.spec.ts             # Confirmation modal behavior
  tasks-and-cleanup.spec.ts         # Task management and data cleanup
  settings.spec.ts                  # Settings panel interactions
  summaries-evidence.spec.ts        # Summaries and evidence review
```

## Test Structure Patterns (Go)

### Standard Test Structure

Tests follow table-driven patterns for multiple cases and `t.Helper()` for shared setup:

```go
func TestStorePutValidates(t *testing.T) {
    s, _ := NewStore(dir, nil)
    tests := []PutInput{
        {Kind: "", Filename: "a.pdf", Bytes: []byte("x")},
        {Kind: "invalid", Filename: "a.pdf", Bytes: []byte("x")},
        {Kind: KindPDF, Filename: "", Bytes: []byte("x")},
    }
    for i, tc := range tests {
        if _, _, err := s.Put(context.Background(), tc); err == nil {
            t.Errorf("case %d: expected error, got nil", i)
        }
    }
}
```

Sub-tests with `t.Run()`:
```go
func TestWikiPage_BadInputs(t *testing.T) {
    e := newTestEnv(t)
    cases := []struct {
        name   string
        path   string
        status int
    }{
        {"missing slug", "/wiki/page", http.StatusBadRequest},
        {"empty slug", "/wiki/page?slug=", http.StatusBadRequest},
        ...
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            rr := e.do("GET", tc.path)
            if rr.Code != tc.status {
                t.Errorf("status %d, want %d, body %s", rr.Code, tc.status, rr.Body)
            }
        })
    }
}
```

### Test Helpers / Fixture Factories

All test helpers call `t.Helper()` at the top. Patterns:

**Store factory:**
```go
func newTestStore(t *testing.T) *Store {
    t.Helper()
    path := filepath.Join(t.TempDir(), "auth.db")
    s, err := OpenStore(path)
    if err != nil {
        t.Fatalf("open store: %v", err)
    }
    t.Cleanup(func() { s.Close() })
    return s
}
```

**HTTP test environment:**
```go
type testEnv struct {
    t       *testing.T
    dir     string
    wiki    *wiki.Store
    sources *source.Store
    sched   *scheduler.Store
    router  http.Handler
}

func newTestEnv(t *testing.T) *testEnv {
    t.Helper()
    dir := t.TempDir()
    // Create stores, wire router...
    t.Cleanup(func() { schedStore.Close() })
    return &testEnv{...}
}
```

**Seed helpers:**
```go
func (e *testEnv) seedPage(title, body, category string, related []string) *wiki.Page
func (e *testEnv) seedSource(content []byte, kind source.Kind, filename string) *source.Source
func (e *testEnv) seedTask(name string, kind scheduler.TaskKind, status scheduler.Status, nextRun time.Time) *scheduler.Task
```

**Auth test environment:**
```go
type authedTestEnv struct {
    *testEnv
    authStore *auth.Store
    allowed   map[string]bool
    router    http.Handler
}
```

### Assertion Style

- `t.Fatal()` for setup failures that make subsequent assertions meaningless
- `t.Fatalf()` for HTTP status mismatches (no point reading body after 500)
- `t.Errorf()` for field value mismatches (test continues to report all failures)
- `errors.Is(err, ...)` for sentinel error checking
- JSON unmarshalling errors are checked explicitly before accessing fields

### Cleanup Patterns

- `t.TempDir()` for isolated filesystem state — automatically cleaned up
- `t.Cleanup(func() { ... })` for resource teardown — `s.Close()`, `logger cleanup`
- `defer cleanup()` for logging setup teardown
- `defer os.Unsetenv(...)` for environment variable isolation in config tests

## Mocking / Fakes

**Go tests use real implementations, not mocks:**
- Tests spin up real SQLite databases in `t.TempDir`
- HTTP endpoints are tested with `httptest.Server` (OCR/LLM tests) or `httptest.ResponseRecorder` (handler tests)
- `internal/llm/openai_test.go` creates a fake HTTP server responding with predefined JSON
- No mocking framework is used — `httptest` and test doubles are considered sufficient
- The `internal/api/router.go` defines **interfaces** (`WikiStore`, `SourceStore`, `SchedulerStore`) explicitly designed for test substitution

**What is NOT mocked:**
- SQLite — always real, isolated per test
- File I/O — `t.TempDir()` provides real filesystem
- HTTP routing — real router with real handlers
- Middleware — exercised through the full stack

## Integration / E2E Tests

### Integration Tests (Go)

Several `*_integration_test.go` files exist:
- `internal/telegram/sandbox_integration_test.go` — sandbox + Telegram integration
- `internal/telegram/scheduler_handlers_test.go` — scheduler agent job dispatch
- `cmd/debug_agent_jobs/main_test.go` — end-to-end agent job harness

These tests wire multiple subsystems together but still use `t.TempDir()` for isolation.

### E2E Tests (Playwright)

The Playwright suite tests the React dashboard through a real browser (Chromium):

```typescript
import { test, expect } from './fixtures';

test('three Phase 12 nav items render', async ({ authedPage: page }) => {
    await page.goto('/');
    const nav = page.getByRole('navigation');
    await expect(nav.getByRole('link', { name: /conversations/i })).toBeVisible();
});
```

**Coverage:**
- Dashboard sidebar navigation, chord shortcuts (`g v`, `g u`, `g x`), help dialog (`?`)
- Conversations route: panel rendering, chat_id filter, drawer open
- Tasks: creation, filtering, cancellation, cleanup
- Settings: reading and updating settings
- Summaries: evidence review flow
- Confirmation modals: approve/reject dialogs

**Limitations:**
- Tests are serialized (`workers: 1`) because the dashboard reads a shared SQLite database
- Some tests use `test.skip()` when `AURA_E2E_CHAT_ID` is not set
- Telegram-interaction steps remain manual (no MTProto stub available)

### Live Tests

Several `*_live_test.go` files test against real external services:
- `internal/ocr/live_test.go` — real Mistral OCR API calls
- `internal/ingest/live_test.go` — real ingestion pipeline
- `internal/sandbox/pyodide_runner_test.go` — `TestPyodideRunner_LivePyodideBundle` tests the bundled Pyodide runtime

## Test Data / Fixtures

**No `testdata/` directories exist** in the repository. All test data is generated inline or seeded through helper methods (`seedPage`, `seedSource`, `seedTask`).

Inline test data examples:
- `[]byte("%PDF-1.4 fake content")` for PDF sources
- `"Hello from AI"` JSON responses for LLM mock servers
- `t.TempDir()` for scratch filesystems

## What Is Tested (High Coverage Areas)

| Package | Coverage Notes |
|---------|---------------|
| `internal/auth` | Full round-trip testing: Issue → Lookup → Revoke, bootstrap, approval queue |
| `internal/config` | `Load()` with all defaults, `IsAllowlisted()`, `IsBootstrapped()`, first-run bootstrap |
| `internal/source` | Put, dedup, Get, List, Update, validation, path traversal rejection |
| `internal/api` | Every read endpoint (health, wiki, sources, tasks) and write endpoint (wiki rebuild, log append, source ingest, task operations) |
| `internal/llm` | OpenAI client Send/Stream, error handling, model override, retry logic |
| `internal/conversation` | Context creation, message management, system prompt rendering |
| `internal/conversation/summarizer` | Scoring, dedup, proposals, applying, runner |
| `internal/health` | Secret sanitization, health server |
| `internal/scheduler` | Upsert, Get, List, Cancel, daily time parsing, next-run calculation |
| `internal/sandbox` | Pyodide runner execute, availability check, manifest validation, code validation |
| `internal/toolsets` | Profile resolution, deduplication, role presets |

## What Is NOT Tested (Coverage Gaps)

| Area | Risk |
|------|------|
| `internal/telegram/` handlers | Bot message processing tested only through integration tests; no isolated unit tests |
| `internal/ingest/` pipeline | Only has `live_test.go` (external dependency) |
| `internal/search/` | No test files found for semantic search |
| `internal/skills/` | Skill loading covered via API tests; no isolated skill loader tests |
| `internal/mcp/` client | MCP protocol tested through `client_test.go` only |
| `internal/settings/` | No test files found for settings store |
| `internal/tray/` | Platform-specific; no tests |
| `internal/api/static.go` | Only basic test (`static_test.go`) for embedding and SPA fallback |
| `cmd/aura/main.go` | Main binary is untested |
| Most `cmd/debug_*` utilities | Only `debug_agent_jobs` has tests |
| React components | No unit tests (Jest/Vitest) for React components — coverage comes only from Playwright E2E |

### Untested wiki store functions (0% coverage):

From `cover.out` analysis:
- `lintAt` — 0.0%
- `RepairLink` — 0.0%
- `sortedCategoryKeys` — 0.0%
- `memoryDecayIssue` — 41.7% (partial)

## Build Verification

`make all` runs the full build verification pipeline:
1. `make test` — `go test ./...`
2. `make build` — `go run github.com/josephspurrier/goversioninfo@latest ... && go build -o aura.exe ./cmd/aura`

Build prerequisites include:
- Go 1.25.5+ (see `go.mod`)
- Node 20+ for the web dashboard
- A running web-build produces the embedded `dist/` directory

## Continuous Integration

**No CI configuration files detected** (no `.github/workflows/`, `.gitlab-ci.yml`, Jenkinsfile). The project appears to rely on local developer testing:
- `go test ./...` before pushing
- `make all` before submitting PRs (per `CONTRIBUTING.md`)
- `npm run lint` for frontend code quality

---

*Testing analysis: 2026-05-04*
