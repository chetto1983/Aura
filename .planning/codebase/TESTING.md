# Testing Patterns

**Analysis Date:** 2026-09-07

Measured at HEAD: 1,255 `*_test.go` files, ~237.9k test LOC against ~155.8k non-test
LOC (excluding `internal/db/sqlc/`). Tests are the larger half of the repository.

## Test Framework

**Runner:** Go stdlib `testing` — no assertion framework, no testify. Assertions are
hand-written `if got != want { t.Fatalf(...) }`.

**Supporting libraries:**
- `go.uber.org/goleak` — goroutine-leak detection (158 files reference it).
- `pgregory.net/rapid` — property-based testing (~20 files).
- `github.com/google/uuid`, `github.com/jackc/pgx/v5/pgxpool` in integration tiers.
- Frontend: `vitest` + `@testing-library/react` + `jsdom`, `@playwright/test` (E2E),
  `@stryker-mutator/core` (mutation), `axe-core` (a11y).

**Run commands:**
```bash
make test                    # go test -count=1 $(scripts/go_packages.sh)   — unit tier
make test-race               # same, with -race
make quality                 # deadcode vet file-size model-contracts lint test-race vuln + build
make quality-full            # quality + coverage gate (needs the stack up)
make coverage                # scripts/coverage_gate.sh   — owned-surface floor >=85%
make coverage-docker         # same floor, DISPOSABLE databases only (safe locally)
make tagged-tier-compile     # compile every discovered tagged tier
make agent-memory-eval       # blocking MRS over the live memory stack
make critical-mutation       # >=70% killed per critical boundary, no averaging
make web-test                # vitest run --coverage (85% thresholds)
make web-mutation            # stryker, break=70
```

## Build-Tag Tiers

Every non-unit test is behind a `//go:build` tag. Tag occurrences measured at HEAD:

| Tag | Files | What it needs |
|---|---|---|
| `db_integration` | 170 | Postgres + `AURA_DB_URL` / `AURA_DB_MIGRATE_URL` |
| `arcadedb_integration` | 31 | live ArcadeDB + embed sidecar |
| `docker_integration` | 13 | reachable dockerd (`internal/sandbox/usersandbox`) |
| `web_integration` / `!web_integration` | 2 / 12 | live SearXNG; the negated form guards unit-only variants |
| `live_e2e` | 8 | full stack + a real model |
| `garage_integration` | 6 | Garage object store + Admin API |
| `spike_casbin`, `spike_telegram`, `spike_multimodal`, `spike_agui` | 7/5/4/3 | spike harnesses, not product gates |
| `whatsapp_integration`, `calendar_integration`, `telegram_integration`, `multimodal_integration`, `mcp_live_integration`, `authula_integration`, `webauth_integration`, `calculator_integration`, `musr_e2e`, `document_live_e2e`, `backup_live`, `serve_smoke`, `live_finalize`, `reasoning_live` | 1–3 each | the named live dependency |
| `agent_eval`, `cot_eval`, `retrieval_eval`, `measure` | 1–4 | paid model turns; deliberately outside CI |

`make tagged-tier-compile` (`scripts/tagged_tier_compile.sh` +
`scripts/tagged_tier_compile_test.sh`) discovers every tag in the tree and compiles it,
so a tier can never rot silently behind a tag nobody runs. It also runs at pre-push.

## No-Skip-As-Green

The rule: an integration test whose env is missing **skips locally and `t.Fatal`s under
`$CI`**. There is no shared helper — each package defines its own `envOrSkip` with the
same body, ~30 of them, e.g.:

- `internal/agui/server_integration_test.go:44` `envOrSkip`
- `internal/db/db_test.go:39`, `internal/identity/store_test.go:36`,
  `internal/cron/store_test.go:33`, `internal/conversations/store_test.go:37`
- prefixed variants where a package has several dependencies:
  `assetEnvOrSkip`, `recoveryEnvOrSkip`, `oauthEnvOrSkip`, `registryEnvOrSkip`,
  `objEnvOrSkip`, `pipelineEnvOrSkip`, `aclEnvOrSkip`, `liveEnvOrSkip`,
  `sidecarEnvOrSkip`, `envOrSkipLive`
- daemon gates: `internal/sandbox/usersandbox/dockertest_support.go:61`
  `skipUnlessDockerd`, `internal/agent/tools/shell_exec_sandbox_docker_test.go:31`
  `skipUnlessDockerdTools`, `egressFQDNImageOrSkip`, `skipUnlessEnforcingBridge`

Canonical body (`internal/agui/server_integration_test.go`):
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

**Tell:** a sub-second "integration" run is a skipped run. Verify execution, not `PASS`.

## Test Environment and DSNs

**`internal/dbtest`** (`live_target_guard.go`) is the shared safety guard, not a
fixture library. `dbtest.MigrateURL(t, raw)` **fails** (never skips) when the DSN's
database is named `aura` and `GITHUB_ACTIONS` is unset — in CI that name is a throwaway
container, on a developer host it is the live deployment. It exists because a local
`db_integration` run applied migration 0095 to the live database on 2026-08-13, and the
2026-07-10 coverage incident destroyed the operator identity table.
`scripts/coverage_gate.sh` and `scripts/coverage_docker.sh` enforce the same rule at the
shell level (`AURA_COVERAGE_ALLOW_LIVE_AURA_DB=1` is the documented danger override).

**Env sources:** tests read the composed DSNs `AURA_DB_URL` / `AURA_DB_MIGRATE_URL`
(not the `POSTGRES_*` primitives that `internal/config` composes for the CLI). `.env`
carries the ArcadeDB and embed-sidecar vars. CI jobs export exactly the vars their tests
read — that is what makes the `t.Fatal` branch above meaningful.

**Stack bring-up:** `make db-up`, `make db-migrate`, `make memory-up` (ArcadeDB + MCP +
embed sidecar), `make memory-up-core` (graph substrate **without** the MCP sidecar, and
therefore without the aura daemon — the MCP `depends_on: aura`, and the daemon's
scheduler races tests for notification rows; measured on CI #1809, 2026-09-06).

**Serial execution is mandatory for integration:** `-p 1` everywhere the tiers touch the
one shared Postgres — concurrent packages collide on `CREATE ROLE` (`tuple concurrently
updated`) and golang-migrate's advisory lock.

## Mocking

**Shared Agent fakes live in `internal/agent/agenttest`** (`mocks.go`, `fakeclient.go`),
one source of truth, zero inline mock duplication. Import direction is one-way
(`agenttest` → `agent`), and every mock carries a compile-time assertion:

```go
var (
	_ agent.Agent = (*InfiniteToolCallAgent)(nil)
	_ agent.Agent = (*EmitNThenEscalate)(nil)
	_ agent.Agent = (*RecordingAgent)(nil)
	_ agent.Agent = (*CountingAgent)(nil)
)
```

Mocks encode invariants, not just behaviour: no mock calls `NewBudgetFromEnv`, because a
forked budget would silently break the shared-counter guarantee that makes depth-3 fan-3
bounded by `max_steps` rather than `max_steps³`.

**What to mock:** the LLM (`agenttest.FakeClient`), external HTTP via `httptest.Server`,
and nothing else.
**What NOT to mock:** Postgres, ArcadeDB, Docker, the embed sidecar. Those get a real
instance behind a build tag. `internal/agent/agenttest` and `internal/dbtest` are
excluded from the coverage denominator precisely because they are test-support, not
owned runtime surface.

## Fixtures and Golden Files

- Package-local `testdata/` directories: `internal/agent/tools/testdata`,
  `internal/agui/testdata`, `internal/channels/telegram/testdata`,
  `internal/gateway/testdata`, `internal/llm/testdata`,
  `internal/llm/openai_compat/testdata`, `internal/mcp/testdata`,
  `internal/share/testdata`.
- Cross-cutting fixtures in `scripts/fixtures/`: `cache_invariant/`,
  `document_pipeline_e2e/`, `document_retrieval_eval/`, `chat_50_prompts.tsv`.
- Golden-file refresh is opt-in via `UPDATE_GOLDEN`
  (`internal/channels/telegram/tables_test.go`).

## Goroutine Leaks, Race, Parallelism

- **goleak:** 158 files; the standard form is a package-wide
  `func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }`
  (`internal/cron/main_test.go`), with `goleak.VerifyNone` for narrower scopes.
- **Race detector:** default posture, not an extra. `make test-race`, the CI
  `unit-test` job (`go test -race -count=1 $(scripts/go_packages.sh)`), the
  `db_integration` suite (`-race -p 1`), the `webauth_integration` provider-lifecycle
  job, and the MUSR live E2E all run `-race`. WSL is the native-race environment
  (`CGO_ENABLED=1`).
- **`t.Parallel()`:** 1,420 call sites in `internal/` unit tests. Integration tiers stay
  serial (`-p 1`).

## Property-Based Testing

`pgregory.net/rapid`, applied where an invariant is stated rather than an example:
`internal/agent/workflow/loop_property_test.go`,
`internal/gateway/classify_property_test.go`,
`internal/conversations/context_active_round_property_test.go`,
`internal/mcp/manager/envedit_property_test.go`,
`internal/share/share_property_test.go`, `internal/swarm/swarm_property_test.go`,
plus property assertions inside `internal/canonicaljson`, `internal/agui`,
`internal/scoring`, `internal/agent/tools`.

## Coverage

**Aggregate floor: 85%** of the owned surface, enforced by `scripts/coverage_gate.sh`
(`make coverage`). How it actually works:

1. Refuses to run `db_integration` against a database named `aura` outside CI (exit 5).
2. Runs `go test -tags db_integration -p 1 -count=1 -covermode=atomic
   -coverpkg=./internal/... ./internal/... ./cmd/aura/...` writing **native covdata**
   (`-test.gocoverdir`), so cross-package attribution is truthful. `cmd/aura` tests
   contribute *execution* but `cmd/aura` is not in the denominator.
3. Merges with `go tool covdata textfmt` into `cover_gate.out`.
4. Filters out `/internal/db/sqlc/`, `/internal/agent/agenttest/`, `/internal/dbtest/`,
   `/internal/llm/client.go` — anchored at path-segment boundaries; a filter that
   leaves zero rows fails the gate.
5. `scripts/coverage_profile_gate.sh` enforces the aggregate `AURA_COVERAGE_MIN`
   (default 85) and writes
   `artifacts/production-readiness/coverage-report.json`.
6. `scripts/coverage_package_gate.py` enforces the per-package policy.

**Per-package policy (`scripts/coverage_package_policy.json`, schema_version 1,
target_percent 85, 76 packages) at HEAD:**
- **64 packages `mode: target`** — must stay ≥85%.
- **10 packages `mode: baseline`** — pinned exact covered/total ratios that may not
  regress and whose denominator may not drift silently:
  `internal/approvalgrants` 4/57, `internal/procgroup` 3/4,
  `internal/webauth` 187/360, `internal/objectstore` 329/476,
  `internal/objectstore/garageadmin` 85/111, `internal/db` 269/322,
  `internal/documents` 668/806, `internal/assets` 700/871,
  `internal/multimodal` 107/127, `internal/tracesink` 47/56.
- **2 packages `mode: delegated`** — measured by a separate release-blocking authority
  at the same 85% floor, never averaged into this denominator:
  `internal/sandbox/usersandbox` → `docker_coverage`
  (`scripts/docker_coverage_gate.sh`: `go test -tags docker_integration -p 1
  -covermode=atomic -coverpkg=./internal/sandbox/usersandbox/...,./internal/agent/tools/...`,
  `MIN=85`), and `internal/arcadedb` → `arcadedb_coverage` (the live
  `arcadedb_integration` profile, an Agent Memory hard gate re-checked by release
  readiness).

**Local safety:** run `bash scripts/coverage_docker.sh` (`make coverage-docker`) — it
provisions and drops a disposable `aura_cov` database and refuses the live one.

**Frontend coverage:** `web/vitest.config.ts` thresholds — statements/branches/
functions/lines all 85.

## Mutation Testing

- **Go:** `go-mutesting` (the avito-tech fork, the only one supporting go1.26).
  `PASS` = killed, `FAIL` = survived; score = killed/total. Threshold **≥70% per
  critical boundary, never averaged** — `scripts/critical_mutation_gate.py`
  (`make critical-mutation`) pins four Go scopes:
  `internal/gateway/classify.go`, `internal/identityctx/operator.go`,
  `internal/config/config_runtimeprofile.go`,
  `internal/sandbox/usersandbox/spec.go`.
- **Frontend:** Stryker (`make web-mutation`, `break=70`), report consumed by the same
  gate from `web/reports/mutation/mutation.json` with a 24h freshness check.

## Evidence and Behaviour Gates

Beyond code-level tests, release readiness is measured by self-tested Python/bash
harnesses under `scripts/`, each with its own `*_test.py` / `*_test.sh` contract test
run by `make evidence-contracts`:
`audit_closure_gate`, `agent_memory_eval`, `capability_eval`,
`critical_mutation_gate`, `observability_evidence`, `production_load_chaos`,
`release_check_run_gate`, `release_readiness_gate`, `rollback_rehearsal`,
`security_evidence`, plus `coverage_profile_gate`, `coverage_gate`,
`docker_coverage_gate`, `restore_drill_name`.
`make release-readiness` validates the twelve fresh reports against the current Git SHA.

**Behaviour tier (`make agent-eval`, tag `agent_eval`)** — real turns against a real
model, deliberately **not** in CI because each case costs money. Requires
`AURA_EVAL_IDENTITY` and the live stack. Run before shipping anything touching the
prompt, the tool manifest, the registry, or memory retrieval. This is the tier that
catches an answer that is wrong, loops, or reaches for the open internet — no other
gate can see it.

## Common Patterns

**Integration setup — env, guard, migrated pool:**
```go
raw := envOrSkip(t, "AURA_DB_MIGRATE_URL")
dsn := dbtest.MigrateURL(t, raw)   // t.Fatal if this is the live database
pool := migratedPool(t)            // per-package helper, e.g. internal/agui
```

**Table-driven tests** are the default shape (`dupl` is disabled on `_test.go` for
exactly this reason), keyed by a `map[string]case` with `t.Run(name, ...)` — see
`internal/dbtest/live_target_guard_test.go`.

**Daemon-gated code still needs daemon-free unit tests.** When adding
container-gated runtime code, also test the pure logic without a daemon: spec/tar
builders, path-traversal and symlink guards, nil/disabled early-return paths, and
"not supported" structural-capability errors. Otherwise the delegated coverage
authority carries a surface it cannot execute.

---

*Testing analysis: 2026-09-07*
