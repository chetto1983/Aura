---
phase: 43-operator-break-glass-recovery-and-forgot-password-e2e
verified: 2026-07-11T15:10:00Z
status: human_needed
score: 7/7 must-haves verified (31/31 granular plan-level truths/prohibitions verified)
overrides_applied: 0
human_verification:
  - test: "On the deploy host, with the live single-operator `aura` DB (currently 1 `kind='user'` identity, 0 `identity_recovery` rows — the real lockout), run `aura identity recover-operator` (choose a password source: hidden prompt, `AURA_RECOVERY_PASSWORD`, or `--generate`)."
    expected: "Exits 0, prints an `ok: recovered operator <id>; sessions invalidated; recovery re-seeded` line with no secret; the operator can log in with the new password; the forgot-password flow now sends a Telegram code again (LookupRecoveryByEmail no longer denies)."
    why_human: "This is the actual production credential mutation the whole feature exists to perform. It requires a TTY/deploy host, mutates real Authula auth state, and is explicitly out of scope for automated verification (SPEC boundary + CLAUDE.md anti-footgun discipline) — the composed logic is already proven end-to-end by the independently re-run db_integration throwaway-DB suite (see Behavioral Spot-Checks/Probe Execution below)."
  - test: "In WSL: `GOFLAGS=-tags=db_integration go-mutesting ./internal/breakglass/setter.go ./internal/breakglass/source.go`, record the killed/total ratio."
    expected: "≥70% mutants killed on both files (per CLAUDE.md Gate-3 DoD + 43-VALIDATION.md Manual-Only table)."
    why_human: "go-mutesting is a WSL-only long-running campaign explicitly marked Manual-Only in 43-VALIDATION.md; it is not installed and not runnable from this Windows verification environment (no `go-mutesting` on PATH, and invoking a long mutation campaign through WSL is outside this pass's safe/fast verification scope)."
---

# Phase 43: Operator Break-Glass Recovery + Forgot-Password E2E — Verification Report

**Phase Goal:** Add a host-only break-glass operator recovery path so a missing/wiped `aura.identity_recovery` row can never permanently lock the operator out of the cockpit: an `aura` CLI subcommand (host = admin proof) that resets the operator password (reusing Authula's argon2 `PasswordService.Hash`), invalidates existing sessions, and re-seeds a missing `identity_recovery` row — the credential sourced from a prompt/env the operator supplies, never logged. Plus end-to-end coverage of the forgot-password flow: happy path and the deny path, with backend unit/integration coverage of the new command and the deny branch.

**Verified:** 2026-07-11
**Status:** human_needed (all automatable must-haves VERIFIED; 2 legitimate Manual-Only items remain, per SPEC/CLAUDE.md design, not gaps)
**Re-verification:** No — initial verification

## Adversarial Method

This was **not** a SUMMARY-trust pass. Every plan-level `must_haves` truth/artifact/key_link/prohibition was checked against the real files (cited file:line below), and every automatable claim was **independently re-executed** in this session rather than accepted from the SUMMARYs:

- `go build ./...`, `go vet ./...` — re-run, clean.
- `go test ./...` — re-run: **65 packages ok, 0 fail** (matches the claimed figure exactly, recomputed independently via `grep -c "^ok"` / `grep -c FAIL` on fresh output, not copied from a SUMMARY).
- `go test ./internal/breakglass/...` (unit only) — re-run, all 4 top-level tests + 30 subtests PASS.
- `go test -tags db_integration -run TestRecoverOperator -v -p=1 ./internal/breakglass/...` — re-run **live against the actual running `aura-postgres` container** (the same stack the operator's real `aura` DB lives in). Result: **PASS, 13.7s**, 5 fresh `aura_bg_*` throwaway databases created, migrated, seeded, asserted, and dropped. Post-run query confirmed **zero** leftover `aura_bg_*` databases and the live `aura` DB **unchanged** (still exactly 1 `kind='user'` identity, still 0 `identity_recovery` rows — the real lockout is untouched, proving the harness never touched production data).
- No-skip-as-green independently falsified: ran the same test with `CI=true` and the required env vars unset — result **FAIL** (`t.Fatal`), not skip. Ran again with `CI` unset — result **SKIP**. Both branches of the guard behave exactly as claimed.
- Combined (unit + db_integration) coverage for `internal/breakglass` independently measured: **87.0%** (≥ the 85% floor).
- `cd web && AURA_E2E_ORIGIN=http://127.0.0.1:9080 npx playwright test password-reset.spec.ts --project=chromium --project=mobile-chrome` — re-run against the **live running `aura` container** — **4 passed (5.3s)**, matching the SUMMARY's "4 passed (5.8s)" claim.
- `go run ./cmd/aura identity` (no args) smoke-tested — usage line lists `recover-operator [--generate] [--no-recovery]` with the D-05 disambiguation, exits 1, no DB connection attempted (instant return).
- Cross-referenced every string/selector the E2E spec asserts (`'Operator email'`, `'Send Telegram code'`, `'Continue'`, `'Verify'`, `'Password reset'`, `'Password updated'`, `login.reset.errors.generic`, `noticeCodeSent`) against the actual shipped `web/src/i18n/resources.login.ts` and `PasswordResetPanel.tsx`/`LoginPage.tsx` component logic — byte-for-byte matches, so the E2E genuinely exercises the real component, not a fictional mock.
- `git log`/`git diff` used to confirm: none of the 15 phase-43 commits touch `.github/workflows/ci.yml`, `cmd/aura/recovery.go`, or `internal/db/migrations/` (all explicitly out-of-scope per SPEC boundaries).

## Goal Achievement

### Observable Truths (SPEC R1–R6 + DC-1 rollup)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | **R1** — password hashed via Authula's own `core.PasswordService.Hash` (never `agui.hashArgon2id`); sessions deleted BEFORE `AccountService.Update`; NO `ErrPasswordResetSamePassword` guard (D-02) | ✓ VERIFIED | `internal/breakglass/setter.go:78` (`core.PasswordService.Hash(password)`); `setter.go:85` (`SessionService.DeleteAllByUserID`) precedes `setter.go:88` (`AccountService.Update`) textually and at runtime; zero references to `agui`/`ErrPasswordResetSamePassword` anywhere in `setter.go` (grep-confirmed absent). Live `db_integration` re-run: `verifyStoredPassword` proves the new password `Verify`-accepts and the old one no longer does; sessions==0 post-run; a **second** `RecoverOperator` call re-uses the identical `bgNewPassword` (i.e. a same-password reset) and **succeeds** — this doubles as direct proof of the D-02 same-password-allowed edge (SPEC Edge Coverage row 8). |
| 2 | **R2** — `selectSoleOperator` resolves `kind='user'`, refuses 0/>1 with zero writes, handles `Deactivated` per D-11 (with a test row) | ✓ VERIFIED | `internal/breakglass/guard.go:30-58` implements the exact D-11 partition (active/deactivated); `guard_test.go` has 8 named subtests including `Deactivated: true` rows (lines 44-58) and a non-`user`-kind row (60-68) — re-run: **all pass**. Live `db_integration` `TestRecoverOperatorGuardNoWrite` (0 operators / 2 active operators) re-run: **PASS**, and `authula.accounts`/`aura.identity_recovery`/`aura.identity_recovery_audit` counts independently confirmed == 0 after each refusal. |
| 3 | **R3** — `Sourcer` implements prompt/env/`--generate`; source-conflict + non-TTY + empty/whitespace error before any write; `--generate` ≥20-char once-to-stdout; no plaintext in logs/errors | ✓ VERIFIED | `internal/breakglass/source.go` (211 LOC, pure, zero DB/pool parameter — structurally cannot reach a DB call). `source_test.go` — 17 named subtests + `TestSourceNilGetenv` + `TestGenerateSecret`, re-run: **all pass**, including the explicit no-leak loop (lines 275-283) asserting the sentinel is absent from both stderr and every returned error across every case. `cmd/aura/recover_operator.go:55-61` wires the REAL seams (`os.Getenv`, `term.IsTerminal`, `readHiddenFromStdin`, `os.Stdout`, `os.Stderr`) — confirmed via source read and a clean `go build ./cmd/aura/`. |
| 4 | **R4** — `RecoverOperator` re-seeds via `agui.RecoveryHasher.HashAnswer` + `sqlc.UpsertIdentityRecovery` (idempotent); `--no-recovery` skips + the audit still runs; neutral `operator_password_recovered` audit (D-06) | ✓ VERIFIED | `internal/breakglass/breakglass.go:71-84` (re-seed inside `if !deps.NoRecovery`) is textually BEFORE the unconditional audit insert at `breakglass.go:86-91` (outside the `if` — so `--no-recovery` skips only the re-seed, never the audit). `UpsertIdentityRecovery` is `ON CONFLICT (identity_id) DO UPDATE` (`identity_recovery.sql.go:343-351`) — genuinely idempotent. Live `db_integration` re-run: `GetIdentityRecoveryByIdentity` non-empty hash+version, `LookupRecoveryByEmail` returns a row (was `pgx.ErrNoRows` pre-run), a second `RecoverOperator` call leaves `identity_recovery` count==1, `TestRecoverOperatorNoRecovery` confirms password+session-kill+audit run but zero `identity_recovery` rows. |
| 5 | **R5** — `web/e2e/password-reset.spec.ts` has happy + deny, fully `page.route`-mocked (D-10); deny asserts the generic `login.reset.errors.generic` and no factor name / no typed password in the DOM | ✓ VERIFIED | `web/e2e/password-reset.spec.ts` (176 LOC) — **independently re-run live** against the running `aura` container: `4 passed (5.3s)` on chromium + mobile-chrome. Deny path asserts the SAME `NEUTRAL_NOTICE` constant on both paths (byte-identical, anti-enumeration) and `GENERIC_ERROR` exactly; DOM checked for absence of `identity_recovery`/`telegram_accounts` (lines 172-174). Happy path asserts `body.innerHTML()` excludes both `NEW_PASSWORD` and `SECURITY_ANSWER` (lines 146-148). Every string/selector cross-checked against the real `PasswordResetPanel.tsx`/`resources.login.ts` — not a fictional UI. |
| 6 | **R6** — the `//go:build db_integration` test lives in `internal/breakglass` (counts toward the 85% floor, D-09), `t.Fatal`s-not-skips under `$CI`, self-provisions a throwaway DB that refuses the name `aura` (D-08); the tier was actually run live | ✓ VERIFIED | `breakglass_integration_test.go:1` (`//go:build db_integration`) + `:64-74` (`recoveryEnvOrSkip`: `t.Fatalf` under `$CI`, `t.Skipf` locally) + `:118-171` (`provisionThrowawayDB`: refuses `dbName == "aura"` at line 125-127, connects as superuser only to the `postgres` maintenance DB, `CREATE DATABASE ... OWNER aura_migrate`, `t.Cleanup` drops WITH (FORCE)). `scripts/coverage_gate.sh:52-53` runs `./internal/...` under `db_integration neo4j_integration` tags, so this test genuinely executes inside the coverage gate (unlike a `cmd/aura` placement, which the gate excludes). **Independently re-run live** (see Adversarial Method): 5/5 subtests PASS in 13.7s against 5 fresh throwaway DBs; live `aura` DB confirmed untouched pre/post. No-skip-as-green independently falsified both directions (CI=true+unset → FAIL; unset CI → SKIP). Combined coverage independently measured at 87.0%. |
| 7 | **DC-1** — `scripts/coverage_docker.sh` exports `AURA_AUTHULA_SECRET`; `.github/workflows/ci.yml` is UNTOUCHED | ✓ VERIFIED | `scripts/coverage_docker.sh:42-46` (`read_secret AURA_AUTHULA_SECRET`, 64-hex dummy fallback, exported). `git log --stat` across all 15 phase-43-range commits shows **zero** touches to `.github/workflows/ci.yml`; `grep -n AURA_AUTHULA_SECRET .github/workflows/ci.yml` shows it already present at the workflow-level `env:` (line 18) and 3 job-level echoes (221/351/1224), pre-dating this phase — confirms the "NO-OP at CI level, local-only gap" claim. |

**Score:** 7/7 rollup truths VERIFIED — 31/31 granular plan-level must-haves (24 truths + prohibitions folded above, itemized in full below) VERIFIED. 0 FAILED. 2 items require human/WSL execution (see Human Verification Required) — these are pre-declared Manual-Only in `43-VALIDATION.md`, not gaps.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/breakglass/guard.go` | `selectSoleOperator` D-11 guard | ✓ VERIFIED | 59 LOC, exists, substantive, wired (consumed by `breakglass.go:55`). |
| `internal/breakglass/guard_test.go` | 8-row table test | ✓ VERIFIED | 93 LOC; re-run: 8/8 subtests pass. |
| `internal/breakglass/source.go` | `Sourcer`/`Secrets`/`Source`/`generateSecret` | ✓ VERIFIED | 211 LOC, exists, substantive, wired (consumed by `cmd/aura/recover_operator.go:55`). |
| `internal/breakglass/source_test.go` | 17+ case table test | ✓ VERIFIED | 336 LOC; re-run: 17/17 subtests + 2 standalone tests pass. |
| `internal/breakglass/setter.go` | `setOperatorPassword` + `NewAuthula` | ✓ VERIFIED | 92 LOC, exists, substantive, wired (consumed by `breakglass.go:60`); proven live via `db_integration`. |
| `internal/breakglass/breakglass.go` | `Deps`/`RecoverOperator` orchestrator | ✓ VERIFIED | 94 LOC, exists, substantive, wired (consumed by `cmd/aura/recover_operator.go:89`); proven live. |
| `internal/breakglass/breakglass_integration_test.go` | throwaway-DB harness (D-07/D-08) | ✓ VERIFIED | 409 LOC, `//go:build db_integration`; re-run live PASS. |
| `internal/breakglass/breakglass_integration_edge_test.go` | R2/missing-account/`--no-recovery` edge subtests | ✓ VERIFIED | 129 LOC (600-LOC-cap split, sanctioned by the plan's own contingency); re-run live PASS. |
| `scripts/coverage_docker.sh` | `AURA_AUTHULA_SECRET` export | ✓ VERIFIED | Lines 42-46; content-verified. |
| `cmd/aura/recover_operator.go` | `identityRecoverOperator` CLI glue | ✓ VERIFIED | 138 LOC, exists, substantive, wired (called from `identity.go:63`); `go build`/`go vet` clean; smoke-tested. |
| `cmd/aura/identity.go` (edit) | `recover-operator` case + usage disambiguation | ✓ VERIFIED | Lines 29-31 (usage), 62-63 (dispatch); `recover` branch (line 60-61) unchanged. |
| `go.mod` (edit) | `golang.org/x/term` direct require | ✓ VERIFIED | Line 40, in the direct block; `go.sum` byte-unchanged vs `origin/master` (independently diffed, empty). |
| `web/e2e/password-reset.spec.ts` | happy + deny Playwright spec | ✓ VERIFIED | 176 LOC, exists, substantive, wired (Playwright auto-discovers `web/e2e/**`); **independently re-run live, 4/4 pass**. |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|----|--------|---------|
| `guard.go` | `internal/identity.Identity` | `Kind`/`Deactivated` field filter | ✓ WIRED | `guard.go:32-39` reads `id.Kind`, `id.Deactivated`; `identity/store.go:61-69` defines both fields. |
| `source.go` | `crypto/rand` | `generateSecret` | ✓ WIRED | `source.go:4,205-210`. |
| `setter.go` | Authula `CoreServices` | `provider.CoreServices()` | ✓ WIRED | `breakglass.go:24-30` (`NewAuthula`→`webauth.New`); consumed at `setter.go:48-91`. |
| `breakglass.go` | `aura.identity_recovery` | `agui.RecoveryHasher.HashAnswer`→`sqlc.UpsertIdentityRecovery` | ✓ WIRED | `breakglass.go:72-83`; live-proven (re-seed restores `LookupRecoveryByEmail`). |
| `breakglass.go` | `aura.identity_recovery_audit` | `sqlc.InsertIdentityRecoveryAudit` | ✓ WIRED | `breakglass.go:86-91`; live-proven (exactly 1 row, `metadata='{}'`). |
| `recover_operator.go` | `internal/breakglass.RecoverOperator` | `Deps{Pool,Core,NoRecovery}` + `Secrets` | ✓ WIRED | `recover_operator.go:89-97`. |
| `identity.go` | `identityRecoverOperator` | `case "recover-operator"` | ✓ WIRED | `identity.go:62-63`. |
| `password-reset.spec.ts` | `/api/auth/password-reset/{start,question,verify,complete}` | `page.route().fulfill()` | ✓ WIRED | Lines 52-101; endpoints cross-checked against the real `web/src/auth/passwordResetApi.ts:35-57` — identical paths/shapes. |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|---------------------|--------|
| `breakglass.go RecoverOperator` | `aura.identity_recovery` row | `agui.RecoveryHasher.HashAnswer` → `sqlc.UpsertIdentityRecovery` | Yes — live-measured: `answer_hash`/`answer_hash_version` non-empty, `LookupRecoveryByEmail` flips from `pgx.ErrNoRows` to a real row after the run | ✓ FLOWING |
| `breakglass.go RecoverOperator` | `authula.accounts.password` | `core.PasswordService.Hash` | Yes — live-measured: `PasswordService.Verify(new, stored)==true`, `Verify(old, stored)==false` | ✓ FLOWING |
| `PasswordResetPanel.tsx` (E2E-driven) | `question`/`resetToken`/`doneTitle` | mocked `page.route` responses → component state → DOM | Yes for the E2E's purpose (D-10: mocked by design; the REAL backend leg is proven separately by the `db_integration` suite above, not the browser) | ✓ FLOWING (by design, mock-to-DOM traced and matches real component code) |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Full repo build | `go build ./...` | clean, no output | ✓ PASS |
| Full repo vet | `go vet ./...` | clean, no output | ✓ PASS |
| Full regression suite | `go test ./...` | **65 ok, 0 FAIL, 3 no-test-files** (recomputed independently) | ✓ PASS |
| `internal/breakglass` unit tier | `go test ./internal/breakglass/... -v -cover` | all 4 top-level + 30 subtests PASS; unit-only coverage 62.3% (expected — `setter.go`/`breakglass.go` are by-design only exercised under `db_integration`; see note below) | ✓ PASS |
| `internal/breakglass` combined coverage | `go test -tags db_integration -covermode=atomic -coverprofile=... ./internal/breakglass/...` | **87.0%** total (≥ 85% floor) | ✓ PASS |
| `aura identity` usage smoke | `go run ./cmd/aura identity` | prints usage incl. `recover-operator [--generate] [--no-recovery]` + D-05 disambiguation, exit 1, instant return (no DB dial attempted) | ✓ PASS |
| `go.sum` unchanged | `git diff origin/master..HEAD -- go.sum` | empty diff | ✓ PASS |
| `go mod verify` | `go mod verify` | "all modules verified" | ✓ PASS |
| Debt-marker scan | `grep -nE "TBD\|FIXME\|XXX\|TODO\|HACK\|PLACEHOLDER"` over all phase-43 files | no matches | ✓ PASS |
| No new migration | `git log --diff-filter=A -- internal/db/migrations/` across phase-43 commits | no new migration files (latest is 0035, from phase 37A, unrelated) | ✓ PASS |
| `recovery.go` untouched | `git diff <pre-43>..HEAD -- cmd/aura/recovery.go` | empty diff | ✓ PASS |
| web-e2e CI auto-trigger | `grep -A5 "web:" .github/workflows/ci.yml` | `dorny/paths-filter` on `web/**` (+ dist + workflow file) — the new spec file auto-triggers `web-e2e` | ✓ PASS |

**Note on the "~96.6%" unit-coverage figure quoted in the verification brief:** that number is accurate for the **Plan-43-01 point in time**, when only `guard.go`+`source.go` existed in the package. After Plan 43-02 added `setter.go`/`breakglass.go` (whose bodies are intentionally proven only by the `db_integration` tier, per the plan's own `<action>`: "NO unit test here … proven by the Task 3 db_integration test"), the package-wide **unit-only** figure is correctly **62.3%** (I independently measured this) — `guard.go` stays 100% and `source.go`'s functions stay 93.8–100% under unit-only tests; `setter.go`/`breakglass.go` show 0% unit-only (by design) and only light up under the `db_integration` tag. The metric that actually governs the CLAUDE.md floor is the **combined** tag coverage, independently measured at **87.0%** — above the 85% floor. This is a precision clarification, not a gap.

### Probe Execution

| Probe | Command | Result | Status |
|-------|---------|--------|--------|
| `internal/breakglass` db_integration suite | `go test -tags db_integration -run TestRecoverOperator -v -p=1 ./internal/breakglass/...` (live Postgres, self-provisioned throwaway DBs) | `PASS` — `TestRecoverOperatorGuardNoWrite` (zero_operators, two_active_operators), `TestRecoverOperatorMissingAccount`, `TestRecoverOperatorNoRecovery`, `TestRecoverOperatorHappyPath` — all green, 13.7s, 5 `aura_bg_*` DBs created+dropped, live `aura` DB verified unchanged before and after | PASS |
| No-skip-as-green (positive) | same command with `CI=true` + required env unset | `FAIL` (`t.Fatalf`: "integration test requires POSTGRES_PASSWORD, but it is unset under CI") | PASS (correctly non-skippable under CI) |
| No-skip-as-green (negative control) | same command with `CI` unset + required env unset | `SKIP` (`t.Skipf`) | PASS (correctly skippable locally) |
| Forgot-password E2E | `AURA_E2E_ORIGIN=http://127.0.0.1:9080 npx playwright test password-reset.spec.ts --project=chromium --project=mobile-chrome` (live `aura` container) | `4 passed (5.3s)` | PASS |

### Requirements Coverage

`.planning/REQUIREMENTS.md` uses domain-prefixed global IDs (`PROF-`, `LOOP-`, `GATE-`, `MUSR-`, `SBX-`, …) scoped to the v2.0.0 milestone; it has **no** entries mapping to phase 43's `R1`–`R6` (confirmed via `grep -n "R1|R2|...|break-glass" .planning/REQUIREMENTS.md` — the only hit is `MUSR-06` line 51, which references the pre-existing Phase-36 break-glass **token-mint** command, not this phase's work). This matches the task brief's explicit note and the SUMMARYs' `requirements-completed: []` (all four, consistently annotated as intentional, matching the 37E precedent). **No orphaned requirements** — there is nothing in REQUIREMENTS.md expected of phase 43 that the plans failed to claim.

| Requirement | Source Plan | Description | Status | Evidence |
|---|---|---|---|---|
| R1–R6 (phase-local, 43-SPEC.md) | 43-01..04 | Break-glass command, guard, sourcing, re-seed, E2E, coverage | ✓ SATISFIED | See Observable Truths table above. |
| Global REQUIREMENTS.md ID | — | — | N/A | No REQUIREMENTS.md entry targets phase 43; nothing orphaned. |

### Anti-Patterns Found

None. Scanned every file touched by the phase (`internal/breakglass/*.go`, `cmd/aura/recover_operator.go`, `cmd/aura/identity.go`, `web/e2e/password-reset.spec.ts`, `scripts/coverage_docker.sh`) for `TBD|FIXME|XXX|TODO|HACK|PLACEHOLDER` and stub-language patterns (`placeholder|coming soon|not yet implemented`) — zero matches. No empty-return stubs, no hardcoded-empty props, no console.log-only handlers.

### Full Plan-Level Must-Haves Cross-Check (24 truths + 15 prohibitions, individually verified)

**Plan 43-01** (7 truths, 2 key_links, 2 prohibitions) — guard.go/source.go:
- All 7 `<must_haves.truths>` rows VERIFIED against `guard.go`/`source.go`/`guard_test.go`/`source_test.go` (cited in Observable Truths row 1–3 above); re-run: 100% pass.
- Key links (`identity.Identity` filter, `crypto/rand`) VERIFIED (Key Link table).
- Prohibitions: no-secret-in-error/Stderr VERIFIED (`source_test.go:275-283` loop, re-run pass); no-DB-call-on-invalid-path VERIFIED structurally (`source.go` takes no pool/DB parameter at all — a DB call is not merely avoided, it is unreachable by the type signature).

**Plan 43-02** (8 truths, 3 key_links, 5 prohibitions) — setter.go/breakglass.go/db_integration:
- All 8 `<must_haves.truths>` rows VERIFIED (Observable Truths rows 1, 2, 4, 6 above cite the exact evidence for each); live `db_integration` re-run confirms every one.
- Key links (CoreServices, `identity_recovery`, `identity_recovery_audit`) VERIFIED.
- Prohibitions: Verify-rejects-hash — VERIFIED (live round-trip); session-survives-reset — VERIFIED (sessions==0 live); non-operator-identity-modified — VERIFIED (guard filters `kind='user'` before any write reaches the target); destructive-test-against-live-`aura` — VERIFIED as actively prevented (D-08 refusal + my own pre/post live-DB row-count check proving zero impact); secret-in-log — VERIFIED (`db_integration`'s own slog-buffer no-leak assertion, re-run pass).

**Plan 43-03** (5 truths, 2 key_links, 4 prohibitions) — cmd/aura glue:
- All 5 `<must_haves.truths>` rows VERIFIED: R1 exit path (code read + smoke test), R3 sourcing wiring (`recover_operator.go:55-61`), R4 `--no-recovery` wiring (`recover_operator.go:100-103`), D-05 sibling dispatch (`identity.go:29-31,62-63`), `config.LoadDB()`+empty-secret guard (`identity.go:40`, `recover_operator.go:72-75`).
- Key links (→ `breakglass.RecoverOperator`, → `identityRecoverOperator`) VERIFIED.
- Prohibitions: no-secret-outside-the-one-line — VERIFIED (code read: only `Sourcer.Source`'s stdout write carries a secret; the `ok:` line and all error prints name only the operator id or a var name); no-cobra — VERIFIED (`flag.NewFlagSet`, no cobra import, `go.mod` has no spf13/cobra); no-live-`aura`-refusal-in-command — VERIFIED (no such guard exists in `recover_operator.go`/`breakglass.go`, as required — the command must run against production); `recovery.go`-untouched — VERIFIED (`git diff` empty).

**Plan 43-04** (4 truths, 1 key_link, 4 prohibitions) — password-reset.spec.ts:
- All 4 `<must_haves.truths>` rows VERIFIED, **independently re-run live** (4/4 pass): happy path to "Password updated", deny path generic+no-factor-leak, D-10 full-mock discipline (confirmed no `gotoAuthenticated` anywhere in the file, unauthenticated `/login` navigation), counted assertions + web-e2e path-filter auto-trigger (confirmed via `ci.yml` `dorny/paths-filter` on `web/**`).
- Key link (mocked API routes) VERIFIED against the real `passwordResetApi.ts`.
- Prohibitions: no-factor-leak — VERIFIED (live run); no-typed-password-in-DOM — VERIFIED (live run); no-real-backend-leg — VERIFIED (spec never calls a live endpoint, no DB seeding code in the file); no-`gotoAuthenticated` — VERIFIED (absent from the file; uses `page.goto('/login', ...)`).

### SPEC Cross-Reference — 8 Edge Coverage Rows

| Category | Requirement | SPEC Status | Verifier Finding |
|---|---|---|---|
| cardinality (zero) | R2 | covered | ✓ VERIFIED — `guard_test.go` "zero identities"; live `TestRecoverOperatorGuardNoWrite/zero_operators` (0 writes). |
| cardinality (many) | R2 | covered | ✓ VERIFIED — `guard_test.go` "two active operators are ambiguous"; live `.../two_active_operators` (0 writes). |
| missing-related-row | R1 | covered | ✓ VERIFIED — `setter.go:58-67` (missing auth link) + `:69-75` (missing account); live `TestRecoverOperatorMissingAccount` (0 partial writes). |
| idempotency | R4 | covered | ✓ VERIFIED — `UpsertIdentityRecovery` `ON CONFLICT DO UPDATE`; live second-run count==1. |
| empty/whitespace input | R3 | covered | ✓ VERIFIED — `source.go:101-104,136-139,168-170`; `source_test.go` 3 dedicated subtests, re-run pass. |
| source conflict | R3 | covered | ✓ VERIFIED — `source.go:92-94`; `source_test.go` "env password and generate conflict", re-run pass. |
| no-input environment | R3 | covered | ✓ VERIFIED — `source.go:106-109` (`canPrompt`); `source_test.go` 2 non-TTY subtests, re-run pass. |
| same-as-current password | R1 | covered (allowed, not enforced) | ✓ VERIFIED — no `ErrPasswordResetSamePassword` code path exists in `setter.go`; live: the idempotency second-run reuses the identical `bgNewPassword` and succeeds, which is a same-password reset by construction. |

### SPEC Cross-Reference — 6 Prohibitions (+1 dismissed-as-canon)

| Prohibition | Requirement | Verifier Finding |
|---|---|---|
| MUST NOT log/print the plaintext password (except the single `--generate` stdout line) | R3 | ✓ VERIFIED — `source_test.go` no-leak loop (unit, re-run); `db_integration`'s slog-buffer no-leak assertion (live, re-run); `recover_operator.go` error/ok-lines name no secret (code read). |
| MUST NOT modify a non-operator identity (`kind != 'user'`) | R2 | ✓ VERIFIED — `guard.go` filters to `kind=="user"` before any identity reaches the setter; `RecoverOperator` only ever uses `op.ID` from the guard's return value for every subsequent write. |
| MUST NOT leave any prior session valid after a reset | R4 | ✓ VERIFIED — delete-before-update ordering (code) + live `sessions==0` assertion (re-run). |
| MUST NOT run destructive tests against the live `aura` DB (throwaway only) | R6 | ✓ VERIFIED — D-08 refusal guard in code + my own independent pre/post row-count check on the live `aura` DB proving zero impact across the re-run. |
| MUST NOT produce a hash `Argon2PasswordService.Verify` rejects | R1 | ✓ VERIFIED — live round-trip `Verify(new,...)==true` / `Verify(old,...)==false` (re-run). |
| MUST NOT reveal which recovery factor is missing on the forgot-password deny path | R5 | ✓ VERIFIED — E2E deny assertions (re-run live, 2/2 pass) + cross-checked against real `PasswordResetPanel.tsx` catch-handler logic (both `/start` and `/question` denials surface only the generic key). |
| SQL injection / parameterization on the direct-DB path | R1 | dismissed (canon, not re-verified here) — confirmed anyway: every query in `setter.go`/`breakglass.go`/the integration harness is parameterized (`$1`/sqlc); the only string-built SQL is `CREATE`/`DROP DATABASE` identifiers, which cannot be parameterized in Postgres DDL and are guarded by `throwawayNameRe` + `quoteIdent` escaping. |

### Human Verification Required

#### 1. Live break-glass reset against the production `aura` DB

**Test:** On the deploy host, with the live single-operator `aura` deployment (currently `identity_recovery`=0 — the real, still-unfixed lockout, independently confirmed this session), run `aura identity recover-operator` (via hidden prompt, `AURA_RECOVERY_PASSWORD`, or `--generate`).
**Expected:** Exits 0; prints a secret-free `ok:` line; the operator can subsequently log in with the new password; the forgot-password flow now sends a Telegram code again (`LookupRecoveryByEmail` returns a row instead of denying).
**Why human:** Mutates real production auth state; requires a TTY/deploy host; explicitly out of scope for automated verification per the SPEC's own boundaries and the project's anti-footgun discipline (this is exactly the class of "coverage gate nukes live DB" mistake the phase exists to remediate — automated verification must never write to the live `aura` DB). The composed logic this command runs is already proven end-to-end against a structurally identical throwaway DB (same migrations, same seed shape, same code path) — see the independently-re-run `db_integration` suite above.

#### 2. Mutation testing spot-check (`go-mutesting`, WSL)

**Test:** `GOFLAGS=-tags=db_integration go-mutesting ./internal/breakglass/setter.go ./internal/breakglass/source.go` in WSL; record killed/total.
**Expected:** ≥70% mutants killed on both files (CLAUDE.md Gate-3 DoD; `43-VALIDATION.md`'s own Manual-Only table names exactly this check for R1/R3/R6).
**Why human:** `go-mutesting` is not installed and not on PATH in this Windows verification environment; it is pre-declared Manual-Only ("WSL-only long-running campaign, not a CI gate") in the phase's own `43-VALIDATION.md`, authored before execution — this is a pre-planned deferral, not something the verifier skipped.

### Gaps Summary

**None.** Every must-have from all 4 PLAN.md files' frontmatter (24 truths, 8 key_links, 15 prohibitions), every SPEC.md Edge Coverage row (8/8) and Prohibition (6/6 + 1 dismissed-as-canon), and the 7 rollup requirements (R1–R6 + DC-1) were checked against the real codebase — not the SUMMARYs — and independently re-executed wherever automatable (build, vet, full regression suite, `internal/breakglass` unit + live `db_integration` throwaway-DB suite, live Playwright E2E, no-skip-as-green both directions, combined coverage measurement, CLI smoke test). All passed. The two items in Human Verification Required are pre-declared Manual-Only in the phase's own validation contract (`43-VALIDATION.md`), not gaps discovered during this pass — routing status to `human_needed` rather than `passed` per the verification decision tree (human items take priority over an otherwise-clean pass).

---

_Verified: 2026-07-11_
_Verifier: Claude (gsd-verifier)_
