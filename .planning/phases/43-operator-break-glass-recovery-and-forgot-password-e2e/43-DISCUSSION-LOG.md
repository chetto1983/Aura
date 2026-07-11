# Phase 43: Operator Break-Glass Recovery + Forgot-Password E2E - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-11
**Phase:** 43-operator-break-glass-recovery-and-forgot-password-e2e
**Areas discussed:** Password-set path, Recovery Q&A sourcing UX, Coexistence with existing `recover`, Throwaway-DB test harness, plus an online research pass on industrial break-glass / disaster-recovery patterns

---

## Password-set path

| Option | Description | Selected |
|--------|-------------|----------|
| Reuse Authula services offline | New offline setter constructing Authula account/password/session services against the pool; `PasswordService.Hash` → update `authula.accounts` → `SessionService.DeleteAllByUserID`; no same-password guard; online `SetPassword` untouched | ✓ |
| Refactor online SetPassword | Extract a shared helper with a break-glass flag used by both paths; more reuse but touches the live online reset path | |
| Hash-only + direct SQL | Reuse only `PasswordService.Hash`; accounts UPDATE + sessions DELETE via direct SQL | |

**User's choice:** Reuse Authula services offline.
**Notes:** The online `authulaPasswordResetter.SetPassword` rejects same-password (`ErrPasswordResetSamePassword`) and requires the server's `CoreServices`, so it cannot be reused verbatim. Break-glass must allow the same password (SPEC Edge Coverage). Reuses Authula's `PasswordService.Hash` per SPEC constraint (no argon2 re-implementation).

---

## Recovery Q&A sourcing UX

| Option | Description | Selected |
|--------|-------------|----------|
| Symmetric with password | `AURA_RECOVERY_QUESTION` + `AURA_RECOVERY_ANSWER` env, hidden prompt default, `--generate` also generates the answer with a fixed default question | ✓ |
| Q&A always interactive | Password gets env/generate/prompt, but the question+answer are always an interactive prompt | |
| Answer = the new password | Reuse the password as the recovery answer with a fixed question (one secret) | |

**User's choice:** Symmetric with password.
**Notes:** Keeps fully-non-interactive and `--generate` runs working end-to-end for the re-seed. Answer hashing reuses `agui.RecoveryHasher{}.HashAnswer` (`argon2id-v1`) + `UpsertIdentityRecovery` (idempotent), mirroring bootstrap/onboarding.

---

## Coexistence with existing `recover`

| Option | Description | Selected |
|--------|-------------|----------|
| Siblings + disambiguated usage | Keep both subcommands; update `identityUsage` with a one-line distinction + clarifying stdout line; existing `recover` untouched | ✓ |
| Merge into recover flags | Fold operator reset into `recover --operator --set-password`; overloads one command with two mechanisms | |
| Add silently | Add without changing the usage line or cross-referencing `recover` | |

**User's choice:** Siblings + disambiguated usage.
**Notes:** The existing `recover <name>` mints a short-lived reset *token* (0023 infra) — a different mechanism from `recover-operator`'s direct password set + recovery re-seed. Disambiguation avoids operator confusion in an emergency.

---

## Throwaway-DB test harness

| Option | Description | Selected |
|--------|-------------|----------|
| Self-provisioned + refusal guard | Test helper `CREATE DATABASE`s a throwaway (e.g. `aura_recover_test`), migrates, seeds one operator, runs the command, asserts, `DROP`s on cleanup; refuses if target DB name == `aura` | ✓ |
| Reuse aura_cov | Piggyback on the coverage-gate's disposable `aura_cov` DB | |
| Ephemeral schema in test DB | Throwaway schema/rows inside the existing db_integration DB | |

**User's choice:** Self-provisioned + refusal guard.
**Notes:** Mirrors `scripts/coverage_gate.sh`'s live-`aura` refusal — the exact 37D-05 footgun that produced the lockout. The refusal guard is TEST-ONLY; the production command must run against live `aura`.

---

## Online research pass — industrial break-glass / disaster recovery

Searched break-glass emergency-access and disaster-recovery best practices. Applied findings:
- **Meticulous audit logging** of every use (who/when/what, never the secret) → added **D-06**: a neutral, secret-free `operator_password_recovered` audit event mirroring the sibling's `break_glass_token_minted`. Grounded in the SPEC's own "explicit, **audited** operator action" language, so not scope creep.
- **One-time generated-secret display** → `--generate` prints the password (and answer) exactly once.
- **Treat-as-compromised / rotate after use** → the operator supplies a fresh password each run.

## Claude's Discretion

- Flag-parsing style (hand-rolled switch tree mirroring `runDB`/`runIdentity`, not cobra).
- Exact stderr wording for the operator-count guard (0/>1) and source-conflict errors (no plaintext secret).
- File split to stay ≤600 LOC.

## Deferred Ideas

- TOTP break-glass reset / re-provision — separate phase.
- General per-email / any-identity recovery — larger risk surface, out of scope.
- Web UI for break-glass recovery — CLI only.
- Boot-time auto-healing of a missing recovery row — recovery stays explicit + audited.
