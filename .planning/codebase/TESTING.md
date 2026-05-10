---
focus: quality
generated: 2026-05-10
last_mapped_commit: 6b6fa8245e19b9f49fb48e39a28a424cdfcda03f
---

# Testing — Aura Codebase

## Overview

- **Go tests:** 156 `*_test.go` files with 1,307 test functions
- **E2E tests:** 10 Playwright spec files in `web/e2e/`
- **CI:** GitHub Actions runs tests during Docker image build (`Dockerfile.test`)
- **Coverage:** No enforced thresholds
- **Test data:** Real SQLite databases in temp directories, no mocks for storage

## Go Testing

### Framework
Standard library `testing` package exclusively. No third-party test frameworks.

### Table-Driven Tests
Used for systematic input/output verification. 44 `t.Run()` call sites across the codebase.

```go
// Pattern from internal/api/router_test.go
for _, tc := range cases {
    t.Run(tc.name, func(t *testing.T) {
        // setup, execute, assert
    })
}
```

### Key Test Helper Patterns
- `t.Helper()` — standard for helper functions to get correct failure line numbers
- `t.TempDir()` — automatic cleanup temp directories (used for SQLite DBs, workspace paths, sandbox roots)
- `t.Cleanup()` — resource teardown registration
- `t.Setenv()` — scoped environment variable overrides (auto-restored after test)
- `t.Parallel()` — not widely used (many tests share mutable state via temp directory setup)

### HTTP Handler Testing
Uses `net/http/httptest` for handler-level tests:
- `httptest.NewServer()` for full server lifecycle tests
- Direct `http.Handler` calls with `httptest.NewRecorder()` for unit-level handler tests

### Database Testing
No mocking. Tests use real SQLite databases:
- `t.TempDir()` provides isolated file paths
- `internal/scheduler/testdb.go` — helper for scheduler test databases
- `internal/db/migrations/testdata/` — migration test fixtures
- Migrations are tested against committed schema snapshots

### Source Code Analysis Tests
Several tests validate the codebase itself:
- `cmd/aura/main_test.go` — verifies `main.go` wiring order (migrations before store, bootstrap before DB open, tray after Aura start)
- `TestComposeSetsContainerTimezone`, `TestComposeSetsContainerSQLiteJournalMode`, `TestComposeMountsNarrowRuntimeWorkspace` — verify `compose.yaml` invariants
- `cmd/debug_qdrant/main_test.go` — compares Qdrant vs local chromem-go results

### Sandbox & Runtime Tests
- `cmd/debug_sandbox/main_test.go` — smoke tests for code execution through registered tool boundaries
- `cmd/debug_telegram_sandbox/main_test.go` — table-driven Telegram sandbox interaction verification (469+ line test file)

### Mock/Stub Strategy
No mocking frameworks (gomock, mockery). Hand-rolled fakes:
- In-memory stores implementing the same interface as real stores
- Test-only constructors accepting pre-configured dependencies
- `io.Discard` loggers for tests that don't verify log output

## E2E Testing — Playwright

### Configuration
`web/playwright.config.ts`:
- Test directory: `web/e2e/`
- Single worker (`workers: 1`, `fullyParallel: false`) — dashboard reads shared SQLite
- CI: 2 retries, GitHub reporter
- Local: no retries, list reporter, `trace: on-first-retry`, `screenshot: only-on-failure`

### Auth Fixture
`web/e2e/fixtures.ts` — custom Playwright fixture that:
1. Reads `AURA_E2E_TOKEN` from environment
2. Stores token in localStorage
3. Sets `Authorization: Bearer <token>` header for authenticated API calls

### Test Suites
| Spec | Coverage |
|---|---|
| `all-pages.spec.ts` | Every dashboard route loads without crash |
| `dashboard.spec.ts` | Health dashboard data display |
| `settings.spec.ts` | Settings CRUD operations |
| `mcp-connectors.spec.ts` | MCP connector configuration |
| `source-universal-upload.spec.ts` | Source ingestion upload flow |
| `summaries-evidence.spec.ts` | Summary evidence display |
| `tasks-and-cleanup.spec.ts` | Scheduled tasks + cleanup operations |
| `confirm-modal.spec.ts` | Confirmation modal behavior |
| `form-labels.spec.ts` | Form label accessibility |
| `other-pages.spec.ts` | Miscellaneous page coverage |

### Run Commands
```bash
cd web
AURA_DASHBOARD_URL=http://localhost:8081 AURA_E2E_TOKEN=<token> npx playwright test
npm run e2e:headed   # Debug visually
npm run e2e:debug    # Step-through debugging
npm run e2e:report   # View last run report
```

## CI/CD Testing

- **Docker image workflow** (`.github/workflows/docker-image.yml`): Builds multi-stage Docker image on tag push
- **Dockerfile.test**: Separate test Docker image definition
- **Makefile targets**: Standard `test`, `build`, `lint` targets
- **GoReleaser** (`.goreleaser.yml`): Release automation with build verification

## Test Patterns Summary

| Aspect | Approach |
|---|---|
| Test framework | Go: `testing` stdlib; Web: Playwright |
| Test location | Go: co-located `*_test.go`; Web: `e2e/` directory |
| Database | Real SQLite in temp directories |
| HTTP | `httptest` (server + recorder) |
| Mocks | Hand-rolled fakes, no frameworks |
| Coverage | Not enforced |
| Parallelism | Limited (shared state, single playwright worker) |
| CI | GitHub Actions — Docker build includes tests |
| Debug CLIs | Each has `main_test.go` verifying behaviour |
