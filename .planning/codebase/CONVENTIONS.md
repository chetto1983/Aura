# Coding Conventions

**Analysis Date:** 2026-07-17

**Provenance rule for this document:** every count, version, and threshold below was produced by a command run on 2026-07-17 against the working tree, or read out of the named config file. Where a fact could not be verified it is marked **NOT VERIFIED** rather than guessed. Do not propagate a number from a sibling doc into this one — that is exactly how the previous map rotted.

**Scope:** Go module `github.com/chetto1983/aura` (Go **1.26.5**, per `go.mod:3`) plus the `web/` Vite + React frontend.

**Measured surface (2026-07-17):**
- **98,150** LOC non-test across `cmd/` + `internal/` (602 files)
- **143,580** LOC of tests (777 `*_test.go` files) — test:source ratio **~1.46:1**
- **68** packages under `internal/...` (`go list ./internal/... | wc -l`)

## Naming Patterns

**Files:**
- `snake_case.go`, lowercase. One concern per file.
- **Concern-split suffix `<name>_<concern>.go`** is the dominant idiom, driven by the 600-LOC cap. Canonical example — `internal/agent/`: `llm_agent.go`, `llm_agent_args.go`, `llm_agent_completion.go`, `llm_agent_construct.go`, `llm_agent_consume.go`, `llm_agent_dispatch.go`, `llm_agent_display.go`, `llm_agent_events.go`, `llm_agent_finalize.go`, `llm_agent_parallel.go`, `llm_agent_pause.go`, `llm_agent_prefix.go`, `llm_agent_promote.go`.
- Sentinel errors get their own file where a package has several: `internal/agent/errors.go`.
- Tests mirror the source file: `internal/share/expiry.go` → `internal/share/expiry_test.go`.
- Test-kind suffixes are meaningful and consistent: `*_integration_test.go`, `*_property_test.go` (e.g. `internal/swarm/swarm_property_test.go`), `*_fuzz_test.go` (e.g. `internal/agent/agent_fuzz_test.go`), `*_internal_test.go` (same-package white-box, e.g. `internal/agent/llm_agent_pause_internal_test.go`), `main_test.go` (goleak `TestMain`).

**Packages / directories:**
- Single lowercase word, no underscores, no plural-vs-singular drift: `askuser`, `breakglass`, `canonicaljson`, `identityctx`, `reasoningtrace`, `toolselectstore`, `usersandbox`.
- All library code lives under `internal/` (52 top-level dirs, 68 packages incl. subpackages). `cmd/aura` is CLI glue only.

**Types / functions:**
- Standard Go: `PascalCase` exported, `camelCase` unexported. `revive`'s `exported` rule is on with `disableStutteringCheck` (`.golangci.yml`), so `share.ShareConfig`-style stutter is tolerated but exported doc comments are **enforced**.
- Sentinel errors: `Err<Condition>`, package-scoped `var`, message prefixed with the package name.
  ```go
  // internal/share/expiry.go
  var ErrNonPositiveCustomExpiry = errors.New("share: custom expiry days must be positive")
  // internal/agent/tools/spec.go
  var ErrNoNonDeferredTool = errors.New("registry: at least one non-deferred tool is required (excluding tool_search)")
  ```
  Verified examples across `internal/agent`, `internal/agui`, `internal/askuser`, `internal/agent/tools`.
- Test doubles: `fake*` / `stub*` prefix on an unexported struct (`fakeOracle`, `stubAgent`, `fakePauseTool`, `fakeSearchEngine`, `fakeEmbedder`). **170** such declarations across `internal/` tests.

**Env vars:**
- Convention `AURA_<DOMAIN>_<UNIT>`: `AURA_AGUI_BUFFER_CAP`, `AURA_ASSET_PRESIGN_TTL_SEC`, `AURA_SHARE_MAX_EXPIRY_DAYS`, `AURA_COVERAGE_MIN`.
- Third-party/sidecar vars keep their upstream canonical names (`TELEGRAM_BOT_TOKEN`, `OPENROUTER_API_KEY`, `POSTGRES_PASSWORD`, `NEO4J_PASSWORD`).
- A grep for the literal token `AURA_[A-Z0-9_]+` over `internal/` + `cmd/` returns **271** distinct strings. Treat that as an **upper bound, not a knob count** — it includes prefix fragments built by concatenation (e.g. `AURA_DB_`). The authoritative catalog is the PRD §Caps & Limits env index.
- Reading env is centralized: `internal/config/` (split per domain, e.g. `internal/config/config_share.go`) and helpers in `internal/envutil/envutil.go`. Do **not** call `os.Getenv` from deep runtime code — see the file-header rationale in `internal/share/expiry.go`, which takes `now` and `capDays` as parameters precisely so it never reaches for ambient global state.

## Code Style

**Formatting:**
- `gofmt`, enforced as a gate not a suggestion. Configured under `formatters:` in `.golangci.yml`; auto-fixed by the lefthook pre-commit hook via `scripts/gofmt-staged.sh` (`stage_fixed: true`).
- Frontend: `prettier`. `npm run format:check` is part of the pre-push web gate.

**Linting (`.golangci.yml`, version `"2"`):**
- `default: none` — linters are opt-in, and **adding one without a slice-level rationale is forbidden** per the file's own header comment.
- Enabled: `errcheck`, `govet`, `ineffassign`, `staticcheck`, `unused`, `misspell`, `gosec`, `revive`, `dupl`.
- `dupl` threshold **100** tokens; `_test.go` excluded (table-driven cases are intentionally repetitive).
- `gosec` excludes **G115** (integer overflow — noisy on int32 pool fields with safe sources).
- `run.tests: true`, `timeout: 5m`, `modules-download-mode: readonly`.
- Version pin: **golangci-lint v2.12.2**, identical in `.github/workflows/ci.yml:85` and the `Makefile` `tools:` target — local/CI parity is deliberate.

**Lint exclusion paths** (both `linters.exclusions.paths` and `formatters.exclusions.paths`):
`internal/db/sqlc` (generated, golden), `internal/agent/tools` (pre-rewrite skeleton), `internal/llm/client.go` (pre-rewrite skeleton), `third_party`, `web/node_modules`, `.planning`.

**Test-only lint relaxations:**
- `_test.go` is exempt from `gosec`, `errcheck`, `dupl`.
- `_test.go` + text `SA5011` exempt from `staticcheck` — a documented false positive: the go1.26-built staticcheck does not model `testing.T.Fatal` as no-return, so nil-guard-then-use trips it. Production nil-deref detection stays on.

**File size — the 600-LOC cap (`scripts/check-file-size.sh`):**
- `CAP=600`, applied to `.go`, `.ts`, `.tsx` — **tests are not exempt**.
- Exempt: `internal/db/sqlc/`, `third_party/`, `vendor/`, `node_modules/`, `dist/`, `*.d.ts`.
- Two modes: whole tracked tree (`make file-size`, CI) vs. named files (lefthook passes `{staged_files}`). The staged-file mode exists because the 600-LOC cap is a per-file property and a full-tree `wc` sweep cost >2min on Windows Git Bash.
- Violation → refactor on touch, split into `<name>_<concern>.{go,ts,tsx}`.

## Import Organization

**Order** (observed in `internal/runner/runner.go`; goimports default, **no `-local` prefix is configured** so there are exactly two groups):
1. Standard library — `context`, `errors`, `fmt`, `iter`, `log/slog`, `os`, `path/filepath`, `strings`, `sync`, `time`
2. *(blank line)*
3. Everything else in **one** alphabetically-sorted group — module-internal (`github.com/chetto1983/aura/internal/...`) and third-party (`github.com/google/uuid`) intermixed, since `chetto1983` sorts among the other `github.com/*` paths.

Do not hand-split internal vs. third-party into a third group; `gofmt`/`goimports` will not preserve it.

**Path aliases:** none in Go (full module paths). Frontend aliases: **NOT VERIFIED** (not inspected this session — check `web/vite.config.ts` / `web/tsconfig.json` before relying on any).

## Error Handling

**Wrap with `%w`, always.** **961** occurrences of `%w` across non-test Go in `internal/` + `cmd/` — this is the default, not the exception.
```go
return fmt.Errorf("share: resolve expiry: %w", err)
```

**Sentinel + `errors.Is`/`errors.As`** for conditions a caller branches on. Declared as package-level `var`, either individually or in a grouped `var (...)` block:
```go
// internal/agui/governance_write_seam.go
var (
    ErrMCPServerExists   = errors.New("mcp server already exists")
    ErrMCPServerNotFound = errors.New("mcp server not found")
)
```

**Message style:** lowercase, no trailing punctuation, prefixed with the package/subsystem (`"share: …"`, `"registry: …"`, `"agui: …"`, `"onboarding: …"`). Messages are written to be actionable — the coverage gate and skip-helpers emit remediation text, not just a condition.

**`errcheck` is on** for production code — every returned error is handled or explicitly discarded with `_ =`. Discards should carry a why-comment when non-obvious (see `_ = json.Unmarshal(raw, &calls)` in `internal/runner/main_test.go`, a test-only best-effort decode).

## Logging

**Framework:** stdlib `log/slog` — **71** non-test files under `internal/` + `cmd/` import it. No third-party logger.

**Patterns:**
- Structured key/value, never `fmt.Sprintf`-ed prose.
- Loggers are injected (constructor field), not package-global.
- Never log secrets, tokens, or DSNs. `internal/secret/` exists for handling sensitive values.

## Comments

**When to comment — the rule that shapes this codebase:** *comments explain WHY, never WHAT.* Identifier names already carry the what. Comments earn their place only for hidden constraints, workarounds, surprising behavior, or a decision that a future reader would otherwise undo.

The codebase takes this unusually seriously, and it is the single most distinctive convention here. Real examples worth imitating:

- **File-header rationale blocks.** `internal/share/expiry.go` opens with ~17 lines explaining that the file is deliberately I/O-free and clock-free, *why* (`a function that reaches for ambient global state internally is not deterministically testable`), and explicitly what it does **not** do and which downstream plan owns that.
- **Comments that defend a constraint against future "cleanup".** `scripts/coverage_gate.sh` explains why `-p 1` is MANDATORY (shared Postgres → `CREATE ROLE` races to `tuple concurrently updated (XX000)`, golang-migrate advisory-lock deadlock). Without that comment someone deletes the flag and gets a flake.
- **Comments encoding incident history.** The `AURA_COVERAGE_ALLOW_LIVE_AURA_DB` guard in `scripts/coverage_gate.sh` cites the 2026-07-10 data-loss incident. `scripts/check-file-size.sh` documents the MSYS here-string mangling bug (`transport_test.go` → phantom `t.go`) that caused false commit-blocking failures.
- **Comments justifying a lint suppression.** The SA5011 exclusion in `.golangci.yml` states the analyzer's exact limitation.
- **Doc comments on exported identifiers** are enforced by `revive`'s `exported` rule and state intent + the governing decision ID, e.g. `ExpiryDefault` documents that the zero value `""` must resolve to the default, "never to an error and never (silently) to `ExpiryCustom`".

Traceability markers (`D-04`, `T-37F-27`, `OQ3`, `amendment #54`, `WR-01`, `Pitfall 3`) link code to the PRD/plan. Keep them when editing.

**Do not** write `// increment i` noise. **Do** update the comment in the same commit as the code (CLAUDE.md deep-refactor-on-touch).

## Function Design

**Size:** small enough that the enclosing file stays ≤600 LOC. That cap is the real forcing function.

**Purity where it buys testability:** the strong preference is to take `now time.Time`, caps, and config **as parameters** rather than reading the clock or env inside. `ResolveExpiry(opt, customDays, now, capDays)` in `internal/share/expiry.go` is the reference shape — its test fixes `now := time.Date(2026, 7, 17, ...)` so assertions are exact rather than tolerance-based.

**Context:** `ctx context.Context` first parameter on anything doing I/O; cancellation propagates end-to-end. Where a call must outlive the turn it uses `context.WithoutCancel` **plus** an explicit bound — never unbounded:
```go
// internal/runner/runner.go
const defaultTitleTimeout = 30 * time.Second
```

**Return values:** `(T, error)`. Errors wrapped with `%w`; sentinels for branchable conditions.

## Module Design

**Exports:** minimal. Everything under `internal/`, so the module boundary is the real encapsulation; within it, export only what a sibling package genuinely needs.

**Barrel files:** none. Import the concrete package.

**Test-support packages:** shared fakes live in a dedicated package (`internal/agent/agenttest/` — `fakeclient.go`, `mocks.go`) rather than being duplicated per consumer. Note it is **excluded from the coverage floor** (`scripts/coverage_gate.sh:64`) because its own self-coverage measures no owned runtime surface.

## Deferred-Tool Pattern (mandatory for agent tools)

Large tool specs must not bloat the LLM-visible manifest (it would break prompt caching every turn). `internal/agent/tools/spec.go`:

```go
type Spec struct {
    Name        string
    Summary     string          // one line, always shown in the manifest
    Description string          // full description; only shown when not Deferred OR after a tool_search hit
    Parameters  json.RawMessage // JSON-schema for the tool arguments
    Deferred    bool            // true → full spec hidden until tool_search loads it
    Mutating    bool            // can change host state → arms the completion critic gate (amendment #54 / D-43)
    Multiplexed bool            // fronts several sub-actions behind an `action` discriminator
}
```

Rules:
- Tool implementation goes in `internal/agent/tools/<name>.go`, spec metadata constant in that same file.
- Big tools: `Deferred: true` (**15** occurrences). Small always-visible tools (`text_response`, `ask_user`): `Deferred: false` (**7** occurrences).
- `Mutating` is **conservative by design** — `shell_exec` is `Mutating` even though `ls` mutates nothing, because the agent cannot statically know whether a command writes.
- `Mutating` and `Multiplexed` are runtime hints, **never wire-encoded / never LLM-visible**.

## Gates — Where Each Check Runs

`make quality` (`Makefile`) = prerequisites `vet file-size lint deadcode test-race vuln`, and its own recipe then runs `go build $(GO_PACKAGES)`. **There is no standalone `build:` make target** — `build` exists separately as a *lefthook pre-push command*.

**lefthook split (`lefthook.yml`), and the reasoning is explicit in the file:**

| Hook | Commands | Notes |
|---|---|---|
| **pre-commit** (`parallel: true`) | `gofmt` (staged, `stage_fixed`), `vet`, **`lint`**, `file-size` (staged), `dup` (jscpd, web TS/TSX) | **Lint is on COMMIT, not push** — deliberate: "a lint regression surfaces at the commit that introduced it instead of after a range of commits accumulates for a push (the caught issue is then one small diff, not a bisect)". Lint is glob-gated on `*.go` but still scans **whole owned packages** — staged-file-only linting misses cross-file findings, so the glob decides *whether* to run, not *what* to scan. |
| **pre-push** (`parallel: true`) | `quality-snapshot` freshness, `build`, `deadcode`, `web-deadcode` (knip), `web` (eslint + tsc + prettier) | Fast whole-module "still compiles + no dead code" checks before the network. |

Heavy gates (race, integration, coverage, mutation, Playwright) intentionally stay in CI / `make quality-full` — hooks must be fast enough not to be bypassed habitually.

**Frontend parity is deliberate** — every Go gate has a TS twin: `dupl`→`jscpd` (`web/.jscpd.json`, same 100-token threshold), `deadcode`→`knip` (`web/knip.json`), `golangci-lint`→`eslint --max-warnings=0`, `gofmt`→`prettier --check`.

**Emergency bypass:** `git commit --no-verify` / `git push --no-verify`. CI re-runs everything regardless.

**Quality-snapshot freshness gate (PRD amendment #20):** if a changed file matches a row's CI-gate-path glob in `docs/aura-quality-snapshot.md`, that row's `Last measured` date must be re-attested in the same push, with either a fresh measurement or a metric-neutral justification naming exactly what changed and why the number cannot move. Enforced by `scripts/quality_snapshot_gate.sh` (CI) and `scripts/quality_snapshot_prepush.sh` (lefthook). Verify locally before pushing:
```bash
AURA_QUALITY_CHANGED_FILES="$(git diff --name-only origin/master..HEAD)" \
AURA_QUALITY_BASE_DATE="$(git log -1 --format=%cs origin/master)" \
bash scripts/quality_snapshot_gate.sh   # must print: ok: … checked N row(s)
```

---

*Convention analysis: 2026-07-17*
