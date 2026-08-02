# Testing Patterns

**Analysis Date:** 2026-08-02

## Test Framework

**Runner:**
- Go standard-library `testing` on Go 1.26.5 (`go.mod`) is the primary backend runner. `go.uber.org/goleak` supplies goroutine-leak gates and `pgregory.net/rapid` supplies property-based generators.
- Vitest 4.1.9 runs frontend unit/component tests in jsdom with V8 coverage. Config: `web/vitest.config.ts`; shared environment setup: `web/src/test/setup.ts`.
- Playwright 1.61 runs browser E2E tests against the served, embedded application. Config: `web/playwright.config.ts`; tests: `web/e2e/*.spec.ts`.
- Python quality-gate tests use the standard-library `unittest` runner under `scripts/`. Shell gate contracts use executable `scripts/*_test.sh` files with `set -euo pipefail`.

**Assertion Library:**
- Go uses `testing.T`/`testing.B`/`testing.F` methods and `rapid.T`; `testify` is not used by repository tests.
- Frontend tests use Vitest `expect`, Testing Library DOM queries/events, and Playwright `expect`. Accessibility E2E injects `axe-core` directly in `web/e2e/graph-a11y.spec.ts`.
- Python uses `unittest.TestCase` assertions and `unittest.mock`.

**Run Commands:**
```bash
make test                         # Go unit tier, no build tags
make test-race                    # Go unit tier with race detector
make tagged-tier-compile          # compile every discovered live/integration/eval tag
make coverage-docker              # disposable-stack combined coverage gate (recommended locally)
make quality                      # vet, build, lint, deadcode, race, vuln, file-size
make quality-full                 # quality plus >=85% combined coverage
cd web && npm run test            # Vitest with enforced V8 coverage
cd web && npm run test:e2e        # Playwright browser matrix
cd web && npm run mutation        # Stryker mutation suite
make web-quality                  # frontend lint, unit coverage, and mutation
make evidence-contracts           # Python + shell release-gate self-tests
```

## Test File Organization

**Location:**
- Co-locate Go tests with their package under `cmd/`, `internal/`, and `observability/`. Most use the implementation package for white-box coverage; public-contract suites use an external package such as `package agent_test` in `internal/agent/llm_agent_test.go`.
- Put reusable Go fixtures in package-local `testdata/` directories, including `internal/agui/testdata/`, `internal/agent/testdata/`, and `internal/llm/openai_compat/testdata/`. Put shared deterministic LLM fakes in `internal/agent/agenttest/`.
- Co-locate frontend tests either beside the module or in a feature `__tests__/` directory. Keep cross-page/browser flows in `web/e2e/` and shared browser helpers in `web/e2e/support/`.
- Keep release-evidence parser/gate tests beside their scripts in `scripts/`. The `finetune/aura_finetune/` Python spike has no detected automated test suite.

**Naming:**
- Go: `<subject>_test.go`, with meaningful tier suffixes such as `tx_integration_test.go`, `classify_property_test.go`, `validator_fuzz_test.go`, and `budget_bench_test.go`.
- TypeScript: `*.test.ts(x)` for Vitest and `*.spec.ts` for Playwright.
- Python/shell: `*_test.py` and `*_test.sh`.

**Structure:**
```text
internal/<package>/
├── <subject>.go
├── <subject>_test.go
├── <subject>_integration_test.go   # optional; //go:build <tier>
├── main_test.go                    # optional package-wide goleak gate
└── testdata/                       # package-owned golden/captured fixtures

web/src/<feature>/
├── <Module>.tsx
├── <Module>.test.tsx               # co-located option
└── __tests__/<Module>.test.tsx      # grouped option

web/e2e/<flow>.spec.ts
scripts/<gate>_test.{py,sh}
```

## Test Structure

**Suite Organization:**
```go
func TestSanitizeName(t *testing.T) {
	tests := []struct {
		name    string
		skill   string
		dir     string
		wantErr bool
	}{
		{"valid lowercase", "xlsx", "xlsx", false},
		{"uppercase rejected", "Xlsx", "Xlsx", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SanitizeName(tt.skill, tt.dir)
			if tt.wantErr && err == nil {
				t.Fatalf("SanitizeName(%q,%q) = nil, want error", tt.skill, tt.dir)
			}
		})
	}
}
```
Pattern source: `internal/skills/validator_test.go`.

**Patterns:**
- Use behavior-oriented names and assert observable contracts, including negative/fail-closed paths. Prefer table-driven subtests when inputs share one contract; use standalone tests when setup or invariants differ materially.
- Call `t.Helper()` in fixture/assertion helpers so failures point to the caller. Register cleanup with `t.Cleanup`, allocate files with `t.TempDir`, and isolate environment changes with `t.Setenv`.
- Use `t.Parallel()` only for tests that do not share process globals, ports, databases, or mutable fixtures. Integration packages sharing Postgres run serially with `-p 1`.
- Use `t.Context()` for new context-aware unit tests where the test lifetime is the correct cancellation boundary. Use explicit timeouts when exercising processes, networks, or lifecycle drains.
- Use `TestMain` plus `goleak.VerifyTestMain(m)` for goroutine-owning packages; examples include `internal/runner/main_test.go`, `internal/db/db_test.go`, and `internal/sandbox/usersandbox/main_test.go`.
- Frontend suites use `describe`/`it`, render through Testing Library, and query by accessible role/name before falling back to text or selectors. `web/src/chat/displays/__tests__/SourcesButton.test.tsx` is representative.
- `web/src/test/setup.ts` owns jsdom polyfills and calls Testing Library `cleanup()` after each test. Add missing browser APIs there only when the real browser supplies them and the no-op/fake preserves the unit-test contract.

## Mocking

**Framework:** Hand-written Go fakes; Vitest `vi`; Playwright route interception; Python `unittest.mock`.

**Patterns:**
```go
type FakeClient struct {
	mu       sync.Mutex
	Turns    []FakeTurn
	Requests []llm.Request
	next     int
}

var _ llm.Client = (*FakeClient)(nil)
```
Pattern source: `internal/agent/agenttest/fakeclient.go`.

- Implement the consumer-owned interface, record calls/arguments, script ordered responses, and expose only the inspection helpers required by tests. Protect fakes with a mutex when production code can call them concurrently.
- Keep package-specific fakes in `*_test.go` (for example `internal/runner/fakes_test.go`) and reusable cross-package fakes in a clearly named support package (`internal/agent/agenttest/`). Inject errors explicitly to exercise failure paths.
- In Vitest, use `vi.fn()` for callbacks and `vi.mock()` for module boundaries; stub `fetch`/browser APIs at the narrowest scope and restore them in teardown. Prefer Testing Library interaction over calling component internals.
- In Playwright, use `page.route()` to supply deterministic API/SSE legs while exercising the real built SPA, router, state adapters, and DOM. `web/e2e/chat.spec.ts` uses captured AG-UI golden frames for this purpose.
- In Python, patch process/network boundaries with `mock.patch.object` and assert both command order and fail-closed recovery, as in `scripts/rollback_rehearsal_test.py`.

**What to Mock:**
- LLM/network clients, time/ID sources, subprocess runners, browser APIs missing from jsdom, and narrow storage interfaces in unit tests.
- External HTTP responses in deterministic frontend E2E when the goal is the browser/runtime integration rather than the external provider.

**What NOT to Mock:**
- Postgres transaction/RLS/migration behavior: exercise a real migrated database in `db_integration` tests such as `internal/db/tx_integration_test.go`.
- Docker sandbox lifecycle/egress, ArcadeDB memory behavior, Garage object storage, or sidecar protocols when testing those integration contracts; use their dedicated live build tags and CI jobs.
- Component accessibility semantics: render the real component and query roles/names; Playwright/axe tests must inspect the actual browser DOM.

## Fixtures and Factories

**Test Data:**
```typescript
function source(over: Partial<DisplaySource> & { ref_id: string; index: number }): DisplaySource {
  return { type: 'web_result', title: 'Source', url: 'https://example.test', ...over };
}

const ALPHA = source({ ref_id: 'src-1', index: 1, title: 'Alpha', cited: true });
```
The TypeScript form of this factory pattern appears in `web/src/chat/displays/__tests__/SourcesButton.test.tsx`; Go uses analogous typed constructors such as `agenttest.MakeToolCall` in `internal/agent/agenttest/fakeclient.go`.

**Location:**
- Store immutable captured/golden files under the owning package's `testdata/`. `web/vitest.config.ts` intentionally permits the frontend SSE reducer to read the real captured fixture from `internal/agui/testdata/` so Go and TypeScript validate the same wire frames.
- Generate per-test filesystem data under `t.TempDir()` or Python `tempfile.TemporaryDirectory()`; do not write transient fixtures into the repository.
- Create real database state through migration/setup helpers and register cleanup with `t.Cleanup`. Use reserved identifiers/version bands when concurrent suites could collide, as documented in `internal/db/tx_integration_test.go`.
- Shell gate self-tests create isolated fixtures with `mktemp -d` and always remove them with `trap`, as in `scripts/tagged_tier_compile_test.sh`.

## Coverage

**Requirements:**
- The owned Go surface must be at least 85% across the configured tag matrix. `scripts/coverage_gate.sh` measures `./internal/...` with atomic coverage, defaults to `db_integration`, runs packages serially, and compares the exact covered/total statement ratio rather than the rounded display value.
- The Go gate excludes generated `internal/db/sqlc/`, shared test support `internal/agent/agenttest/`, and the explicitly excluded skeleton `internal/llm/client.go`. `cmd/aura` is behaviorally covered but intentionally outside the statement floor.
- Frontend statements, branches, functions, and lines must each remain at least 85%. `web/vitest.config.ts` includes all `web/src/**/*.{ts,tsx}` except tests, test setup, and the browser bootstrap `web/src/main.tsx`.
- Critical mutation scopes must kill at least 70% of scored mutants. Frontend targets and thresholds live in `web/stryker.config.json`; the cross-language per-boundary gate is `scripts/critical_mutation_gate.py`.

**View Coverage:**
```bash
make coverage-docker
go tool cover -func=cover_gate.out.filtered
go tool cover -html=cover_gate.out.filtered
cd web && npm run test
# Frontend HTML report: web/coverage/index.html
```

- Prefer `make coverage-docker` locally because it provisions disposable database state. `scripts/coverage_gate.sh` refuses a local `db_integration` run against a database named `aura` unless an explicit dangerous override is set.
- The machine-readable Go report is written to `artifacts/production-readiness/coverage-report.json`; Stryker writes `web/reports/mutation/mutation.json`.

## Test Types

**Unit Tests:**
- Fast, deterministic, service-free tests cover pure validation, state machines, adapters, reducers, formatters, and error paths. Run Go unit tests with `-race`; use hand-written fakes instead of real network/model calls.
- Property tests use `pgregory.net/rapid` for invariants such as fail-closed action classification (`internal/gateway/classify_property_test.go`) and canonical JSON determinism (`internal/canonicaljson/canonicaljson_test.go`).
- Native fuzz targets seed protocol/security edge cases and remain runnable with `go test -fuzz=...`; `internal/skills/validator_fuzz_test.go` also carries a deterministic 10K-case corpus for ordinary CI.

**Integration Tests:**
- Put service-dependent tests behind explicit build tags. Current tiers include `db_integration`, `arcadedb_integration`, `docker_integration`, `garage_integration`, `web_integration`, `multimodal_integration`, `calendar_integration`, `telegram_integration`, `integrations_integration`, `serve_smoke`, and paid/live E2E tags.
- Tagged tests may skip locally when prerequisites are absent, but helpers must call `t.Fatal` when `CI` is set. Follow the `envOrSkip` pattern in `internal/db/db_test.go`; a CI job must export the exact variables the test reads.
- `scripts/tagged_tier_compile.sh` discovers every non-structural build tag, fails on unknown tags, and compiles each tagged package even when its live job is conditional.

**E2E Tests:**
- Playwright runs the real embedded web application through `aura serve`, with desktop Chrome and mobile Chrome by default and mobile Safari when an HTTPS test origin is supplied. `web/playwright.config.ts` forbids focused tests in CI, retries once in CI, and retains trace/screenshot/video evidence on failure.
- Browser flows assert accessibility, responsive behavior, authentication expiry, streamed AG-UI events, approvals, artifacts, governance, documents, onboarding, voice, and graph behavior under `web/e2e/`.
- Live Go E2E/smoke tests use dedicated build tags and must remain outside the default unit tier when they require paid APIs, real daemons, or spawned binaries.

## CI Practices

- `.github/workflows/ci.yml` separates static/build gates, race-enabled unit tests, vulnerability scanning, database integration, coverage, ArcadeDB, Docker sandbox, sidecar integrations, frontend lint/test/mutation/dist-freshness, and Playwright E2E into named jobs.
- The baseline Go job runs tier compilation, `go vet`, `go build`, the 600-line cap, boundary checks, `deadcode`, and pinned `golangci-lint` 2.12.2. The unit job runs `go test -race -count=1` over owned packages.
- Shared-Postgres integration jobs use `-p 1` to prevent role/migration/advisory-lock collisions. Race-plus-goleak DB coverage is repeated over orchestration-heavy packages in the `race-db-integration-gates` job.
- Frontend change filters have explicit no-skip-as-green backstops. CI runs `npm ci` before lint/typecheck/format, Vitest coverage, Stryker, fresh embedded-dist comparison, and Playwright.
- CI pins third-party actions to immutable SHAs and tests its own workflow/gate parsers through `scripts/workflow_pin_gate_test.sh`, `scripts/coverage_gate_test.sh`, and Python evidence-contract suites.
- Pre-commit hooks in `lefthook.yml` run staged Go formatting, vet, whole-package lint, file-size checks, and frontend duplication checks. Pre-push hooks add build, tagged-tier compilation, dead-code checks, frontend static checks, and quality-snapshot freshness; heavy race/integration/coverage/mutation/E2E work remains in Make targets and CI.

## Common Patterns

**Async Testing:**
```typescript
render(<SourcesButton sources={sources} onOpen={onOpen} />);
fireEvent.click(screen.getByRole('button', { name: 'View 2 sources' }));
expect(onOpen).toHaveBeenCalledWith(sources);
```
For asynchronous React work, await `waitFor` or a `findBy*` query. In Playwright, await role-based locators and `expect(...).toBeVisible()`; never use fixed sleeps when a readiness condition exists.

**Error Testing:**
```go
err := WithTx(ctx, pool, func(q *sqlc.Queries) error {
	return sentinel
})
if !errors.Is(err, sentinel) {
	t.Fatalf("WithTx: want sentinel, got %v", err)
}
```
Assert the stable sentinel/type/status plus externally visible effects. For transactional failures, also assert rollback state; for frontend failures, assert the user-visible error or auth state; for gate parsers, assert malformed/stale/missing evidence fails closed.

---

*Testing analysis: 2026-08-02*
