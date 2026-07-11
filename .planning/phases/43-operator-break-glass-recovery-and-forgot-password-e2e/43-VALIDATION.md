---
phase: 43
slug: operator-break-glass-recovery-and-forgot-password-e2e
status: approved
nyquist_compliant: true
wave_0_complete: false
created: 2026-07-11
---

# Phase 43 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from `43-RESEARCH.md` § Validation Architecture. The command is one-shot and
> deterministic — the "signal" is a set of discrete DB state transitions, each directly
> observed post-run (SELECT/Verify assertions), which is the maximal sampling rate (no aliasing risk).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` (std) + table tests, `-race` mandatory, `goleak` where goroutines spawn (Authula `provider.Close()`); Playwright for E2E |
| **Config file** | none for Go (`go test`); `web/playwright.config.ts` for E2E |
| **Quick run command** | `go vet ./... && go build ./... && go test -race ./internal/breakglass/` |
| **Full suite command** | `go test -tags "db_integration" -race -p 1 ./internal/breakglass/` (stack up; throwaway DB) + `bash scripts/coverage_docker.sh` (owned-surface ≥85%, disposable `aura_cov`) |
| **Estimated runtime** | unit <5s; `db_integration` ~30–60s (throwaway DB create/migrate/drop); E2E ~30s |

---

## Sampling Rate

- **After every task commit:** Run `go vet ./... && go build ./... && go test -race ./internal/breakglass/` (unit, <5s)
- **After every plan wave:** Run `bash scripts/coverage_docker.sh` (full `db_integration neo4j_integration` matrix on a disposable DB; owned-surface ≥85%)
- **Before `/gsd-verify-work`:** Full matrix green **and** `cd web && npx playwright test password-reset.spec.ts` green, `-race` clean
- **Max feedback latency:** <5s (unit) between task commits

---

## Per-Task Verification Map

> Task IDs are assigned by the planner. This maps each SPEC requirement to its observable signal and tier
> (from RESEARCH § "Observable signals → SPEC acceptance criteria" and "Phase Requirements → Test Map").

| Req | Behavior | Observable signal | Test Type | Automated Command | File Exists | Status |
|-----|----------|-------------------|-----------|-------------------|-------------|--------|
| R1 | argon2 hash Verify-accepted; sessions=0; reset | re-read account → `core.PasswordService.Verify(newPw, *account.Password)==true`; `SELECT count(*) FROM authula.sessions WHERE user_id=$1`==0 | `db_integration` | `go test -tags db_integration -race -p1 ./internal/breakglass/ -run TestRecoverOperator` | ❌ W0 | ⬜ pending |
| R2 | operator guard 0/1/>1 (pure) | `selectSoleOperator` returns error for 0 and >1 with zero writes; proceeds for exactly 1 | unit (guard) + `db_integration` (no-write) | `go test -race ./internal/breakglass/ -run TestSelectSoleOperator` | ❌ W0 | ⬜ pending |
| R3 | sourcing matrix (prompt/env/generate; conflict/non-TTY/empty) | function errors for {env+generate}, {non-TTY,no env,no generate}, {empty/whitespace} **before any DB call**; `--generate` prints exactly one line, len≥20, charset `[A-Za-z0-9_-]` | unit (no DB, capture stdout) | `go test -race ./internal/breakglass/ -run TestSourceSecret` | ❌ W0 | ⬜ pending |
| R4 | re-seed + idempotency + `--no-recovery` | `GetIdentityRecoveryByIdentity` non-empty hash+version; `LookupRecoveryByEmail` returns a row (was `pgx.ErrNoRows`); run twice → row count==1; `--no-recovery` → no row + warning | `db_integration` | `go test -tags db_integration -race -p1 ./internal/breakglass/ -run TestRecoverOperatorReseed` | ❌ W0 | ⬜ pending |
| R5 | forgot-password happy + deny (UI) | happy: `/complete` mock → `doneTitle` "Password updated"; deny: `/start` neutral notice, `/question` deny → `login.reset.errors.generic`, DOM names no factor (`identity_recovery`/`telegram`) and no typed password | Playwright | `cd web && npx playwright test password-reset.spec.ts` | ❌ W0 | ⬜ pending |
| R6 | deny branch generic denial + no-leak (backend) | `errors.Is(err, agui.ErrPasswordResetDenied)`; captured slog+stderr buffer does not contain the sentinel plaintext | unit (fake store) + `db_integration` | `go test -tags db_integration -race -p1 ./internal/breakglass/ -run TestDenyGeneric` | ❌ W0 | ⬜ pending |
| D-06 | neutral audit event, no secret | `SELECT event, metadata FROM aura.identity_recovery_audit` → `operator_password_recovered`, metadata `{}` | `db_integration` | (covered by `TestRecoverOperator`) | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/breakglass/` package skeleton — so its `db_integration` test is picked up by `coverage_gate.sh ./internal/...` (R6 coverage + runs-in-CI). **cmd/aura is excluded from the floor and its `db_integration` tests never run in CI** (RESEARCH Pitfall 1).
- [ ] **Local gate (NOT CI — DRIFT-CORRECTED, PATTERNS DC-1):** `AURA_AUTHULA_SECRET` is already workflow-level in `ci.yml:18`, inherited by the coverage-gate job — **no ci.yml edit**. Instead export it in `scripts/coverage_docker.sh` (via the existing `read_secret` helper, from `.env`) so the new `db_integration` test doesn't `t.Fatal` locally under `CI=true`. *(Leave `AURA_AUTHULA_DATABASE_URL` unset → derives from the throwaway `AURA_DB_URL`.)*
- [ ] `internal/breakglass/breakglass_integration_test.go` — throwaway-DB harness with the D-08 `aura`-name refusal + `envOrSkip`-style `t.Fatal`-under-`$CI` guard (copy `authula_integration_test.go:32-43` + `coverage_docker.sh:44-47`).
- [ ] `web/e2e/password-reset.spec.ts` — happy + deny (the web-e2e job is path-filtered on `web/**`, so the new spec auto-triggers it).

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Mutation spot-check ≥70% killed | R1/R3/R6 | `go-mutesting` is a WSL-only long-running campaign, not a CI gate | WSL: `GOFLAGS=-tags=db_integration go-mutesting ./internal/breakglass/setter.go ./internal/breakglass/source.go` — target ≥70% killed. `setter.go` = reset orchestration (delete-then-update order, missing-account/missing-link early returns, no same-password guard); `source.go` = conflict/non-TTY/empty decision tree (highest branch density). Record the score in the phase VALIDATION Manual-Only table. |
| Live break-glass against real `aura` (single operator) | R2 | The command's whole purpose is to run against the live DB, which cannot be exercised in CI (would mutate real auth state) | On the deploy host, confirm exactly one `kind='user'` identity, run `aura identity recover-operator`, verify login works + forgot-password now delivers a Telegram code. |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (new `internal/breakglass` package + `coverage_docker.sh` secret export [DC-1, not a CI edit] + throwaway-DB harness + E2E spec)
- [x] No watch-mode flags
- [x] Feedback latency < 5s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved 2026-07-11 (plans 43-01→43-04 verified by gsd-plan-checker — zero blockers; all Wave 0 items + Nyquist checks 8a/8c/8d satisfied). `wave_0_complete` stays false until the Wave 0 files are actually created during `/gsd-execute-phase`.

> **`-race` sampling note (checker W4):** `-race` is enforced at **plan granularity** — every PLAN.md `<verification>` block runs `go test -race ./internal/breakglass/` (and `-tags db_integration -race` where applicable) before a plan is done, satisfying CLAUDE.md's per-package `-race` rule. Some per-task `<automated>` commands (Plan 01 T1/T2, Plan 02 T1/T2) omit `-race` for a faster inner loop; the plan-level gate closes the gap.
