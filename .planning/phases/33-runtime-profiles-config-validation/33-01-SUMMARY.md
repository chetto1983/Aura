---
phase: 33-runtime-profiles-config-validation
plan: 01
subsystem: config
tags: [prof-01, prof-05, runtime-profile, config-validate, loc-unblock, contract-types, tdd]

# Dependency graph
requires: []
provides:
  - "internal/config/config_validate.go — Validate() relocated verbatim (config.go 557->534 LOC, freeing headroom under the whole-tree 600-LOC file-size hook) PLUS the Severity (Warn/Fatal) + Violation{Knob,Sev,Msg} contract types plans 02-05 consume"
  - "internal/config/config_runtimeprofile.go — type RuntimeProfile string {dev,local_trusted,single_user_hardened,server_production} (D-01) + total ParseProfile (unknown/empty -> dev, D-03, never panics) + (RuntimeProfile).Strict() lenient/strict tier helper (D-07/D-14)"
  - "internal/config/config.go — Config.Profile / Config.ObjectStoreReplicationFactor / Config.GarageRPCSecret fields populated in loadBase (AURA_PROFILE -> ParseProfile default dev; AURA_OBJECTSTORE_REPLICATION_FACTOR default 1; upstream GARAGE_RPC_SECRET); clearPostgresEnv extended with the four new knobs"
affects: [config, cmd/aura, garage]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Total typed-enum parser (config_runtimeprofile.go): switch over strings.TrimSpace(s) with a default arm — unknown/empty resolves to the loudest/most-permissive tier (dev) so a garbage AURA_PROFILE never silently selects a stricter posture the operator did not intend (T-33-01)."
    - "Contract-type pre-placement: Violation/Severity defined in the same file that will host ValidateProfile (plan 04) so the downstream re-parse pass + CLI build against fixed types without re-reading the codebase."
    - "Concern-split for LOC relief (config_validate.go / config_runtimeprofile.go): follows the existing config_env.go precedent ('refactor-on-touch, CLAUDE.md <=600 LOC NO GOD CLASS') — the Validate() move lands FIRST so every subsequent commit clears the whole-tree file-size hook (RESEARCH Pitfall 3)."

key-files:
  created:
    - internal/config/config_validate.go
    - internal/config/config_runtimeprofile.go
    - internal/config/config_runtimeprofile_test.go
    - .planning/phases/33-runtime-profiles-config-validation/33-01-SUMMARY.md
  modified:
    - internal/config/config.go
    - internal/config/config_test.go
  deleted: []

key-decisions:
  - "Validate() was MOVED before any field was added (Task 1 first). config.go was 557/600 LOC; the file-size git hook scans the whole tree and blocks ALL commits the moment any file exceeds 600, so the relief had to land first — config.go is now 534 (post-move) / 550 (post-fields), comfortable headroom for plans 02-05."
  - "Naming-collision avoidance (RESEARCH Pitfall 1) honored end-to-end: the runtime deployment profile uses RuntimeProfile / AURA_PROFILE (no _DIR) / config_runtimeprofile*.go and does NOT touch internal/profile, Config.ProfileDir, AURA_PROFILE_DIR, or config_profile_test.go (all unmodified, git-verified). The collision-guard grep on config_runtimeprofile.go returns 0 — the explanatory header was reworded to avoid even the literal forbidden substrings."
  - "GarageRPCSecret is a config-contract READ only this plan (T-33-02 accept): stored from upstream GARAGE_RPC_SECRET (CLAUDE.md sidecar-naming exception, like SEARXNG_URL), never logged/printed (grep-verified). Redaction in operator-facing output is plan 03/05's KnobSpec.Secret flag."
  - "ObjectStoreReplicationFactor default 1 matches docker/garage/garage.toml; it is declared durability INTENT for the plan-04 validation gate (D-13/PROF-06), NOT runtime enforcement and NOT a garage.toml parse — keeping garage.toml in sync is a deployment follow-on, per the plan's scope fence."

patterns-established:
  - "type RuntimeProfile string + total ParseProfile + .Strict() — the single severity decision the plan-03 re-parse pass and the plan-04 gates key on."
  - "Severity/Violation as the phase-wide validation contract, pre-placed in config_validate.go beside the (future) ValidateProfile."

requirements-completed: []  # PROF-01/PROF-05 foundation only — PROF-01 needs the `aura config validate` CLI (plan 05) + gates (plan 04); PROF-05 is the pre-existing AURA_RUN_DIR normalization. Verifier/orchestrator owns the flip at phase close.

# Coverage metadata
coverage:
  - id: D1
    description: "ParseProfile is total: four named profiles -> their constant, empty/garbage/whitespace -> dev (D-03), and .Strict() collapses lenient {dev,local_trusted} vs strict {hardened,prod} (D-07/D-14)."
    requirement: "PROF-01"
    verification:
      - kind: unit
        ref: "internal/config/config_runtimeprofile_test.go#TestParseProfile (7 cases, go test -race ./internal/config/)"
        status: pass
    human_judgment: false
  - id: D2
    description: "loadBase populates Config.Profile (default dev), Config.ObjectStoreReplicationFactor (default 1), Config.GarageRPCSecret (round-trips from upstream env); clearPostgresEnv clears the four new knobs so the baseline is known."
    requirement: "PROF-05"
    verification:
      - kind: unit
        ref: "internal/config/config_runtimeprofile_test.go#TestRuntimeProfileFieldsLoad (defaults + overrides, go test -race ./internal/config/)"
        status: pass
    human_judgment: false
  - id: D3
    description: "Validate() relocation is behavior-preserving; Violation/Severity contract types exist for plans 02-05; config.go under the 600-LOC cap."
    requirement: "PROF-05"
    verification:
      - kind: unit
        ref: "internal/config/config_validate_test.go#TestConfigValidate (unchanged, green) + grep config.go LOC=550<600"
        status: pass
    human_judgment: false

# Execution metrics
metrics:
  duration: ~11min
  completed: 2026-06-30
  tasks: 3
  files: 5
---

# Phase 33 Plan 01: RuntimeProfile primitives + config_validate split Summary

Unblocked Phase 33 by relocating `Validate()` out of the 557/600-LOC `config.go` into a new `config_validate.go` (behavior-identical; frees headroom under the whole-tree file-size git hook before any field is added), then landed the runtime-profile foundation the rest of the phase builds on: the total `RuntimeProfile` enum + `ParseProfile` + `Strict()` helper, three new load-time `Config` fields (`Profile`, `ObjectStoreReplicationFactor`, `GarageRPCSecret`), and the `Violation`/`Severity` contract types plans 02-05 consume.

## What was built

**Task 1 — Validate() split + contract types (`refactor`, commit `2734f43c`).**
Moved `func (c *Config) Validate() error` (plus its doc comment) verbatim from `config.go` into a new `internal/config/config_validate.go` (`package config`, imports `fmt`+`strings`). config.go dropped 557 -> 534 LOC and its now-unused `strings` import was removed (deep-refactor-on-touch). Defined the phase-wide validation contract in the same file: `type Severity int` (`Warn`=iota, `Fatal`), `type Violation struct {Knob string; Sev Severity; Msg string}`. `TestConfigValidate` passes unchanged.

**Task 2 — RuntimeProfile enum + ParseProfile + Strict() (`feat`, commit `0d439f16`, TDD).**
New `internal/config/config_runtimeprofile.go`: `type RuntimeProfile string` with constants `ProfileDev`/`ProfileLocalTrusted`/`ProfileSingleUserHardened`/`ProfileServerProduction`; `ParseProfile(s string)` is a total `switch` over `strings.TrimSpace(s)` defaulting unknown/empty to `ProfileDev` (D-03); `(RuntimeProfile).Strict()` returns true only for the hardened/prod tiers (D-07/D-14). `TestParseProfile` is a 7-case anonymous-struct table (four named + empty + garbage + whitespace) mirroring `TestGuardWebBind`. Distinct names from the Agent.md profile surface — collision-guard grep returns 0.

**Task 3 — Config fields + loadBase reads + clearPostgresEnv (`feat`, commit `39f8dbf1`, TDD).**
Added `Config.Profile RuntimeProfile` (near the top-level fields, deliberately apart from `ProfileDir`), `Config.ObjectStoreReplicationFactor int`, and `Config.GarageRPCSecret string` (object-store-adjacent). `loadBase()` populates them: `Profile: ParseProfile(os.Getenv("AURA_PROFILE"))`, `ObjectStoreReplicationFactor: envutil.IntDefault("AURA_OBJECTSTORE_REPLICATION_FACTOR", 1)`, `GarageRPCSecret: os.Getenv("GARAGE_RPC_SECRET")`. Extended `clearPostgresEnv` with `AURA_PROFILE`, `AURA_OBJECTSTORE_REPLICATION_FACTOR`, `GARAGE_RPC_SECRET`, `AURA_SHELL_DESTRUCTIVE_PATTERNS`. `TestRuntimeProfileFieldsLoad` asserts defaults (dev / 1 / "") and overrides (server_production / 3 / "abc").

## Test results (WSL, CGO_ENABLED=1)

- `go test ./internal/config/ -run TestConfigValidate -count=1` -> `ok` (Task 1, behavior-preserving).
- `go test ./internal/config/ -run TestParseProfile -race -count=1` -> PASS, all 7 subtests (Task 2).
- `go test ./internal/config/ -run 'TestRuntimeProfileFieldsLoad|TestParseProfile|TestConfigValidate|TestWebAuthConfigLoad' -race -count=1` -> PASS (Task 3 verify command).
- Full package `go test ./internal/config/ -race -count=1` -> `ok` (no regression; `TestProfileConfigDefaultsAndOverrides` and all siblings green).
- Native `go build ./...` -> green; `go vet ./...` -> clean; `gofmt -l` -> clean.

TDD note: both `tdd="true"` tasks had their test written first and observed RED via WSL (Task 2: `undefined: RuntimeProfile/ParseProfile...`; Task 3: `cfg.Profile undefined ...`) before the implementation. Because the project's pre-commit hook runs `go vet` (which rejects a non-compiling package), a bare compile-failure RED cannot be committed through the gate, so each TDD task is committed atomically as a single `feat` after GREEN — the RED observation is recorded here and in each commit body.

## Acceptance criteria — verified

| Criterion | Result |
|-----------|--------|
| config_validate.go declares package config + Validate() + Violation + Severity | yes |
| config.go no longer contains Validate() (`grep -c` = 0) | yes |
| config.go <= 600 LOC | 550 |
| ParseProfile + Strict() present; collision-guard grep = 0 | yes (0) |
| config_profile_test.go / internal/profile / ProfileDir / AURA_PROFILE_DIR untouched | yes (git-verified) |
| Profile/ObjectStoreReplicationFactor/GarageRPCSecret struct fields present | 1 / 2 / 2 |
| `ParseProfile(os.Getenv("AURA_PROFILE"))` in loadBase (`grep -c` = 1) | yes |
| clearPostgresEnv contains the four new knobs | yes |
| GarageRPCSecret never logged/printed (T-33-02) | grep-verified NONE |

## Deviations from Plan

**1. [Rule 1 - Bug] Collision-guard grep failed on the explanatory header comment.**
- Found during: Task 2 post-edit verification.
- Issue: the file-header NOTE in `config_runtimeprofile.go` named the forbidden symbols (`Config.ProfileDir`, `AURA_PROFILE_DIR`) to explain the avoidance, so the acceptance grep `grep -c "AURA_PROFILE_DIR\|ProfileDir"` returned 2 instead of the required 0.
- Fix: reworded the comment to describe the Agent.md-profile surface without the literal `ProfileDir` / `AURA_PROFILE_DIR` substrings (kept the warning intact). Grep now returns 0.
- Files: internal/config/config_runtimeprofile.go. Commit: 0d439f16.

**2. [Rule 3 - Blocking] gofmt struct-comment realignment.**
- Found during: Task 3 post-edit `gofmt -l`.
- Issue: the two new object-store struct fields broke the trailing-comment column alignment of the block; `gofmt -l` flagged config.go (the pre-commit gofmt hook would have blocked the commit).
- Fix: ran `gofmt -w internal/config/config.go`; re-verified build green. No behavior change.
- Files: internal/config/config.go. Commit: 39f8dbf1.

No architectural (Rule 4) changes; no auth gates; no package installs (pure-Go, deps pre-present). Plan prohibitions honored: NO per-profile runtime enforcement added; `internal/profile` / `Config.ProfileDir` / `AURA_PROFILE_DIR` / `config_profile_test.go` untouched.

## Known Stubs

None. Every field is wired to a real env read and exercised by a passing test. The validation gates that consume `Violation`/`Severity` are intentionally deferred to plans 03-05 (documented phase sequencing, not a stub).

## Self-Check: PASSED
- FOUND: internal/config/config_validate.go
- FOUND: internal/config/config_runtimeprofile.go
- FOUND: internal/config/config_runtimeprofile_test.go
- FOUND commit 2734f43c (refactor: split Validate)
- FOUND commit 0d439f16 (feat: RuntimeProfile enum)
- FOUND commit 39f8dbf1 (feat: Config fields)
