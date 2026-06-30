---
phase: 33-runtime-profiles-config-validation
plan: 05
subsystem: cli
tags: [config-validation, runtime-profiles, cli, security, redaction, go]

# Dependency graph
requires:
  - phase: 33-01
    provides: RuntimeProfile/ParseProfile + Violation/Severity contract + Config.Profile
  - phase: 33-03
    provides: KnobSpec registry with Secret flag + generic reparsePass
  - phase: 33-04
    provides: (*Config).ValidateProfile(p) aggregating gate + reparse pass into one []Violation
provides:
  - "aura config validate [--profile <p>] [--json] operator subcommand (PROF-01)"
  - "configValidate(args) + testable runConfigValidate(args, io.Writer) int — thin presenter over ValidateProfile"
  - "Fail-closed exit code (1 on any Fatal), value-free renderer (secret redaction by construction, T-33-10)"
affects: ["Phase 33 verifier", "operator CI lint of any runtime profile"]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Testable CLI core: configValidate calls os.Exit(runConfigValidate(args, out)); the inner func returns the exit code so a test asserts it in-process (no subprocess, AV-safe)"
    - "Redaction by construction: the renderer is a pure function of []config.Violation — no knob VALUE is ever read into it, so no secret can reach stdout/CI logs"
    - "D-02 --profile override via config.ParseProfile (total, unknown→dev) so an operator lints any profile without mutating env"

key-files:
  created:
    - cmd/aura/config_validate.go
    - cmd/aura/config_validate_test.go
    - .planning/phases/33-runtime-profiles-config-validation/33-05-SUMMARY.md
  modified:
    - cmd/aura/config.go

key-decisions:
  - "Renderer takes only []config.Violation (never the Config / env) — redaction is structural, not a runtime check; satisfies T-33-10 more strongly than a per-value REDACTED substitution"
  - "Exit codes follow the plan literally: 2 on flag parse error, 1 on load failure OR any Fatal violation (fail-closed), 0 otherwise"
  - "Reworded the header doc comment so `grep -c ValidateProfile cmd/aura/config_validate.go` == 1 (only the load-bearing call), honoring the acceptance grep exactly"

patterns-established:
  - "cmd/aura e2e exit-code test via the inner func() int core capturing a bytes.Buffer (PATTERNS §No Analog Found resolution)"
  - "t.Setenv-composed unsafe server_production posture (DB/Neo4j present so the run reaches the gates) asserting every offending AURA_* knob name appears + no secret value echoed"

requirements-completed: [PROF-01]

# Metrics
duration: ~20min
completed: 2026-07-01
---

# Phase 33 Plan 05: `aura config validate` Operator CLI Summary

**Exposes the plan-04 validation core as the operator-facing `aura config validate [--profile <p>] [--json]` subcommand (PROF-01): a thin presenter over `(*Config).ValidateProfile` that lists EVERY unmet requirement naming its `AURA_*` knob, exits non-zero on any Fatal (fail-closed), supports a `--profile` override (D-02) + CI-parseable `--json`, and never echoes a secret VALUE (T-33-10).**

## Performance

- **Duration:** ~20 min
- **Completed:** 2026-07-01 (UTC+2 commit timestamps 00:31–00:34)
- **Tasks:** 2
- **Files created/modified:** 3 (2 created, 1 modified)

## Accomplishments
- `cmd/aura/config_validate.go`: `configValidate(args)` calls `os.Exit(runConfigValidate(args, os.Stdout))`; the testable inner `runConfigValidate(args []string, out io.Writer) int` builds a `flag.NewFlagSet("config validate", flag.ContinueOnError)` with `--profile string` + `--json bool`, loads `config.LoadServe()` (tolerant of empty LLM key), resolves the profile (`--profile` via `config.ParseProfile`, else `cfg.Profile`, D-02), calls `cfg.ValidateProfile(p)`, renders a human table (default) or `json.NewEncoder(out).Encode(violations)` (`--json`), and returns 1 if any violation is `config.Fatal` (fail-closed), 2 on flag error, else 0.
- Wired `case "validate": configValidate(args[1:])` into `runConfig` and documented `validate [--profile <p>] [--json]` in `configUsage`.
- `renderViolations` is a pure function of `[]config.Violation` — it NEVER reads or prints a knob VALUE, only the knob NAME, severity (`FATAL`/`WARN`) and the value-free message. Redaction is therefore structural (T-33-10): no code path can leak a secret to stdout/CI logs.
- `cmd/aura/config_validate_test.go`: `TestConfigValidate_ServerProduction` with four sub-tests — (1) an unsafe `server_production` posture exits 1 and the output names every offending knob (`AURA_OBJECTSTORE_ACCESS_KEY`, `AURA_OBJECTSTORE_SECRET_KEY`, `AURA_AGUI_CORS_PERMISSIVE`, `AURA_OBJECTSTORE_REPLICATION_FACTOR`, `GARAGE_RPC_SECRET`, `AURA_AUTHULA_SECRET`, `AURA_SHELL_DESTRUCTIVE_PATTERNS`); (2) a benign `--profile dev` env exits 0; (3) `--json` decodes into `[]config.Violation` listing the violations; (4) the sample secret VALUES never appear in the rendered report while the secret knob is named.

## Task Commits

Each task committed atomically (direct `git commit`, hooks ran: gofmt + vet + file-size all green):

1. **Task 1: `aura config validate` thin-presenter subcommand (flags, LoadServe, ValidateProfile, render, exit code)** — `5ef3ab37` (feat)
2. **Task 2: e2e — server_production exits 1 listing every unmet knob; dev exits 0; --json; redaction** — `2e0c8d51` (test)

**Plan metadata:** _this commit_ (docs: complete plan)

## Files Created/Modified
- `cmd/aura/config_validate.go` (NEW, 119 LOC, well under the 600 cap) — `configValidate`, testable `runConfigValidate(args, out) int`, `renderViolations`, `severityLabel`, `anyFatal`.
- `cmd/aura/config_validate_test.go` (NEW, 138 LOC) — `TestConfigValidate_ServerProduction` (4 sub-tests) + `setUnsafeServerProductionEnv` helper + sample-sentinel consts.
- `cmd/aura/config.go` (MODIFIED) — `case "validate"` dispatch + `configUsage` documentation line.

## Decisions Made
- **Renderer is value-free by construction.** Rather than read each knob's value and substitute `REDACTED` (the `configShow` analog), the renderer accepts only `[]config.Violation` — whose messages are value-free — so no secret value is ever in scope to leak. This satisfies T-33-10 more strongly than a per-value redaction and keeps the presenter thin (no env reads in cmd/aura).
- **Exit codes per the plan literal:** 2 on `flag.Parse` error (ContinueOnError already printed usage to stderr), 1 on a `LoadServe` failure OR any Fatal violation (fail-closed, T-33-11), 0 otherwise.
- **Comment reworded for the acceptance grep:** the header comment initially mentioned `ValidateProfile` (pushing `grep -c ValidateProfile` to 2); reworded to "the internal/config per-profile validator" so only the load-bearing `cfg.ValidateProfile(p)` call counts (== 1). Cosmetic, no logic change.

## Deviations from Plan

None — plan executed exactly as written. No bugs, missing critical functionality, or blocking issues surfaced; both tasks landed as specified.

## Issues Encountered
- One acceptance grep is a literal-substring check (`grep -c "ValidateProfile" cmd/aura/config_validate.go == 1`). The initial header doc comment contained the literal and tripped the count to 2; reworded the comment. Verified `ValidateProfile == 1`, thin-presenter grep (`defaultObjectStore\|replication_factor\|ParseBool`) `== 0`, `case "validate" == 1`.

## TDD Gate Compliance
The plan's Task 2 is `tdd="true"` but the implementation (Task 1, `runConfigValidate`) precedes it as the presenter; Task 2 is the e2e test over that core. A literal RED-before-symbol commit is not possible on this host (the lefthook pre-commit `go vet ./...` rejects a non-compiling commit). The test was authored against the Task-1 core and verified green under `-race` in WSL before the `test(33-05)` commit. The git log shows one `feat(33-05)` (presenter) followed by one `test(33-05)` (e2e), recorded here per the executor TDD-gate guidance.

## Verification (actual WSL output)
- `go build ./cmd/aura/` + `go vet ./cmd/aura/` — green natively on Windows (build/vet do not execute a test binary).
- `go build ./...` (whole tree) — `BUILD_ALL_OK`.
- `wsl ... CGO_ENABLED=1 go test ./cmd/aura/ -run TestConfigValidate_ServerProduction -race -count=1 -v` →
  ```
  --- PASS: TestConfigValidate_ServerProduction (0.01s)
      --- PASS: TestConfigValidate_ServerProduction/lists_every_unmet_knob_and_exits_1 (0.00s)
      --- PASS: TestConfigValidate_ServerProduction/benign_--profile_dev_exits_0 (0.00s)
      --- PASS: TestConfigValidate_ServerProduction/--json_decodes_into_[]config.Violation (0.00s)
      --- PASS: TestConfigValidate_ServerProduction/secret_knob_values_are_redacted (0.00s)
  PASS
  ok  	github.com/chetto1983/aura/cmd/aura	1.071s
  ```
- Grep invariants: `case "validate"` in config.go = 1; `ValidateProfile` in config_validate.go = 1; thin-presenter grep (`defaultObjectStore|replication_factor|ParseBool`) in config_validate.go = 0; `func TestConfigValidate_ServerProduction` = 1; config_validate.go = 119 LOC (<600).

## Next Phase Readiness
- PROF-01 is now fully delivered: the operator acceptance command `aura config validate --profile server_production` reports all unmet requirements and fails closed. This is the LAST plan of Phase 33 — the phase verifier runs next.
- Optional manual smoke (host): build+run `aura config validate --profile server_production` in WSL/container (never the native `.exe` per MEMORY/AV).

## Self-Check: PASSED

- All 3 created/modified files present on disk (`cmd/aura/config_validate.go`, `cmd/aura/config_validate_test.go`, `cmd/aura/config.go`, plus this SUMMARY).
- Both task commits present in git log (`5ef3ab37`, `2e0c8d51`).
- Requirement PROF-01 marked complete in REQUIREMENTS.md (the CLI acceptance command is delivered).

---
*Phase: 33-runtime-profiles-config-validation*
*Completed: 2026-07-01*
