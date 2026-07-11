# Phase 43: Operator Break-Glass Recovery + Forgot-Password E2E — Specification

**Created:** 2026-07-11
**Ambiguity score:** 0.18 (gate: ≤ 0.20)
**Requirements:** 6 locked

## Goal

Add a host-only, offline `aura` CLI command that resets the **operator** account's password (argon2id, Authula-compatible), invalidates its sessions, and re-seeds a missing `aura.identity_recovery` row — so a wiped/missing recovery row can never permanently lock the operator out — and add end-to-end coverage of the forgot-password flow (happy path + the deny-when-recovery-missing path).

## Background

Grounded in the live codebase and DB (diagnosed 2026-07-11):

- **CLI shape:** `cmd/aura/main.go` dispatches subcommands via a plain `switch os.Args[1]` (NOT cobra). An `identity` subcommand already exists (`runIdentity`). No recovery/reset command exists.
- **Auth store:** Authula v1.15.0. `Argon2PasswordService.Hash` = `argon2.IDKey(pw, salt16, t=1, m=64*1024, p=4, keyLen=32)`, stored as `base64.RawStdEncoding(salt‖hash)`. Accounts live in `authula.accounts`, keyed by `authula_user_id` via `aura.identity_auth_links`; sessions in `authula.sessions`. `authulaPasswordResetter.SetPassword` (cmd/aura/serve_password_reset.go:422) already does hash→update→delete-sessions inside the *online* flow.
- **Recovery data:** `aura.identity_recovery` (question + answer_hash + answer_hash_version), `aura.telegram_accounts` (delivery), the 4-step forgot-password flow (`/api/auth/password-reset/{start,question,verify,complete}`), and `UpsertIdentityRecovery` (called at bootstrap + onboarding). `LookupRecoveryByEmail` INNER-JOINs `identity_auth_links` + `identity_recovery` + `telegram_accounts`.
- **The lockout (why this phase exists):** the live operator `dvdmarchetto@gmail.com` (`b130c94d-…`) has `identity_auth_links`=1 and `telegram_accounts`=1 but **`identity_recovery`=0** — a residue of the 37D-05 coverage-gate DB-wipe footgun. The INNER JOIN therefore returns 0 rows → `ErrPasswordResetDenied` → forgot-password silently sends no Telegram code, and there is **no offline recovery path** → permanent lockout.
- **Tests:** password_reset has some unit/integration coverage; there is **no E2E** for the forgot-password flow, and no test asserts the deny-path's no-leak behavior.

## Requirements

1. **Break-glass recovery command**: An offline, host-only `aura identity recover-operator` command resets the operator account.
   - Current: no offline recovery command; a missing `identity_recovery` row is unrecoverable without direct DB surgery.
   - Target: `aura identity recover-operator` connects directly to Postgres (config DSNs; no running server required), resolves the **single** operator identity (`kind = 'user'`), sets a new argon2id password on its Authula account, deletes its sessions, and (unless opted out) re-seeds `identity_recovery`. Exit 0 on success.
   - Acceptance: on a throwaway DB seeded with one operator whose `identity_recovery` row is deleted, the command sets a new password that `Argon2PasswordService.Verify` accepts, leaves `authula.sessions`=0 for that user, and inserts an `identity_recovery` row; exits 0.

2. **Operator resolution guard**: The command targets the operator only, and refuses ambiguous/absent state.
   - Current: n/a.
   - Target: resolve exactly one identity with `kind='user'`; on 0 or >1 matches, exit non-zero with a clear stderr message and change nothing.
   - Acceptance: against a DB with 0 operator identities → exit ≠0, no writes; with 2 operator identities → exit ≠0, no writes; with exactly 1 → proceeds.

3. **Password sourcing (prompt / env / generate)**: The new password comes from the operator, three ways, never logged.
   - Current: n/a.
   - Target: default = hidden interactive TTY prompt (with confirm); `AURA_RECOVERY_PASSWORD` env for non-interactive use; `--generate` prints one strong random password (≥ 20 chars) to stdout exactly once. Conflicting sources error rather than silently pick one. Non-TTY with no env and no `--generate` → error (cannot prompt).
   - Acceptance: env set → no prompt, env value applied; `--generate` → a ≥20-char random password printed once and applied; env + `--generate` together → exit ≠0; non-TTY with neither → exit ≠0.

4. **Recovery re-seed**: The command re-seeds `identity_recovery` so the online forgot-password path works again.
   - Current: missing `identity_recovery` row → forgot-password denied with no way to repopulate it offline.
   - Target: upsert an `identity_recovery` row (question + argon2 `answer_hash` + `answer_hash_version`) for the operator, question/answer sourced from prompt/env; `--no-recovery` skips the re-seed and prints a warning that online forgot-password stays unavailable until reconfigured.
   - Acceptance: after a default run, `aura.identity_recovery` has a row for the operator with non-empty `answer_hash`+`answer_hash_version` and `LookupRecoveryByEmail` returns a row; with `--no-recovery`, no row is written and a warning is printed.

5. **Forgot-password E2E (happy + deny)**: Playwright coverage of the online flow.
   - Current: no E2E exists for `/api/auth/password-reset/*` or `PasswordResetPanel`.
   - Target: `web/e2e/password-reset.spec.ts` — happy path (recovery configured → `/start` → security question → recovery code [Telegram delivery mocked] → `/verify` → set new password → land on login) and deny path (recovery row missing → `/start` → generic denial that does not reveal which factor is absent).
   - Acceptance: both specs pass in the Playwright CI job; the deny-path spec asserts the response/UI text is identical to a generic "if an account exists…" denial and never names `identity_recovery`/`telegram`.

6. **Backend coverage (command + deny branch)**: Unit + integration tests, no skip-as-green.
   - Current: the new command has no tests; the `LookupRecoveryByEmail` 0-rows deny branch is not asserted for no-leak.
   - Target: unit tests for password sourcing (prompt/env/generate/conflict), the operator-count guard (0/1/>1), argon2 round-trip (Authula `Verify` accepts the produced hash), re-seed, and session-delete; a `db_integration`-tagged test exercising the full command against a **throwaway** DB; a test asserting the deny path yields a generic denial.
   - Acceptance: owned-surface coverage ≥ 85% across the new files; `go test -race` clean; the `db_integration` test actually runs in CI (fails, not skips, if its env is unset under `$CI`).

## Boundaries

**In scope:**
- `aura identity recover-operator` — offline, host-only, operator-only: reset password + kill sessions + re-seed recovery.
- Password sourcing: hidden prompt (default), `AURA_RECOVERY_PASSWORD` env, `--generate`; `--no-recovery` opt-out for the re-seed.
- `web/e2e/password-reset.spec.ts` — happy + deny paths (Telegram delivery mocked).
- Backend unit + `db_integration` tests for the command and the deny branch.

**Out of scope:**
- **TOTP reset / re-provision** — the command does not touch `authula.totp`; a locked TOTP is a separate concern (would be its own phase).
- **Any-identity recovery** — operator-only (`kind='user'`); a general per-email reset is scope creep and a larger risk surface.
- **A web UI for break-glass recovery** — CLI only; the whole point is it works when the cockpit is unreachable.
- **Changing the online forgot-password security model or Telegram delivery** — the flow is unchanged; we only re-seed the data it reads and add tests.
- **A new DB migration** — `identity_recovery`, `telegram_accounts`, `identity_auth_links`, `authula.*` already exist; no schema change.
- **Boot-time auto-healing** of a missing recovery row — recovery is an explicit, audited operator action, not silent self-mutation of auth state.

## Constraints

- Reuse Authula v1.15 argon2id parameters exactly (`t=1, m=64*1024, p=4, keyLen=32, salt=16`, `base64.RawStdEncoding(salt‖hash)`) — `Argon2PasswordService.Verify` MUST accept the produced hash. Prefer calling Authula's `PasswordService.Hash` over re-implementing it.
- Host-only / offline: requires the Postgres admin DSNs (`AURA_DB_URL` / migrate / bootstrap as `config.Load` composes); no running `aura serve` and no network.
- No plaintext secret in logs (`slog`/stderr), argv, or error strings — the only place a password is emitted is the single `--generate` stdout line.
- No new file under `internal/db/migrations/`.
- Coverage floor 85% owned-surface; `-race` clean; the `db_integration` tier must actually execute in CI (no skip-as-green); mutation spot-check on the command's critical file.
- Tests MUST use a disposable DB, **never** the live `aura` database — this is the exact footgun (37D-05 / coverage-gate) that produced this lockout; mirror the `scripts/coverage_gate.sh` live-DB refusal.

## Acceptance Criteria

- [ ] `aura identity recover-operator` runs offline (no server) and, on a seeded throwaway DB, sets an Authula-`Verify`-accepted password, deletes the operator's sessions, and re-seeds `identity_recovery`; exit 0.
- [ ] 0 or >1 `kind='user'` identities → exit ≠0 with a clear message and zero writes.
- [ ] Password sourced from hidden prompt (default), `AURA_RECOVERY_PASSWORD` (env), or `--generate` (one-time stdout); conflicting sources and non-TTY-without-source → exit ≠0.
- [ ] `--no-recovery` skips the re-seed and warns; default run makes `LookupRecoveryByEmail` return a row again.
- [ ] `web/e2e/password-reset.spec.ts` happy + deny paths pass in the Playwright CI job.
- [ ] The deny path never reveals which recovery factor is missing (generic denial asserted).
- [ ] Owned-surface coverage ≥ 85%; `-race` clean; `db_integration` test executes (not skipped) in CI.
- [ ] No plaintext password appears in any log/slog/stderr output (asserted).
- [ ] No new migration file added.

## Edge Coverage

**Coverage:** 8/8 applicable edges resolved · 0 unresolved

| Category | Requirement | Status | Resolution / Reason |
|----------|-------------|--------|---------------------|
| cardinality (zero) | R2 | ✅ covered | 0 operator identities → exit ≠0, no writes (AC line 2) |
| cardinality (many) | R2 | ✅ covered | >1 operator identities → exit ≠0, no writes (AC line 2) |
| missing-related-row | R1 | ✅ covered | operator has auth_link but no `authula.accounts` row → clear error, no partial write |
| idempotency | R4 | ✅ covered | `identity_recovery` already present → upsert (update-in-place), never a duplicate row |
| empty/whitespace input | R3 | ✅ covered | empty/whitespace password rejected before any write |
| source conflict | R3 | ✅ covered | env + `--generate` set together → exit ≠0 (AC line 3) |
| no-input environment | R3 | ✅ covered | non-TTY with no env and no `--generate` → exit ≠0 (cannot prompt) |
| same-as-current password | R1 | ✅ covered | break-glass MUST allow setting the same value (does NOT enforce `ErrPasswordResetSamePassword`, unlike the online flow) — specified as allowed |

## Prohibitions (must-NOT)

**Coverage:** 6/6 applicable prohibitions resolved · 0 unresolved · (canon items referred out)

| Prohibition (must-NOT statement) | Requirement | Status | Verification / Reason |
|----------------------------------|-------------|--------|------------------------|
| MUST NOT log/print the plaintext password (except the single `--generate` stdout line) | R3 | resolved | test — assert no `slog`/log sink and no error string receives the password |
| MUST NOT modify a non-operator identity (`kind != 'user'`) | R2 | resolved | test — attempt against a non-user identity is refused |
| MUST NOT leave any prior session valid after a reset | R4 | resolved | test — `authula.sessions`=0 for the user post-run; stale cookie rejected |
| MUST NOT run destructive tests against the live `aura` DB (throwaway only) | R6 | resolved | test — mirror `coverage_gate.sh` live-DB refusal guard |
| MUST NOT produce a hash `Argon2PasswordService.Verify` rejects | R1 | resolved | test — argon2 round-trip via Authula's own `Verify` |
| MUST NOT reveal which recovery factor is missing on the forgot-password deny path | R5 | resolved | judgment+test — E2E deny assertion: response identical to generic denial |
| SQL injection / parameterization on the direct-DB path | R1 | dismissed | canon — owned by /gsd-secure-phase + parameterized queries (sqlc/pgx); not minted here |

## Ambiguity Report

| Dimension          | Score | Min  | Status | Notes                                              |
|--------------------|-------|------|--------|----------------------------------------------------|
| Goal Clarity       | 0.90  | 0.75 | ✓      | Behavior fully locked (reset+reseed+kill, offline) |
| Boundary Clarity   | 0.85  | 0.70 | ✓      | Explicit out-of-scope: TOTP, any-identity, web UI  |
| Constraint Clarity | 0.72  | 0.65 | ✓      | argon2 params + throwaway-DB + no-migration locked  |
| Acceptance Criteria| 0.75  | 0.70 | ✓      | 9 pass/fail criteria                                |
| **Ambiguity**      | 0.18  | ≤0.20| ✓      | Gate passed in round 1                              |

## Interview Log

| Round | Perspective            | Question summary                          | Decision locked                                              |
|-------|------------------------|-------------------------------------------|-------------------------------------------------------------|
| 0     | Researcher (pre-scout) | Why is no Telegram code sent?             | Operator `identity_recovery` row missing → INNER JOIN denies |
| 1     | Simplifier + Boundary  | What should the command do?               | Reset password **+ re-seed recovery** + kill sessions       |
| 1     | Boundary Keeper        | How does it run / prove admin?            | **Offline, direct-DB, host-only** (host access = admin proof)|
| 1     | Failure Analyst        | Where does the new password come from?    | **Prompt** default, `AURA_RECOVERY_PASSWORD` env, `--generate` |
| 1     | Boundary Keeper        | Which identities can it target?           | **Operator only** (`kind='user'`)                            |

---

*Phase: 43-operator-break-glass-recovery-and-forgot-password-e2e*
*Spec created: 2026-07-11*
*Next step: /gsd-discuss-phase 43 — implementation decisions (command/flag naming, prompt UX, test harness wiring)*
