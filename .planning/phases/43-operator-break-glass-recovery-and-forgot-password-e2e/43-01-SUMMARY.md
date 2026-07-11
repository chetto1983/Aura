---
phase: 43-operator-break-glass-recovery-and-forgot-password-e2e
plan: 01
subsystem: auth
tags: [breakglass, operator-recovery, argon2, crypto-rand, password-sourcing, identity, go]

# Dependency graph
requires:
  - phase: 42-and-earlier
    provides: "internal/identity.Identity{ID,Name,Kind,Deactivated} projection (store.go:61) consumed by the guard"
provides:
  - "new internal/breakglass package (pure, DB-free, TTY-free) — a coverage-gate target under ./internal/... (D-09)"
  - "selectSoleOperator([]identity.Identity) (identity.Identity, error) — operator-resolution guard with the locked D-11 active/deactivated rule (R2)"
  - "Sourcer + Secrets — password/question/answer sourcing engine (env / --generate / injected hidden prompt) with the conflict/non-TTY/empty decision tree (R3, D-03)"
  - "generateSecret(nBytes) — crypto/rand + base64.RawURLEncoding strong-secret generator (>= 20 chars)"
  - "exported seam contract for Wave 2-3: Getenv/IsTTY/ReadHidden/Stdout/Stderr injection points + AURA_RECOVERY_{PASSWORD,QUESTION,ANSWER} env-name consts"
affects: [43-02, 43-03, break-glass-orchestration, recover-operator-cli, authula-setter, recovery-reseed]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Function-field seams (Getenv/IsTTY/ReadHidden/Stdout/Stderr) over premature interfaces — pure, injectable, zero-value-useful"
    - "Active/deactivated partition guard (D-11): count ACTIVE operators for the >1 ambiguity refusal; fall back to the full user set for the lone-deactivated recoverable case"
    - "Single-emission secret discipline: the ONLY secret write is one --generate Stdout line; never Stderr, never an error string"

key-files:
  created:
    - "internal/breakglass/guard.go — selectSoleOperator (D-11 rule)"
    - "internal/breakglass/guard_test.go — cardinality x deactivated table test"
    - "internal/breakglass/source.go — Secrets, Sourcer, Source, generateSecret, env-name consts"
    - "internal/breakglass/source_test.go — env / generate / conflict / non-TTY / empty / prompt / no-leak table test"
  modified: []

key-decisions:
  - "Locked the D-11 rule exactly: exactly one ACTIVE operator wins (even alongside deactivated stragglers); a lone deactivated operator is returned (recoverable lockout); >1 active OR >1 deactivated-with-none-active is refused; non-'user' kinds never counted"
  - "generateSecret uses 24 bytes -> 32-char base64url (>= the 20-char R3 floor), mirroring cmd/aura/serve_password_reset.go newPasswordResetToken"
  - "Source resolves the password first, then (unless --no-recovery) the Q&A symmetrically; every conflict/non-TTY/empty case errors BEFORE returning Secrets (the functions take NO pool, so no DB call is reachable on an invalid path)"
  - "R2/R3/R6 remain unchecked — this plan ships only the Wave-0 pure primitives; the requirements become user-observable only after the orchestration + CLI + db_integration plans (Waves 2-3). requirements mark-complete intentionally NOT run (37E precedent)"

patterns-established:
  - "Pattern 1: injectable function-field seams make a high-branch-density decision tree fully unit-testable with no real TTY/DB"
  - "Pattern 2: white-box table test asserts a fixed secret sentinel is absent from BOTH the captured Stderr buffer and every returned error string"

requirements-completed: []  # R2/R3/R6 are phase-spanning; the terminal plan owns the mark (see key-decisions)

coverage:
  - id: D1
    description: "selectSoleOperator resolves exactly one operator under the locked D-11 active/deactivated rule and refuses 0/ambiguous state (R2)"
    requirement: "R2"
    verification:
      - kind: unit
        ref: "internal/breakglass/guard_test.go#TestSelectSoleOperator"
        status: pass
    human_judgment: false
  - id: D2
    description: "Sourcer.Source resolves password + recovery Q&A across env/--generate/hidden-prompt, rejects conflict/non-TTY/empty before any Secrets, emits a generated secret exactly once to Stdout, never leaks to Stderr or an error (R3, D-03)"
    requirement: "R3"
    verification:
      - kind: unit
        ref: "internal/breakglass/source_test.go#TestSource"
        status: pass
    human_judgment: false
  - id: D3
    description: "generateSecret produces a >= 20-char [A-Za-z0-9_-] value from crypto/rand (strong generated password, T-43-08)"
    requirement: "R3"
    verification:
      - kind: unit
        ref: "internal/breakglass/source_test.go#TestGenerateSecret"
        status: pass
    human_judgment: false

# Metrics
duration: 23min
completed: 2026-07-11
status: complete
---

# Phase 43 Plan 01: breakglass guard + sourcing primitives Summary

**New pure `internal/breakglass` package: the D-11 operator-resolution guard (`selectSoleOperator`) and the env/`--generate`/hidden-prompt password + recovery-Q&A sourcing engine (`Sourcer.Source`) with a crypto/rand generator — DB-free, TTY-free, 96.6% covered.**

## Performance

- **Duration:** ~23 min
- **Started:** 2026-07-11T10:36:00Z
- **Completed:** 2026-07-11T10:59:00Z
- **Tasks:** 2
- **Files modified:** 4 (all created)

## Accomplishments
- `selectSoleOperator` implements the LOCKED D-11 active/deactivated rule (R2/T-43-07): one active operator wins even alongside deactivated stragglers; a lone deactivated operator is recoverable; every 0/ambiguous state is refused so the caller performs zero writes.
- `Sourcer.Source` implements the full R3/D-03 decision tree behind five injectable seams: env vs `--generate` vs hidden-prompt-with-confirm, rejecting the env+generate conflict, the non-TTY-with-no-source case, and any empty/whitespace value before returning any `Secrets`.
- `generateSecret` mirrors the online `newPasswordResetToken` generator (crypto/rand + `base64.RawURLEncoding`, 24 bytes -> 32 chars), asserted `>= 20` chars over `[A-Za-z0-9_-]` (T-43-08).
- Single-emission secret discipline proven: the ONE `--generate` Stdout line is the only secret write; a fixed sentinel is asserted absent from both the captured Stderr buffer and every returned error string (T-43-01).
- Package is now a coverage-gate target under `./internal/...` (D-09) at **96.6%** owned-surface (only the unreachable `crypto/rand.Read` failure guards remain uncovered).

## Task Commits

Each task was committed atomically:

1. **Task 1: guard.go — selectSoleOperator with the D-11 active/deactivated rule (R2)** - `40c9cb5d` (feat)
2. **Task 2: source.go — password + recovery Q&A sourcing (env / --generate / hidden prompt) (R3, D-03)** - `19730963` (feat)

**Plan metadata:** captured in the docs commit that carries this SUMMARY + STATE.md/ROADMAP.md.

_Note: both tasks are `tdd="true"` — see ## TDD Gate Compliance for the RED->GREEN handling._

## Files Created/Modified
- `internal/breakglass/guard.go` (59 LOC) - `selectSoleOperator` pure operator-resolution guard.
- `internal/breakglass/guard_test.go` (93 LOC) - table test over every cardinality x deactivated combination + a non-'user' kind row.
- `internal/breakglass/source.go` (211 LOC) - `Secrets`, `Sourcer` (5 seams), `Source`, `sourcePassword`/`sourceRecovery`/`promptHiddenConfirmed`/`emitGenerated`/`canPrompt`/`getenv`, `generateSecret`, `AURA_RECOVERY_*` consts + default question.
- `internal/breakglass/source_test.go` (~360 LOC) - table test: env / generate(length+charset+single-emission) / conflict / non-TTY / empty-whitespace / hidden-prompt / read-error / no-leak + `TestGenerateSecret`.

## Decisions Made
- **D-11 rule locked** as specified in the plan's `<behavior>` (active-count for the >1 guard; lone-deactivated fall-through). Error strings lowercase, no trailing punctuation, no secret (golang-error-handling).
- **Question default fallback:** a non-generate path with `AURA_RECOVERY_ANSWER` set but no `AURA_RECOVERY_QUESTION` pairs the answer with the fixed `defaultRecoveryQuestion` (never an empty question the upsert would reject downstream).
- **Value receiver on `Sourcer`** (small, immutable seams) per golang-structs-interfaces.

## Deviations from Plan

### Auto-added (Rule 2 — defensive / panic-avoidance)

**1. [Rule 2 - Missing Critical] Nil-seam guards on the injectable `Sourcer`**
- **Found during:** Task 2 (source.go)
- **Issue:** A partially-wired or zero-value `Sourcer` would panic — `fmt.Fprintf(nil, ...)` on a nil `Stdout` in the `--generate` path, and calling a nil `ReadHidden`/`Getenv` func value.
- **Fix:** `emitGenerated` returns a clear error when `Stdout == nil`; `canPrompt()` treats a nil `IsTTY` or nil `ReadHidden` as "cannot prompt" (clean error, not a panic); a `getenv` helper tolerates a nil `Getenv` (zero value useful, per golang-structs-interfaces).
- **Files modified:** internal/breakglass/source.go
- **Verification:** covered by `TestSource/"generate with no stdout errors"`, `.../"non-tty ..."`, and `TestSourceNilGetenv`.
- **Committed in:** `19730963` (Task 2 commit)

**2. [Rule 2 - Test thoroughness] Coverage rows beyond the literal `<behavior>` matrix**
- **Found during:** Task 2 (source.go is the plan's mutation spot-check target; success criteria require the full branch matrix + >= 85%)
- **Issue:** The `<behavior>` rows alone left the `ReadHidden` error paths, the empty-typed-prompt branch, and the nil-`Getenv` zero-value path unexercised.
- **Fix:** Added `hidden read fails on the first/confirmation prompt`, `tty empty password at the prompt is rejected`, and `TestSourceNilGetenv` — raising package coverage 92.1% -> 96.6% and locking `promptHiddenConfirmed` at 100%.
- **Files modified:** internal/breakglass/source_test.go
- **Verification:** `go test ./internal/breakglass/ -cover` -> 96.6%; `go tool cover -func` shows only the unreachable `crypto/rand.Read` guards uncovered.
- **Committed in:** `19730963` (Task 2 commit)

---

**Total deviations:** 2 auto-added (both Rule 2 — defensive robustness + specified coverage completion)
**Impact on plan:** No behavior change vs the specified decision trees; both additions serve the plan's own success criteria (no panics on the injected seam; full branch matrix + >= 85% floor). No scope creep — no source behavior outside the `<behavior>` spec.

## TDD Gate Compliance
Both tasks are `tdd="true"`. RED->GREEN was observed in-tree for each:
- Task 1 RED: `go test ./internal/breakglass/` -> `undefined: selectSoleOperator` (build failed); GREEN after `guard.go`.
- Task 2 RED: `go test ...` -> `undefined: Secrets`/`Sourcer`/`EnvRecoveryPassword ...` (build failed); GREEN after `source.go`.

Each task was committed as a single atomic `feat` commit rather than a separate `test(...)` RED commit + `feat(...)` GREEN commit: the lefthook pre-commit `go vet` gate rejects a non-compiling RED-only commit (a test referencing an undefined symbol), and `--no-verify` is prohibited. This matches the established repo precedent (37E-02/04/05). The `test(...)`-then-`feat(...)` split is not achievable here without bypassing the hooks.

## Issues Encountered
- `go test -race` requires cgo, which is disabled on the Windows host; `-race` was run green in WSL (CGO_ENABLED=1) for both the guard and full package, per the repo's WSL-primary discipline.
- During state-tooling discovery, `state advance-plan` and `state record-session` executed as a side effect of probing (they are not dry-run) — this left `current_plan` at 2 (the intended post-completion position) and refreshed the session timestamp; both are the desired end state, so `advance-plan` was not re-run.

## User Setup Required
None - no external service configuration required (this plan is pure Go, no env/deps/migrations added).

## Next Phase Readiness
- Wave 2-3 consume these exact symbols: `selectSoleOperator` (orchestrator guard), `Sourcer`/`Secrets`/`Source` (CLI glue injects the real `x/term` `IsTTY`/`ReadHidden` + `os.Getenv` + `os.Stdout`/`os.Stderr`), `generateSecret`, and the `AURA_RECOVERY_*` env-name consts.
- `internal/breakglass` will be picked up by `scripts/coverage_gate.sh ./internal/...`; the `db_integration` setter/orchestration test (R6) lands in a later plan and must run-not-skip in CI.
- No blockers.

## Self-Check: PASSED
- Files: FOUND internal/breakglass/{guard,guard_test,source,source_test}.go
- Commits: FOUND 40c9cb5d, FOUND 19730963

---
*Phase: 43-operator-break-glass-recovery-and-forgot-password-e2e*
*Completed: 2026-07-11*
