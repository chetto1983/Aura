# Testing Patterns

**Analysis Date:** 2026-08-25

## Test Framework

**Runner:**
- Go 1.26.6 standard `testing` package is the primary runner. The repository contains 995 `*_test.go` files across 72 package directories.
- `go.uber.org/goleak` 1.3.0 provides package-level and per-test goroutine leak detection; 52 Go test files invoke it.
- `pgregory.net/rapid` 1.3.0 provides property-based tests; 21 Go test files use Rapid. Native Go fuzz targets cover additional parsers and validators.
- Vitest 4.1.9 with jsdom, Testing Library React 16.3.2, and V8 coverage runs 208 frontend unit/component test files. Config: `web/vitest.config.ts`.
- Playwright 1.61.0 runs 27 browser specs. Config: `web/playwright.config.ts`.
- Python tests use pytest for `services/ingest/tests/` and `unittest`/`unittest.mock` for release/evidence gates under `scripts/`.

**Assertion Library:**
- Go tests use standard `testing` assertions (`t.Fatal`, `t.Fatalf`, `t.Error`, `t.Errorf`). `stretchr/testify` is only an indirect dependency and is not used by repository tests.
- Frontend tests use Vitest `expect` plus semantic Testing Library queries.
- Python tests use plain `assert` under pytest and `unittest.TestCase` assertions for script contracts.

**Run Commands:**
```bash
make test                         # Go unit tier, uncached (-count=1), owned packages
make test-race                    # Go unit tier with race detector
make tagged-tier-compile          # Discover and compile every custom build-tag tier
make coverage-docker              # Disposable-DB owned-surface coverage gate (preferred locally)
make quality                      # deadcode + vet + build + size + lint + race + vuln
make quality-full                 # quality plus live db_integration coverage

cd web && npm run test            # Vitest with V8 coverage and 85% thresholds
cd web && npx vitest              # Frontend watch mode
cd web && npm run test:e2e        # Playwright browser suite
cd web && npm run mutation        # Stryker mutation campaign

make evidence-contracts           # Python unittest + shell gate self-tests
make ingest-test                  # pytest inside the deployed ingest image
```

Use WSL or CI Linux for the complete Go race/integration/mutation toolchain. Do not run `scripts/coverage_gate.sh` against the live local `aura` database; its anti-footgun intentionally refuses that target. Use `make coverage-docker` or an explicit disposable database.

## Test File Organization

**Location:**
- Go tests are co-located with production packages under `cmd/` and `internal/`. Keep black-box infrastructure fixtures in `testdata/` and shared test helpers in `internal/agent/agenttest/` or `internal/dbtest/`.
- Go integration tests remain in the owning package and use a top-of-file `//go:build <tier>` constraint, such as `internal/db/tx_integration_test.go` and `internal/arcadedb/memory_integration_test.go`.
- Frontend unit tests are either co-located (`web/src/api/json.test.ts`) or under feature-local `__tests__/` directories (`web/src/chat/__tests__/toolGrouping.test.ts`).
- Browser tests live in `web/e2e/*.spec.ts`; screenshots are stored under `web/e2e/__screenshots__/` using Playwright's configured snapshot template.
- Ingest tests live in `services/ingest/tests/test_*.py`; evidence-gate tests are `scripts/*_test.py`.

**Naming:**
- Go: `TestFunction_Behavior`, `TestType_Method`, `BenchmarkName`, and `FuzzName`. Named subtests use `t.Run`.
- TypeScript: `describe('<unit>')` plus sentence-style `it('<observable behavior>')`; parameterized cases use `it.each`.
- Python: `test_<behavior>` functions for pytest and `test_<behavior>` methods on `unittest.TestCase`.

**Structure:**
```text
internal/<package>/
├── feature.go
├── feature_test.go                  # unit/property/fuzz tests
├── feature_integration_test.go      # //go:build db_integration or another live tier
└── testdata/                         # checked-in golden/protocol fixtures

web/src/<feature>/
├── Feature.tsx
├── Feature.test.tsx                 # focused co-located test, or
└── __tests__/Feature.test.tsx       # feature suite

web/e2e/<surface>.spec.ts            # browser/E2E contract
services/ingest/tests/test_<area>.py  # container-side pytest
```

## Test Structure

**Suite Organization:**
```go
func TestOperation(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "normal case", in: "input", want: "output"},
		{name: "boundary", in: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Operation(tt.in)
			if got != tt.want {
				t.Fatalf("Operation(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
```

This matches table-driven cases in `internal/canonicaljson/canonicaljson_test.go`. Add `t.Parallel()` only when the case does not share mutable state, process environment, ports, or a live database. The repository has 1,248 `t.Parallel()` calls, but live Postgres packages deliberately run serially with `-p 1`.

**Patterns:**
- Use Arrange/Act/Assert without ceremonial comments. Create state with `t.TempDir`, `t.Setenv`, and small helpers that call `t.Helper()`.
- Register cleanup immediately with `t.Cleanup`; do not leave databases, goroutines, servers, files, or global redactor state behind.
- Use `t.Context()` for new cancellable tests where the production API accepts a context; use explicit timeout contexts when the timeout itself is part of the contract.
- Use `httptest.Server`, in-memory protocol transports, and hand-written fakes for unit tests. Assert the request and the observable response, not private fields.
- Use `TestMain` plus `goleak.VerifyTestMain(m)` in packages owning background goroutines. See `internal/agent/main_test.go`.
- Keep integration setup fail-loud in CI. `envOrSkip` in `internal/db/db_test.go` skips locally when prerequisites are absent but calls `t.Fatalf` when `$CI` is set.
- For live shared Postgres tiers, use `-p 1` to avoid role/migration/global-table collisions (`scripts/coverage_gate.sh`).
- Prefer exact boundary and mutation-killing assertions over line-only coverage. `TestNewResult_ExactlyCapNoSidecar` in `internal/agent/tools/result_test.go` documents the load-bearing comparison.

Frontend canonical shape:

```typescript
describe('Button', () => {
  it('renders an accessible default action', () => {
    render(<Button>Stage</Button>);
    const button = screen.getByRole('button', { name: 'Stage' });
    expect(button.className).toContain('bg-primary');
  });
});
```

Use semantic role/name queries before test IDs or CSS selectors. `web/src/components/ui/__tests__/button.test.tsx` is the local pattern.

## Mocking

**Framework:**
- Go: hand-written interface fakes/stubs/recorders plus `httptest`; no mock-generation or testify mock framework is established.
- TypeScript: Vitest `vi.fn`, `vi.spyOn`, `vi.mock`, and `vi.stubGlobal`; Testing Library for DOM interaction.
- Playwright: `page.route`/`route.fulfill` for deterministic HTTP/SSE fixtures, with dedicated specs for live server paths.
- Python: `unittest.mock` in evidence-gate tests and pytest `monkeypatch` where local substitution is needed.

**Patterns:**
```go
type recordingProcessor struct {
	requests []Request
	err      error
}

func (r *recordingProcessor) Process(_ context.Context, req Request) error {
	r.requests = append(r.requests, req)
	return r.err
}
```

Use the naming and ownership pattern found throughout `cmd/aura/*_test.go`, `internal/agui/*_test.go`, and `internal/documents/*_test.go`.

```typescript
const fetchMock = vi.fn<typeof fetch>();
vi.stubGlobal('fetch', fetchMock);
fetchMock.mockResolvedValue(new Response(JSON.stringify(payload), { status: 200 }));
```

**What to Mock:**
- Mock the network, model, clock, filesystem, or persistence interface in unit tests when the behavior under test is the caller's decision logic.
- Use recorders to assert call order, arguments, idempotency keys, and cancellation propagation.
- In browser replay specs, mock stable surrounding APIs and feed the real captured AG-UI fixture from `internal/agui/testdata/golden-events.json` through the public UI path.

**What NOT to Mock:**
- Do not mock Postgres/ArcadeDB/Docker behavior in an integration tier whose purpose is to prove the real engine contract. Use disposable resources and real clients.
- Do not replace the production reducer/rendering path with a test-only implementation; drive it through its public API.
- Do not mock implementation details or private helper calls. Assert observable output, persisted state, protocol frames, audit rows, or UI semantics.
- Do not let a missing live prerequisite become a green CI skip.

## Fixtures and Factories

**Test Data:**
```go
func ctxWith(t *testing.T, sessionID, callID string) context.Context {
	t.Helper()
	return tools.WithToolCallContext(t.Context(), sessionID, callID, t.TempDir(), 2048)
}
```

Use factory/helper functions for valid defaults, then override only the field under test. `internal/agent/tools/result_test.go` and `scripts/release_readiness_gate_test.py` demonstrate this pattern.

**Location:**
- Package-specific static fixtures: `<package>/testdata/`, including golden output under `internal/channels/telegram/testdata/`.
- Cross-backend AG-UI golden frames: `internal/agui/testdata/golden-events.json`, shared by Go and browser tests.
- Real document corpus fixtures: `scripts/fixtures/document_pipeline_e2e/`, mounted into the ingest test container by `Makefile`.
- Ephemeral filesystem state: always `t.TempDir()`/pytest `tmp_path`/`tempfile.TemporaryDirectory()`.
- Golden updates require an explicit flag such as `UPDATE_GOLDEN=1` or `-update`; default test runs compare and fail.

## Coverage

**Requirements:**
- Go owned-surface coverage must be at least 85% across the active tag matrix. `scripts/coverage_gate.sh` currently defaults to `db_integration`, runs `./internal/...` with `-p 1`, and computes one aggregate statement ratio.
- The Go coverage filter excludes generated `internal/db/sqlc/`, test-support `internal/agent/agenttest/` and `internal/dbtest/`, and the designated skeleton `internal/llm/client.go`. `cmd/aura` CLI glue is outside the owned-surface statement floor and is covered behaviorally.
- Frontend V8 coverage must meet 85% independently for statements, branches, functions, and lines (`web/vitest.config.ts`). Test files, test setup, and `web/src/main.tsx` are excluded.
- Coverage is a gate, not completion proof. Race, live integration, E2E, mutation, security, and release-evidence gates still apply.
- Do not cite `artifacts/production-readiness/coverage-report.json` as current unless its `candidate_commit` equals `git rev-parse HEAD`; evidence reports are candidate-bound by design.

**View Coverage:**
```bash
make coverage-docker
go tool cover -func=cover_gate.out.filtered
go tool cover -html=cover_gate.out.filtered

cd web && npm run test
# HTML/text artifacts are written under web/coverage/
```

## Mutation Testing

**Requirements:**
- Every critical boundary must kill at least 70% of scored mutants; scores are checked per scope, not averaged (`scripts/critical_mutation_gate.py`).
- Current Go critical scopes are `internal/gateway/classify.go`, `internal/identityctx/operator.go`, `internal/config/config_runtimeprofile.go`, and `internal/sandbox/usersandbox/spec.go`.
- Frontend mutation targets are explicitly enumerated in `web/stryker.config.json`; Stryker's break threshold is 70 and its report is written to `web/reports/mutation/mutation.json`.
- `make critical-mutation` validates the gate parser and fresh evidence. `make web-mutation` runs the frontend campaign.

## Test Types

**Unit Tests:**
- Go unit tests cover pure logic, error paths, HTTP handlers via `httptest`, protocol translation, filesystem guards, and dependency orchestration with hand-written fakes.
- Rapid properties cover serialization, canonicalization, normalization, classification, and boundary invariants. Native fuzz targets exist in `internal/canonicaljson/`, `internal/agent/`, `internal/channels/telegram/`, `internal/obs/`, and `internal/skills/`.
- Vitest/jsdom covers utilities, hooks, reducers, API adapters, components, accessibility semantics, and failure states.
- Python unittest covers evidence parsers and release gates; pytest covers ingest transformations.

**Integration Tests:**
- `db_integration`: live Postgres migrations, RLS, stores, orchestration, and audit behavior. It is the default coverage tag.
- `arcadedb_integration`: live ArcadeDB schema/query/memory behavior. It is race-tested in its own CI job and does not currently feed the default coverage aggregate.
- `docker_integration`: real sandbox lifecycle, execution, and egress; compiled and run separately from coverage.
- Other explicit tiers include `garage_integration`, `web_integration`, `calendar_integration`, `whatsapp_integration`, `telegram_integration`, `multimodal_integration`, `integrations_integration`, and `webauth_integration`.
- Run `make tagged-tier-compile` whenever a custom-tagged test is added so no tier silently rots at compile time.

**E2E Tests:**
- Playwright drives the served cockpit in desktop Chrome and mobile Chrome; mobile Safari is enabled when an HTTPS origin is supplied. CI forbids focused tests, retries once, and retains trace/screenshot/video on failure (`web/playwright.config.ts`).
- Go `live_e2e`, `agent_eval`, `serve_smoke`, and shell smoke scripts exercise real model/channel/service behavior outside the unit tier.
- The ingest suite runs inside the production-like `aura-ingest` image because host Python cannot prove image tools such as LibreOffice and iscc-tika (`Makefile` target `ingest-test`).

## Common Patterns

**Async Testing:**
```go
synctest.Test(t, func(t *testing.T) {
	startWork()
	synctest.Wait()
	if got := state(); got != want {
		t.Fatalf("state = %v, want %v", got, want)
	}
})
```

Use `testing/synctest` for deterministic timer/goroutine behavior where applicable (`internal/channels/telegram/status_pane_test.go`). Otherwise use bounded `select`/timeouts and fail with a diagnostic; never use unbounded sleeps as synchronization.

```typescript
render(<AsyncView />);
await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('ready'));
```

Use Testing Library's async queries/waits for DOM state and Playwright's locator auto-waiting for browser state.

**Error Testing:**
```go
sentinel := errors.New("boom")
err := WithTx(t.Context(), pool, func(*sqlc.Queries) error { return sentinel })
if !errors.Is(err, sentinel) {
	t.Fatalf("WithTx error = %v, want sentinel", err)
}
```

For typed errors use `errors.As`; for Postgres assert SQLSTATE rather than unstable message text. For frontend APIs, reject with `HttpError` and assert the status/reason or the rendered user-facing state.

---

*Testing analysis: 2026-08-25*
