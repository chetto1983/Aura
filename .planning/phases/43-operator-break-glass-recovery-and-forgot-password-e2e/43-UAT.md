---
status: resolved
phase: 43-operator-break-glass-recovery-and-forgot-password-e2e
source: [43-VERIFICATION.md]
started: 2026-07-11
updated: 2026-07-11
---

## Current Test

number: 1
name: Live break-glass / forgot-password on the live aura DB
expected: |
  forgot-password sends a Telegram recovery code again once identity_recovery is restored.
awaiting: none (resolved)

## Tests

### 1. Live break-glass / forgot-password on the live aura DB
expected: The operator can recover: identity_recovery restored, forgot-password delivers a Telegram code.
result: passed — 2026-07-11 VALIDATED END-TO-END on the live `aura` DB. Re-seeded `aura.identity_recovery` for the operator `b130c94d` (question "Come si chiama tua madre?", `argon2id-v1`, password + sessions untouched); the `LookupRecoveryByEmail` 4-table join now returns a row. Triggered `POST /api/auth/password-reset/start` (operator email) → HTTP 202 `{"status":"ok"}`; the operator received the real `Aura recovery code: …` on Telegram (`chetto983`), no delivery error in the aura logs. Lockout resolved. (Applied via a targeted reseed rather than the full `recover-operator` command — the command's composed logic is separately proven by the db_integration throwaway tests; it stays available for a future full break-glass.)

### 2. Mutation-testing spot-check (WSL)
expected: `go-mutesting ./internal/breakglass/{setter,source}.go` ≥70% killed.
result: skipped — DEFERRED as documented Manual-Only (WSL-only long campaign, not CI-blocking; 43-VALIDATION.md Manual-Only table). Run in WSL when convenient.

## Summary

total: 2
passed: 1
issues: 0
pending: 0
skipped: 1
blocked: 0

## Gaps

None. Phase goal validated end-to-end on the live system; the single deferred item is a non-blocking WSL mutation spot-check.
