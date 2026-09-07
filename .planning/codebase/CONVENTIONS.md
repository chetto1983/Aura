# Coding Conventions

**Analysis Date:** 2026-09-07

Measured at HEAD on 2026-09-07: 927 non-test Go files (~155.8k LOC excluding
`internal/db/sqlc/`), 1,255 test files (~237.9k LOC), 79 packages under `internal/`,
5 binaries under `cmd/` (`aura`, `arcadedb-mcp`, `aura-filecard`,
`aura-ingest-supervisor`, `aura-media-index`), plus a React frontend in `web/`.

## Naming Patterns

**Files:**
- Snake-case, one concern per file, named `<subject>_<concern>.go`. The 600-LOC cap
  (below) forces splitting rather than growth: `internal/arcadedb/memory.go`,
  `memory_graph.go`, `memory_graph_path.go`, `memory_graph_temporal.go`,
  `memory_mentions.go`, `memory_mentions_link.go`, `memory_mentions_read.go`.
- Config composites split the same way: `internal/config/config.go`,
  `config_embed.go`, `config_runtimeprofile.go`.
- Tests carry the tier in the name, not only in the build tag:
  `*_test.go` (unit), `*_integration_test.go`, `*_live_test.go`,
  `*_property_test.go`, `*_pure_test.go`, `*_e2e_test.go`.
  Examples: `internal/share/share_property_test.go`,
  `internal/arcadedb/memory_mentions_pure_test.go`,
  `internal/runner/live_e2e_test.go`.

**Functions:**
- Standard Go: exported `CamelCase`, unexported `camelCase`. Exported symbols carry
  doc comments (revive `exported` is on, with `disableStutteringCheck`).
- The only blanket doc-comment exemption is `Spec`/`Execute` on the ~30 tool
  implementations in `internal/agent/tools` (`.golangci.yml` exclusion rule) — the
  contract is documented once on the interface in `internal/agent/tools/spec.go`.

**Types:**
- Interfaces are small and named for the behaviour (`agent.Agent`).
- Compile-time interface assertions are the convention for fakes and implementations:
  `var _ agent.Agent = (*RecordingAgent)(nil)` in `internal/agent/agenttest/mocks.go`.

**Packages:**
- Lowercase, no underscores, single-word where possible: `internal/envutil`,
  `internal/canonicaljson`, `internal/identityctx`, `internal/boundedbuffer`.
- Leaf helper packages are extracted deliberately rather than duplicated
  (`internal/envutil` folded `IntDefault`/`BoolDefault` that had been copied across
  `internal/config`, `internal/channels`, `internal/channels/telegram`).

## Code Style

**Formatting:**
- `gofmt`, enforced as a gate, not a suggestion. `formatters.enable: [gofmt]` in
  `.golangci.yml`; the lefthook `pre-commit` hook runs `bash scripts/gofmt-staged.sh`
  with `stage_fixed: true`.
- Exempt from formatting/lint: `internal/db/sqlc` (generated), `internal/llm/client.go`
  (pre-rewrite skeleton), `third_party`, `web/node_modules`, `.planning`.

**Linting (`.golangci.yml`, `default: none` + explicit enable list):**
`errcheck`, `govet`, `ineffassign`, `staticcheck`, `unused`, `misspell`, `gosec`
(G115 excluded), `revive`, `dupl` (token threshold 100), `modernize`.
Test files are exempt from `gosec`, `errcheck`, `dupl` (table tests are intentionally
repetitive) and from staticcheck `SA5011`.

**File size — VERIFIED, and the codebase holds it.**
`bash scripts/check-file-size.sh` at HEAD: *all 2,851 tracked source files within the
600-LOC cap.* The largest owned files sit exactly at or just under the line
(`internal/askuser/store.go` 600, `internal/arcadedb/memory.go` 597,
`internal/channels/registry_test.go` 600). The cap covers `.go`, `.ts`, `.tsx`
including tests; exemptions are `internal/db/sqlc/`, `third_party/`, `vendor/`,
`node_modules/`, `dist/`, `*.d.ts`, and the shadcn-vendored
`web/src/components/model-selector*.tsx`.

**Duplication:** Go via `dupl` (threshold 100) inside golangci-lint; TypeScript via
`jscpd` at the same threshold (`web/.jscpd.json`, run by `scripts/check-dup.sh`).

**Dead code:** `deadcode -test` over all packages (`scripts/deadcode_gate.sh`,
`make deadcode`, lefthook pre-push); frontend parity via `knip`
(`scripts/check-deadcode-web.sh`, `web/knip.json`).

## Import Organization

**Order** (gofmt-grouped, one blank line between groups):
1. stdlib
2. `github.com/chetto1983/aura/internal/...` — own packages
3. third-party

Observed in `internal/config/config.go` and `internal/agui/server_integration_test.go`;
own-module and third-party imports frequently share the second block sorted
alphabetically, which is what gofmt preserves.

**Module path:** `github.com/chetto1983/aura`. No path aliases; `internal/` enforces
the boundary. Test-support packages import production packages one-way only —
`agenttest` imports `internal/agent`, never the reverse (documented at
`internal/agent/agenttest/mocks.go`).

**Package selection for gates:** never `./...` literally — `bash scripts/go_packages.sh`
produces the governed package list used by `make vet`, `lint`, `test`, `build`.

## Error Handling

**Wrapping is the norm:** 1,517 `fmt.Errorf(... %w ...)` sites across `internal/`;
820 `errors.Is` / `errors.As` call sites. Errors are wrapped with a package-prefixed
message (`"arcadedb: empty statement"`, `"agui: threadId must not be empty"`).

**Sentinels are exported when callers must branch on them:**
`internal/agent/errors.go:10` `ErrBudgetExhausted`,
`internal/agent/tools/spec.go:31` `ErrNoNonDeferredTool`,
`internal/agui/bootstrap_api.go` `ErrBootstrapAlreadyConfigured` / `ErrBootstrapInvalid`,
`internal/conversations/context.go:69` `ErrContextWindowExceeded`,
`internal/assets/browser.go` `ErrBrowserUnconfigured` / `ErrReservedPrefix`.

**Errors never pass silently** — `errcheck` is enabled on production code. The one
sanctioned silent path is explicitly documented and scoped:
`internal/envutil/envutil.go` absorbs malformed optional env values to a fallback
rather than failing boot, with the rationale in the package doc; required secrets
fail loudly in their own `Validate`.

**Fail-loud boot:** unparseable required config is surfaced through struct fields and
`Validate` (e.g. `Config.RunDirErr` in `internal/config/config.go`) rather than being
swallowed at load time.

## Logging

**Framework:** `log/slog` (136 references under `internal/`). The stdlib `log` package
is not used in `internal/` or `cmd/`.

**Patterns:** structured key/value attributes; no `fmt.Println` debugging in production
paths. Tracing knobs are `AURA_OTEL_*`; observability evidence has its own gates
(`make observability-check`, `make observability-evidence`).

## Comments

**Rule:** no comments unless the *why* is non-obvious.

**Measured reality:** the codebase interprets this as "few trivial comments, but long,
dated, evidence-bearing comments where behaviour is surprising." This is a genuine
divergence in volume from a naive reading of the rule, and it is deliberate — the
comments carry incident dates and measurements, not restatements of the code:
- `internal/dbtest/live_target_guard.go` — ~20 lines explaining why a live database
  named `aura` is refused, citing the 2026-08-13 and 2026-07-10 incidents.
- `Makefile` `memory-up-core` — explains the daemon/scheduler race measured on CI #1809.
- `lefthook.yml` — each hook records why it moved between pre-commit and pre-push.
- `internal/config/config.go` `Timezone` — records the 2026-08-16 wrong-clock measurement.

There are essentially no "returns the X" restatement comments outside required doc
comments on exported symbols.

**Doc comments:** every package has a package comment stating what it owns and, often,
what it deliberately does not (`internal/envutil`, `internal/dbtest`,
`internal/agent/agenttest`, `internal/config`).

## Function and Module Design

**Size:** subordinate to the 600-LOC file cap; functions are short and single-purpose,
with concerns split into sibling files instead of long functions.

**Exports:** minimal surface; helpers unexported unless a sibling package needs them.
No barrel files (not a Go idiom here); `internal/` is the visibility boundary.

**Tools:** the deferred-tool pattern is a hard convention — one tool per file under
`internal/agent/tools/<name>.go`, big tools flagged `Deferred: true` so they stay out
of the LLM-visible manifest and are fetched via `tool_search`.

## Environment Variables

**Convention:** `AURA_<DOMAIN>_<UNIT>`. 338 distinct `AURA_*` names are referenced in
Go source at HEAD.

**Sanctioned exceptions** (upstream/third-party canonical names, read directly):
`ARCADEDB_*` (URL, USER, PASSWORD, DATABASE, ADMIN_USER, ADMIN_PASSWORD,
TIMEOUT_SECONDS), `POSTGRES_PASSWORD`, `PGHOST`, `PGPORT`, `TELEGRAM_BOT_TOKEN`,
`TELEGRAM_API_BASE_URL`, `TELEGRAM_FILE_BASE_URL`, `OPENROUTER_API_KEY`,
`MULTIMODAL_BASE_URL`, `MULTIMODAL_MODEL`, `STT_BASE_URL`, `STT_MODEL`, `TTS_BASE_URL`,
`SEARXNG_URL`, `GARAGE_RPC_SECRET`, `COCOINDEX_DB`, `MCP_OAUTH_JWKS_URL`,
`MCP_OAUTH_TRUSTED_ISSUERS`, `DOCKER_HOST`. Plus the CI/tooling primitives
`CI`, `GITHUB_ACTIONS`, `UPDATE_GOLDEN`, `USER`, `EXPLICIT_PUBLIC`.

**Reading env:** optional knobs go through `internal/envutil` (`IntDefault`,
`BoolDefault`); root composition and DSN assembly live in `internal/config/config.go`
(`.env` loaded via `godotenv`). Hard-coded env reads scattered through business logic
are the anti-pattern the config root exists to prevent.

## Gates (what actually runs)

**Local, per commit (`lefthook.yml` `pre-commit`, staged-file scoped):**
`gofmt` → `scripts/vet-staged.sh` → `scripts/lint-staged.sh` →
`scripts/check-file-size.sh {staged_files}` → `scripts/check-dup.sh` (web only).

**Local, per push (`pre-push`):**
`go build $(scripts/go_packages.sh)`, `scripts/tagged_tier_pre_push.sh`,
`scripts/payload_manifest_gate.sh`, `scripts/sqlc_sync_gate.sh` (on `internal/db/**`),
`scripts/deadcode_pre_push.sh`, `scripts/check-deadcode-web.sh`,
`scripts/web_quality_hook.sh` (on `web/**`).

**Make targets (`Makefile`):**
- `make quality` — no containers: `deadcode vet file-size embedding-model-contract
  llm-model-contract lint test-race vuln`, then `go build`.
- `make quality-full` — `quality` + `make coverage`.
- `make web-quality` — `web-lint` (eslint `--max-warnings=0` + `tsc --noEmit` +
  `prettier --check`) + `web-test` + `web-mutation`.
- `make critical-mutation`, `make evidence-contracts`, `make release-readiness`,
  `make observability-evidence` — release-blocking evidence gates.

**CI (`.github/workflows/ci.yml`, 23 jobs):** `build-and-lint`, `unit-test`,
`capability-eval`, `cache-invariant`, `vulncheck`, `observability-contract`,
`integration-test`, `sqlc-golden`, `web-integration-test`,
`knowledge-integration-test`, `arcadedb-integration-test`, `ingest-sidecar-test`,
`reasoning-tier-test`, `multimodal-integration-test`, `telegram-integration-test`,
`whatsapp-integration-test`, `calendar-integration-test`, `web-lint`, `web-test`,
`web-mutation`, `race-db-integration-gates`, `sandbox-docker-integration`, plus the
MUSR two-identity E2E job. Other workflows: `skills.yml`, `codeql.yml`,
`production-readiness.yml`, `release.yml`, the publish/retire image workflows.

---

*Convention analysis: 2026-09-07*
