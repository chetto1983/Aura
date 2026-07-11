---
status: testing
phase: 43-operator-break-glass-recovery-and-forgot-password-e2e
source: [43-VERIFICATION.md]
started: 2026-07-11
updated: 2026-07-11
---

## Current Test

number: 1
name: Live break-glass reset on the deploy host (the real operator lockout fix)
expected: |
  Exits 0, prints an `ok: recovered operator <id>; sessions invalidated; recovery re-seeded`
  line with no secret; the operator can log in with the new password; the forgot-password
  flow sends a Telegram code again (LookupRecoveryByEmail no longer denies).
awaiting: user response

## Tests

### 1. Live break-glass reset on the deploy host
expected: On the deploy host, with the live single-operator `aura` DB (currently 1 `kind='user'` identity, 0 `identity_recovery` rows — the real lockout), run `aura identity recover-operator` (password source: hidden prompt, `AURA_RECOVERY_PASSWORD`, or `--generate`). Requires `AURA_AUTHULA_SECRET` + the Postgres admin DSNs in the environment. Exit 0 with a secret-free `ok:` line; the operator logs in with the new password; the forgot-password flow delivers a Telegram code again.
result: [pending]

### 2. Mutation-testing spot-check (WSL)
expected: In WSL, `GOFLAGS=-tags=db_integration go-mutesting ./internal/breakglass/setter.go ./internal/breakglass/source.go` reports ≥70% mutants killed on both files (CLAUDE.md Gate-3 DoD + 43-VALIDATION.md Manual-Only table).
result: [pending]

## Summary

total: 2
passed: 0
issues: 0
pending: 2
skipped: 0
blocked: 0

## Gaps
