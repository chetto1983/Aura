---
phase: 33-runtime-profiles-config-validation
plan: 03
subsystem: infra
tags: [config, validation, env-catalog, property-based-testing, rapid, strconv]

# Dependency graph
requires:
  - phase: 33-01
    provides: RuntimeProfile/ParseProfile/Strict() enum + Violation/Severity contract types
provides:
  - KnobKind/KnobSpec types + knobRegistry() — single source of truth for Tier A+B hot-path AURA_* knobs (QUAL-04/D-08)
  - Generic kind-driven reparsePass(RuntimeProfile) []Violation — Fatal under strict tiers, Warn under lenient (PROF-04/F-016/D-07)
  - TestKnobRegistry (structural round-trip) + TestReparsePass (table) + 3 rapid PBT invariants (strictness/no-false-positive/aggregation-monotonicity)
affects: [33-04, 33-05]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Data registry IS the validation engine (D-08): one []KnobSpec slice + ~30 LOC stdlib strconv/slices, zero per-knob code"
    - "Re-parse for diagnostics, not fallback: same strconv mechanics as envutil leaf but emit a named Violation instead of silently absorbing (D-06)"
    - "Profile coupling is one line: sev := Warn; if p.Strict() { sev = Fatal }"

key-files:
  created:
    - internal/config/config_knobs.go
    - internal/config/config_knobs_test.go
  modified: []

key-decisions:
  - "Registry secret set is EXACTLY the four AURA_OBJECTSTORE_ACCESS_KEY/SECRET_KEY + GARAGE_RPC_SECRET + AURA_AUTHULA_SECRET; DB/Neo4j secrets stay out (covered by existing Validate(), not the reparse surface) so the 'four secret knobs' assertion is exact."
  - "Garbage generator [g-su-zG-SU-Z]{2,8} (no digits, no t/f/T/F) is provably invalid for int AND bool AND enum simultaneously — one generator drives the strictness/aggregation invariants across all three checkable kinds."
  - "rapid properties inject via os.Setenv + per-iteration defer os.Unsetenv over a clearPostgresEnv(t) baseline; invariants assert only about the knob under test, so unrelated host env never causes false failures."

patterns-established:
  - "KnobSpec.Secret flags data for the plan-05 renderer; the reparse pass itself NEVER echoes a knob value (Violation.Msg = 'not a valid <kind>') — T-33-03b mitigation lives in the data, not the rendering."
  - "Non-enum rows must carry no Enum and enum rows must carry a non-empty Enum — enforced by TestKnobRegistry to keep the slice honest."

requirements-completed: [PROF-04, QUAL-04]

coverage:
  - id: D1
    description: "KnobSpec registry as single source of truth for Tier A+B hot-path AURA_* knobs (no dup Name, enum rows non-empty, exactly four secrets, no Tier C leakage)"
    requirement: "QUAL-04"
    verification:
      - kind: unit
        ref: "internal/config/config_knobs_test.go#TestKnobRegistry"
        status: pass
    human_judgment: false
  - id: D2
    description: "Generic kind-driven reparsePass: invalid int/bool/enum ⇒ Fatal under strict (hardened/production), Warn under lenient (dev/local_trusted), naming the knob; unset/whitespace/valid ⇒ no violation"
    requirement: "PROF-04"
    verification:
      - kind: unit
        ref: "internal/config/config_knobs_test.go#TestReparsePass"
        status: pass
      - kind: unit
        ref: "internal/config/config_knobs_test.go#TestRapidEnvStrictness (rapid, -race, 2000 checks ×2 stable)"
        status: pass
      - kind: unit
        ref: "internal/config/config_knobs_test.go#TestRapidEnvNoFalsePositive"
        status: pass
      - kind: unit
        ref: "internal/config/config_knobs_test.go#TestRapidEnvAggregationMonotonic"
        status: pass
    human_judgment: false

# Metrics
duration: 22min
completed: 2026-06-30
status: complete
---

# Phase 33 Plan 03: KnobSpec Registry + Generic Re-parse Pass Summary

**Data-registry validation engine: a `[]KnobSpec` single source of truth for the Tier A+B hot-path `AURA_*` knobs plus a generic kind-driven `reparsePass` that fails fast (Fatal) on any invalid int/bool/enum under hardened/production and warns under dev/local_trusted — proven by a truth-table test and three `pgregory.net/rapid` invariants under `-race`.**

## Performance

- **Duration:** ~22 min
- **Started:** 2026-06-30T23:29:00Z
- **Completed:** 2026-06-30T23:50:00Z
- **Tasks:** 2 (both TDD)
- **Files modified:** 2 created

## Accomplishments
- `internal/config/config_knobs.go`: `KnobKind` (KindString/Int/Bool/Enum) + `KnobSpec{Name,Kind,Default,Enum,Secret}` + `knobRegistry()` — one slice literal cataloguing 13 Tier A knobs (profile selector + security/reliability gate surface) and 30 Tier B int/bool reliability knobs, with the `AURA_PROFILE` KindEnum row and exactly four `Secret`-flagged knobs.
- `reparsePass(p RuntimeProfile) []Violation`: re-reads every cataloged knob with stdlib `strconv.Atoi`/`ParseBool` + `slices.Contains`, emitting one named `Violation` per bad knob — Fatal under `p.Strict()`, Warn otherwise; unset/whitespace skipped; KindString unchecked; never echoes a value.
- `config_knobs_test.go`: `TestKnobRegistry` (structural round-trip), `TestReparsePass` (13-row table over profile × knob × value), and three `rapid.Check` invariants — strictness, no-false-positive, aggregation-monotonicity — all green under `-race` (re-run at 2000 checks ×2, no flaky seeds).
- Prohibitions honored: `internal/envutil` byte-for-byte unmodified (D-06 leaf), no Tier C knob (`AURA_LOOP_`/`AURA_LLM_`/`AURA_SWARM_MAX_DEPTH`/`AURA_FS_`) catalogued, no per-profile runtime enforcement.

## Task Commits

Each task was committed atomically:

1. **Task 1: KnobSpec type + knobRegistry() + TestKnobRegistry** - `0f75ce48` (feat)
2. **Task 2: Generic kind-driven reparsePass + table test + 3 rapid invariants** - `72da6f0b` (feat)

**Plan metadata:** _(this docs commit)_

## Files Created/Modified
- `internal/config/config_knobs.go` (159 LOC) - KnobKind/KnobSpec types, knobRegistry() single source of truth, generic reparsePass.
- `internal/config/config_knobs_test.go` (279 LOC) - registry round-trip + reparse table + 3 rapid PBT invariants.

## Decisions Made
- **Exact four-secret registry:** Marked only the four object-store/RPC/Authula secrets `Secret: true` and kept `POSTGRES_PASSWORD`/`NEO4J_PASSWORD`/`AURA_DB_URL` OUT of the registry — they are required-secret checks owned by the existing `Validate()`, not the int/bool/enum reparse surface. This makes the `TestKnobRegistry` secret assertion an exact set-equality (stronger QUAL-04 guarantee).
- **Single garbage generator for all kinds:** `[g-su-zG-SU-Z]{2,8}` excludes digits and the bool-literal letters `t/f/T/F`, so a drawn value can never be a valid int, a valid bool token, or a runtime-profile enum name — one generator validates the strictness and aggregation invariants across int, bool, and enum knobs alike.
- **Per-iteration env isolation in rapid:** properties baseline with `clearPostgresEnv(t)` then `os.Setenv`/`defer os.Unsetenv` per draw, and assert only about the knob under test via `findViolation`, so neither host env nor a second injected knob can produce a false failure.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] golangci-lint bundled gofmt re-alignment of the table struct**
- **Found during:** Task 2 (post-edit lint gate)
- **Issue:** The anonymous table struct in `TestReparsePass` passed system `gofmt -l` but golangci-lint 2.12.2's bundled gofmt formatter flagged it as not properly formatted (field-alignment), which would fail the pre-push `lint` gate.
- **Fix:** Ran `golangci-lint fmt ./internal/config/` to canonicalize; `golangci-lint run` then exits 0 (0 issues). No logic change; tests re-run green under `-race`.
- **Files modified:** internal/config/config_knobs_test.go
- **Verification:** `golangci-lint run ./internal/config/` → 0 issues; full suite green under `-race`.
- **Committed in:** `72da6f0b` (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking — formatting only).
**Impact on plan:** Cosmetic; no scope creep. All behavior matches the plan exactly.

## TDD Gate Compliance

This is a `type: tdd` plan, but the host's lefthook **pre-commit** gate runs `go vet ./...`, which rejects any commit where the package does not compile. A standalone RED `test(...)` commit (test referencing an undefined `knobRegistry`/`reparsePass`) would therefore be **blocked by the vet hook**, so a separate RED commit could not be landed on this host.

RED was instead **observed pre-commit** and captured for each task before the implementation was written:
- Task 1 RED: `undefined: knobRegistry`, `undefined: KnobSpec`, `undefined: KindEnum/KindInt/KindBool` → `build failed`.
- Task 2 RED: `undefined: reparsePass` (×5) → `build failed`.

GREEN was then implemented and each task landed as one atomic compiling `feat(...)` commit (test + implementation together). The RED→GREEN discipline was honored; only the RED-as-separate-commit step was elided due to the `go vet` pre-commit constraint.

## Issues Encountered
- System `gofmt` and golangci-lint's bundled gofmt disagreed on struct-field alignment (see Deviation 1). Resolved via `golangci-lint fmt`. No behavioral impact.

## User Setup Required
None - no external service configuration required. The registry only catalogues existing knobs; no new env var is mandated by this plan (the new `AURA_OBJECTSTORE_REPLICATION_FACTOR` knob already shipped in 33-01).

## Next Phase Readiness
- `knobRegistry()` and `reparsePass()` are ready for plan 33-04, which composes them into `ValidateProfile()` alongside the bespoke profile gates (object-store sample creds, replication factor, CORS/destructive locks) and the profile-aware `Validate()`.
- `KnobSpec.Secret` is ready for the plan 33-05 `aura config validate` renderer to redact secret knob values.

## Self-Check: PASSED

- `internal/config/config_knobs.go` — FOUND
- `internal/config/config_knobs_test.go` — FOUND
- Commit `0f75ce48` — FOUND
- Commit `72da6f0b` — FOUND

---
*Phase: 33-runtime-profiles-config-validation*
*Completed: 2026-06-30*
