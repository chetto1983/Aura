# Testing Patterns

**Analysis Date:** 2026-07-04

Two independent test suites: Go (`internal/`, stdlib `testing` only — **no testify**, no mocking framework) and TypeScript/React (`web/`, Vitest + Testing Library + Playwright). Both suites enforce an **85% coverage floor** (CLAUDE.md "COVERAGE FLOOR 85%" overrides the PRD's ≥75%/≥60% split figures) plus mutation-testing spot checks (≥70% killed).

## Test Framework

**Go:**
- Runner: stdlib `testing` (Go 1.26.4). 573 `_test.go` files vs. 437 non-test `.go` files under `internal/`.
- Property-based: `pgregory.net/rapid` v1.3.0 — used in 14 files (`internal/agent/workflow/loop_property_test.go`, `internal/gateway/classify_property_test.go`, `internal/mcp/manager/envedit_property_test.go`, `internal/swarm/swarm_property_test.go`, `internal/scoring/scoring_test.go`, etc.).
- Goroutine leak detection: `go.uber.org/goleak` v1.3.0, wired via `TestMain` in packages that spawn goroutines (`internal/activelearn/main_test.go` and ~20 other `main_test.go` files across `internal/agent`, `internal/agent/mcptools`, `internal/agent/tools`, `internal/conversations`, etc.).
- No `stretchr/testify` — zero occurrences in `go.mod` or `internal/`. All assertions are hand-written `if got != want { t.Fatalf(...) }` / `t.Errorf(...)`.

**Frontend (`web/`):**
- Runner: Vitest (`web/vitest.config.ts`), environment `jsdom`, globals enabled.
- Assertion/render library: `@testing-library/react` (`render`, `screen`, `fireEvent`, `waitFor`, `within`), Vitest's own `expect`/`vi` mocking API.
- E2E: Playwright (`web/playwright.config.ts`, `web/e2e/`).
- Mutation testing: Stryker (`web/stryker.conf.json`, `npm run mutation`, break threshold 70%).

**Run Commands:**
```bash
# Go
go test ./...                                    # unit tier (no build tags)
go test -race ./...                              # unit tier with race detector
go test -tags "db_integration neo4j_integration" -p 1 -covermode=atomic \
  -coverprofile=cover_gate.out ./internal/...     # full integration + coverage (make coverage)
go test -run TestName ./...
go test -fuzz=FuzzName ./...

# Frontend (web/)
npm run test          # vitest run --coverage
npm run test:e2e       # playwright test
npm run mutation       # stryker run

# Makefile wrappers
make quality           # vet + file-size + lint + deadcode + test-race + vuln (no containers)
make quality-full      # quality + coverage (needs stack up via `make neo4j-migrate`)
make web-quality       # web-lint + web-test + web-mutation
```

## Test File Organization

**Go location:** always co-located with the source file in the same package directory (white-box `package foo`) or, less commonly, the black-box `package foo_test` (seen for property tests, e.g. `internal/agent/workflow/loop_property_test.go` uses `package workflow_test`).

**Naming:**
| Suffix | Meaning | Example |
|---|---|---|
| `_test.go` | unit test, no build tag, runs in default `go test ./...` | `internal/agent/agent_test.go` |
| `_internal_test.go` | white-box test alongside an existing black-box `_test.go` for the same file | `internal/agent/llm_agent_breaker_internal_test.go` |
| `_integration_test.go` | build tag `db_integration` (or similar), needs live Postgres/Neo4j | `internal/conversations/store_branch_test.go`, `internal/channels/telegram/store_integration_test.go` |
| `_property_test.go` | `pgregory.net/rapid` property-based test | `internal/swarm/swarm_property_test.go` |
| `_fuzz_test.go` | Go native fuzz target (`func Fuzz...`) | `internal/agent/agent_fuzz_test.go`, `internal/skills/validator_fuzz_test.go` |
| `_bench_test.go` | benchmark | `internal/agent/budget_bench_test.go` |
| `_live_test.go` / `_live_e2e_test.go` | MANUAL paid-gate test against a real external API (OpenRouter); never a CI job | `internal/agent/live_finalize_test.go`, `internal/llm/openai_compat/adaptive_reasoning_live_e2e_test.go` |
| `main_test.go` | package-level `TestMain` wiring `goleak.VerifyTestMain` | `internal/activelearn/main_test.go` |

**Frontend location:** `__tests__/` subdirectory inside each feature folder, mirroring the component name: `web/src/approvals/__tests__/ApprovalList.test.tsx`, `web/src/chat/displays/__tests__/ChartDisplay.test.tsx`. 121 `.test.{ts,tsx}` files total; zero `.spec.*` files (naming is `.test.` exclusively).

## Test Structure

**Go table-driven pattern** (idiomatic, used throughout, e.g. `internal/agent/llm_agent_retry_test.go`):
```go
tests := []struct {
    name string
    err  error
    want bool
}{
    {"wrapped-deadline", errors.Join(errors.New("fetch"), context.DeadlineExceeded), true},
    {"permanent", errors.New("validation"), false},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) { ... })
}
```

**Build-tag integration test header convention** — every integration file opens with a comment documenting exact migration/env prerequisites and the run command, e.g. (`internal/askuser/store_test.go`):
```go
//go:build db_integration

// Integration tests for internal/askuser. Requires a running Postgres with the
// Phase-4 migrations applied through 0005 ...
//	make db-up && aura db migrate
//	AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set in env
//
// Run via (FIFO determinism wants -count=10):
//	go test -tags db_integration -race ./internal/askuser -count=10
//
// No-skip-as-green: envOrSkip t.Fatals under $CI when the DSN is unset.
package askuser
```

**No-skip-as-green discipline (mandatory, CLAUDE.md):** every integration package defines a local `envOrSkip(t, key)` helper (duplicated per-package, not a shared library — see `internal/askuser/store_test.go`, `internal/runner/integration_helpers_test.go`, `internal/channels/telegram/store_integration_test.go`, `internal/webauth/authula_integration_test.go`, `internal/web/searxng_integration_test.go`, `internal/toolselectstore/store_e2e_test.go`):
```go
func envOrSkip(t *testing.T, key string) string {
    t.Helper()
    v := os.Getenv(key)
    if v == "" {
        if os.Getenv("CI") != "" {
            t.Fatalf("integration test requires %s, but it is unset under CI — "+
                "a skipped integration test must not pass as green; wire it in ci.yml", key)
        }
        t.Skipf("integration test requires %s; set it and re-run (e.g. via .env + make db-up)", key)
    }
    return v
}
```
Under CI (`$CI` set), a missing required env var is a hard `t.Fatal`, not a skip — this prevents a container-less CI job from silently reporting a falsely-green integration suite. Locally (no `$CI`), it degrades to `t.Skip`. `_live_test.go`/`_live_e2e_test.go` files use a different, explicit skip message tagged "MANUAL paid gate, NOT a CI job" when `OPENROUTER_API_KEY` is unset — these are intentionally never wired into CI.

**Setup/teardown:** `t.Cleanup(func() { ... })` for restoring package-level test knobs (`withFastBackoff` in `internal/agent/llm_agent_retry_test.go` saves/restores `toolRetryBaseDelay`); `defer cancel()` for context timeouts in integration helpers (`migratedPool` uses a 30s `context.WithTimeout`).

## Mocking

**No mocking framework** — hand-written fakes/stubs implementing the real interface, kept in a shared test-support package `internal/agent/agenttest/` (excluded from the coverage floor as pure test infrastructure, like generated code):

```go
// internal/agent/agenttest/fakeclient.go
type FakeClient struct {
    mu       sync.Mutex
    Turns    []FakeTurn   // scripted, consumed in order
    Requests []llm.Request // captured for message-history assertions
    next     int
}

type FakeTurn struct {
    Chunks []llm.Chunk
    Err    error
}

var _ llm.Client = (*FakeClient)(nil)

func NewFakeClient(turns ...FakeTurn) *FakeClient {
    return &FakeClient{Turns: turns}
}
```
The channel `FakeClient.Stream` returns is pre-buffered and pre-closed — "goleak-clean by construction," per its doc comment; this is the standard idiom for avoiding goroutine leaks in fakes.

**Leaf/local stubs** are also written inline in the test file itself when only one test needs them (not promoted to `agenttest`), e.g. `stubAgent` in `internal/agent/agent_test.go` (minimal `Agent` implementation to exercise `InvocationContext`), or `flakyTool`/`timeoutErr` in `internal/agent/llm_agent_retry_test.go` (fails N times then succeeds, to drive retry-loop assertions).

**What to mock:** external boundaries only — `llm.Client` (network LLM calls), tool `Execute` (external side effects). Database and Neo4j code paths are NOT mocked; they're exercised via real containers under `db_integration`/`neo4j_integration` build tags.

**What NOT to mock:** internal domain logic, the agent loop itself, config parsing — these run against real code paths with fakes only at the true I/O boundary.

**Frontend mocking:** `vi.fn()` / `vi.mock()` (Vitest's built-in mock API), notably partial-module mocking that preserves the rest of a module:
```ts
const navigateMock = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return { ...actual, useNavigate: () => navigateMock };
});
```
(`web/src/approvals/__tests__/ApprovalList.test.tsx`) Network calls are stubbed via `vi.stubGlobal('fetch', ...)` returning a scripted `Response`.

## Fixtures and Factories

**Go:** small in-file factory functions returning domain structs with sensible defaults plus field overrides, e.g. `pgTimestamp(ts time.Time) pgtype.Timestamptz` and a shared `const localID = "00000000-0000-0000-0000-000000000001"` (the seeded `local` identity from migration 0004, used as an FK parent for throwaway test rows) in `internal/askuser/store_test.go`. Golden/fixture files live under `scripts/fixtures/` (`cache_invariant/`, `neo4j-smoke/`) and `internal/agui/testdata/` (captured real AG-UI SSE frames, shared with the frontend's chat-reducer test via `vitest.config.ts`'s `server.fs.allow: ['..']`).

**Frontend:** factory-function pattern for test fixtures with partial overrides:
```ts
function approval(over: Partial<Approval> & Pick<Approval, 'token' | 'conversation_id'>): Approval {
  return { kind: 'clarification', question: 'Pick a city', priority: 0, ...over };
}
```
(`web/src/approvals/__tests__/ApprovalList.test.tsx`)

## Coverage

**Requirements:** ≥85% owned-surface floor, both stacks (CLAUDE.md overrides the PRD's ≥75%/≥60% split). Current measured: **90.3%** Go (`internal/*`, full tag matrix, re-measured 2026-06-13 at HEAD 882df109).

**Go gate:** `scripts/coverage_gate.sh` (invoked via `make coverage` / `make quality-full`). Key mechanics:
- Runs `go test -tags "db_integration neo4j_integration" -p 1 -covermode=atomic -coverprofile=cover_gate.out ./internal/...` — **`-p 1` is mandatory** (serial package execution) because integration tiers across `internal/*` share one Postgres cluster; parallel execution races on `CREATE ROLE`/advisory locks.
- Filters out generated/test-support rows before computing the percentage: `internal/db/sqlc/`, `internal/agent/agenttest/`, `internal/llm/client.go` (pre-rewrite skeleton).
- `cmd/aura` is excluded entirely from the floor (CLI glue, covered behaviorally by integration/smoke, not unit tests).
- Env override: `AURA_COVERAGE_MIN` (default 85), `AURA_COVERAGE_TAGS` (default `"db_integration neo4j_integration"`).
- A Docker-shimmed variant `make coverage-docker` runs `mcp-neo4j-cypher` in a container instead of requiring a host install.

**Frontend gate:** `web/vitest.config.ts` `coverage.thresholds` — `{ statements: 85, branches: 85, functions: 85, lines: 85 }` via the `v8` provider; the suite **fails** below floor (not just reports). Excludes `src/**/*.{test,spec}.{ts,tsx}`, `src/test/**`, and `src/main.tsx` (bootstrap entry, proven by Playwright E2E instead).

**View Coverage:**
```bash
go tool cover -html=cover_gate.out          # Go
go tool cover -func=cover_gate.out | tail -1
cd web && npm run test                      # writes web/coverage/ (v8 html+json)
```

**Mutation testing:** ≥70% killed required on critical files (spot-check, not full-repo). Go: `go-mutesting` (WSL-only — only fork supporting go1.26; `GOFLAGS=-tags=db_integration` for container-gated code). Frontend: Stryker, `break: 70` in `web/stryker.conf.json`, run via `npm run mutation` / `make web-mutation`.

## Test Types

**Unit Tests:** fast, no external deps, deterministic — the default `go test ./...` tier and default `npm run test` tier.

**Integration Tests:** Go build tags `db_integration` / `neo4j_integration`, require the live Postgres + Neo4j + `mcp-neo4j-cypher` stack (`make db-up`, `make neo4j-migrate`). Frontend integration is largely folded into component tests using `@testing-library/react` against a real-ish DOM (jsdom), not a separate tier.

**E2E Tests:** Playwright (`web/e2e/`), drives a real `aura serve` process — `web/playwright.config.ts` forwards ~25 explicit env vars (`AURA_DB_URL`, `POSTGRES_*`, `NEO4J_*`, `AURA_OBJECTSTORE_*`, `OPENROUTER_API_KEY`, etc.) into the spawned `webServer` process since Playwright does not inherit the runner's env by default.

**Fuzz Tests:** native Go fuzzing (`go test -fuzz=FuzzName`) in 4 files: `internal/agent/agent_fuzz_test.go`, `internal/canonicaljson/canonicaljson_test.go`, `internal/channels/telegram/mdv2_test.go`, `internal/skills/validator_fuzz_test.go`.

**Property-Based Tests:** `pgregory.net/rapid` for invariant-style assertions across a range of generated inputs, e.g. asserting an escalate Event is always yielded before a loop returns regardless of `n`/`maxIter` combination (`internal/agent/workflow/loop_property_test.go`).

## Common Patterns

**Goroutine-leak-safe fakes** (design pattern, not just a test utility): return pre-closed, pre-buffered channels so a consumer that ranges to completion never blocks and no background goroutine is spawned — see `FakeClient.Stream` in `internal/agent/agenttest/fakeclient.go`.

**Goroutine leak detection at package level:**
```go
func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}
```
with a doc comment explaining exactly which goroutine must be reaped and by what mechanism (see `internal/activelearn/main_test.go`).

**Retry/backoff test isolation:** package-level tunable vars (`toolRetryBaseDelay`) are swapped to near-zero durations for the test and restored via `t.Cleanup`, avoiding real sleeps in unit tests while exercising the real retry loop.

**Error testing:** table-driven with an `error`-typed field and `errors.Is`/`errors.As`/string-marker checks against the classifier function under test (`internal/agent/llm_agent_retry_test.go` — `retryableStreamOpenError`, `retryableToolError`).

**Windows-specific test skips:** the sandboxed shell-tool tests explicitly skip POSIX-only behavior when running under the `cmd.exe` fallback shell (not a CI/env gate, a platform-capability gate): `t.Skip("cmd.exe fallback: interleave fixture is POSIX-only")` (`internal/agent/tools/shell_exec_test.go`, `shell_bg_test.go`).

---

*Testing analysis: 2026-07-04*
