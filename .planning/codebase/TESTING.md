# Testing Patterns

**Analysis Date:** 2026-07-17

**Provenance rule for this document:** every count, version, threshold, and tag below was produced by a command run on 2026-07-17 against the working tree, or read out of the named config file. Facts that could not be verified are marked **NOT VERIFIED**. Do not copy a number here from a sibling doc.

**Measured surface (2026-07-17):** **143,580** LOC of tests across **777** `*_test.go` files, against **98,150** LOC of non-test Go (**602** files) — ratio **~1.46:1**. There is more test code than production code, and that is the point: the test discipline below *is* the convention.

## Test Framework

**Runner:** Go stdlib `testing` (Go **1.26.5**). No third-party test framework.

**Assertion library: none — this is deliberate and load-bearing.**
- `github.com/stretchr/testify v1.11.1` appears in `go.mod` as **`// indirect`** and is imported by **zero** test files. **Do not introduce it.**
- `gopter` is **not present in the module at all** (verified against `go.mod`). If a sibling doc says "property-based via gopter/rapid", it is wrong — see the property-based section below.

**Direct test dependencies (`go.mod`):**
| Library | Version | Role |
|---|---|---|
| `go.uber.org/goleak` | v1.3.0 | goroutine-leak detection |
| `pgregory.net/rapid` | v1.3.0 | property-based testing |

**Run commands (`Makefile`):**
```bash
make test          # go test -count=1 $(GO_PACKAGES)  — unit tier, no build tags
make test-race     # go test -race -count=1 $(GO_PACKAGES)
make quality       # vet file-size lint deadcode test-race vuln (+ go build); no containers
make quality-full  # quality + coverage gate (requires the container stack up)
make coverage      # scripts/coverage_gate.sh — owned-surface floor ≥85%
make coverage-docker  # same floor, mcp-neo4j-cypher in a container, DISPOSABLE db (preferred locally)
make web-test      # cd web && vitest run --coverage
make web-mutation  # cd web && stryker run (break=70)
make smoke         # scripts/neo4j_smoke.sh (Italian recall@5 hard gate, p95 reported)
```

`GO_PACKAGES` comes from `scripts/go_packages.sh`, not a bare `./...` — package selection is centralized so every gate scans the same set.

## Test File Organization

**Location:** co-located with source, same directory, same package (or `package x` white-box via `*_internal_test.go`).

**Naming — the suffix tells you the tier:**

| Pattern | Meaning | Example |
|---|---|---|
| `<src>_test.go` | unit, mirrors the source file | `internal/share/expiry_test.go` |
| `*_integration_test.go` | build-tagged, needs live infra | `internal/db/db_test.go`, `internal/agui/server_integration_test.go` |
| `*_property_test.go` | rapid property-based | `internal/swarm/swarm_property_test.go` |
| `*_fuzz_test.go` | native Go fuzzing | `internal/agent/agent_fuzz_test.go` |
| `*_internal_test.go` | same-package white-box | `internal/agent/llm_agent_pause_internal_test.go` |
| `main_test.go` | package-wide `TestMain` (usually goleak) | `internal/runner/main_test.go` |

**Fixtures:** **8** `testdata/` directories — `internal/agent/`, `internal/agent/tools/`, `internal/agui/`, `internal/channels/telegram/`, `internal/eval/`, `internal/llm/`, `internal/llm/openai_compat/`, plus a repo-root `./testdata`.

**The 600-LOC cap applies to tests too** (`scripts/check-file-size.sh` — tests are **not** exempt). A growing suite splits by concern, e.g. `internal/sandbox/usersandbox/`: `lifecycle_integration_test.go`, `egress_integration_test.go`, `reap_integration_test.go`, `materialize_test.go`, `docker_backend_integration_test.go`, `bench_soak_test.go`.

## Test Structure

**Table-driven is the default.** Reference shape (`internal/share/expiry_test.go`):

```go
// TestResolveExpiry is table-driven over every expiry row of the plan's
// <behavior> block (D-04): default, the three presets, custom within cap,
// custom clamped above cap, and the two rejected non-positive custom cases.
// now is fixed (never time.Now()) so assertions are exact.
func TestResolveExpiry(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	const cap = 90

	tests := []struct {
		name       string
		opt        ExpiryOption
		customDays int
		capDays    int
		want       time.Time
		wantErr    error
	}{
		{name: "default option resolves to 7 days", opt: ExpiryDefault, capDays: cap, want: now.AddDate(0, 0, 7)},
		{name: "empty option (zero value) resolves to 7 days", opt: "", capDays: cap, want: now.AddDate(0, 0, 7)},
		// …
	}
	// subtests via t.Run(tt.name, …)
}
```

Conventions visible in that file, all worth imitating:
- The test doc comment names **which plan/decision rows it covers** (`D-04`, `T-37F-27`) — traceability, not decoration.
- **Fixed clock, never `time.Now()`** — assertions are exact, not tolerance-based.
- `wantErr error` compared with `errors.Is`, not string matching.
- Both the happy path and every rejection row are enumerated; `dupl` is disabled on `_test.go` precisely so this repetition is allowed.

**Parallelism:** `t.Parallel()` appears in **119** test files. Note the hard exception below — integration tiers run with `-p 1`.

**`t.Cleanup`** for teardown (deterministically unblocks fakes and joins workers so zero goroutines remain at return — see `internal/runner/runner_stop_leak_test.go`).

## Goroutine-Leak Detection (goleak)

**105** test files reference `goleak`. Two shapes:
- **`goleak.VerifyTestMain(m)`** — package-wide, **40** files. Preferred.
- **`goleak.VerifyNone(t)`** — per-test, **13** files.
- `IgnoreTopFunction` escape hatch: only **2** uses. Keep it that way — a suppression needs a why-comment.

```go
// internal/runner/main_test.go
// TestMain runs the whole runner package (unit tier — no DB, no network) under
// goleak so the auto-title WaitGroup join is asserted: a worker that is not joined
// by Runner.Stop leaks a goroutine and fails the package (Pitfall 3 / D-A5-01).
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
```

Any package that starts goroutines gets a goleak `TestMain`. `-race` runs on every unit push (`make test-race`, CI `unit-test`).

## Property-Based Testing (rapid)

`pgregory.net/rapid` v1.3.0. **18** test files, spanning the invariant-heavy surfaces:

`internal/agent/budget_dedup_test.go`, `internal/agent/event_test.go`, `internal/agent/tools/bm25_test.go`, `internal/agent/tools/result_test.go`, `internal/agent/workflow/loop_property_test.go`, `internal/agui/auth_test.go`, `internal/agui/content_disposition_test.go`, `internal/agui/translator_test.go`, `internal/agui/translator_reasoning_test.go`, `internal/canonicaljson/canonicaljson_test.go`, `internal/config/config_knobs_test.go`, `internal/gateway/classify_property_test.go`, `internal/llm/openai_compat/accumulate_test.go`, `internal/mcp/manager/envedit_property_test.go`, `internal/sandbox/usersandbox/translate_test.go`, `internal/scoring/scoring_test.go`, `internal/share/share_property_test.go`, `internal/swarm/swarm_property_test.go`.

Reach for rapid when the contract is an **invariant** (round-trip, idempotence, ordering, clamping, canonicalization) rather than a fixed example.

## Fuzzing

Native Go fuzzing — **8** `Fuzz*` functions, all on untrusted-input parsers:

| Function | File |
|---|---|
| `FuzzParseTextResponse`, `FuzzNormalizeContentStopAnswer`, `FuzzCanonicalArgs`, `FuzzWrapUntrustedToolOutput`, `FuzzRenderToolResultForPrompt` | `internal/agent/agent_fuzz_test.go` |
| `FuzzCanonical_RoundTripAndDistinctNumbers` | `internal/canonicaljson/canonicaljson_test.go` |
| `FuzzMdv2` | `internal/channels/telegram/mdv2_test.go` |
| `FuzzSkillValidator` | `internal/skills/validator_fuzz_test.go` |

Rule of thumb from the existing set: **anything that parses LLM output, user content, or a skill manifest gets a fuzzer.**

## Mocking

**No mocking framework.** Hand-written fakes — **170** `fake*`/`stub*`/`mock*` type declarations across `internal/` tests.

**Shared fakes** live in `internal/agent/agenttest/` (`fakeclient.go`, `mocks.go`) rather than duplicated per consumer. It is a test-support package and is **excluded from the coverage floor** (`scripts/coverage_gate.sh:64`) — its self-coverage measures no owned runtime.

**Local fakes** are unexported structs next to their test: `fakeOracle` (`internal/activelearn/learner_test.go`), `stubAgent` (`internal/agent/agent_test.go`), `fakeReserveStore`, `fakeSearchEngine`, `fakeEmbedder`, `fakeServer`, `fakeReconnectClient`.

**What to mock:** the LLM client, external HTTP, MCP servers, embedders, clocks (as parameters — see `ResolveExpiry`).

**What NOT to mock:** Postgres and Neo4j. There is no in-memory DB substitute — persistence is exercised against the **real containers** under build tags. **88** test files use `net/http/httptest` for HTTP seams rather than mocking the transport.

## Build-Tagged Tiers

Tags in use across `*_test.go` (file counts, measured 2026-07-17):

| Tag | Files | Runs in CI? |
|---|---:|---|
| `db_integration` | 83 | **Yes** — `integration-test`, `knowledge-integration-test`, `skills-gate` |
| `docker_integration` | 9 (+6 more files carry it in combination; 15 files total reference it) | **NO — see the gap below** |
| `neo4j_integration` | 4 (+ combos) | **Yes** — `knowledge-integration-test` |
| `cot_eval` | 10 | No — paid/live, opt-in only |
| `live_e2e` | 5 | **NOT VERIFIED** |
| `reasoning_live` | 5 | No — live model tier |
| `memory_integration` | 3 | Yes — `memory-integration-test` |
| `web_integration` / `!web_integration` | 2 / 11 | Yes — `web-integration-test` |
| `smoke`, `serve_smoke` | 1 / 1 | Yes — `make smoke` in `knowledge-integration-test` |
| `garage_integration`, `telegram_integration`, `whatsapp_integration`, `webauth_integration`, `calendar_integration`, `calculator_integration`, `multimodal_integration`, `rerank_integration`, `integrations_integration`, `authula_integration`, `musr_e2e`, `retrieval_eval`, `graphrag_live`, `document_ingest_live`, `live_finalize`, `backup_live` | 1–2 each | Mixed — several are **compile-only** in CI (`go vet -tags …`), see below |

**Compile-only tiers.** Some GPU/fixture-dependent tiers are `go vet -tags …`-compiled in CI but never `go test`-run there (`ci.yml`, `knowledge-integration-test`): `rerank_integration`, `document_ingest_live`, `graphrag_live`, `retrieval_eval`. That is an intentional "cheap always-green floor proving the tagged files never rot" — their test code still `t.Fatal`s under `$CI` when env is set, so a GPU runner with the sidecar up runs them live and cannot silently skip.

Invocation example:
```bash
go test -tags 'db_integration neo4j_integration' -p 1 ./internal/...
```

## NO-SKIP-AS-GREEN (the rule that governs every tier)

A skipped integration test must **never** pass as green. Every skip-helper `t.Fatal`s when a required env var is unset **and** `$CI` is set; locally it still skips.

Canonical implementation (`internal/db/db_test.go:34`):
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

The helper is replicated per package under a domain-specific name — `recoveryEnvOrSkip` (`cmd/aura/recovery_integration_test.go`), `skillsBridgeEnvOrSkip`, `smokeEnvOrSkip`, `musrEnvOrSkip`, `assetEnvOrSkip`, `liveEnvOrSkip`, `sidecarEnvOrSkip` (`internal/channels/telegram/multimodal_integration_test.go`) — all with the same `$CI` → `t.Fatal` semantics. **Any new tier must add one.**

**Diagnostic:** a sub-second "integration" runtime is a skip tell. Verify execution, not just `PASS`.

**Corollary for CI wiring:** CI jobs must export the exact env the tests read — the **composed DSNs** `AURA_DB_URL` / `AURA_DB_MIGRATE_URL`, not just the `POSTGRES_*` primitives that `config.Load` composes for the CLI.

## Coverage

**Gate:** `scripts/coverage_gate.sh`.

**Floor: ≥85%** — `MIN="${AURA_COVERAGE_MIN:-85}"` (`scripts/coverage_gate.sh:24`). This is the CI-enforced, verifiable bar and it overrides the PRD's older ≥75% unit / ≥60% integration split. A bare unit-only number under 85% is **not** an acceptable closing metric — report the combined full-matrix figure.

**Do not quote a punctual percentage from any document, including this one.** The last full-matrix measurement predates recent phases and is stale by construction. For current per-row numbers read **`docs/aura-quality-snapshot.md`**, which is re-attested per phase under amendment #20. What is durably true and worth writing down is the **floor**.

**Scope — the "owned surface":** `./internal/...`, minus rows filtered at `scripts/coverage_gate.sh:64`:
- `/internal/db/sqlc/` — generated, golden
- `/internal/agent/agenttest/` — test-support (shared fakes); its low self-coverage dilutes the floor without measuring owned runtime (T-04)
- `/internal/llm/client.go:` — pre-rewrite skeleton owned by Slice 1

`cmd/aura` is excluded by scope: it is CLI glue (flag parsing, `os.Exit` dispatch) covered behaviourally by integration + smoke; folding it into a statement floor measures the wrong thing (it sits ~20% by nature). Filters are anchored at a path-segment boundary (`/<x>`) so a future sibling whose name merely *contains* one of these is not silently dropped.

**`-p 1` is MANDATORY** and must not be "optimized" away. The integration tiers across `internal/*` share ONE Postgres, so concurrent packages collide on global cluster state: `CREATE ROLE` (`EnsureRoles`) races to `tuple concurrently updated (XX000)` on `pg_authid`, and golang-migrate's `pg_advisory_lock` deadlocks. The default parallel run went flaky once >2 integration packages existed.

**The gate runs in TWO CI jobs — and they are not the same gate:**

| Job | File | Tags | Note |
|---|---|---|---|
| `knowledge-integration-test` | `.github/workflows/ci.yml:651` | `db_integration neo4j_integration` (default) | reuses the stack + composed DSNs + `mcp-neo4j-cypher` already wired in the job |
| `skills-gate` | `.github/workflows/skills.yml:158` | **`db_integration` ONLY** (`AURA_COVERAGE_TAGS: "db_integration"`) | **the stricter one** — no neo4j tier folds in, so the same code must clear 85% on a narrower tag set. Verify THIS number at phase close. |

**Local run — the data-loss footgun.** Prefer `make coverage-docker` (containerized `mcp-neo4j-cypher`, provisions a **disposable** DB and drops it on exit). The gate refuses `db_integration` against a database named `aura` when `GITHUB_ACTIONS` is unset:
```
FATAL: refusing db_integration coverage against the live 'aura' database — it TRUNCATEs
       shared auth tables (data loss, see the 2026-07-10 incident).
```
Exit code **5**. Escape hatch `AURA_COVERAGE_ALLOW_LIVE_AURA_DB=1` (danger). This guard exists because on 2026-07-10 the gate truncated the live deployment's auth tables — operator identity + `authula` wiped, no backup. CI provisions a fresh ephemeral `aura` and always sets `GITHUB_ACTIONS`, so it is exempt.

**Gate failure modes are loud, never silent:** a failed test run dumps the log and exits 1 (never discarded, so a real failure can't look like a coverage miss); an over-aggressive filter that leaves zero statement rows also fails.

## KNOWN GAP — `docker_integration` contributes ZERO coverage

**Verified 2026-07-17: `grep -rn "docker_integration" .github/workflows/` returns ZERO matches. There is no `docker_integration` CI job.**

Consequence: every `//go:build docker_integration` test **compiles and skips in CI and contributes nothing to the coverage floor**. The affected runtime is real:

- `internal/sandbox/usersandbox/` — DockerBackend lifecycle / exec / egress / reap: `docker_backend_integration_test.go`, `docker_backend_egress_test.go`, `lifecycle_integration_test.go`, `egress_integration_test.go`, `reap_integration_test.go`, `materialize_test.go`, `router_tools_test.go`, `bench_soak_test.go`, `main_test.go`
- `internal/agent/tools/` — routed sandbox branches: `shell_exec_sandbox_docker_test.go`, `shell_bg_sandbox_docker_test.go`, `shell_bg_sandbox_test.go`, `send_file_sandbox_test.go`, `owner_coverage_test.go`
- `cmd/aura/serve_dispatch_egress_integration_test.go`

This is not theoretical: it is **why the CAP_NET_ADMIN cap-assertion bug (WR-01) stayed latent**.

**Mandatory mitigation when adding daemon/container-gated code:** you MUST also write **daemon-free unit tests for its pure logic** — spec/tar builders, path-traversal + symlink guards, nil/disabled early-return paths, structural-capability "not supported" errors. Otherwise the aggregate silently drops below 85% and CI fails ~20 min after push. Verify locally **before** pushing with `bash scripts/coverage_docker.sh`. A green local full-matrix run is worth more than a push-and-wait CI cycle.

## Mutation Testing

**Tool:** `go-mutesting` — the **`github.com/avito-tech/go-mutesting`** fork (`Makefile` `tools:`); it is the only fork supporting go1.26. **WSL-only** in practice.

**Policy floor: ≥70% killed**, spot-checked on each phase's critical file(s) and documented in the phase `VALIDATION.md` Manual-Only table. As with coverage, **state the floor, not a measurement** — per-file numbers live in `docs/aura-quality-snapshot.md` and the phase validation docs.

**Enforcement is tiered (`.github/workflows/skills.yml`):**
- `internal/skills/validator.go` — **hard gate**: score `< 0.70` fails the job.
- Other files — **advisory**: a sub-0.70 score prints `advisory: … (db-subprocess artifact, witnessed by live db_integration tests)` and does not fail.

Reading the output: `PASS` = mutant **killed**; `FAIL` = mutant **survived**; score = killed/total. go-mutesting's final line is `mutation score is 0.894118 (76 passed, …)`.

For container/DB-gated code add `GOFLAGS=-tags=db_integration` plus the DSN env, or every mutant "survives" as a subprocess artifact.

**Before chasing a score:** autopsy the survivors. `%w`-dense error-wrap paths are frequently near-equivalent mutants — classify them, kill what has a real seam, and advisory-accept the rest rather than contorting tests.

## Frontend Testing (`web/`)

**The frontend gates match the Go floors — that parity is deliberate.**

**Unit + coverage — vitest** (`web/vitest.config.ts`):
```ts
coverage: {
  provider: 'v8',
  include: ['src/**/*.{ts,tsx}'],
  // main.tsx is the bootstrap entry (createRoot + render) — behaviourally proven by
  // the Playwright E2E against the served shell, not unit-testable in jsdom.
  exclude: ['src/**/*.{test,spec}.{ts,tsx}', 'src/test/**', 'src/main.tsx'],
  // Frontend quality gate: parity with the Go backend's ≥85% floor.
  thresholds: { statements: 85, branches: 85, functions: 85, lines: 85 },
}
```
The suite **fails** below 85% — coverage cannot silently regress.

**Mutation — Stryker** (`web/stryker.config.json`): `testRunner: "vitest"`, `coverageAnalysis: "perTest"`, `concurrency: 4`, `thresholds: { high: 85, low: 70, break: 70 }` — **`break: 70` fails the run below 70% killed**, matching the Go ≥70% policy. `mutate` is an **explicit allowlist of 17 files** (logic-bearing modules: `src/api/json.ts`, `src/chat/artifacts/*`, `src/chat/voice/*`, `src/chat/displays/*`, `src/graph/*`, `src/governance/*`, `src/approvals/approvalState.ts`, `src/onboarding/*`), not a glob — add new logic modules to it deliberately. Uses a separate `vitest.stryker.config.ts`.

**Scripts (`web/package.json`):**
```bash
npm run lint        # eslint . --max-warnings=0   (the golangci-lint parity bar)
npm run typecheck   # tsc --noEmit
npm run format:check# prettier --check .
npm run test        # vitest run --coverage
npm run test:e2e    # playwright test
npm run mutation    # stryker run
npm run dup         # jscpd@4   (dupl parity)
npm run deadcode    # knip@5    (deadcode parity)
```

**Platform note:** WSL has no Node. Run vitest / tsc / prettier / playwright on **Windows Git Bash**; run Go build/test/`-race` in **WSL**.

## CI Job Map

**`.github/workflows/ci.yml`** — 24 jobs: `build-and-lint`, `unit-test`, `windows-unit`, `cache-invariant`, `vulncheck`, `integration-test`, `musr-e2e`, `sqlc-golden`, `web-integration-test`, `knowledge-integration-test`, `multimodal-integration-test`, `telegram-integration-test`, `memory-integration-test`, `calendar-integration-test`, `integrations-proxy-test`, `web-lint`, `web-test`, `web-mutation`, `web-dist-freshness`, `web-e2e`, `compaction-evaluator`, `compaction-distributed-gates`, `compaction-mutation`, `compaction-e2e-acceptance`.

**`.github/workflows/skills.yml`** — `skills-gate` (mutation hard-gate on `internal/skills/validator.go` + the stricter `db_integration`-only coverage gate).

Also: `.github/workflows/codeql.yml`, `.github/workflows/release.yml`.

Notable: `multimodal-integration-test` sets `CI: "true"` explicitly *to arm the `sidecarEnvOrSkip` no-skip-as-green guards* — the guard is the reason the env var is set, not an accident.

## Writing a New Test — Checklist

1. Co-locate; name it for its tier (`_integration_test.go`, `_property_test.go`, `_fuzz_test.go`).
2. Table-driven; fixed clock (never `time.Now()`); `errors.Is` for error assertions.
3. Doc-comment the decision/plan rows the test covers (`D-04`, `T-37F-27`).
4. Package starts goroutines → add `goleak.VerifyTestMain(m)` in `main_test.go`.
5. Invariant rather than examples → reach for `rapid`. Parses untrusted input → add a `Fuzz*`.
6. Needs live infra → build tag + an `envOrSkip` helper that `t.Fatal`s under `$CI`, **and** wire the env in the CI job.
7. Real Postgres/Neo4j, never an in-memory substitute. Fakes for LLM/HTTP/MCP; shared ones in `internal/agent/agenttest/`.
8. Adding `docker_integration`-gated runtime → **also** add daemon-free unit tests for the pure logic (see the gap above).
9. Keep the file ≤600 LOC — tests are not exempt.
10. Before pushing: `make quality`; if the change touches DB/graph surface, `bash scripts/coverage_docker.sh`.

---

*Testing analysis: 2026-07-17*
