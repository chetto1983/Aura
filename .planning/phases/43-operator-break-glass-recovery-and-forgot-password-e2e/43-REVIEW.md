---
phase: 43-operator-break-glass-recovery-and-forgot-password-e2e
reviewed: 2026-07-11T00:00:00Z
depth: deep
reviewer: gsd-code-reviewer (adversarial)
advisory: true
diff_range: ecb3c550..HEAD
files_reviewed: 13
files_reviewed_list:
  - internal/breakglass/setter.go
  - internal/breakglass/breakglass.go
  - internal/breakglass/guard.go
  - internal/breakglass/source.go
  - internal/breakglass/breakglass_integration_test.go
  - internal/breakglass/breakglass_integration_edge_test.go
  - internal/breakglass/guard_test.go
  - internal/breakglass/source_test.go
  - cmd/aura/recover_operator.go
  - cmd/aura/identity.go
  - web/e2e/password-reset.spec.ts
  - scripts/coverage_docker.sh
  - go.mod
findings:
  blocker: 0
  high: 0
  medium: 1
  low: 5
  nit: 4
  total: 10
status: issues_found
---

# Phase 43: Code Review Report — Operator Break-Glass Recovery + Forgot-Password E2E

**Reviewed:** 2026-07-11
**Depth:** deep (cross-file: `breakglass` ↔ `webauth` ↔ `agui` ↔ `sqlc` ↔ `identity` ↔ `config`)
**Status:** issues_found (ADVISORY — no BLOCKER/HIGH; will not block phase completion)

## Summary

This is security-sensitive offline auth-recovery code and it is **carefully built**. I traced every one of the phase's stated security-critical concerns to ground truth (the Authula services, the online resetter it models on, the sqlc queries, the identity store, and the throwaway-DB harness) and **all six check out**:

1. **Argon2 correctness — CORRECT.** The password is hashed *only* via Authula's own `core.PasswordService.Hash` (`setter.go:78`), never `agui.hashArgon2id` (whose `$aura$…` envelope Authula's `Verify` rejects). The recovery *answer* correctly uses `agui.RecoveryHasher.HashAnswer` (`breakglass.go:72`), which is verified by agui's own `VerifyAnswer` (not Authula) — and the case-fold/NFKC normalization is symmetric on hash and verify, so a printed mixed-case generated answer still verifies. `TestRecoverOperatorHappyPath` proves the round-trip through Authula's real `Verify`.
2. **Session-kill ordering — CORRECT.** `SessionService.DeleteAllByUserID` runs *before* `AccountService.Update` (`setter.go:85` then `:88`), byte-for-byte matching the online `authulaPasswordResetter.SetPassword` (`serve_password_reset.go:450→453`). There is no window where the new password is live alongside an old session; and because the tool is offline (no server), no concurrent login can race the delete→update gap.
3. **Secret leakage — NONE FOUND.** I traced every error/log/argv path. Errors name env-var *names* and field *labels*, never values (`source.go`); the hash failure returns a generic string that deliberately does not wrap the underlying error (`setter.go:80`); the audit row sets only `IdentityID`+`Event` (`breakglass.go:86-89`), so `metadata` defaults to `'{}'`. The plaintext password is never a CLI argument. The only secret emission is the sanctioned single `--generate` stdout line (`source.go:176-187`). Both the integration test (slog sink) and the unit test (stderr + error string) assert no leak.
4. **Throwaway-DB safety (D-08) — CORRECT.** The harness refuses `dbName == "aura"` (`breakglass_integration_test.go:125`), validates the name against an identifier regex, connects only to the `postgres` maintenance DB and a disposable DB it CREATE/DROPs, and composes its *own* DSNs from `POSTGRES_PASSWORD`+`PGHOST`+`PGPORT` — `AURA_DB_URL`/`AURA_DB_MIGRATE_URL` are read purely as no-skip-as-green presence gates and are never used for a destructive connection. No path reaches the live `aura` DB. The 37D-05 footgun is closed.
5. **Guard correctness (R2/D-11) — CORRECT.** `ListIdentities` returns *all* identities including deactivated (`store.go:79,84` — no deactivated filter), so `selectSoleOperator` genuinely partitions kind='user' into active/all and applies the locked rule: exactly-one-active wins even with deactivated stragglers; >1 active refuses (counting only active, so a stale deactivated row never trips it); a lone deactivated operator is recoverable; everything else refuses. All eight branches are unit-covered and the >1 / zero cases are proven no-write by the integration edge tests.
6. **Partial-write integrity — MOSTLY CORRECT.** The guard refuses before any write; the setter fails cleanly (no session-delete, no update) on a missing auth link or missing account, proven by `TestRecoverOperatorMissingAccount`. The residual gaps are the two findings below (MD-01, LO-03) about the *non-transactional* ordering across the Authula and aura writes.

Resource lifecycle is clean (deferred `provider.Close()`, `t.Cleanup(pool.Close)`, deferred `rows.Close()`), CLI arg handling fails closed (unknown flag / positional / non-TTY / empty secret all rejected with secret-free messages), and `config.LoadDB()` does populate `AuthulaSecret`/`AuthulaDatabaseURL` (verified in `loadBase`, `config.go:335-466`), so the command is not dead on arrival.

The findings are robustness/consistency improvements, not correctness or security failures.

---

## Medium

### MD-01: Recovery answer is hashed (and thus validated) only *after* the irreversible Authula reset

**File:** `internal/breakglass/breakglass.go:60-84`
**Issue:** `RecoverOperator` performs the irreversible, cross-database step first — `setOperatorPassword` kills every session and rewrites the Authula password (`:60`) — and only *afterwards* hashes the recovery answer (`agui.RecoveryHasher{}.HashAnswer`, `:72`) and writes the re-seed (`:76`). The answer hash can fail (`HashAnswer` returns `"empty recovery answer"` when the answer normalizes to empty, or a `crypto/rand` error), and `UpsertIdentityRecovery`/`InsertIdentityRecoveryAudit` can fail on any transient DB error. Because these four writes span two schemas/pools with no enclosing transaction, a failure *after* the reset leaves the operator half-recovered: **password changed + all sessions killed, but recovery not re-seeded and no audit row written.** For a security-sensitive break-glass credential reset, an unaudited-yet-successful privileged credential change is the concerning outcome.

**Failure scenario:** Operator (or a future direct caller of the exported `RecoverOperator`) supplies an answer that survives `Source`'s `TrimSpace` non-empty check but normalizes to empty under NFKC+case-fold+`Fields` (e.g. an all-control-character string), or the DB blips on the re-seed. The Authula password + sessions are already mutated; `RecoverOperator` returns `"hash recovery answer: …"` / `"re-seed identity recovery: …"`. The credential change is now live and **unaudited**.

**Mitigations already present (why this is Medium, not High):** the CLI's `Source` pre-validates the answer is non-whitespace-empty; the CLI surfaces the error non-zero so the operator knows; a re-run is idempotent (`UpsertIdentityRecovery` upserts in place, the password re-sets, and the audit INSERT then lands exactly once — self-healing). The break-glass audit handling is also *stricter* than the online precedent, which ignores its audit error outright (`agui/password_reset.go:362`, `_ = s.recordEvent(...)`).

**Fix:** Validate + hash the cheap, fallible re-seed input *before* the irreversible Authula mutation, so a bad/failing answer never leaves a changed password behind:

```go
// Hash the answer FIRST (cheap, fallible) so a bad answer aborts before any
// irreversible Authula session-kill + password rewrite.
var recHash, recVer string
if !deps.NoRecovery {
    var err error
    recHash, recVer, err = agui.RecoveryHasher{}.HashAnswer(secrets.Answer)
    if err != nil {
        return "", fmt.Errorf("hash recovery answer: %w", err)
    }
}
if err := setOperatorPassword(ctx, deps.Pool, deps.Core, op.ID, secrets.Password); err != nil {
    return "", err
}
// ... then UpsertIdentityRecovery(... recHash, recVer ...) and the audit row.
```

---

## Low

### LO-01: `recover-operator` is unusable (refuses, no override) whenever more than one active `kind='user'` identity exists

**File:** `internal/breakglass/guard.go:42-47`, `cmd/aura/recover_operator.go` (no `--identity` flag)
**Issue:** `selectSoleOperator` refuses on `len(active) > 1`. The platform explicitly supports multi-user deployments (PRD amendment #64 / Phase 28 enrolls a second web-loginable `kind='user'` identity, per `webauth/authula.go:236-248`). In any such deployment, this last-resort recovery command can never pick a target and always errors — precisely the configuration where a total lockout would most need break-glass. There is no `--identity <name>`/`--identity-id` disambiguator.
**Assessment:** The refusal itself is the **safe** choice (break-glass must never *guess* which operator to reset — no false-accept), and single-active-operator scope is the locked D-11 decision, so this is a scoped limitation rather than a defect. Flagged so the operational gap is on record.
**Fix (advisory, future):** Add an optional `--identity <name>` selector that, when supplied, bypasses the sole-operator guard and targets the named `kind='user'` identity (still refusing a non-existent or non-user name).

### LO-02: Offline setter enforces no minimum password length/strength; the online path enforces ≥ 8

**File:** `internal/breakglass/setter.go:78` (vs `internal/agui/password_reset.go:312`, `len(in.Password) < 8`)
**Issue:** `setOperatorPassword` hashes whatever password it is handed. A break-glass reset can therefore set a password (e.g. `AURA_RECOVERY_PASSWORD=a`) that is *weaker than the online forgot-password flow would ever permit*, and that weak password then works for online login. The `--generate` path is strong (32 chars), but the env/prompt paths accept any non-empty value.
**Assessment:** Break-glass is host-only and operator-controlled ("availability over policy"), so this is low impact — but the asymmetry with the online 8-char floor is worth a guard.
**Fix:** Enforce a minimum length in `Source` (env + prompt paths) or in `setOperatorPassword`, mirroring the online `< 8` rejection, with a secret-free error (`"operator password must be at least 8 characters"`).

### LO-03: "recovery re-seeded" success message can over-promise when the operator has no `telegram_accounts` row

**File:** `cmd/aura/recover_operator.go:105` (message) vs `internal/db/sqlc/identity_recovery.sql.go:296-315` (`LookupRecoveryByEmail` INNER JOINs `telegram_accounts`)
**Issue:** The online forgot-password lookup INNER-JOINs `identity_auth_links` ∧ `identity_recovery` ∧ `telegram_accounts`. `RecoverOperator` re-seeds only `identity_recovery`. If the operator's lockout state also lacks a `telegram_accounts` row, the re-seed is necessary-but-not-sufficient — `LookupRecoveryByEmail` still returns zero rows and forgot-password stays denied — yet the CLI prints `"…; recovery re-seeded"`, and the command's own doc comment (`:4-5`) states the re-seed is "so the online forgot-password flow works again."
**Assessment:** In the common lockout (password forgotten, Telegram binding intact) it is fine; the harness seeds the Telegram row so the happy-path test passes. Low, because the message is technically true (it *did* re-seed `identity_recovery`).
**Fix:** Either (a) check for a live `telegram_accounts` row and downgrade the message when absent (`"…; recovery re-seeded (WARNING: no Telegram delivery channel linked — online forgot-password still unavailable)"`), or (b) soften the doc comment to say it re-seeds the security Q&A only.

### LO-04: `--generate` silently ignores `AURA_RECOVERY_ANSWER` / `AURA_RECOVERY_QUESTION` (asymmetric with the password path)

**File:** `internal/breakglass/source.go:121-128`
**Issue:** `sourcePassword` treats `--generate` together with `AURA_RECOVERY_PASSWORD` as a hard conflict error (`:93-94`). But `sourceRecovery` short-circuits on `generate` (`:122`) and returns a freshly generated answer + the default question, **silently discarding** any `AURA_RECOVERY_ANSWER`/`AURA_RECOVERY_QUESTION` the operator set. The operator who intended a known answer gets a random one instead (it is printed, so they can see it, but the intent was ignored without warning).
**Assessment:** No security impact (the generated answer is emitted); purely a UX/consistency inconsistency.
**Fix:** Mirror the password path — reject `--generate` when `AURA_RECOVERY_ANSWER` (or `AURA_RECOVERY_QUESTION`) is also set: `"AURA_RECOVERY_ANSWER is set together with --generate; pass only one answer source"`.

### LO-05: Deny-path E2E factor-name assertions are effectively vacuous

**File:** `web/e2e/password-reset.spec.ts:172-174`
**Issue:** The deny test's no-enumeration proof asserts `expect(html).not.toContain('identity_recovery')` and `not.toContain('telegram_accounts')`. Those are backend table/column identifiers that the frontend has no path to ever render, so the assertions pass regardless of whether a real enumeration leak exists — they add false confidence. A genuine enumeration leak would read like *"No Telegram number is registered for this account,"* which contains neither literal.
**Assessment:** The test is not *wrong* — the real anti-enumeration guarantee **is** proven, by asserting the byte-identical `NEUTRAL_NOTICE` on both happy and deny `/start` and the generic `GENERIC_ERROR` on the denied `/question` (same constants, `:128/:161` and `:167`). That is the meaningful check; the factor-name lines are theater.
**Fix:** Replace the two `not.toContain(<backend identifier>)` lines with assertions on the actual user-facing copy that *would* leak a factor — e.g. assert the DOM contains no string matching `/no .*(telegram|recovery|security question).* (configured|registered|found)/i` — so the test would actually fail on a real enumeration regression.

---

## Nit

### NT-01: Doc comment claims the provider is "always Close()d, even on the error paths" — but `os.Exit` skips the deferred Close

**File:** `cmd/aura/recover_operator.go:46` (comment) vs `:64-97` (every error branch calls `os.Exit(1)`)
**Issue:** `defer func() { _ = provider.Close() }()` (`:87`) runs only on the *normal* return (success). Every post-construction error branch (`:96`) calls `os.Exit`, which does **not** run deferred functions, so `Close()` is skipped there. The outcome is benign (process termination stops Authula's expiry workers anyway, and goleak is a test-only concern), but the comment overstates the guarantee.
**Fix:** Reword to "the provider is Close()d on the success path; on error paths the process exits immediately, so its workers are reclaimed by process death," or restructure to a single `run() error` helper that returns (letting the defer fire) with `os.Exit` only at the top.

### NT-02: `op.ID` is `uuid.Parse`d twice

**File:** `internal/breakglass/breakglass.go:64` and `internal/breakglass/setter.go:53`
**Issue:** `setOperatorPassword` parses `identityID` (`setter.go:53`) and `RecoverOperator` re-parses the same `op.ID` (`breakglass.go:64`). `op.ID` originates from `aura.identities.id` (always a valid UUID), so the second parse cannot fail meaningfully. Harmless redundancy.
**Fix:** Optional — have `setOperatorPassword` accept/return the parsed `pgtype.UUID`, or parse once in `RecoverOperator` and pass it down.

### NT-03: Deliberate non-wrapping of the hash error lacks an explanatory comment

**File:** `internal/breakglass/setter.go:78-81`
**Issue:** `return errors.New("hash operator password: authula password service failed")` intentionally drops the underlying `err` (no `%w`) — a good secret-safety choice — but there is no comment saying *why*. A future maintainer applying the `golang-error-handling` "always wrap with `%w`" rule could "fix" it and reintroduce a (theoretical) leak vector.
**Fix:** Add `// intentionally NOT %w-wrapped: never risk the plaintext leaking through the underlying error chain`.

### NT-04: `parseRecoverOperatorFlags` branches (unknown flag / positional rejection) are untested

**File:** `cmd/aura/recover_operator.go:108-124` (no `recover_operator_test.go` exists)
**Issue:** The hand-rolled flag parser's error branches (`fs.Parse` failure, `fs.NArg() > 0`) have no unit test. `cmd/aura` glue is coverage-floor-exempt per CLAUDE.md, so this is acceptable, but the "password is never accepted as an argv element" guarantee (`:120-122`) is exactly the kind of security-relevant branch worth pinning.
**Fix:** Optional — a small table test asserting `parseRecoverOperatorFlags([]string{"foo"})` and `{"--nope"}` both error and that neither `generate` nor `noRecovery` is set.

_(Note: `scripts/coverage_docker.sh` hardcodes a 64-hex dummy `AURA_AUTHULA_SECRET` fallback — verified exactly 64 chars, test-only, self-consistent against a fresh throwaway `authula` schema, never a production secret. A secret-scanner may flag it; consider an inline `# nosecret: test fixture` tag. Not counted as a finding.)_

---

_Reviewed: 2026-07-11_
_Reviewer: Claude (gsd-code-reviewer), adversarial deep pass_
_Diff range: ecb3c550..HEAD_
