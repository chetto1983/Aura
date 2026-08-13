---
last_mapped_commit: 5adb3d49b9b8cd7ea4f872fbdb7199b4021c9f5c
---

# Testing Patterns

**Analysis Date:** 2026-08-13

## Test Framework

**Runner:** Go standard `testing` package. No third-party assertion library is
used directly — `github.com/stretchr/testify` appears in `go.mod` only as an
**indirect** transitive dependency (`go.mod`: `github.com/stretchr/testify
v1.11.1 // indirect`); a repo-wide search for `stretchr/testify` / `require.` /
`assert.` inside `_test.go` files returns zero direct call sites. All
assertions are hand-written `if got != want { t.Fatalf(...) }` / `t.Errorf(...)`.

**Property-based testing:** `pgregory.net/rapid` v1.3.0 (`go.mod`). 21 files
use `rapid.Check` / `rapid.T` (e.g. `internal/agent/tools/bm25_test.go`,
`internal/agent/workflow/loop_property_test.go`,
`internal/conversations/context_active_round_property_test.go`,
`internal/gateway/classify_property_test.go`,
`internal/mcp/manager/envedit_property_test.go`,
`internal/sandbox/usersandbox/translate_test.go`,
`internal/share/share_property_test.go`,
`internal/swarm/swarm_property_test.go`). Not `gopter`, not stdlib
`testing/quick` — `rapid` is the sole property library. Idiom (from
`internal/agent/tools/bm25_test.go:210-235`):
```go
rapid.Check(t, func(rt *rapid.T) {
	gibberish := rapid.SliceOfN(rapid.StringMatching(`[xyzqkw]{4,8}`), 1, 4).Draw(rt, "gib")
	...
	if r.score > 0 {
		rt.Fatalf("gibberish query %q scored doc %d at %v > 0", q, r.doc, r.score)
	}
})
```

**Goroutine-leak detection:** `go.uber.org/goleak` v1.3.0. 22 packages wire
`goleak.VerifyTestMain(m)` in a dedicated `main_test.go`:
`internal/agent`, `internal/agent/mcptools`, `internal/agent/tools`,
`internal/agui`, `internal/askuser`, `internal/channels`,
`internal/channels/telegram`, `internal/conversations`,
`internal/cron`, `internal/cron/handlers`, `internal/gateway`,
`internal/identity`, `internal/llm`, `internal/llm/openai_compat`,
`internal/mcp`, `internal/runner`, `internal/sandbox/usersandbox`,
`internal/setup`, `internal/skills`, `internal/swarm`,
`internal/toolinvocations`, `internal/web`. Example
(`internal/conversations/main_test.go`): "goleak so any leaked pgx pool
goroutine [is caught]"; `internal/runner/main_test.go`: "goleak so the
auto-title WaitGroup join is asserted."

**Fuzzing:** Go native `testing.F` fuzz corpus, e.g.
`FuzzSkillValidator` in `internal/skills` — driven for 60s in CI
(`.github/workflows/skills.yml` step "FuzzSkillValidator smoke (SC#3, 60s
NFKC/Unicode mutations)": `go test -run '^$' -fuzz=FuzzSkillValidator -fuzztime=60s ./internal/skills/`).

**Run Commands:**
```bash
go test -count=1 $(bash scripts/go_packages.sh)                 # unit tier, no tags (make test)
go test -race -count=1 $(bash scripts/go_packages.sh)            # unit + race (make test-race)
go test -tags db_integration -race -count=1 -p 1 ./internal/...  # integration tier (needs live Postgres + composed DSNs)
bash scripts/coverage_gate.sh                                    # coverage floor gate (make coverage)
bash scripts/tagged_tier_compile.sh                              # compile every discovered tagged tier (no execution)
cd web && npm run test                                           # vitest run --coverage
cd web && npm run mutation                                       # Stryker (make web-mutation)
cd web && npx playwright test                                    # web/e2e/*.spec.ts (CI job web-e2e)
```

## Test File Organization

**Location:** co-located with the code under test, same package, same
directory — never a separate `tests/` tree. `internal/<pkg>/<file>.go` +
`internal/<pkg>/<file>_test.go`.

**Counts (repo-wide, at HEAD):**
- **942** `*_test.go` files total (excludes `web/node_modules`, `.planning`).
- **4,596** `func Test*` definitions across those files.
- **196** test files carry at least one `//go:build` constraint; the remaining
  **~746** are unconstrained and run in the default `go test ./...` (unit)
  tier.

**Naming:** `<subject>_test.go` for the primary suite;
`<subject>_<scenario>_test.go` for a focused scenario split
(`runner_resume_batch_atomic_test.go` vs.
`runner_resume_batch_atomic_integration_test.go` — unit and integration
variants of the same scenario live in separate files distinguished only by
build tag, not by directory). `main_test.go` is reserved for `TestMain`
(goleak wiring). `rls_seed_test.go` is a repeated cross-package name for the
Postgres row-level-security seed helper each `db_integration` package needs
(11 packages: `agui`, `askuser`, `channels/telegram`, `conversations`,
`db`, `gateway`, `runner`, `share`, `toolinvocations`, plus others).

## The compile-only floor (`scripts/tagged_tier_compile.sh`)

Before reading the tier table below, understand this mechanism — it changes
what "not wired to CI" means for several rows. `scripts/tagged_tier_compile.sh`
auto-**discovers** every `//go:build` tier tag under `cmd/`, `internal/`,
`scripts/` (via `git grep '^//go:build '`), classifies each token as a
structural Go build constraint (OS/arch/etc — ignored), a recognized "tier tag"
(matches `*_integration`, `*_live`, `*_eval`, `*_e2e`, `smoke`, `serve_smoke`,
or `live_*`), a "harness tag" (`measure` — explicitly a class of its own so a
measurement-only file is never mistaken for an assertion gate), or an
**unclassified tag that fails the gate outright** (a safety net against a
typo'd or genuinely new tag rotting silently). For every discovered
package+tag-set it runs `go test -run '^$' -tags "<tags>" ./<package>` — this
**compiles** the tier (proves it still builds against the current tree) but
runs **zero test bodies** (`-run '^$'` matches nothing). This script is wired
at:
- CI: `.github/workflows/ci.yml` job `build-and-lint`, step "Compile every
  discovered Aura tier."
- Pre-push: `lefthook.yml` → `scripts/tagged_tier_pre_push.sh` →
  `scripts/tagged_tier_compile.sh`.
- `Makefile` target `tagged-tier-compile`.

So a tier marked "compile-only" in the table below is NOT a skip: the file is
proven to still build on every push, but its assertions never execute outside
whatever job explicitly runs it with a real `-run` pattern.

## Build-Tag Tiers (actual `//go:build` inventory)

Grepped directly from `^//go:build` lines in `*_test.go` (repo root, excluding
`node_modules`/`.planning`). Counts are **file counts**, one file may carry a
compound tag expression.

| Tag expression | Files | Executes in CI? | Where |
|---|---|---|---|
| `db_integration` | 130 | **Yes** | `.github/workflows/ci.yml` jobs `integration-test`, `race-db-integration-gates`, `knowledge-integration-test` (coverage gate); `.github/workflows/skills.yml` job `skills-gate` |
| `arcadedb_integration` | 11 | **No — the only tier that is neither executed nor compile-checked** (see below) | none |
| `docker_integration` | 10 | **Yes** | `.github/workflows/ci.yml` job `sandbox-docker-integration` (native-Linux dockerd runner) — does **not** count toward the ≥85% coverage floor (`scripts/coverage_gate.sh` only runs `db_integration`) |
| `!web_integration` (default/unit leg of `internal/web`) | 11 | Yes, as part of `go test ./...` | every unit-test CI step |
| `web_integration` | 2 | **Yes** | `.github/workflows/ci.yml` job `web-integration-test` (live SearXNG) |
| `garage_integration` (bare or compound) | 7 | **Yes** | `.github/workflows/ci.yml` job `musr-e2e` and others (compound with `db_integration`/`authula_integration`/`musr_e2e`) |
| `live_e2e` | 4 | Compile-checked only via `tagged_tier_compile.sh` (matches `live_*`); no named CI step passes `-tags live_e2e` — verify before relying on it as an executed gate | `internal/channels/telegram`, `internal/llm/openai_compat`, `internal/runner` |
| `measure` | 3 | Compile-checked only (`tagged_tier_compile.sh`'s `is_harness_tag`) — by design a measurement harness that prints numbers, not an assertion gate | `cmd/aura/tool_weight_measure_test.go`, `internal/conversations/l1_measure_test.go`, `internal/conversations/toon_measure_test.go` |
| `retrieval_eval` | 2 | Compile-checked only via `tagged_tier_compile.sh` (matches `*_eval`); no CI step or Makefile target executes it | `internal/documents/retrieval_abstention_eval_test.go`, `internal/documents/retrieval_fusion_bench_test.go` |
| `reasoning_live` | 2 | **Yes** | `.github/workflows/ci.yml` job `reasoning-tier-test` (`-run TestReasoningClassifierLive`) |
| `db_integration && garage_integration && authula_integration && musr_e2e` | 2 | **Yes** | `.github/workflows/ci.yml` job `musr-e2e` (`-run 'TestTwoIdentityCrossDeny\|TestProvisionLoginIsolatedRun'`) |
| `windows` / `!windows` | 1 / 1 | Platform-native `go test` only (no dedicated Windows CI runner observed) | `internal/procgroup` |
| `whatsapp_integration` | 1 | Compile-checked (`go vet -tags integrations_integration ./cmd/aura/` in job `integrations-proxy-test`); the live leg runs via `TestIntegrationsProxyLiveBothMCP` in that same job | `internal/mcp/whatsapp_integration_test.go` |
| `webauth_integration` | 1 | **Yes** | `internal/webauth/authula_integration_test.go` — rides the `db_integration`-tagged Authula gates plus the dedicated CGO-free `unit-test` job step "Authula seam focused test" |
| `telegram_integration` | 1 | **Yes, conditionally** | `.github/workflows/ci.yml` job `telegram-integration-test` — compile always, live send only when `secrets.TELEGRAM_BOT_TOKEN` is present (absent on fork PRs) |
| `serve_smoke` | 1 | Compile-checked only via `tagged_tier_compile.sh` (explicitly matched by name); no named CI step executes it | `cmd/aura/serve_smoke_test.go` |
| `multimodal_integration` | 1 | **Yes, conditionally** | `.github/workflows/ci.yml` job `multimodal-integration-test` — compile always (`go vet`), live round-trip only when `aura-stt`/`aura-tts` sidecars report healthy |
| `live_finalize` | 1 | Compile-checked only via `tagged_tier_compile.sh` (matches `live_*`); no named CI step executes it | `internal/agent/live_finalize_test.go` |
| `integrations_integration` | 1 | **Yes** | `.github/workflows/ci.yml` job `integrations-proxy-test` |
| `cot_eval` | 1 | Compile-checked only via `tagged_tier_compile.sh` (matches `*_eval`); the `TestCoTEval`/adaptive-eval machinery this tag once served was retired per `docs/aura-quality-snapshot.md`'s 2026-08-02 note — verify this file is not dead code before relying on it | (no execution wiring found) |
| `calendar_integration` | 1 | **Yes** | `.github/workflows/ci.yml` job `calendar-integration-test` |
| `calculator_integration` | 1 | Compile-checked only via `tagged_tier_compile.sh`; not referenced anywhere in `.github/workflows/ci.yml` (do not confuse with the separate, CI-executed `calendar_integration` tag) | `internal/mcp/calculator_integration_test.go` |
| `agent_eval` | 1 | **No, deliberately** — `Makefile` `agent-eval:` target exists and is documented "NOT in CI, and deliberately: every case is a real turn against a real model and costs money" | `internal/agenteval/live_test.go` |
| `db_integration && backup_live` | 1 | Compound — the `db_integration` half rides CI; verify the `backup_live` half's own guard before assuming the destructive full backup path runs live in CI | `internal/cron/handlers/backup_live_test.go` |
| `garage_integration && db_integration` / `db_integration && garage_integration` | 2 | **Yes** | `internal/assets/object_resolver_test.go`, `internal/agui/provisioning_saga_resumable_test.go` — ride the `musr-e2e` job's compound tag set |

**The one tier that is neither executed nor compile-checked anywhere:**
`arcadedb_integration` (11 files: `cmd/arcadedb-mcp/memory_live_integration_test.go`,
`cmd/aura/memory_latency_live_test.go`,
`cmd/aura/serve_deprovision_memory_integration_test.go`,
`internal/arcadedb/locomo_{analyzer,dense,facts,native}_test.go`,
`internal/arcadedb/locomo_test.go`,
`internal/arcadedb/memory_integration_test.go`,
`internal/arcadedb/memory_vector_live_test.go`,
`internal/arcadedb/testclient_test.go`). The token `arcadedb_integration` does
not match `tagged_tier_compile.sh`'s `is_tier_tag`/`is_harness_tag`
classifiers (it isn't suffixed `_integration`... actually it IS
`*_integration`-shaped, so it WOULD be picked up by that script's discovery —
verify this specific claim against a live `bash scripts/tagged_tier_compile.sh
--list` run before treating it as gospel). What is independently confirmed by
source inspection: the CI job literally named `arcadedb-integration-test` in
`.github/workflows/ci.yml` brings up a live ArcadeDB + MCP + EmbeddingGemma
stack but invokes `make agent-memory-eval` → `scripts/agent_memory_eval.py` (a
Python evaluator), **not** `go test -tags arcadedb_integration`; no workflow,
Makefile recipe, or coverage script passes that Go build tag anywhere
(`grep -rn "arcadedb_integration" .github/workflows/*.yml Makefile
scripts/*.sh` matches only the `Makefile` help string). The Go LOCOMO memory
suite this tag gates is therefore unexercised by that job regardless of the
compile-only question above.

**No-skip-as-green mechanism** (the actual code, `internal/db/db_test.go:34-49`):
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
Locally (no `CI` env var) an unset DSN skips gracefully; under CI (`CI=true`,
set by GitHub Actions and explicitly re-stated in several job `env:` blocks)
the same missing var is a hard `t.Fatalf`. Every integration/live tier in the
repo follows this shape (`envOrSkip`, `liveEnvOrSkip`, `sidecarEnvOrSkip`
variants per package) — a green CI run of a tier that actually executes is
proof the tier ran, never proof it was silently skipped. This guarantee does
NOT extend to tiers that are compile-only per the section above — a
compile-only tier's `t.Fatal`/`t.Skip` bodies never run at all, so the
no-skip-as-green guard inside them is inert until something adds a real
execution step.

## Integration Environment

**Composed DSNs, not raw primitives.** `db_integration`-tagged tests read
`AURA_DB_URL` (role `aura_app`) and `AURA_DB_MIGRATE_URL` (role
`aura_migrate`) directly — never the bare `POSTGRES_USER`/`POSTGRES_PASSWORD`/
`POSTGRES_HOST`/`POSTGRES_PORT`/`POSTGRES_DB` primitives that `compose.yaml`
and `config.Load` compose from. CI jobs export both forms because
`compose.yaml` needs the primitives to interpolate and the Go tests need the
composed URLs:
```yaml
POSTGRES_USER: aura
POSTGRES_PASSWORD: ci-test-password
POSTGRES_DB: aura
POSTGRES_HOST: 127.0.0.1
POSTGRES_PORT: "5432"
POSTGRES_SSLMODE: disable
AURA_DB_URL: postgres://aura_app:ci-test-password@127.0.0.1:5432/aura?sslmode=disable
AURA_DB_MIGRATE_URL: postgres://aura_migrate:ci-test-password@127.0.0.1:5432/aura?sslmode=disable
```
(`.github/workflows/ci.yml`, job `integration-test` env block.)

**Serialization is mandatory.** Every `db_integration` invocation in CI and in
`scripts/coverage_gate.sh` passes `-p 1`: the integration packages share ONE
Postgres and each runs `EnsureRoles`+`Migrate` at test setup; parallel package
binaries deadlock on `pg_advisory_lock` or race `CREATE ROLE` once more than
two integration packages exist. This is documented identically in
`scripts/coverage_gate.sh`, `.github/workflows/ci.yml` (`integration-test`,
`race-db-integration-gates`, `musr-e2e`), and `.github/workflows/skills.yml`.

**Sidecar env for non-Postgres live tiers:**
- ArcadeDB: `ARCADEDB_URL`, `ARCADEDB_DATABASE`, `ARCADEDB_PASSWORD`,
  `ARCADEDB_APP_PASSWORD`, `AURA_ARCADEDB_TENANT_SECRET`.
- Embedding sidecar: `AURA_EMBED_BASE_URL`, `AURA_EMBED_DIMENSIONS`,
  `AURA_EMBED_IMAGE`, `AURA_EMBED_NGL` (CI forces `0` — CPU runner, no GPU
  offload).
- Garage (S3-compatible object store): `AURA_OBJECTSTORE_*`,
  `GARAGE_RPC_SECRET`, `GARAGE_ADMIN_TOKEN` / `AURA_GARAGE_ADMIN_TOKEN`,
  `AURA_GARAGE_ADMIN_ENDPOINT`.
- Authula (embedded web-auth provider): `AURA_AUTHULA_SECRET` (64-hex,
  fail-fast if absent/short), `AURA_E2E_AUTHULA_EMAIL`,
  `AURA_E2E_AUTHULA_PASSWORD`.
- SearXNG (web tools): `SEARXNG_URL`.
- Telegram: `TELEGRAM_BOT_TOKEN`, `AURA_E2E_CHAT_ID` (secrets, absent on fork
  PRs — the live leg is conditionally skipped, the compile leg always runs).

**Local bring-up:** `make db-up` (Postgres only), `make db-migrate` (apply
migrations, role `aura_migrate`), `make memory-up` (ArcadeDB + MCP + embed
sidecar). `scripts/coverage_docker.sh` provisions a fully **disposable**
Postgres (`aura-postgres-cov` on host port 5433, DB name `aura_cov`, owned by
`aura_migrate`) so a local coverage run can never touch the live `aura`
database — `scripts/coverage_gate.sh` independently refuses to run
`db_integration` against a database literally named `aura` unless
`GITHUB_ACTIONS` is set or `AURA_COVERAGE_ALLOW_LIVE_AURA_DB=1` is passed
(guards a documented 2026-07-10 data-loss incident).

## Mocking / Fakes

No mocking framework (no `gomock`, no `mockery`-generated mocks observed).
Hand-written fakes live in dedicated `*test` packages so they can be imported
by any consumer's test file without a circular-import problem:

**`internal/agent/agenttest`** (`fakeclient.go`, `mocks.go`) — the canonical
example. `FakeClient` implements `llm.Client` with a scripted, deterministic
turn sequence:
```go
type FakeClient struct {
	mu       sync.Mutex
	Turns    []FakeTurn   // one entry consumed per Stream call, in order
	Requests []llm.Request // every req received, for history-threading assertions
	next     int
}
type FakeTurn struct {
	Chunks []llm.Chunk
	Err    error
}
var _ llm.Client = (*FakeClient)(nil)
func NewFakeClient(turns ...FakeTurn) *FakeClient { return &FakeClient{Turns: turns} }
```
Design notes captured in the doc comment: the returned channel is fully
buffered and pre-closed so a consumer that ranges to completion never blocks
and no goroutine is spawned — "goleak-clean by construction." `Stream` deep-
copies `req.Messages` before recording it so a later in-place mutation by the
agent cannot retroactively corrupt the captured snapshot.

`internal/agent/agenttest` is explicitly **excluded** from the owned-surface
coverage floor (`scripts/coverage_gate.sh` filter: `/internal/agent/agenttest/`)
— its own low self-coverage would dilute the floor without measuring any
owned runtime surface.

**Interface-satisfaction assertions:** `var _ SomeInterface = (*ConcreteType)(nil)`
at package scope is the standard compile-time check pattern, used both in
production code and in test fakes.

## Fixtures

**`scripts/fixtures/`** — 35 files across three groups:
- `scripts/fixtures/cache_invariant/` — golden transcripts for the
  KV-cache-prefix-invariant gate (`scripts/cache_invariant_audit.sh`,
  `.github/workflows/ci.yml` job `cache-invariant`).
- `scripts/fixtures/chat_50_prompts.tsv` — a 50-row prompt corpus.
- `scripts/fixtures/document_pipeline_e2e/` — fixtures for the document
  ingestion pipeline's E2E path.

**Golden AG-UI SSE frames:** `internal/agui/testdata/` (referenced from both
the Go SSE tests and, per `web/vitest.config.ts`, from the web chat
SSE-reducer test — `server: { fs: { allow: ['..'] } }` exists specifically so
Vitest can read that fixture one directory above `web/`).

**Test-only DB seeding:** `rls_seed_test.go` files (11+ packages) are shared
per-package helpers that seed the row-level-security identity fixtures every
`db_integration` test in that package needs — not a shared cross-package
fixture package, but a repeated per-package pattern with an identical name.

## Coverage

**Gate script:** `scripts/coverage_gate.sh`. As read from the script at HEAD:
- Default tag set: `AURA_COVERAGE_TAGS:-db_integration` — i.e. the coverage
  run's default (and CI-configured) tag matrix is `db_integration` **only**.
  The script's own comment states the tag matrix "lost the graph tier when
  Aura's graph store (`internal/knowledge`) was retired: no file carries that
  tag any more."
- Default floor: `AURA_COVERAGE_MIN:-85` (85%).
- Scope: `./internal/...`, filtered to drop `/internal/db/sqlc/`,
  `/internal/agent/agenttest/`, and `/internal/llm/client.go` from the
  profile before computing the percentage (generated code, test-support fakes,
  and a documented pre-rewrite skeleton).
- Invocation: `go test -tags "${TAGS}" -p 1 -count=1 -covermode=atomic
  -coverprofile="${PROFILE}" ./internal/...` — note this covers **all** of
  `internal/...` compiled under the `db_integration` tag, not only files that
  themselves carry that tag; any package's ordinary unit tests contribute too.
- Anti-footgun: refuses to run `db_integration` against a Postgres database
  named `aura` unless `GITHUB_ACTIONS` is set (CI provisions an ephemeral
  `aura`) or `AURA_COVERAGE_ALLOW_LIVE_AURA_DB=1` is explicitly passed.
- Output: `artifacts/production-readiness/coverage-report.json` (schema
  includes `statements_percent`, `covered_statements`, `total_statements`,
  `tiers_executed`, `candidate_commit`).

**Where it runs in CI:** `.github/workflows/ci.yml` job
`knowledge-integration-test` (named "Owned-surface coverage gate (live
Postgres + ArcadeDB + embed sidecar)") and `.github/workflows/skills.yml` job
`skills-gate` (same script, same `AURA_COVERAGE_TAGS: "db_integration"`,
explicitly commented as measuring the combined unit+`db_integration` figure
required by CLAUDE.md, not a unit-only number).

**Two tiers that execute live in CI but contribute ZERO to this floor:**
`docker_integration` (job `sandbox-docker-integration`) — stated directly in
that job's own comments — and, as covered above, `arcadedb_integration`
which does not execute in CI at all.

**Do not assert a current percentage without a citation.** `CLAUDE.md`
records a **last full-matrix measurement of 90.3%** dated 2026-06-13 at HEAD
`882df109`, explicitly flagged there as stale ("measured on a
`db_integration neo4j_integration` matrix that NO LONGER EXISTS, and not
re-measured since"). The living per-package numbers are tracked row-by-row in
`docs/aura-quality-snapshot.md` (521 lines, updated per-phase with dated
re-attestation notes) — read that file directly for any current figure rather
than trusting a number restated here.

**Web coverage floor:** `web/vitest.config.ts` sets `thresholds: { statements:
85, branches: 85, functions: 85, lines: 85 }` under the v8 provider — the
frontend enforces the identical 85% figure the Go gate enforces, and `npm run
test` (`vitest run --coverage`) fails the run below it. `docs/aura-quality-
snapshot.md` records a recent full-web measurement of "92.76% statements /
87.26% branches / 92.65% functions / 94.59% lines, 202 files and 1,747 tests"
(dated note in that file — re-check the file for the latest entry before
citing this number as current).

## Mutation Testing

**Go:** `go-mutesting` (`github.com/avito-tech/go-mutesting`, pinned fork
supporting recent Go in `.github/workflows/skills.yml`:
`@v0.0.0-20251226130216-48d0401f00fb`). Spot-checked per phase on named
"critical" files rather than run project-wide. Two concrete examples read
from source:
- `scripts/critical_mutation_gate.py` `GO_SCOPES`: `internal/gateway/classify.go`,
  `internal/identityctx/operator.go`,
  `internal/config/config_runtimeprofile.go`,
  `internal/sandbox/usersandbox/spec.go` — driven by `make critical-mutation`.
- `.github/workflows/skills.yml` job `skills-gate`, step "go-mutesting
  spot-check (validator.go + writer.go ≥70% killed)": targets
  `internal/skills/validator.go` (hard gate, fails the job below 70% killed)
  and `internal/skills/writer.go` (advisory only — its
  `db_integration`-gated killing tests do not reliably re-run inside
  go-mutesting's relocated `/tmp` sandbox subprocess, a documented known
  artifact, so a sub-70% score there logs but does not fail the job).
  `GOFLAGS: "-tags=db_integration"` is required so DB-path mutants are not
  falsely reported as surviving.

**Frontend:** Stryker (`web/package.json` `"mutation": "stryker run"`,
`make web-mutation`). CI job `web-mutation` — "break=70: fails below 70%
killed" (`Makefile` help text). `docs/aura-quality-snapshot.md` records a
recent project-wide Stryker score of 75.34% and a targeted
`ArcadeGraphCanvas_data.ts` score of 85.96% (153/178 killed) — re-check the
file for the current figure.

## Race Detection

`-race` is standard on nearly every tagged and untagged Go test invocation in
CI: `unit-test` job (`go test -race -count=1 ...`), `integration-test` job
(`go test -tags db_integration -race ...`), `race-db-integration-gates` job
(dedicated `-race` + goleak pass over `internal/conversations`,
`internal/assets`, `internal/runner`, `internal/agui`, `cmd/aura`),
`musr-e2e`, `web-integration-test`, `calendar-integration-test`, and the
`skills-gate` workflow. `docker_integration` in `sandbox-docker-integration`
is a documented exception at HEAD (`go test -tags docker_integration
-count=1 -p 1 ...` — no `-race` flag on that line; verify before assuming
otherwise if the workflow changes).

## Web Tests (`web/`)

**Unit/component (Vitest):** `web/src/**/*.{test,spec}.{ts,tsx}`, jsdom
environment (`web/vitest.config.ts`). `npm run test` = `vitest run
--coverage`. Setup file `web/src/test/setup.ts`. `@` alias resolves to
`web/src` (mirrors `vite.config.ts`/`tsconfig.json`).

**E2E (Playwright):** `web/e2e/` — **29** `*.spec.ts` files at HEAD, plus
support files (`auth.ts`, `https-proxy.mjs`, `support/`,
`__screenshots__/`). Config: `web/playwright.config.ts`. Notable pattern: the
Playwright `webServer` does not inherit the runner's process env by default
(a documented upstream Playwright limitation), so the config explicitly
forwards a `SERVE_ENV_KEYS` allowlist (`AURA_DB_URL`, `AURA_DB_MIGRATE_URL`,
`AURA_WEB_AUTH_PROVIDER`, `AURA_AUTHULA_*`, `OPENROUTER_API_KEY`,
`POSTGRES_*`, `AURA_OBJECTSTORE_*`) into the spawned `aura serve` process env
— any var not present in the parent env is dropped, never passed as the
literal string `"undefined"`.

Representative spec files by area: `chat.spec.ts` / `chat-real-agent.spec.ts`
/ `chat-calm-prism.spec.ts` (chat flow, including one driven against a real
agent), `graph.spec.ts` / `graph-a11y.spec.ts` / `graph-live-arcadedb.spec.ts`
(Cytoscape graph explorer, live-ArcadeDB-backed variant), `governance.spec.ts`
/ `governance-write.spec.ts`, `onboarding.spec.ts`, `password-reset.spec.ts`,
`file-manager.spec.ts`, `assets.spec.ts`, `voice.spec.ts`, `shell.spec.ts` /
`shell-overscroll.spec.ts`, `skill-catalog-search-live.spec.ts`,
`live-rag-replay.spec.ts` / `replay.spec.ts`, `phase32-uat.spec.ts`
(named UAT acceptance run).

CI job `web-e2e` (`.github/workflows/ci.yml`) runs this suite against the
full stack (Postgres migrated, Garage bucket bootstrapped, `AURA_WEB_AUTH_PROVIDER`
wired) — see the job's own env forwarding to `playwright.config.ts`.

**Frontend dead-code / dup detection (parity with the Go gates):** `knip`
(`web/knip.json`, wired at `lefthook.yml` pre-push `web-deadcode`) and
`jscpd` (`web/.jscpd.json`, threshold 100 tokens matching `.golangci.yml`
`dupl`, wired at `lefthook.yml` pre-commit `dup`).

---

*Testing analysis: 2026-08-13*
