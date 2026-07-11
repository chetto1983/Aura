---
phase: 43-operator-break-glass-recovery-and-forgot-password-e2e
plan: 02
subsystem: auth
tags: [breakglass, operator-recovery, argon2, authula, session-invalidation, recovery-reseed, db-integration, throwaway-db, go]

# Dependency graph
requires:
  - phase: 43-01
    provides: "internal/breakglass selectSoleOperator (D-11 guard) + Secrets (password/Q&A sourcing output) — the symbols this orchestrator consumes"
provides:
  - "setOperatorPassword — offline Authula reset (resolve authula_user_id -> core.PasswordService.Hash -> SessionService.DeleteAllByUserID -> AccountService.Update), NO same-password guard (D-01/D-02)"
  - "NewAuthula(dsn, secret) -> *webauth.Provider — the offline-provider helper (CoreServices + Close) mirroring authula_seed_e2e.go"
  - "Deps + RecoverOperator(ctx, Deps, Secrets) (identityID, error) — the guard -> setter -> re-seed -> neutral-audit orchestrator (R1/R2/R4, D-04/D-06)"
  - "breakglass_integration_test.go — //go:build db_integration self-provisioning throwaway-DB harness (D-07/D-08) that RUNS-not-skips and counts toward the 85% floor (R6/D-09)"
  - "scripts/coverage_docker.sh AURA_AUTHULA_SECRET export — closes the DC-1 local gap"
affects: [43-03, recover-operator-cli, identity-dispatch]

# Tech tracking
tech-stack:
  added: []   # zero net-new packages — every primitive already shipped (Authula, agui, sqlc, db, webauth)
  patterns:
    - "Offline Authula service construction: webauth.New(Config{DSN,Secret,TrustedOrigins}) -> CoreServices() with a mandatory defer Close() (goleak-clean expiry workers)"
    - "Argon2 envelope discipline: the PASSWORD goes through Authula's OWN core.PasswordService.Hash (Verify-compatible); the ANSWER separately through agui.RecoveryHasher (never cross the envelopes)"
    - "Delete-then-update ordering: SessionService.DeleteAllByUserID BEFORE AccountService.Update, so a mid-op failure never leaves a live session on a changed password"
    - "Self-provisioning throwaway-DB harness: superuser -> `postgres` maintenance DB CREATE/DROP OWNER aura_migrate, a name of `aura` REFUSED (D-08), dropped WITH (FORCE) on t.Cleanup"

key-files:
  created:
    - "internal/breakglass/setter.go — setOperatorPassword + NewAuthula (offline reset, no same-password guard)"
    - "internal/breakglass/breakglass.go — Deps, RecoverOperator, auditEventOperatorRecovered const"
    - "internal/breakglass/breakglass_integration_test.go — throwaway-DB harness + TestRecoverOperatorHappyPath (R1/R4/D-06/idempotency/no-leak)"
    - "internal/breakglass/breakglass_integration_edge_test.go — R2 guard no-write + missing-account + --no-recovery (split for the 600-LOC cap)"
  modified:
    - "scripts/coverage_docker.sh — export AURA_AUTHULA_SECRET (read_secret + 64-hex dummy fallback), DC-1 local gap; NO ci.yml edit"

key-decisions:
  - "Password hashed ONLY via Authula core.PasswordService.Hash (the base64.RawStdEncoding(salt‖hash) envelope Verify accepts) — never agui.hashArgon2id; the recovery ANSWER separately via agui.RecoveryHasher.HashAnswer (D-04)"
  - "D-02: no ErrPasswordResetSamePassword comparison — break-glass allows re-setting the same password (omits serve_password_reset.go:442-444)"
  - "Re-seed ordered BEFORE the neutral audit so a failed re-seed never leaves a 'recovered' audit row; the audit carries only IdentityID+Event -> metadata '{}' (D-06)"
  - "Throwaway DB self-provisioned per subtest (isolation); the D-08 live-`aura` refusal is TEST-ONLY (the command must run against live `aura`, no refusal there)"
  - "DC-1: the real gap was LOCAL — coverage_docker.sh now exports AURA_AUTHULA_SECRET; .github/workflows/ci.yml is UNTOUCHED (the secret is already workflow-level at ci.yml:18)"
  - "R1/R2/R4/R6 remain unchecked — the flow is proven at the package level here, but the requirements become user-observable only after the CLI glue (43-03, terminal); requirements mark-complete intentionally NOT run (43-01 / 37E precedent)"

patterns-established:
  - "Pattern 1: an offline break-glass reset composes webauth.New->CoreServices (Authula) with the aura pgxpool (identity/auth-link/re-seed/audit) in one flow, minus the online same-password guard"
  - "Pattern 2: a db_integration test self-provisions + drops a disposable DB (never live `aura`) and lives under internal/ so scripts/coverage_gate.sh actually RUNS it (D-09), unlike the never-run cmd/aura tier"

requirements-completed: []   # R1/R2/R4/R6 are phase-spanning; the terminal CLI plan (43-03) owns the mark (see key-decisions)

coverage:
  - id: D4
    description: "R1 argon2 round-trip: after RecoverOperator, Authula's OWN PasswordService.Verify accepts the reset password and rejects the old one (never agui.hashArgon2id)"
    requirement: "R1"
    verification:
      - kind: db_integration
        ref: "internal/breakglass/breakglass_integration_test.go#TestRecoverOperatorHappyPath"
        status: pass
    human_judgment: false
  - id: D5
    description: "R1 sessions killed: authula.sessions==0 for the user post-run (a session seeded before the run is gone); delete-then-update ordering"
    requirement: "R1"
    verification:
      - kind: db_integration
        ref: "internal/breakglass/breakglass_integration_test.go#TestRecoverOperatorHappyPath"
        status: pass
    human_judgment: false
  - id: D6
    description: "R4 re-seed + idempotency: GetIdentityRecoveryByIdentity non-empty hash+version, LookupRecoveryByEmail returns a row (was pgx.ErrNoRows), a second run leaves exactly one identity_recovery row"
    requirement: "R4"
    verification:
      - kind: db_integration
        ref: "internal/breakglass/breakglass_integration_test.go#TestRecoverOperatorHappyPath"
        status: pass
    human_judgment: false
  - id: D7
    description: "D-06 neutral audit: exactly one aura.identity_recovery_audit row, event=operator_password_recovered, metadata '{}' (no secret)"
    requirement: "R1"
    verification:
      - kind: db_integration
        ref: "internal/breakglass/breakglass_integration_test.go#TestRecoverOperatorHappyPath"
        status: pass
    human_judgment: false
  - id: D8
    description: "prohibition no-leak: a captured slog buffer across the full run contains neither the sentinel password nor the answer"
    requirement: "R1"
    verification:
      - kind: db_integration
        ref: "internal/breakglass/breakglass_integration_test.go#TestRecoverOperatorHappyPath"
        status: pass
    human_judgment: false
  - id: D9
    description: "R2 guard no-write: 0 and >1 active kind='user' operators each error with zero writes to authula.accounts / aura.identity_recovery / audit"
    requirement: "R2"
    verification:
      - kind: db_integration
        ref: "internal/breakglass/breakglass_integration_edge_test.go#TestRecoverOperatorGuardNoWrite"
        status: pass
    human_judgment: false
  - id: D10
    description: "missing-account edge: an operator with an auth link but no authula.accounts row -> clear error, NO partial write (no reseed, no audit)"
    requirement: "R1"
    verification:
      - kind: db_integration
        ref: "internal/breakglass/breakglass_integration_edge_test.go#TestRecoverOperatorMissingAccount"
        status: pass
    human_judgment: false
  - id: D11
    description: "--no-recovery (D-04): password reset + sessions==0 + audit row, but NO identity_recovery row written"
    requirement: "R4"
    verification:
      - kind: db_integration
        ref: "internal/breakglass/breakglass_integration_edge_test.go#TestRecoverOperatorNoRecovery"
        status: pass
    human_judgment: false
  - id: D12
    description: "R6 runs-not-skips + D-07/D-08: the tier self-provisions an aura_bg_* throwaway DB (name of `aura` refused), drops it on cleanup, and t.Fatals under $CI when its env is unset"
    requirement: "R6"
    verification:
      - kind: db_integration
        ref: "internal/breakglass/breakglass_integration_test.go#provisionThrowawayDB"
        status: pass
    human_judgment: false

# Metrics
duration: 48min
completed: 2026-07-11
status: complete
---

# Phase 43 Plan 02: offline break-glass setter + RecoverOperator orchestrator + throwaway-DB proof Summary

**The DB-touching heart of break-glass in `internal/breakglass`: an offline Authula reset (`setOperatorPassword` — hash via Authula's own argon2, kill sessions, no same-password guard), the `RecoverOperator` orchestrator (guard -> setter -> idempotent re-seed -> neutral audit), and a `//go:build db_integration` harness that self-provisions a disposable DB (refuses live `aura`) and RUNS-not-skips to prove R1/R2/R4/R6/D-06 — verified green under `-race` on the live stack.**

## Performance

- **Duration:** ~48 min
- **Completed:** 2026-07-11T11:53Z
- **Tasks:** 3 (2 source commits + 1 test commit)
- **Files:** 5 (4 created, 1 modified)

## Accomplishments
- `setOperatorPassword` performs the offline reset through Authula's own `CoreServices`: resolve `authula_user_id` via `aura.identity_auth_links`, hash with `core.PasswordService.Hash` (the ONLY encoding `Argon2PasswordService.Verify` accepts), `SessionService.DeleteAllByUserID` FIRST, then `AccountService.Update` — with NO `ErrPasswordResetSamePassword` guard (D-02) and a clean, secret-free failure on a missing link/account (no partial write).
- `RecoverOperator` composes the whole flow: `ListIdentities` -> `selectSoleOperator` (refuses 0/ambiguous BEFORE any write) -> `setOperatorPassword` -> (unless `NoRecovery`) `agui.RecoveryHasher.HashAnswer` + idempotent `sqlc.UpsertIdentityRecovery` -> neutral `InsertIdentityRecoveryAudit(operator_password_recovered)`. Re-seed is ordered before the audit so a failed re-seed leaves no "recovered" row.
- The `db_integration` harness self-provisions a disposable database as superuser on the `postgres` maintenance DB, **refuses a name of `aura`** (D-08 — the exact 37D-05 footgun), migrates + seeds the lockout, runs the flow, and DROPs the DB WITH (FORCE) on cleanup (D-07). It lives under `internal/breakglass` so `scripts/coverage_gate.sh ./internal/...` actually EXECUTES it (D-09), unlike the never-run `cmd/aura` tier.
- `scripts/coverage_docker.sh` now exports `AURA_AUTHULA_SECRET` (via `read_secret`, 64-hex dummy fallback) so the new tier does not `t.Fatal` locally under `CI=true` (DC-1). `.github/workflows/ci.yml` is untouched — the secret is already workflow-level at `ci.yml:18`.

## HONEST db_integration run status

**RAN and PASSED** — WSL, `CGO_ENABLED=1`, `-race`, live Docker stack (`aura-postgres` 127.0.0.1:5432), `go test -tags db_integration -race -p=1 -count=1 -v -run TestRecoverOperator ./internal/breakglass/`:

```
--- PASS: TestRecoverOperatorGuardNoWrite (2.08s)
    --- PASS: TestRecoverOperatorGuardNoWrite/zero_operators (1.05s)      DB "aura_bg_bc14fbb8b0db"
    --- PASS: TestRecoverOperatorGuardNoWrite/two_active_operators (1.03s) DB "aura_bg_08257a9436d4"
--- PASS: TestRecoverOperatorMissingAccount (1.05s)                        DB "aura_bg_b6fe790227e5"
--- PASS: TestRecoverOperatorNoRecovery (1.26s)                           DB "aura_bg_423330253c2f"
--- PASS: TestRecoverOperatorHappyPath (2.10s)                            DB "aura_bg_21987d9f25f4"
PASS
ok  github.com/chetto1983/aura/internal/breakglass  7.552s
```

- Every run logged `provisioned disposable break-glass DB "aura_bg_XXXX" (dropped on cleanup)` and real Authula bun-migration output (core/totp `migration applied`, plugins initialized) — NOT a sub-second skip. The 5 throwaway DB names are all `aura_bg_*`, **never `aura`**.
- Post-run verification: **zero** leftover `aura_bg_*` databases (`SELECT datname ... LIKE 'aura_bg_%'` empty — cleanup worked); the live `aura` DB is intact (1 `kind='user'` operator identity, untouched).
- Unit tier stays green under `-race`: `TestSelectSoleOperator` + `TestSource` PASS (from the full-package `-race` run, `ok ... 8.306s`).

## Task Commits

1. **Tasks 1+2: setter.go + breakglass.go (offline setter + RecoverOperator orchestrator)** - `d6042c17` (feat)
2. **Task 3: db_integration throwaway-DB harness + coverage_docker.sh secret** - `270ecd9c` (test)

**Plan metadata:** captured in the docs commit that carries this SUMMARY + STATE.md/ROADMAP.md.

## Files Created/Modified
- `internal/breakglass/setter.go` (92 LOC) - `NewAuthula` + `setOperatorPassword` (offline reset, no same-password guard).
- `internal/breakglass/breakglass.go` (94 LOC) - `Deps`, `RecoverOperator`, `auditEventOperatorRecovered`.
- `internal/breakglass/breakglass_integration_test.go` (409 LOC) - env guard, `provisionThrowawayDB` (D-07/D-08), `setupBreakglassDB`, `seedOperatorLockout`, `TestRecoverOperatorHappyPath` (R1/R4/D-06/idempotency/no-leak) + assertion helpers.
- `internal/breakglass/breakglass_integration_edge_test.go` (129 LOC) - `TestRecoverOperatorGuardNoWrite` (R2 0/>1), `TestRecoverOperatorMissingAccount`, `TestRecoverOperatorNoRecovery`.
- `scripts/coverage_docker.sh` (+15 lines) - `AURA_AUTHULA_SECRET` export (DC-1).

## Decisions Made
- **Argon2 envelope split enforced:** password -> `core.PasswordService.Hash`; answer -> `agui.RecoveryHasher.HashAnswer`. The happy-path test proves the reset password passes Authula's OWN `Verify` and the old one does not (Pitfall 3 closed).
- **Nil sub-service guard** on `setOperatorPassword` (mirrors serve_password_reset.go:423-425) returns a generic "backend unavailable" — no secret, no panic.
- **Per-subtest throwaway DB** (not a shared reset) for clean R2/missing-account/no-recovery isolation; each provisions + drops its own `aura_bg_*` DB.
- **`pgx.ErrNoRows` via `errors.Is`** for the pre-run lockout assertion (not string matching).

## Deviations from Plan

### Rule 3 — blocking (lint gate) — Tasks 1 and 2 committed together

**1. [Rule 3 - Blocking] setter.go + breakglass.go landed in ONE atomic `feat` commit**
- **Found during:** Task 1 commit attempt.
- **Issue:** The lefthook pre-commit `golangci-lint` `unused` check rejects a setter-only commit — package-private `setOperatorPassword` has no caller until the orchestrator (`breakglass.go`, Task 2) exists — and `--no-verify` is prohibited (CLAUDE.md).
- **Fix:** Wrote `breakglass.go` (Task 2) so `setOperatorPassword` has its caller, then committed both files as one atomic `feat(43-02)`. The two are inherently coupled (the setter is the orchestrator's private helper).
- **Files:** internal/breakglass/setter.go, internal/breakglass/breakglass.go
- **Committed in:** `d6042c17`
- **Precedent:** identical to the documented 37E-02/04/05 and 43-01 handling.

### Rule 3 — blocking (600-LOC cap) — edge subtests split to a second file

**2. [Rule 3 - Blocking] db_integration subtests split into breakglass_integration_edge_test.go**
- **Found during:** Task 3.
- **Issue:** The plan's own LOC contingency: the harness + `provisionThrowawayDB`/seed helpers + the happy path already put the main file at 409 LOC; adding the R2/missing-account/--no-recovery subtests would breach the 600-LOC cap.
- **Fix:** Moved the three edge tests into `breakglass_integration_edge_test.go` (same `//go:build db_integration`, same package) exactly as the plan's `<action>` contingency sanctions. Main 409 / edge 129 LOC.
- **Files:** internal/breakglass/breakglass_integration_edge_test.go
- **Committed in:** `270ecd9c`

### Judgment — goleak omitted

**3. [Judgment] No goleak TestMain**
- **Reason:** The plan said "consider goleak". Authula's expiry workers are stopped via `provider.Close()` (registered on `t.Cleanup`), but pgxpool's background health-check goroutines and Authula/bun internals can trip goleak with third-party false positives. To keep the tier reliably green under `-race`, correctness is enforced by the explicit `Close`/`pool.Close`/DROP cleanups rather than a goleak gate. `-race` is clean (no DATA RACE, no leak-driven hang).

---

**Total deviations:** 2 Rule-3 blocking (both forced by CLAUDE.md gates: the `unused` lint + the 600-LOC cap) + 1 judgment (goleak). No behavior deviated from the plan's `<action>`/`<behavior>`; no scope creep.

## TDD Gate Compliance
All three tasks are `tdd="true"`. The plan's own design makes Tasks 1-2 (the setter + orchestrator) DB+Authula-bound and explicitly proven by the Task-3 `db_integration` test rather than by isolated unit tests (`<action>`: "NO unit test here … proven by the Task 3 db_integration test"). RED->GREEN was observed as a compile/integration gate:
- Tasks 1+2 RED: with the orchestrator absent the package's intended callers do not exist; GREEN after setter.go+breakglass.go compile and `go test ./internal/breakglass/` (unit) stays green.
- Task 3 is the behavioural RED->GREEN: the `db_integration` harness is the test that exercises Tasks 1-2 end-to-end; it PASSED on first green execution against the live stack.

A separate `test(...)` RED commit preceding the `feat(...)` is not achievable here without `--no-verify` (the `unused` gate rejects a symbol-less RED) — the same repo-wide accommodation as 43-01 / 37E. The Task-3 commit is correctly typed `test(...)`.

## Issues Encountered
- `go test -race` requires cgo (disabled on the Windows host: `CGO_ENABLED=0`, no gcc). The whole `-race` + `db_integration` run was executed in WSL (`gcc` 15.2.0, go1.26.5, `CGO_ENABLED=1`), which reaches the Windows Docker stack via 127.0.0.1 — the repo's WSL-primary discipline.
- `.env` on the Windows host is CRLF; the WSL invocation strips `\r` from the read secrets before composing the DSNs.
- The plan/phase-note `-p1` flag form is misparsed by `go test` (it falls back to package `.` -> "no Go files"); the correct form is `-p=1` (or `-p 1`). Used `-p=1`.

## User Setup Required
None - zero net-new packages, no migration, no new persistent env var. `AURA_AUTHULA_SECRET` is already catalogued and set (ci.yml:18 workflow-level; `.env` locally).

## Next Phase Readiness
- 43-03 (terminal, Wave 3) consumes: `NewAuthula`, `Deps`, `RecoverOperator`, and `Secrets`/`selectSoleOperator` (43-01) — the CLI glue (`cmd/aura/recover_operator.go` + `identity.go` dispatch, D-05) wires the real `x/term` prompt + `config.LoadDB` pool + the offline provider, then calls `RecoverOperator` and owns the `requirements mark-complete` for R1/R2/R4/R6.
- `internal/breakglass` is now a full coverage-gate target: the `db_integration` tier runs inside `scripts/coverage_gate.sh` and `scripts/coverage_docker.sh` (secret exported).
- No blockers.

## Self-Check: PASSED
- Files: FOUND internal/breakglass/{setter,breakglass,breakglass_integration_test,breakglass_integration_edge_test}.go; FOUND scripts/coverage_docker.sh (AURA_AUTHULA_SECRET export)
- Commits: FOUND d6042c17 (feat), FOUND 270ecd9c (test)
- db_integration: RAN + PASSED under -race on the live stack (5 aura_bg_* throwaway DBs provisioned + dropped; live `aura` untouched)

---
*Phase: 43-operator-break-glass-recovery-and-forgot-password-e2e*
*Completed: 2026-07-11*
