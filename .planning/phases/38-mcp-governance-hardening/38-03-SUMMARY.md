---
phase: 38-mcp-governance-hardening
plan: 03
subsystem: config
tags: [config-validation, mcp, security, fail-closed, runtime-profiles, boot-gate]

# Dependency graph
requires:
  - phase: 33-runtime-profiles-config-validation
    provides: "RuntimeProfile/ValidateProfile/Violation contract + reparsePass engine + KnobSpec registry this plan extends"
provides:
  - "gateMCPLegacyEnv(RuntimeProfile) []Violation — prod-only fail-closed gate for the legacy AURA_MCP_SERVERS_JSON env override, wired into ValidateProfile/cfg.Validate() boot enforcement"
  - "AURA_MCP_LEGACY_ENV_COMPAT KindBool KnobSpec registered in config_knobs.go's Tier A group (Default false)"
affects: [38-VALIDATION]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Prod-only raw-env fail-closed gate template (mirrors gateDestructiveShell verbatim): early-return nil unless ProfileServerProduction, then a raw os.Getenv check, then a Fatal Violation naming both the offending knob and its opt-out flag"

key-files:
  created: []
  modified:
    - internal/config/config_validate.go
    - internal/config/config_validate_test.go
    - internal/config/config_knobs.go

key-decisions:
  - "Used a compiling-stub RED pattern (not a compile-failure RED) because the lefthook pre-commit `vet` gate runs `go vet ./...` unconditionally across the whole repo with no TDD carve-out; the RED commit added gateMCPLegacyEnv as a stub returning nil unconditionally so the package builds while the new table test fails on assertions expecting a Fatal violation."
  - "Wired gateMCPLegacyEnv into ValidateProfile between gateMUSRIsolation and gateObjectStoreEndpoint — grouping the server_production-only Fatal gates together, with the WARN-only endpoint check last."
  - "Left config_mcp.go's env-parsing/merge precedence untouched (planner assumption, Probe #18) — this plan only gates the env at boot, it does not change how the env is parsed once allowed."

patterns-established:
  - "TDD RED phase under a whole-repo vet pre-commit gate: stub the new symbol to a zero-value/no-op body first so the package compiles, let the new test fail on assertions (not a build error), then replace the stub body in GREEN. Needed whenever a plan's RED test references a wholly new symbol in a repo with an unconditional `go vet ./...` pre-commit hook."

requirements-completed: [MCPH-08]

coverage:
  - id: D1
    description: "gateMCPLegacyEnv prod-only fail-closed gate: a non-empty AURA_MCP_SERVERS_JSON under server_production is a Fatal boot violation unless AURA_MCP_LEGACY_ENV_COMPAT=1 is explicitly set; every other profile (dev, local_trusted, single_user_hardened) is untouched regardless of the env/flag combination."
    requirement: "MCPH-08"
    verification:
      - kind: unit
        ref: "internal/config/config_validate_test.go#TestGateMCPLegacyEnv"
        status: pass
      - kind: unit
        ref: "internal/config/config_validate_test.go#TestValidateProfile (unregressed)"
        status: pass
    human_judgment: false
  - id: D2
    description: "AURA_MCP_LEGACY_ENV_COMPAT registered as a KindBool KnobSpec (Default false) in config_knobs.go's Tier A group; a malformed value is now flagged by the existing generic reparsePass engine under a strict tier."
    requirement: "MCPH-08"
    verification:
      - kind: unit
        ref: "internal/config/config_knobs_test.go#TestKnobRegistry"
        status: pass
      - kind: unit
        ref: "internal/config/config_knobs_test.go#TestRapidEnvStrictness (property-based, now generically covers the new row)"
        status: pass
      - kind: unit
        ref: "internal/config/config_knobs_test.go#TestRapidEnvNoFalsePositive (property-based, now generically covers the new row)"
        status: pass
    human_judgment: false

# Metrics
duration: ~26min
completed: 2026-07-18
status: complete
---

# Phase 38 Plan 03: Legacy MCP env production gate Summary

**gateMCPLegacyEnv fail-closed boot gate: a non-empty AURA_MCP_SERVERS_JSON under server_production is a Fatal violation unless AURA_MCP_LEGACY_ENV_COMPAT=1 explicitly opts in, wired into the existing cfg.Validate() boot enforcement with zero new boot wiring.**

## Performance

- **Duration:** ~26 min
- **Started:** 2026-07-18T09:37:00Z (approx.)
- **Completed:** 2026-07-18T10:03:00Z
- **Tasks:** 1 (TDD: RED + GREEN; no REFACTOR needed)
- **Files modified:** 3

## Accomplishments
- `gateMCPLegacyEnv(RuntimeProfile) []Violation` added to `config_validate.go`, mirroring `gateDestructiveShell`'s exact template (prod-only, raw-env read, Fatal `Violation` naming both the offending knob and its opt-out flag)
- Wired into `ValidateProfile`'s aggregation so boot's existing `cfg.Validate()` enforces it with no new boot wiring
- `AURA_MCP_LEGACY_ENV_COMPAT` registered as a `KindBool` `KnobSpec` (Default `"false"`) in `config_knobs.go`, automatically picked up by the generic `reparsePass` engine and its pre-existing property-based tests (`TestRapidEnvStrictness`, `TestRapidEnvNoFalsePositive`, `TestKnobRegistry`) with zero new test code required for that path

## Task Commits

Each task committed atomically (TDD):

1. **Task 1 — RED**: add failing test for gateMCPLegacyEnv — `b3df2b5a` (test)
2. **Task 1 — GREEN**: implement gateMCPLegacyEnv prod-only fail-closed gate — `6fc9d87d` (feat)

_No REFACTOR commit: the GREEN implementation was already minimal, matching the plan's exact specified template (the gateDestructiveShell shape) verbatim — nothing left to clean up._

**Plan metadata:** (this SUMMARY's own commit, immediately following)

## Files Created/Modified
- `internal/config/config_validate.go` (300 LOC) — adds `gateMCPLegacyEnv` + wires it into `ValidateProfile` + updates the now-stale `ValidateProfile` doc comment to name the 3rd raw-env-reading gate
- `internal/config/config_validate_test.go` (443 LOC) — adds `TestGateMCPLegacyEnv` table test (9 subtests + a 3-profile no-op sub-loop)
- `internal/config/config_knobs.go` (197 LOC) — adds the `AURA_MCP_LEGACY_ENV_COMPAT` `KnobSpec` row to the Tier A group

## Decisions Made
See `key-decisions` frontmatter. In summary: a compiling-stub RED pattern (forced by this repo's whole-tree `vet` pre-commit hook), gate placement in `ValidateProfile` (grouped with the other prod-only Fatal gates), and confirming `config_mcp.go`'s env-parsing/merge precedence stays untouched (explicitly out of scope per the plan's planner assumption).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Stale `ValidateProfile` doc comment after adding the 3rd raw-env gate**
- **Found during:** Task 1 (GREEN)
- **Issue:** `ValidateProfile`'s doc comment stated "The only env reads are reparsePass ... and gateDestructiveShell" — no longer accurate once `gateMCPLegacyEnv` (which also reads raw env directly via `os.Getenv`/`envutil.BoolDefault`) was wired in.
- **Fix:** Updated the comment to name all three raw-env-reading gates (`reparsePass`, `gateDestructiveShell`, `gateMCPLegacyEnv`).
- **Files modified:** `internal/config/config_validate.go`
- **Verification:** Comment now accurately reflects the code; reviewed by inspection (comment text is not itself test-asserted).
- **Committed in:** `6fc9d87d` (Task 1 GREEN commit)

**2. [Rule 3 - Blocking] TDD RED phase adapted to a compiling stub instead of a compile-failure**
- **Found during:** Task 1 (RED phase, first commit attempt)
- **Issue:** The phase's own 38-01 precedent (and the generic TDD convention) writes a RED test against a wholly undefined symbol, producing an intentional compile failure. This repo's lefthook pre-commit hook runs `go vet ./...` unconditionally across the whole repository with no TDD/RED carve-out, so a genuine compile-failure commit is rejected outright by the hook — confirmed: the first RED commit attempt (test referencing the not-yet-defined `gateMCPLegacyEnv`) was blocked at the `vet` pre-commit stage.
- **Fix:** Added `gateMCPLegacyEnv` as a compiling stub (`return nil` unconditionally, deliberately NOT wired into `ValidateProfile` yet) in the RED commit, so the package builds while the new table test fails at runtime on its Fatal-violation-expecting assertions. Verified: 3 of 9 subtests failed for the right reason (`Fatal=false, want true`) against the stub, before the GREEN commit replaced the stub body with the real check. This is arguably a more textbook RED (the test fails on behavior, not on a trivial undefined-symbol build error) and keeps `go vet`/lint/gofmt/file-size green throughout, honoring the "hooks run normally, no `--no-verify`" execution constraint.
- **Files modified:** `internal/config/config_validate.go` (stub added in RED, replaced with the real implementation in GREEN)
- **Verification:** RED commit's pre-commit hook passed (gofmt/file-size/vet/lint all green); the new test failed exactly on the 3 subtests expecting a Fatal violation; GREEN commit's hook also passed, with all subtests green afterward.
- **Committed in:** `b3df2b5a` (RED), `6fc9d87d` (GREEN)

---

**Total deviations:** 2 auto-fixed (1 bug — stale comment; 1 blocking — TDD RED mechanics adapted to the repo's hook constraints)
**Impact on plan:** Both necessary for correctness/consistency; no scope creep. The gate's behavior, wiring, and knob registration exactly match the plan's `<action>`/`<behavior>` spec.

## Issues Encountered
- `-race` is unavailable in this Windows worktree (`go: -race requires cgo`; no gcc/w64devkit toolchain present). Ran the full non-race suite instead (`go test ./internal/config/...` — all green, including `TestGateMCPLegacyEnv` and the pre-existing `TestValidateProfile`/`TestKnobRegistry`/`TestRapid*` suites with zero regressions) per the plan's own documented Windows fallback ("if `-race` fails to link on Windows, fall back to non-race and note it in the SUMMARY"). The phase-close WSL full-matrix verification (per CLAUDE.md's Quality tooling table) is the authoritative `-race` run for this plan's code.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- MCPH-08 / F-014 closed: a `server_production` deploy can no longer silently inherit an un-governed legacy `AURA_MCP_SERVERS_JSON` server set carried over from a dev config; boot now fails closed unless `AURA_MCP_LEGACY_ENV_COMPAT=1` is explicit (T-38-09 mitigated).
- Fully independent of the classifier/lifecycle work in the phase's other plans (Wave 1, `depends_on: []`) — no blockers for 38-02/38-04/38-05/38-06/38-07. (38-02's planner_assumptions already reference this plan by name for `AURA_MCP_LEGACY_ENV_COMPAT`'s Tier A registration ownership — consistent naming confirmed.)
- Phase-close verification should include a live `-race` re-run (WSL) and confirm this gate's behavior against the full threat-model register (T-38-09).

---
*Phase: 38-mcp-governance-hardening*
*Completed: 2026-07-18*

## Self-Check: PASSED

- `internal/config/config_validate.go` — FOUND
- `internal/config/config_validate_test.go` — FOUND
- `internal/config/config_knobs.go` — FOUND
- Commit `b3df2b5a` (test RED) — FOUND in `git log --oneline --all`
- Commit `6fc9d87d` (feat GREEN) — FOUND in `git log --oneline --all`
- `grep "func (c \*Config) gateMCPLegacyEnv("` in config_validate.go — PASS
- `grep "vs = append(vs, c.gateMCPLegacyEnv(p)...)"` in config_validate.go (ValidateProfile wiring) — PASS
- `grep '{Name: "AURA_MCP_LEGACY_ENV_COMPAT", Kind: KindBool'` in config_knobs.go — PASS
- `go test ./internal/config/...` — PASS (full package green, no regressions)
- `go vet ./...` (whole repo) — PASS
- `go build ./...` (whole repo) — PASS
- `golangci-lint run ./internal/config/...` — 0 issues
- `bash scripts/check-file-size.sh` on all 3 touched files — PASS (300/443/197 LOC, all ≤600)
- `go test ./internal/config/ -race` — NOT RUN (no CGO/gcc toolchain in this Windows worktree; documented Windows fallback used, non-race suite green)
